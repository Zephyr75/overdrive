#include "Backend.hpp"
#include "Shader.hpp"
#include "settings/Settings.hpp"

#define GLFW_INCLUDE_NONE
#include <GLFW/glfw3.h>
#include <stb_image.h>

#include <algorithm>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <memory>
#include <vector>

static constexpr VkShaderStageFlags kPushStages =
    VK_SHADER_STAGE_VERTEX_BIT | VK_SHADER_STAGE_GEOMETRY_BIT |
    VK_SHADER_STAGE_FRAGMENT_BIT;

#define VK_CHECK(call)                                                         \
  do {                                                                         \
    VkResult result_ = (call);                                                 \
    if (result_ != VK_SUCCESS)                                                 \
      std::cerr << "Vulkan error " << result_ << " at " << __FILE__ << ":"     \
                << __LINE__ << "\n";                                           \
  } while (0)

std::unique_ptr<Backend> createBackend() {
  return std::make_unique<VKBackend>();
}

// ---- window / init -----------------------------------------------------------

void VKBackend::configureWindow() {
  glfwWindowHint(GLFW_CLIENT_API, GLFW_NO_API);
}

void VKBackend::init(GLFWwindow *win) {
  window = win;

  createInstance();
  VK_CHECK(glfwCreateWindowSurface(instance, window, nullptr, &surface));
  pickPhysicalDevice();
  createDevice();

  VmaAllocatorCreateInfo allocatorCI{};
  allocatorCI.physicalDevice = physicalDevice;
  allocatorCI.device = device;
  allocatorCI.instance = instance;
  allocatorCI.vulkanApiVersion = VK_API_VERSION_1_3;
  allocatorCI.flags = VMA_ALLOCATOR_CREATE_BUFFER_DEVICE_ADDRESS_BIT;
  VK_CHECK(vmaCreateAllocator(&allocatorCI, &allocator));

  VkCommandPoolCreateInfo poolCI{VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO};
  poolCI.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
  poolCI.queueFamilyIndex = queueFamily;
  VK_CHECK(vkCreateCommandPool(device, &poolCI, nullptr, &commandPool));

  createSwapchain();
  createFrameData();
  createSamplers();
  createDescriptors();
  createGlobalPipelineLayout();

  // Reserved index 0 in every handle table (0 = "no resource" for the engine)
  buffers.emplace_back();
  meshes.emplace_back();
  shadowTargets.emplace_back();

  createDefaultTextures();
  createTimestampPool();
}

void VKBackend::createTimestampPool() {
  gpuTiming = std::getenv("OD_GPU_TIMING") != nullptr;
  if (!gpuTiming)
    return;
  VkPhysicalDeviceProperties props;
  vkGetPhysicalDeviceProperties(physicalDevice, &props);
  timestampPeriodNs = props.limits.timestampPeriod;
  if (timestampPeriodNs == 0.0f) {
    gpuTiming = false;
    std::cerr << "[gpu] timestamps unsupported on this device\n";
    return;
  }
  VkQueryPoolCreateInfo qi{VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO};
  qi.queryType = VK_QUERY_TYPE_TIMESTAMP;
  qi.queryCount = kFramesInFlight * kTimestampsPerFrame;
  VK_CHECK(vkCreateQueryPool(device, &qi, nullptr, &timestampPool));
  std::cerr << "[gpu] timing enabled (timestampPeriod " << timestampPeriodNs
            << " ns); reporting bake/main GPU ms every 120 frames\n";
}

void VKBackend::readTimestamps() {
  uint32_t base = frameIndex * kTimestampsPerFrame;
  uint64_t ts[kTimestampsPerFrame] = {};
  VkResult r = vkGetQueryPoolResults(device, timestampPool, base,
                                     kTimestampsPerFrame, sizeof(ts), ts,
                                     sizeof(uint64_t), VK_QUERY_RESULT_64_BIT);
  if (r != VK_SUCCESS)
    return; // fence already waited at beginFrame, so this should not happen
  accBakeMs += double(ts[1] - ts[0]) * timestampPeriodNs / 1e6;
  accMainMs += double(ts[2] - ts[1]) * timestampPeriodNs / 1e6;
  if (++timedFrames >= 120) {
    double b = accBakeMs / timedFrames, m = accMainMs / timedFrames;
    std::cerr << "[gpu] shadow-bake " << b << " ms  main-pass " << m
              << " ms  total " << (b + m) << " ms\n";
    accBakeMs = accMainMs = 0.0;
    timedFrames = 0;
  }
}

static VKAPI_ATTR VkBool32 VKAPI_CALL
debugCallback(VkDebugUtilsMessageSeverityFlagBitsEXT,
              VkDebugUtilsMessageTypeFlagsEXT,
              const VkDebugUtilsMessengerCallbackDataEXT *data, void *) {
  std::cerr << "[vulkan] " << data->pMessage << "\n";
  return VK_FALSE;
}

void VKBackend::createInstance() {
  // Enable validation when the layer is installed
  bool validation = false;
  uint32_t layerCount = 0;
  vkEnumerateInstanceLayerProperties(&layerCount, nullptr);
  std::vector<VkLayerProperties> layers(layerCount);
  vkEnumerateInstanceLayerProperties(&layerCount, layers.data());
  for (auto &l : layers)
    if (std::strcmp(l.layerName, "VK_LAYER_KHRONOS_validation") == 0)
      validation = true;

  uint32_t glfwCount = 0;
  const char **glfwExts = glfwGetRequiredInstanceExtensions(&glfwCount);
  std::vector<const char *> extensions(glfwExts, glfwExts + glfwCount);
  if (validation)
    extensions.push_back(VK_EXT_DEBUG_UTILS_EXTENSION_NAME);

  VkApplicationInfo appInfo{VK_STRUCTURE_TYPE_APPLICATION_INFO};
  appInfo.pApplicationName = "overdrive";
  appInfo.apiVersion = VK_API_VERSION_1_3;

  const char *validationLayer = "VK_LAYER_KHRONOS_validation";
  VkInstanceCreateInfo ci{VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO};
  ci.pApplicationInfo = &appInfo;
  ci.enabledExtensionCount = static_cast<uint32_t>(extensions.size());
  ci.ppEnabledExtensionNames = extensions.data();
  if (validation) {
    ci.enabledLayerCount = 1;
    ci.ppEnabledLayerNames = &validationLayer;
  }
  VK_CHECK(vkCreateInstance(&ci, nullptr, &instance));

  if (validation) {
    VkDebugUtilsMessengerCreateInfoEXT dbgCI{
        VK_STRUCTURE_TYPE_DEBUG_UTILS_MESSENGER_CREATE_INFO_EXT};
    dbgCI.messageSeverity =
        VK_DEBUG_UTILS_MESSAGE_SEVERITY_WARNING_BIT_EXT |
        VK_DEBUG_UTILS_MESSAGE_SEVERITY_ERROR_BIT_EXT;
    dbgCI.messageType = VK_DEBUG_UTILS_MESSAGE_TYPE_VALIDATION_BIT_EXT |
                        VK_DEBUG_UTILS_MESSAGE_TYPE_PERFORMANCE_BIT_EXT |
                        VK_DEBUG_UTILS_MESSAGE_TYPE_GENERAL_BIT_EXT;
    dbgCI.pfnUserCallback = debugCallback;
    auto create = reinterpret_cast<PFN_vkCreateDebugUtilsMessengerEXT>(
        vkGetInstanceProcAddr(instance, "vkCreateDebugUtilsMessengerEXT"));
    if (create)
      create(instance, &dbgCI, nullptr, &debugMessenger);
  }
}

void VKBackend::pickPhysicalDevice() {
  uint32_t count = 0;
  vkEnumeratePhysicalDevices(instance, &count, nullptr);
  std::vector<VkPhysicalDevice> devices(count);
  vkEnumeratePhysicalDevices(instance, &count, devices.data());

  int bestScore = -1;
  for (auto pd : devices) {
    VkPhysicalDeviceProperties props;
    vkGetPhysicalDeviceProperties(pd, &props);
    if (props.apiVersion < VK_API_VERSION_1_3)
      continue;

    VkPhysicalDeviceFeatures feats;
    vkGetPhysicalDeviceFeatures(pd, &feats);
    if (!feats.geometryShader)
      continue;

    // Need one family doing graphics + present
    uint32_t famCount = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(pd, &famCount, nullptr);
    std::vector<VkQueueFamilyProperties> fams(famCount);
    vkGetPhysicalDeviceQueueFamilyProperties(pd, &famCount, fams.data());
    int family = -1;
    for (uint32_t i = 0; i < famCount; i++) {
      VkBool32 present = VK_FALSE;
      vkGetPhysicalDeviceSurfaceSupportKHR(pd, i, surface, &present);
      if ((fams[i].queueFlags & VK_QUEUE_GRAPHICS_BIT) && present) {
        family = static_cast<int>(i);
        break;
      }
    }
    if (family < 0)
      continue;

    int score = 1;
    if (props.deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU)
      score = 2;
    if (props.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU)
      score = 3;
    if (score > bestScore) {
      bestScore = score;
      physicalDevice = pd;
      queueFamily = static_cast<uint32_t>(family);
    }
  }

  if (!physicalDevice) {
    std::cerr << "No suitable Vulkan 1.3 GPU found\n";
    return;
  }
  VkPhysicalDeviceProperties props;
  vkGetPhysicalDeviceProperties(physicalDevice, &props);
  std::cout << "Vulkan device: " << props.deviceName << "\n";
}

void VKBackend::createDevice() {
  float priority = 1.0f;
  VkDeviceQueueCreateInfo queueCI{VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO};
  queueCI.queueFamilyIndex = queueFamily;
  queueCI.queueCount = 1;
  queueCI.pQueuePriorities = &priority;

  VkPhysicalDeviceVulkan13Features f13{
      VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_3_FEATURES};
  f13.dynamicRendering = VK_TRUE;
  f13.synchronization2 = VK_TRUE;

  // Does the driver let BDA buffers be created capture-replay-capable? Required
  // for RenderDoc (and other tools) to reproduce device addresses and capture
  // the app. Query before enabling so we don't fail device creation where it is
  // unsupported.
  VkPhysicalDeviceVulkan12Features supported12{
      VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_2_FEATURES};
  VkPhysicalDeviceFeatures2 supported{
      VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2};
  supported.pNext = &supported12;
  vkGetPhysicalDeviceFeatures2(physicalDevice, &supported);
  bdaCaptureReplay = supported12.bufferDeviceAddressCaptureReplay;

  VkPhysicalDeviceVulkan12Features f12{
      VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_2_FEATURES};
  f12.pNext = &f13;
  f12.bufferDeviceAddress = VK_TRUE;
  f12.bufferDeviceAddressCaptureReplay = bdaCaptureReplay ? VK_TRUE : VK_FALSE;
  f12.scalarBlockLayout = VK_TRUE;
  f12.descriptorIndexing = VK_TRUE;
  f12.runtimeDescriptorArray = VK_TRUE;
  f12.descriptorBindingPartiallyBound = VK_TRUE;
  f12.descriptorBindingSampledImageUpdateAfterBind = VK_TRUE;
  f12.shaderSampledImageArrayNonUniformIndexing = VK_TRUE;

  VkPhysicalDeviceFeatures2 features{
      VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2};
  features.pNext = &f12;
  features.features.geometryShader = VK_TRUE;

  const char *deviceExts[] = {VK_KHR_SWAPCHAIN_EXTENSION_NAME};

  VkDeviceCreateInfo ci{VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO};
  ci.pNext = &features;
  ci.queueCreateInfoCount = 1;
  ci.pQueueCreateInfos = &queueCI;
  ci.enabledExtensionCount = 1;
  ci.ppEnabledExtensionNames = deviceExts;
  VK_CHECK(vkCreateDevice(physicalDevice, &ci, nullptr, &device));
  vkGetDeviceQueue(device, queueFamily, 0, &queue);
}

// ---- swapchain ----------------------------------------------------------------

void VKBackend::createSwapchain() {
  VkSurfaceCapabilitiesKHR caps;
  vkGetPhysicalDeviceSurfaceCapabilitiesKHR(physicalDevice, surface, &caps);

  uint32_t formatCount = 0;
  vkGetPhysicalDeviceSurfaceFormatsKHR(physicalDevice, surface, &formatCount,
                                       nullptr);
  std::vector<VkSurfaceFormatKHR> formats(formatCount);
  vkGetPhysicalDeviceSurfaceFormatsKHR(physicalDevice, surface, &formatCount,
                                       formats.data());
  VkSurfaceFormatKHR chosen = formats[0];
  for (auto &f : formats)
    if (f.format == VK_FORMAT_B8G8R8A8_UNORM &&
        f.colorSpace == VK_COLOR_SPACE_SRGB_NONLINEAR_KHR)
      chosen = f;
  swapFormat = chosen.format;

  if (caps.currentExtent.width != UINT32_MAX) {
    swapExtent = caps.currentExtent;
  } else {
    int w, h;
    glfwGetFramebufferSize(window, &w, &h);
    swapExtent.width = std::clamp(static_cast<uint32_t>(w),
                                  caps.minImageExtent.width,
                                  caps.maxImageExtent.width);
    swapExtent.height = std::clamp(static_cast<uint32_t>(h),
                                   caps.minImageExtent.height,
                                   caps.maxImageExtent.height);
  }

  uint32_t imageCount = caps.minImageCount + 1;
  if (caps.maxImageCount > 0)
    imageCount = std::min(imageCount, caps.maxImageCount);

  VkSwapchainKHR oldSwapchain = swapchain;

  VkSwapchainCreateInfoKHR ci{VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR};
  ci.surface = surface;
  ci.minImageCount = imageCount;
  ci.imageFormat = chosen.format;
  ci.imageColorSpace = chosen.colorSpace;
  ci.imageExtent = swapExtent;
  ci.imageArrayLayers = 1;
  ci.imageUsage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT;
  ci.imageSharingMode = VK_SHARING_MODE_EXCLUSIVE;
  ci.preTransform = caps.currentTransform;
  ci.compositeAlpha = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
  ci.presentMode = VK_PRESENT_MODE_FIFO_KHR;
  ci.clipped = VK_TRUE;
  ci.oldSwapchain = oldSwapchain;
  VK_CHECK(vkCreateSwapchainKHR(device, &ci, nullptr, &swapchain));
  if (oldSwapchain)
    vkDestroySwapchainKHR(device, oldSwapchain, nullptr);

  uint32_t count = 0;
  vkGetSwapchainImagesKHR(device, swapchain, &count, nullptr);
  swapImages.resize(count);
  vkGetSwapchainImagesKHR(device, swapchain, &count, swapImages.data());

  swapViews.resize(count);
  renderSems.resize(count);
  for (uint32_t i = 0; i < count; i++) {
    VkImageViewCreateInfo viewCI{VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO};
    viewCI.image = swapImages[i];
    viewCI.viewType = VK_IMAGE_VIEW_TYPE_2D;
    viewCI.format = swapFormat;
    viewCI.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1};
    VK_CHECK(vkCreateImageView(device, &viewCI, nullptr, &swapViews[i]));

    VkSemaphoreCreateInfo semCI{VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
    VK_CHECK(vkCreateSemaphore(device, &semCI, nullptr, &renderSems[i]));
  }

  // Depth buffer matching the swapchain
  VkImageCreateInfo depthCI{VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO};
  depthCI.imageType = VK_IMAGE_TYPE_2D;
  depthCI.format = kDepthFormat;
  depthCI.extent = {swapExtent.width, swapExtent.height, 1};
  depthCI.mipLevels = 1;
  depthCI.arrayLayers = 1;
  depthCI.samples = VK_SAMPLE_COUNT_1_BIT;
  depthCI.tiling = VK_IMAGE_TILING_OPTIMAL;
  depthCI.usage = VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT;
  VmaAllocationCreateInfo allocCI{};
  allocCI.usage = VMA_MEMORY_USAGE_AUTO;
  VK_CHECK(vmaCreateImage(allocator, &depthCI, &allocCI, &depthImage,
                          &depthAlloc, nullptr));

  VkImageViewCreateInfo dViewCI{VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO};
  dViewCI.image = depthImage;
  dViewCI.viewType = VK_IMAGE_VIEW_TYPE_2D;
  dViewCI.format = kDepthFormat;
  dViewCI.subresourceRange = {VK_IMAGE_ASPECT_DEPTH_BIT, 0, 1, 0, 1};
  VK_CHECK(vkCreateImageView(device, &dViewCI, nullptr, &depthView));
}

void VKBackend::destroySwapchain() {
  for (auto v : swapViews)
    vkDestroyImageView(device, v, nullptr);
  swapViews.clear();
  for (auto s : renderSems)
    vkDestroySemaphore(device, s, nullptr);
  renderSems.clear();
  if (depthView)
    vkDestroyImageView(device, depthView, nullptr);
  if (depthImage)
    vmaDestroyImage(allocator, depthImage, depthAlloc);
  depthView = VK_NULL_HANDLE;
  depthImage = VK_NULL_HANDLE;
}

void VKBackend::recreateSwapchain() {
  int w = 0, h = 0;
  glfwGetFramebufferSize(window, &w, &h);
  while (w == 0 || h == 0) { // minimized: wait until visible again
    glfwWaitEvents();
    glfwGetFramebufferSize(window, &w, &h);
  }
  vkDeviceWaitIdle(device);
  destroySwapchain();
  createSwapchain();
}

// ---- frame data / descriptors / samplers --------------------------------------

void VKBackend::createFrameData() {
  for (auto &f : frames) {
    VkCommandBufferAllocateInfo cbAI{
        VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO};
    cbAI.commandPool = commandPool;
    cbAI.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
    cbAI.commandBufferCount = 1;
    VK_CHECK(vkAllocateCommandBuffers(device, &cbAI, &f.cb));

    VkFenceCreateInfo fenceCI{VK_STRUCTURE_TYPE_FENCE_CREATE_INFO};
    fenceCI.flags = VK_FENCE_CREATE_SIGNALED_BIT; // frame 0 must not deadlock
    VK_CHECK(vkCreateFence(device, &fenceCI, nullptr, &f.fence));

    VkSemaphoreCreateInfo semCI{VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
    VK_CHECK(vkCreateSemaphore(device, &semCI, nullptr, &f.acquireSem));

    // Per-frame uniform ring, host-visible, addressed via BDA
    VkBufferCreateInfo bufCI{VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
    bufCI.size = kRingSize;
    bufCI.usage = VK_BUFFER_USAGE_STORAGE_BUFFER_BIT |
                  VK_BUFFER_USAGE_SHADER_DEVICE_ADDRESS_BIT;
    if (bdaCaptureReplay)
      bufCI.flags |= VK_BUFFER_CREATE_DEVICE_ADDRESS_CAPTURE_REPLAY_BIT;
    VmaAllocationCreateInfo allocCI{};
    allocCI.usage = VMA_MEMORY_USAGE_AUTO;
    allocCI.flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT |
                    VMA_ALLOCATION_CREATE_MAPPED_BIT;
    VmaAllocationInfo info{};
    VK_CHECK(vmaCreateBuffer(allocator, &bufCI, &allocCI, &f.ring, &f.ringAlloc,
                             &info));
    f.ringMapped = info.pMappedData;

    VkBufferDeviceAddressInfo addrInfo{
        VK_STRUCTURE_TYPE_BUFFER_DEVICE_ADDRESS_INFO};
    addrInfo.buffer = f.ring;
    f.ringAddr = vkGetBufferDeviceAddress(device, &addrInfo);
  }
}

void VKBackend::createSamplers() {
  VkSamplerCreateInfo ci{VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO};
  ci.magFilter = VK_FILTER_LINEAR;
  ci.minFilter = VK_FILTER_LINEAR;
  ci.mipmapMode = VK_SAMPLER_MIPMAP_MODE_NEAREST;
  ci.addressModeU = VK_SAMPLER_ADDRESS_MODE_REPEAT;
  ci.addressModeV = VK_SAMPLER_ADDRESS_MODE_REPEAT;
  ci.addressModeW = VK_SAMPLER_ADDRESS_MODE_REPEAT;
  VK_CHECK(vkCreateSampler(device, &ci, nullptr, &samplerRepeat));

  ci.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
  ci.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
  ci.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
  VK_CHECK(vkCreateSampler(device, &ci, nullptr, &samplerCubeLinear));

  ci.magFilter = VK_FILTER_NEAREST;
  ci.minFilter = VK_FILTER_NEAREST;
  VK_CHECK(vkCreateSampler(device, &ci, nullptr, &samplerShadowCube));

  ci.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_BORDER;
  ci.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_BORDER;
  ci.borderColor = VK_BORDER_COLOR_FLOAT_OPAQUE_WHITE;
  VK_CHECK(vkCreateSampler(device, &ci, nullptr, &samplerShadow2D));
}

void VKBackend::createDescriptors() {
  // Bindings 0/1: the bindless material texture arrays (2D + cube). Bindings
  // 2/3: dedicated single-descriptor shadow maps (2D + cube). The shadow maps
  // are tapped 9× / 20× per fragment by the PCF kernels; sampling them through
  // the dynamically-indexed bindless array makes Intel's Vulkan driver re-fetch
  // the descriptor per tap (the ~1.7× GL/Vulkan gap), so they get plain bound
  // descriptors like the OpenGL backend uses.
  VkDescriptorSetLayoutBinding bindings[4]{};
  bindings[0] = {0, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, kMax2DTextures,
                 VK_SHADER_STAGE_FRAGMENT_BIT, nullptr};
  bindings[1] = {1, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, kMaxCubeTextures,
                 VK_SHADER_STAGE_FRAGMENT_BIT, nullptr};
  bindings[2] = {2, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 1,
                 VK_SHADER_STAGE_FRAGMENT_BIT, nullptr};
  // Binding 3 is an array: one cube shadow map per point-shadow caster.
  bindings[3] = {3, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, MAX_SHADOW_CUBES,
                 VK_SHADER_STAGE_FRAGMENT_BIT, nullptr};

  const VkDescriptorBindingFlags bindlessFlags =
      VK_DESCRIPTOR_BINDING_PARTIALLY_BOUND_BIT |
      VK_DESCRIPTOR_BINDING_UPDATE_AFTER_BIND_BIT;
  // The shadow bindings are written after the set is bound (during Mesh::draw),
  // so they need UPDATE_AFTER_BIND too; PARTIALLY_BOUND tolerates frames before
  // a shadow map exists.
  VkDescriptorBindingFlags flags[4] = {bindlessFlags, bindlessFlags,
                                       bindlessFlags, bindlessFlags};
  VkDescriptorSetLayoutBindingFlagsCreateInfo flagsCI{
      VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_BINDING_FLAGS_CREATE_INFO};
  flagsCI.bindingCount = 4;
  flagsCI.pBindingFlags = flags;

  VkDescriptorSetLayoutCreateInfo layoutCI{
      VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO};
  layoutCI.pNext = &flagsCI;
  layoutCI.flags = VK_DESCRIPTOR_SET_LAYOUT_CREATE_UPDATE_AFTER_BIND_POOL_BIT;
  layoutCI.bindingCount = 4;
  layoutCI.pBindings = bindings;
  VK_CHECK(vkCreateDescriptorSetLayout(device, &layoutCI, nullptr, &setLayout));

  VkDescriptorPoolSize poolSize{VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
                                kMax2DTextures + kMaxCubeTextures + 1 +
                                    MAX_SHADOW_CUBES};
  VkDescriptorPoolCreateInfo poolCI{
      VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO};
  poolCI.flags = VK_DESCRIPTOR_POOL_CREATE_UPDATE_AFTER_BIND_BIT;
  poolCI.maxSets = 1;
  poolCI.poolSizeCount = 1;
  poolCI.pPoolSizes = &poolSize;
  VK_CHECK(vkCreateDescriptorPool(device, &poolCI, nullptr, &descriptorPool));

  VkDescriptorSetAllocateInfo setAI{
      VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO};
  setAI.descriptorPool = descriptorPool;
  setAI.descriptorSetCount = 1;
  setAI.pSetLayouts = &setLayout;
  VK_CHECK(vkAllocateDescriptorSets(device, &setAI, &descriptorSet));
}

void VKBackend::createGlobalPipelineLayout() {
  VkPushConstantRange range{kPushStages, 0, sizeof(VkDeviceAddress)};
  VkPipelineLayoutCreateInfo ci{VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO};
  ci.setLayoutCount = 1;
  ci.pSetLayouts = &setLayout;
  ci.pushConstantRangeCount = 1;
  ci.pPushConstantRanges = &range;
  VK_CHECK(vkCreatePipelineLayout(device, &ci, nullptr, &pipelineLayout));
}

void VKBackend::createDefaultTextures() {
  // 2D slot 0 / handle 0: white (doubles as the engine's "no texture" handle)
  next2DSlot = 0;
  const unsigned char white[4] = {255, 255, 255, 255};
  uploadTexture(white, 1, 1, 1, false, samplerRepeat);

  // Cube slot 0: black dummy, sampled when a cube unit was never bound
  unsigned char black[4 * 6] = {};
  uploadTexture(black, 1, 1, 6, true, samplerCubeLinear);

  // Seed the dedicated shadow descriptors (bindings 2/3) with the defaults so
  // they are valid before the first real shadow map binds; bindTexture2D /
  // bindCubemap overwrite them when the caster's maps are bound. Binding 3 is an
  // array, so every element needs a valid default cube.
  writeDedicatedTexture(2, 0, textures[0].view, samplerShadow2D); // white 2D
  for (uint32_t i = 0; i < MAX_SHADOW_CUBES; i++)
    writeDedicatedTexture(3, i, textures[1].view, samplerShadowCube); // black
}

void VKBackend::writeDedicatedTexture(uint32_t binding, uint32_t arrayElement,
                                      VkImageView view, VkSampler sampler) {
  VkDescriptorImageInfo imageInfo{sampler, view,
                                  VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
  VkWriteDescriptorSet write{VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET};
  write.dstSet = descriptorSet;
  write.dstBinding = binding;
  write.dstArrayElement = arrayElement;
  write.descriptorCount = 1;
  write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
  write.pImageInfo = &imageInfo;
  vkUpdateDescriptorSets(device, 1, &write, 0, nullptr);
}

// ---- helpers ------------------------------------------------------------------

void VKBackend::immediateSubmit(
    const std::function<void(VkCommandBuffer)> &record) {
  VkCommandBufferAllocateInfo ai{
      VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO};
  ai.commandPool = commandPool;
  ai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
  ai.commandBufferCount = 1;
  VkCommandBuffer cb;
  VK_CHECK(vkAllocateCommandBuffers(device, &ai, &cb));

  VkCommandBufferBeginInfo bi{VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};
  bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
  vkBeginCommandBuffer(cb, &bi);
  record(cb);
  vkEndCommandBuffer(cb);

  VkCommandBufferSubmitInfo cbSI{VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO};
  cbSI.commandBuffer = cb;
  VkSubmitInfo2 si{VK_STRUCTURE_TYPE_SUBMIT_INFO_2};
  si.commandBufferInfoCount = 1;
  si.pCommandBufferInfos = &cbSI;
  VK_CHECK(vkQueueSubmit2(queue, 1, &si, VK_NULL_HANDLE));
  vkQueueWaitIdle(queue);

  vkFreeCommandBuffers(device, commandPool, 1, &cb);
}

void VKBackend::imageBarrier(VkCommandBuffer cb, VkImage image,
                             VkImageAspectFlags aspect, uint32_t layerCount,
                             VkImageLayout from, VkImageLayout to,
                             VkPipelineStageFlags2 srcStage,
                             VkAccessFlags2 srcAccess,
                             VkPipelineStageFlags2 dstStage,
                             VkAccessFlags2 dstAccess) {
  VkImageMemoryBarrier2 barrier{VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER_2};
  barrier.srcStageMask = srcStage;
  barrier.srcAccessMask = srcAccess;
  barrier.dstStageMask = dstStage;
  barrier.dstAccessMask = dstAccess;
  barrier.oldLayout = from;
  barrier.newLayout = to;
  barrier.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
  barrier.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
  barrier.image = image;
  barrier.subresourceRange = {aspect, 0, 1, 0, layerCount};

  VkDependencyInfo dep{VK_STRUCTURE_TYPE_DEPENDENCY_INFO};
  dep.imageMemoryBarrierCount = 1;
  dep.pImageMemoryBarriers = &barrier;
  vkCmdPipelineBarrier2(cb, &dep);
}

uint32_t VKBackend::registerTexture(bool cube, VkImage image,
                                    VmaAllocation alloc, VkImageView view,
                                    VkSampler sampler, bool ownsImage) {
  TexEntry e;
  e.cube = cube;
  e.slot = cube ? nextCubeSlot++ : next2DSlot++;
  e.image = image;
  e.alloc = alloc;
  e.view = view;
  e.ownsImage = ownsImage;
  e.valid = true;

  VkDescriptorImageInfo imageInfo{sampler, view,
                                  VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
  VkWriteDescriptorSet write{VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET};
  write.dstSet = descriptorSet;
  write.dstBinding = cube ? 1 : 0;
  write.dstArrayElement = e.slot;
  write.descriptorCount = 1;
  write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
  write.pImageInfo = &imageInfo;
  vkUpdateDescriptorSets(device, 1, &write, 0, nullptr);

  textures.push_back(e);
  return static_cast<uint32_t>(textures.size() - 1);
}

uint32_t VKBackend::uploadTexture(const unsigned char *pixels, int w, int h,
                                  int layers, bool cube, VkSampler sampler) {
  const VkDeviceSize layerSize = static_cast<VkDeviceSize>(w) * h * 4;
  const VkDeviceSize total = layerSize * layers;

  VkBufferCreateInfo stagingCI{VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
  stagingCI.size = total;
  stagingCI.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT;
  VmaAllocationCreateInfo stagingAlloc{};
  stagingAlloc.usage = VMA_MEMORY_USAGE_AUTO;
  stagingAlloc.flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT |
                       VMA_ALLOCATION_CREATE_MAPPED_BIT;
  VkBuffer staging;
  VmaAllocation stagingAllocation;
  VmaAllocationInfo info{};
  VK_CHECK(vmaCreateBuffer(allocator, &stagingCI, &stagingAlloc, &staging,
                           &stagingAllocation, &info));
  std::memcpy(info.pMappedData, pixels, total);
  vmaFlushAllocation(allocator, stagingAllocation, 0, total);

  VkImageCreateInfo imageCI{VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO};
  imageCI.flags = cube ? VK_IMAGE_CREATE_CUBE_COMPATIBLE_BIT : 0u;
  imageCI.imageType = VK_IMAGE_TYPE_2D;
  imageCI.format = VK_FORMAT_R8G8B8A8_UNORM;
  imageCI.extent = {static_cast<uint32_t>(w), static_cast<uint32_t>(h), 1};
  imageCI.mipLevels = 1;
  imageCI.arrayLayers = static_cast<uint32_t>(layers);
  imageCI.samples = VK_SAMPLE_COUNT_1_BIT;
  imageCI.tiling = VK_IMAGE_TILING_OPTIMAL;
  imageCI.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
  VmaAllocationCreateInfo imageAlloc{};
  imageAlloc.usage = VMA_MEMORY_USAGE_AUTO;
  VkImage image;
  VmaAllocation allocation;
  VK_CHECK(
      vmaCreateImage(allocator, &imageCI, &imageAlloc, &image, &allocation,
                     nullptr));

  immediateSubmit([&](VkCommandBuffer cb) {
    imageBarrier(cb, image, VK_IMAGE_ASPECT_COLOR_BIT,
                 static_cast<uint32_t>(layers), VK_IMAGE_LAYOUT_UNDEFINED,
                 VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
                 VK_PIPELINE_STAGE_2_NONE, VK_ACCESS_2_NONE,
                 VK_PIPELINE_STAGE_2_COPY_BIT, VK_ACCESS_2_TRANSFER_WRITE_BIT);

    VkBufferImageCopy region{};
    region.imageSubresource = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 0,
                               static_cast<uint32_t>(layers)};
    region.imageExtent = {static_cast<uint32_t>(w), static_cast<uint32_t>(h),
                          1};
    vkCmdCopyBufferToImage(cb, staging, image,
                           VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &region);

    imageBarrier(cb, image, VK_IMAGE_ASPECT_COLOR_BIT,
                 static_cast<uint32_t>(layers),
                 VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
                 VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
                 VK_PIPELINE_STAGE_2_COPY_BIT, VK_ACCESS_2_TRANSFER_WRITE_BIT,
                 VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT,
                 VK_ACCESS_2_SHADER_SAMPLED_READ_BIT);
  });

  vmaDestroyBuffer(allocator, staging, stagingAllocation);

  VkImageViewCreateInfo viewCI{VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO};
  viewCI.image = image;
  viewCI.viewType = cube ? VK_IMAGE_VIEW_TYPE_CUBE : VK_IMAGE_VIEW_TYPE_2D;
  viewCI.format = VK_FORMAT_R8G8B8A8_UNORM;
  viewCI.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0,
                             static_cast<uint32_t>(layers)};
  VkImageView view;
  VK_CHECK(vkCreateImageView(device, &viewCI, nullptr, &view));

  return registerTexture(cube, image, allocation, view, sampler, true);
}

void VKBackend::waitAllFrames() {
  VkFence fences[kFramesInFlight];
  for (int i = 0; i < kFramesInFlight; i++)
    fences[i] = frames[i].fence;
  vkWaitForFences(device, kFramesInFlight, fences, VK_TRUE, UINT64_MAX);
}

// ---- frame lifecycle -----------------------------------------------------------

void VKBackend::beginFrame() {
  if (!device)
    return;
  FrameData &f = frames[frameIndex];
  vkWaitForFences(device, 1, &f.fence, VK_TRUE, UINT64_MAX);

  for (;;) {
    VkResult r = vkAcquireNextImageKHR(device, swapchain, UINT64_MAX,
                                       f.acquireSem, VK_NULL_HANDLE,
                                       &imageIndex);
    if (r == VK_ERROR_OUT_OF_DATE_KHR) {
      recreateSwapchain();
      continue;
    }
    if (r != VK_SUCCESS && r != VK_SUBOPTIMAL_KHR)
      std::cerr << "vkAcquireNextImageKHR failed: " << r << "\n";
    break;
  }

  vkResetFences(device, 1, &f.fence);
  f.ringOffset = 0;

  vkResetCommandBuffer(f.cb, 0);
  VkCommandBufferBeginInfo bi{VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};
  bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
  vkBeginCommandBuffer(f.cb, &bi);

  vkCmdBindDescriptorSets(f.cb, VK_PIPELINE_BIND_POINT_GRAPHICS,
                          pipelineLayout, 0, 1, &descriptorSet, 0, nullptr);

  if (gpuTiming) {
    // Fence is already waited above, so this frame's prior results are ready.
    if (frameTimed[frameIndex])
      readTimestamps();
    uint32_t base = frameIndex * kTimestampsPerFrame;
    vkCmdResetQueryPool(f.cb, timestampPool, base, kTimestampsPerFrame);
    vkCmdWriteTimestamp2(f.cb, VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT,
                         timestampPool, base + 0);
    mainPassStarted = false;
  }

  boundPipeline = VK_NULL_HANDLE;
  frameActive = true;
}

void VKBackend::endFrame() {
  if (!frameActive)
    return;
  FrameData &f = frames[frameIndex];

  imageBarrier(f.cb, swapImages[imageIndex], VK_IMAGE_ASPECT_COLOR_BIT, 1,
               VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
               VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
               VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT,
               VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
               VK_PIPELINE_STAGE_2_NONE, VK_ACCESS_2_NONE);

  if (gpuTiming) {
    vkCmdWriteTimestamp2(f.cb, VK_PIPELINE_STAGE_2_BOTTOM_OF_PIPE_BIT,
                         timestampPool, frameIndex * kTimestampsPerFrame + 2);
    frameTimed[frameIndex] = true;
  }

  vkEndCommandBuffer(f.cb);
  vmaFlushAllocation(allocator, f.ringAlloc, 0,
                     std::max<VkDeviceSize>(f.ringOffset, 1));

  VkCommandBufferSubmitInfo cbSI{VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO};
  cbSI.commandBuffer = f.cb;
  VkSemaphoreSubmitInfo waitSI{VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO};
  waitSI.semaphore = f.acquireSem;
  waitSI.stageMask = VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT;
  VkSemaphoreSubmitInfo signalSI{VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO};
  signalSI.semaphore = renderSems[imageIndex]; // present waits on this
  signalSI.stageMask = VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT;

  VkSubmitInfo2 si{VK_STRUCTURE_TYPE_SUBMIT_INFO_2};
  si.waitSemaphoreInfoCount = 1;
  si.pWaitSemaphoreInfos = &waitSI;
  si.commandBufferInfoCount = 1;
  si.pCommandBufferInfos = &cbSI;
  si.signalSemaphoreInfoCount = 1;
  si.pSignalSemaphoreInfos = &signalSI;
  VK_CHECK(vkQueueSubmit2(queue, 1, &si, f.fence));

  VkPresentInfoKHR present{VK_STRUCTURE_TYPE_PRESENT_INFO_KHR};
  present.waitSemaphoreCount = 1;
  present.pWaitSemaphores = &renderSems[imageIndex];
  present.swapchainCount = 1;
  present.pSwapchains = &swapchain;
  present.pImageIndices = &imageIndex;
  VkResult r = vkQueuePresentKHR(queue, &present);
  if (r == VK_ERROR_OUT_OF_DATE_KHR || r == VK_SUBOPTIMAL_KHR)
    recreateSwapchain();

  frameIndex = (frameIndex + 1) % kFramesInFlight;
  frameActive = false;
}

void VKBackend::beginPass(uint32_t framebuffer, int w, int h, bool clearColor,
                          float r, float g, float b, float a) {
  if (!frameActive)
    return;
  VkCommandBuffer cb = frames[frameIndex].cb;

  // First non-shadow pass (framebuffer 0 = backbuffer) marks the end of all
  // shadow bakes on the GPU timeline.
  if (gpuTiming && framebuffer == 0 && !mainPassStarted) {
    mainPassStarted = true;
    // BOTTOM_OF_PIPE so the timestamp fires only after all prior (shadow-bake)
    // work has completed, not when this command is merely parsed. TOP_OF_PIPE
    // here would under-count the bake and over-count the main pass.
    vkCmdWriteTimestamp2(cb, VK_PIPELINE_STAGE_2_BOTTOM_OF_PIPE_BIT,
                         timestampPool, frameIndex * kTimestampsPerFrame + 1);
  }

  VkRenderingAttachmentInfo depthAtt{
      VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO};
  depthAtt.imageLayout = VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL;
  depthAtt.loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR;
  depthAtt.clearValue.depthStencil = {1.0f, 0};

  VkRenderingInfo rendering{VK_STRUCTURE_TYPE_RENDERING_INFO};
  rendering.layerCount = 1;
  rendering.pDepthAttachment = &depthAtt;

  VkRenderingAttachmentInfo colorAtt{
      VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO};
  VkViewport viewport{};
  viewport.minDepth = 0.0f;
  viewport.maxDepth = 1.0f;

  if (framebuffer == 0) {
    // Backbuffer pass: rendered with a flipped (negative-height) viewport so
    // the GL-convention scene appears upright on screen.
    imageBarrier(cb, swapImages[imageIndex], VK_IMAGE_ASPECT_COLOR_BIT, 1,
                 VK_IMAGE_LAYOUT_UNDEFINED,
                 VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
                 VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT,
                 VK_ACCESS_2_NONE,
                 VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT,
                 VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT);
    imageBarrier(cb, depthImage, VK_IMAGE_ASPECT_DEPTH_BIT, 1,
                 VK_IMAGE_LAYOUT_UNDEFINED,
                 VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL,
                 VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT |
                     VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
                 VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,
                 VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT |
                     VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
                 VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT |
                     VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_READ_BIT);

    colorAtt.imageView = swapViews[imageIndex];
    colorAtt.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
    colorAtt.loadOp = clearColor ? VK_ATTACHMENT_LOAD_OP_CLEAR
                                 : VK_ATTACHMENT_LOAD_OP_DONT_CARE;
    colorAtt.storeOp = VK_ATTACHMENT_STORE_OP_STORE;
    colorAtt.clearValue.color = {{r, g, b, a}};
    depthAtt.imageView = depthView;
    depthAtt.storeOp = VK_ATTACHMENT_STORE_OP_DONT_CARE;

    rendering.renderArea = {{0, 0}, swapExtent};
    rendering.colorAttachmentCount = 1;
    rendering.pColorAttachments = &colorAtt;

    viewport.x = 0.0f;
    viewport.y = static_cast<float>(swapExtent.height);
    viewport.width = static_cast<float>(swapExtent.width);
    viewport.height = -static_cast<float>(swapExtent.height);

    currentPass = VKPassMain;
    currentShadowTarget = 0;
  } else {
    // Shadow pass: positive viewport keeps the framebuffer memory layout
    // identical to OpenGL's, so shadow lookups in the shaders match.
    ShadowEntry &t = shadowTargets[framebuffer];
    imageBarrier(cb, t.image, VK_IMAGE_ASPECT_DEPTH_BIT, t.cube ? 6 : 1,
                 t.layout, VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL,
                 VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT,
                 VK_ACCESS_2_MEMORY_READ_BIT | VK_ACCESS_2_MEMORY_WRITE_BIT,
                 VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT |
                     VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
                 VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT |
                     VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_READ_BIT);
    t.layout = VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL;

    depthAtt.imageView = t.attachmentView;
    depthAtt.storeOp = VK_ATTACHMENT_STORE_OP_STORE;

    rendering.renderArea = {{0, 0},
                            {static_cast<uint32_t>(w),
                             static_cast<uint32_t>(h)}};
    rendering.layerCount = t.cube ? 6 : 1;

    viewport.x = 0.0f;
    viewport.y = 0.0f;
    viewport.width = static_cast<float>(w);
    viewport.height = static_cast<float>(h);

    currentPass = t.cube ? VKPassShadowCube : VKPassShadow2D;
    currentShadowTarget = framebuffer;
  }

  vkCmdBeginRendering(cb, &rendering);
  vkCmdSetViewport(cb, 0, 1, &viewport);
  VkRect2D scissor = rendering.renderArea;
  vkCmdSetScissor(cb, 0, 1, &scissor);
  vkCmdSetCullMode(cb, cullFront ? VK_CULL_MODE_FRONT_BIT
                                 : VK_CULL_MODE_BACK_BIT);
  vkCmdSetDepthCompareOp(cb, depthLequal ? VK_COMPARE_OP_LESS_OR_EQUAL
                                         : VK_COMPARE_OP_LESS);
}

void VKBackend::endPass() {
  if (!frameActive)
    return;
  VkCommandBuffer cb = frames[frameIndex].cb;
  vkCmdEndRendering(cb);

  if (currentShadowTarget) {
    ShadowEntry &t = shadowTargets[currentShadowTarget];
    imageBarrier(cb, t.image, VK_IMAGE_ASPECT_DEPTH_BIT, t.cube ? 6 : 1,
                 VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL,
                 VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
                 VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
                 VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,
                 VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT,
                 VK_ACCESS_2_SHADER_SAMPLED_READ_BIT);
    t.layout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
    currentShadowTarget = 0;
  }
}

// ---- dynamic state -------------------------------------------------------------

void VKBackend::setCullFace(bool front) {
  cullFront = front;
  if (frameActive)
    vkCmdSetCullMode(frames[frameIndex].cb, front ? VK_CULL_MODE_FRONT_BIT
                                                  : VK_CULL_MODE_BACK_BIT);
}

void VKBackend::setDepthFunc(bool lequal) {
  depthLequal = lequal;
  if (frameActive)
    vkCmdSetDepthCompareOp(frames[frameIndex].cb,
                           lequal ? VK_COMPARE_OP_LESS_OR_EQUAL
                                  : VK_COMPARE_OP_LESS);
}

// ---- shaders / pipelines -------------------------------------------------------

std::unique_ptr<Shader> VKBackend::createShader(const std::string &name,
                                                bool hasGeometry) {
  const std::string base = "shaders/vk/" + name;
  return std::make_unique<VKShader>(*this, base + ".vert.spv",
                                    base + ".frag.spv",
                                    hasGeometry ? base + ".geo.spv" : "");
}

VkPipeline VKBackend::getPipeline(VKShader &shader, VKPass pass,
                                  VKVertexLayout layout) {
  VkPipeline &cached = shader.pipelines[pass][layout];
  if (cached)
    return cached;

  std::vector<VkPipelineShaderStageCreateInfo> stages;
  auto addStage = [&](VkShaderStageFlagBits stage, VkShaderModule module) {
    VkPipelineShaderStageCreateInfo ci{
        VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO};
    ci.stage = stage;
    ci.module = module;
    ci.pName = "main";
    stages.push_back(ci);
  };
  addStage(VK_SHADER_STAGE_VERTEX_BIT, shader.vertModule);
  if (shader.geoModule)
    addStage(VK_SHADER_STAGE_GEOMETRY_BIT, shader.geoModule);
  addStage(VK_SHADER_STAGE_FRAGMENT_BIT, shader.fragModule);

  // Vertex layouts match the GL backend: mesh = pos/normal/uv interleaved,
  // skybox = positions only.
  VkVertexInputBindingDescription binding{};
  binding.binding = 0;
  binding.stride = layout == VKLayoutMesh ? 8 * sizeof(float)
                                          : 3 * sizeof(float);
  binding.inputRate = VK_VERTEX_INPUT_RATE_VERTEX;
  std::vector<VkVertexInputAttributeDescription> attrs = {
      {0, 0, VK_FORMAT_R32G32B32_SFLOAT, 0}};
  // The depth-only shaders consume just the position attribute
  if (layout == VKLayoutMesh && pass == VKPassMain) {
    attrs.push_back({1, 0, VK_FORMAT_R32G32B32_SFLOAT, 3 * sizeof(float)});
    attrs.push_back({2, 0, VK_FORMAT_R32G32_SFLOAT, 6 * sizeof(float)});
  }
  VkPipelineVertexInputStateCreateInfo vertexInput{
      VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO};
  vertexInput.vertexBindingDescriptionCount = 1;
  vertexInput.pVertexBindingDescriptions = &binding;
  vertexInput.vertexAttributeDescriptionCount =
      static_cast<uint32_t>(attrs.size());
  vertexInput.pVertexAttributeDescriptions = attrs.data();

  VkPipelineInputAssemblyStateCreateInfo inputAssembly{
      VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO};
  inputAssembly.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST;

  VkPipelineViewportStateCreateInfo viewportState{
      VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO};
  viewportState.viewportCount = 1;
  viewportState.scissorCount = 1;

  VkPipelineRasterizationStateCreateInfo raster{
      VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO};
  raster.polygonMode = VK_POLYGON_MODE_FILL;
  // Vulkan's y-down framebuffer flips winding relative to GL; the main pass's
  // negative-height viewport flips it back. So GL's CCW front face survives on
  // the main pass, and shadow passes (positive viewport) need CW.
  raster.frontFace = pass == VKPassMain ? VK_FRONT_FACE_COUNTER_CLOCKWISE
                                        : VK_FRONT_FACE_CLOCKWISE;
  raster.lineWidth = 1.0f;

  VkPipelineMultisampleStateCreateInfo multisample{
      VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO};
  multisample.rasterizationSamples = VK_SAMPLE_COUNT_1_BIT;

  VkPipelineDepthStencilStateCreateInfo depthStencil{
      VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO};
  depthStencil.depthTestEnable = VK_TRUE;
  depthStencil.depthWriteEnable = VK_TRUE;
  depthStencil.depthCompareOp = VK_COMPARE_OP_LESS; // dynamic

  VkPipelineColorBlendAttachmentState blendAtt{};
  blendAtt.blendEnable = VK_TRUE;
  blendAtt.srcColorBlendFactor = VK_BLEND_FACTOR_SRC_ALPHA;
  blendAtt.dstColorBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
  blendAtt.colorBlendOp = VK_BLEND_OP_ADD;
  blendAtt.srcAlphaBlendFactor = VK_BLEND_FACTOR_ONE;
  blendAtt.dstAlphaBlendFactor = VK_BLEND_FACTOR_ZERO;
  blendAtt.alphaBlendOp = VK_BLEND_OP_ADD;
  blendAtt.colorWriteMask =
      VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT |
      VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT;
  VkPipelineColorBlendStateCreateInfo blend{
      VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO};
  if (pass == VKPassMain) {
    blend.attachmentCount = 1;
    blend.pAttachments = &blendAtt;
  }

  VkDynamicState dynamics[] = {
      VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR,
      VK_DYNAMIC_STATE_CULL_MODE, VK_DYNAMIC_STATE_DEPTH_COMPARE_OP};
  VkPipelineDynamicStateCreateInfo dynamic{
      VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO};
  dynamic.dynamicStateCount = 4;
  dynamic.pDynamicStates = dynamics;

  VkPipelineRenderingCreateInfo renderingCI{
      VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO};
  renderingCI.depthAttachmentFormat = kDepthFormat;
  if (pass == VKPassMain) {
    renderingCI.colorAttachmentCount = 1;
    renderingCI.pColorAttachmentFormats = &swapFormat;
  }

  VkGraphicsPipelineCreateInfo ci{
      VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO};
  ci.pNext = &renderingCI;
  ci.stageCount = static_cast<uint32_t>(stages.size());
  ci.pStages = stages.data();
  ci.pVertexInputState = &vertexInput;
  ci.pInputAssemblyState = &inputAssembly;
  ci.pViewportState = &viewportState;
  ci.pRasterizationState = &raster;
  ci.pMultisampleState = &multisample;
  ci.pDepthStencilState = &depthStencil;
  ci.pColorBlendState = &blend;
  ci.pDynamicState = &dynamic;
  ci.layout = pipelineLayout;
  VK_CHECK(vkCreateGraphicsPipelines(device, VK_NULL_HANDLE, 1, &ci, nullptr,
                                     &cached));
  return cached;
}

// ---- draws ---------------------------------------------------------------------

void VKBackend::recordDraw(VKVertexLayout layout) {
  FrameData &f = frames[frameIndex];

  VkPipeline pipeline = getPipeline(*currentShader, currentPass, layout);
  if (pipeline != boundPipeline) {
    vkCmdBindPipeline(f.cb, VK_PIPELINE_BIND_POINT_GRAPHICS, pipeline);
    boundPipeline = pipeline;
  }

  // Resolve GL-style texture units into bindless array slots
  for (auto &[name, unit] : currentShader->samplerUnits) {
    const auto &slot = vkSamplerSlots().at(name);
    uint32_t handle = 0;
    if (unit >= 0 && unit < 8)
      handle = static_cast<uint32_t>(slot.cube ? boundCube[unit]
                                               : boundTex2D[unit]);
    int32_t arraySlot = 0; // slot 0 = white (2D) / black dummy (cube)
    if (handle < textures.size() && textures[handle].valid &&
        textures[handle].cube == slot.cube)
      arraySlot = static_cast<int32_t>(textures[handle].slot);
    std::memcpy(reinterpret_cast<char *>(&currentShader->block) + slot.offset,
                &arraySlot, sizeof arraySlot);
  }

  // Snapshot the uniform block into the per-frame ring; pass its address
  f.ringOffset = (f.ringOffset + 63) & ~static_cast<VkDeviceSize>(63);
  if (f.ringOffset + sizeof(VKUniformBlock) > kRingSize) {
    std::cerr << "Uniform ring overflow\n";
    f.ringOffset = 0;
  }
  std::memcpy(static_cast<char *>(f.ringMapped) + f.ringOffset,
              &currentShader->block, sizeof(VKUniformBlock));
  VkDeviceAddress addr = f.ringAddr + f.ringOffset;
  f.ringOffset += sizeof(VKUniformBlock);
  vkCmdPushConstants(f.cb, pipelineLayout, kPushStages, 0, sizeof addr, &addr);
}

void VKBackend::drawMesh(uint32_t vao, size_t indexCount) {
  if (!frameActive || !currentShader || vao >= meshes.size())
    return;
  MeshEntry &m = meshes[vao];
  recordDraw(VKLayoutMesh);

  VkCommandBuffer cb = frames[frameIndex].cb;
  VkDeviceSize offset = 0;
  vkCmdBindVertexBuffers(cb, 0, 1, &buffers[m.vbo].buffer, &offset);
  vkCmdBindIndexBuffer(cb, m.indexBuffer, 0, VK_INDEX_TYPE_UINT32);
  vkCmdDrawIndexed(cb, static_cast<uint32_t>(indexCount), 1, 0, 0, 0);
}

void VKBackend::drawSkybox(uint32_t vao) {
  if (!frameActive || !currentShader || vao >= meshes.size())
    return;
  MeshEntry &m = meshes[vao];
  recordDraw(VKLayoutSkybox);

  VkCommandBuffer cb = frames[frameIndex].cb;
  VkDeviceSize offset = 0;
  vkCmdBindVertexBuffers(cb, 0, 1, &buffers[m.vbo].buffer, &offset);
  vkCmdDraw(cb, 36, 1, 0, 0);
}

// ---- textures ------------------------------------------------------------------

uint32_t VKBackend::loadTexture(const std::string &path) {
  int w, h, channels;
  stbi_set_flip_vertically_on_load(false);
  unsigned char *data = stbi_load(path.c_str(), &w, &h, &channels, 4);
  if (!data) {
    std::cerr << "Failed to load texture: " << path << "\n";
    return 0;
  }
  uint32_t handle = uploadTexture(data, w, h, 1, false, samplerRepeat);
  stbi_image_free(data);
  return handle;
}

uint32_t VKBackend::loadCubemap(const std::vector<std::string> &faces) {
  stbi_set_flip_vertically_on_load(false);
  std::vector<unsigned char> pixels;
  int w = 0, h = 0;
  for (auto &face : faces) {
    int fw, fh, channels;
    unsigned char *data = stbi_load(face.c_str(), &fw, &fh, &channels, 4);
    if (!data) {
      std::cerr << "Failed to load cubemap face: " << face << "\n";
      return 0;
    }
    if (pixels.empty()) {
      w = fw;
      h = fh;
      pixels.reserve(static_cast<size_t>(w) * h * 4 * 6);
    }
    pixels.insert(pixels.end(), data, data + static_cast<size_t>(fw) * fh * 4);
    stbi_image_free(data);
  }
  return uploadTexture(pixels.data(), w, h, 6, true, samplerCubeLinear);
}

uint32_t VKBackend::whiteTexture() {
  return 0; // handle 0 = white, created at init
}

void VKBackend::destroyTexture(uint32_t handle) {
  if (handle == 0 || handle >= textures.size() || !textures[handle].valid)
    return;
  vkDeviceWaitIdle(device);
  TexEntry &e = textures[handle];
  vkDestroyImageView(device, e.view, nullptr);
  if (e.ownsImage)
    vmaDestroyImage(allocator, e.image, e.alloc);
  e.valid = false;
}

void VKBackend::bindTexture2D(int unit, uint32_t handle) {
  if (unit >= 0 && unit < 8)
    boundTex2D[unit] = static_cast<int32_t>(handle);
  // Unit 0 is the directional 2D shadow map (see Mesh::draw). Mirror it into the
  // dedicated binding 2 so the PCF loop samples a plain bound descriptor.
  if (unit == 0 && handle != shadow2DHandle && handle < textures.size() &&
      textures[handle].valid) {
    shadow2DHandle = handle;
    writeDedicatedTexture(2, 0, textures[handle].view, samplerShadow2D);
  }
}

void VKBackend::bindCubemap(int unit, uint32_t handle) {
  if (unit >= 0 && unit < 8)
    boundCube[unit] = static_cast<int32_t>(handle);
  // Units 4..4+MAX_SHADOW_CUBES are the point-light cube shadow maps (see
  // Mesh::draw) -> dedicated binding 3 array, one element per caster slot.
  int slot = unit - Settings::SHADOW_CUBE_UNIT_BASE;
  if (slot >= 0 && slot < MAX_SHADOW_CUBES && handle != shadowCubeHandles[slot] &&
      handle < textures.size() && textures[handle].valid) {
    shadowCubeHandles[slot] = handle;
    writeDedicatedTexture(3, slot, textures[handle].view, samplerShadowCube);
  }
}

// ---- buffers / meshes ----------------------------------------------------------

uint32_t VKBackend::createBufferInternal(const void *data, size_t byteSize,
                                         VkBufferUsageFlags usage) {
  VkBufferCreateInfo ci{VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
  ci.size = byteSize;
  ci.usage = usage;
  VmaAllocationCreateInfo allocCI{};
  allocCI.usage = VMA_MEMORY_USAGE_AUTO;
  allocCI.flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT |
                  VMA_ALLOCATION_CREATE_MAPPED_BIT;

  BufEntry e;
  VmaAllocationInfo info{};
  VK_CHECK(
      vmaCreateBuffer(allocator, &ci, &allocCI, &e.buffer, &e.alloc, &info));
  e.mapped = info.pMappedData;
  e.valid = true;
  if (data) {
    std::memcpy(e.mapped, data, byteSize);
    vmaFlushAllocation(allocator, e.alloc, 0, byteSize);
  }
  buffers.push_back(e);
  return static_cast<uint32_t>(buffers.size() - 1);
}

uint32_t VKBackend::createBuffer(const float *data, size_t byteSize, bool) {
  return createBufferInternal(data, byteSize,
                              VK_BUFFER_USAGE_VERTEX_BUFFER_BIT);
}

void VKBackend::updateBuffer(uint32_t handle, const float *data,
                             size_t byteSize) {
  if (handle >= buffers.size() || !buffers[handle].valid)
    return;
  // The GPU may still be reading this buffer for an in-flight frame; meshes
  // move rarely, so draining the pipeline here is the simple safe option.
  waitAllFrames();
  std::memcpy(buffers[handle].mapped, data, byteSize);
  vmaFlushAllocation(allocator, buffers[handle].alloc, 0, byteSize);
}

void VKBackend::destroyBuffer(uint32_t handle) {
  if (handle >= buffers.size() || !buffers[handle].valid)
    return;
  vkDeviceWaitIdle(device);
  vmaDestroyBuffer(allocator, buffers[handle].buffer, buffers[handle].alloc);
  buffers[handle].valid = false;
}

void VKBackend::createMesh(uint32_t vbo, const uint32_t *indices, size_t count,
                           uint32_t &vao, uint32_t &ebo) {
  MeshEntry e;
  e.vbo = vbo;

  VkBufferCreateInfo ci{VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
  ci.size = count * sizeof(uint32_t);
  ci.usage = VK_BUFFER_USAGE_INDEX_BUFFER_BIT;
  VmaAllocationCreateInfo allocCI{};
  allocCI.usage = VMA_MEMORY_USAGE_AUTO;
  allocCI.flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT |
                  VMA_ALLOCATION_CREATE_MAPPED_BIT;
  VmaAllocationInfo info{};
  VK_CHECK(vmaCreateBuffer(allocator, &ci, &allocCI, &e.indexBuffer,
                           &e.indexAlloc, &info));
  std::memcpy(info.pMappedData, indices, count * sizeof(uint32_t));
  vmaFlushAllocation(allocator, e.indexAlloc, 0, count * sizeof(uint32_t));

  e.valid = true;
  meshes.push_back(e);
  vao = static_cast<uint32_t>(meshes.size() - 1);
  ebo = vao;
}

void VKBackend::destroyMesh(uint32_t vao, uint32_t) {
  if (vao >= meshes.size() || !meshes[vao].valid)
    return;
  vkDeviceWaitIdle(device);
  vmaDestroyBuffer(allocator, meshes[vao].indexBuffer, meshes[vao].indexAlloc);
  meshes[vao].valid = false;
}

void VKBackend::createSkyboxMesh(const float *verts, size_t byteSize,
                                 uint32_t &vao, uint32_t &vbo) {
  vbo = createBufferInternal(verts, byteSize, VK_BUFFER_USAGE_VERTEX_BUFFER_BIT);
  MeshEntry e;
  e.vbo = vbo;
  e.valid = true;
  meshes.push_back(e);
  vao = static_cast<uint32_t>(meshes.size() - 1);
}

void VKBackend::destroySkyboxMesh(uint32_t vao, uint32_t vbo) {
  destroyBuffer(vbo);
  if (vao < meshes.size())
    meshes[vao].valid = false;
}

// ---- shadow targets ------------------------------------------------------------

void VKBackend::createShadowMap2D(int w, int h, uint32_t &fbo, uint32_t &tex) {
  ShadowEntry e;
  e.cube = false;
  e.w = w;
  e.h = h;

  VkImageCreateInfo ci{VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO};
  ci.imageType = VK_IMAGE_TYPE_2D;
  ci.format = kDepthFormat;
  ci.extent = {static_cast<uint32_t>(w), static_cast<uint32_t>(h), 1};
  ci.mipLevels = 1;
  ci.arrayLayers = 1;
  ci.samples = VK_SAMPLE_COUNT_1_BIT;
  ci.tiling = VK_IMAGE_TILING_OPTIMAL;
  ci.usage = VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT |
             VK_IMAGE_USAGE_SAMPLED_BIT;
  VmaAllocationCreateInfo allocCI{};
  allocCI.usage = VMA_MEMORY_USAGE_AUTO;
  VK_CHECK(vmaCreateImage(allocator, &ci, &allocCI, &e.image, &e.alloc,
                          nullptr));

  VkImageViewCreateInfo viewCI{VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO};
  viewCI.image = e.image;
  viewCI.viewType = VK_IMAGE_VIEW_TYPE_2D;
  viewCI.format = kDepthFormat;
  viewCI.subresourceRange = {VK_IMAGE_ASPECT_DEPTH_BIT, 0, 1, 0, 1};
  VK_CHECK(vkCreateImageView(device, &viewCI, nullptr, &e.attachmentView));

  VkImageView sampleView;
  VK_CHECK(vkCreateImageView(device, &viewCI, nullptr, &sampleView));
  e.tex = registerTexture(false, e.image, VK_NULL_HANDLE, sampleView,
                          samplerShadow2D, false);

  e.valid = true;
  shadowTargets.push_back(e);
  fbo = static_cast<uint32_t>(shadowTargets.size() - 1);
  tex = e.tex;
}

void VKBackend::createShadowCubemap(int w, int h, uint32_t &fbo,
                                    uint32_t &cube) {
  ShadowEntry e;
  e.cube = true;
  e.w = w;
  e.h = h;

  VkImageCreateInfo ci{VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO};
  ci.flags = VK_IMAGE_CREATE_CUBE_COMPATIBLE_BIT;
  ci.imageType = VK_IMAGE_TYPE_2D;
  ci.format = kDepthFormat;
  ci.extent = {static_cast<uint32_t>(w), static_cast<uint32_t>(h), 1};
  ci.mipLevels = 1;
  ci.arrayLayers = 6;
  ci.samples = VK_SAMPLE_COUNT_1_BIT;
  ci.tiling = VK_IMAGE_TILING_OPTIMAL;
  ci.usage = VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT |
             VK_IMAGE_USAGE_SAMPLED_BIT;
  VmaAllocationCreateInfo allocCI{};
  allocCI.usage = VMA_MEMORY_USAGE_AUTO;
  VK_CHECK(vmaCreateImage(allocator, &ci, &allocCI, &e.image, &e.alloc,
                          nullptr));

  // Layered 2D-array view for the geometry-shader pass (gl_Layer)
  VkImageViewCreateInfo viewCI{VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO};
  viewCI.image = e.image;
  viewCI.viewType = VK_IMAGE_VIEW_TYPE_2D_ARRAY;
  viewCI.format = kDepthFormat;
  viewCI.subresourceRange = {VK_IMAGE_ASPECT_DEPTH_BIT, 0, 1, 0, 6};
  VK_CHECK(vkCreateImageView(device, &viewCI, nullptr, &e.attachmentView));

  VkImageView sampleView;
  viewCI.viewType = VK_IMAGE_VIEW_TYPE_CUBE;
  VK_CHECK(vkCreateImageView(device, &viewCI, nullptr, &sampleView));
  e.tex = registerTexture(true, e.image, VK_NULL_HANDLE, sampleView,
                          samplerShadowCube, false);

  e.valid = true;
  shadowTargets.push_back(e);
  fbo = static_cast<uint32_t>(shadowTargets.size() - 1);
  cube = e.tex;
}

void VKBackend::destroyFramebuffer(uint32_t fbo) {
  if (fbo == 0 || fbo >= shadowTargets.size() || !shadowTargets[fbo].valid)
    return;
  vkDeviceWaitIdle(device);
  ShadowEntry &e = shadowTargets[fbo];
  vkDestroyImageView(device, e.attachmentView, nullptr);
  vmaDestroyImage(allocator, e.image, e.alloc);
  e.valid = false;
}

// ---- teardown ------------------------------------------------------------------

VKBackend::~VKBackend() {
  if (!device)
    return;
  vkDeviceWaitIdle(device);

  for (auto &e : textures)
    if (e.valid) {
      vkDestroyImageView(device, e.view, nullptr);
      if (e.ownsImage)
        vmaDestroyImage(allocator, e.image, e.alloc);
    }
  for (auto &e : shadowTargets)
    if (e.valid) {
      vkDestroyImageView(device, e.attachmentView, nullptr);
      vmaDestroyImage(allocator, e.image, e.alloc);
    }
  for (auto &e : buffers)
    if (e.valid)
      vmaDestroyBuffer(allocator, e.buffer, e.alloc);
  for (auto &e : meshes)
    if (e.valid && e.indexBuffer)
      vmaDestroyBuffer(allocator, e.indexBuffer, e.indexAlloc);

  for (auto &f : frames) {
    vmaDestroyBuffer(allocator, f.ring, f.ringAlloc);
    vkDestroyFence(device, f.fence, nullptr);
    vkDestroySemaphore(device, f.acquireSem, nullptr);
  }

  vkDestroySampler(device, samplerRepeat, nullptr);
  vkDestroySampler(device, samplerCubeLinear, nullptr);
  vkDestroySampler(device, samplerShadow2D, nullptr);
  vkDestroySampler(device, samplerShadowCube, nullptr);

  if (timestampPool)
    vkDestroyQueryPool(device, timestampPool, nullptr);
  vkDestroyPipelineLayout(device, pipelineLayout, nullptr);
  vkDestroyDescriptorPool(device, descriptorPool, nullptr);
  vkDestroyDescriptorSetLayout(device, setLayout, nullptr);
  vkDestroyCommandPool(device, commandPool, nullptr);

  destroySwapchain();
  vkDestroySwapchainKHR(device, swapchain, nullptr);
  vmaDestroyAllocator(allocator);
  vkDestroyDevice(device, nullptr);

  if (debugMessenger) {
    auto destroy = reinterpret_cast<PFN_vkDestroyDebugUtilsMessengerEXT>(
        vkGetInstanceProcAddr(instance, "vkDestroyDebugUtilsMessengerEXT"));
    if (destroy)
      destroy(instance, debugMessenger, nullptr);
  }
  vkDestroySurfaceKHR(instance, surface, nullptr);
  vkDestroyInstance(instance, nullptr);
}
