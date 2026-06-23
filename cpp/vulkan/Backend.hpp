#pragma once
#include "Shader.hpp"
#include "renderer/Backend.hpp"

#include <vk_mem_alloc.h>
#include <vulkan/vulkan.h>

#include <cstring>
#include <functional>
#include <vector>

class VKBackend final : public Backend {
public:
  ~VKBackend() override;

  void configureWindow() override;
  void init(GLFWwindow *window) override;

  void beginFrame() override;
  void endFrame() override;

  void beginPass(uint32_t framebuffer, int w, int h, bool clearColor, float r,
                 float g, float b, float a) override;
  void endPass() override;

  void setCullFace(bool front) override;
  void setDepthFunc(bool lequal) override;

  std::unique_ptr<Shader> createShader(const std::string &name,
                                       bool hasGeometry) override;

  uint32_t loadTexture(const std::string &path) override;
  uint32_t loadCubemap(const std::vector<std::string> &faces) override;
  uint32_t whiteTexture() override;
  void destroyTexture(uint32_t handle) override;
  void bindTexture2D(int unit, uint32_t handle) override;
  void bindCubemap(int unit, uint32_t handle) override;

  uint32_t createBuffer(const float *data, size_t byteSize,
                        bool dynamic) override;
  void updateBuffer(uint32_t handle, const float *data,
                    size_t byteSize) override;
  void destroyBuffer(uint32_t handle) override;

  void createMesh(uint32_t vbo, const uint32_t *indices, size_t count,
                  uint32_t &vao, uint32_t &ebo) override;
  void destroyMesh(uint32_t vao, uint32_t ebo) override;

  void createSkyboxMesh(const float *verts, size_t byteSize, uint32_t &vao,
                        uint32_t &vbo) override;
  void destroySkyboxMesh(uint32_t vao, uint32_t vbo) override;

  void drawMesh(uint32_t vao, size_t indexCount) override;
  void drawSkybox(uint32_t vao) override;

  void createShadowMap2D(int w, int h, uint32_t &fbo, uint32_t &tex) override;
  void createShadowCubemap(int w, int h, uint32_t &fbo,
                           uint32_t &cube) override;
  void destroyFramebuffer(uint32_t fbo) override;

  // --- used by VKShader ---
  VkDevice vkDevice() const { return device; }
  void setCurrentShader(VKShader *s) { currentShader = s; }

private:
  static constexpr int kFramesInFlight = 2;
  static constexpr VkDeviceSize kRingSize = 1 << 20; // per-frame uniform ring
  static constexpr VkFormat kDepthFormat = VK_FORMAT_D32_SFLOAT;
  static constexpr uint32_t kMax2DTextures = 256;
  static constexpr uint32_t kMaxCubeTextures = 64;

  struct TexEntry {
    bool cube = false;
    uint32_t slot = 0; // index into the bindless array of its kind
    VkImage image = VK_NULL_HANDLE;
    VmaAllocation alloc = VK_NULL_HANDLE;
    VkImageView view = VK_NULL_HANDLE;
    bool ownsImage = false; // shadow map images are owned by ShadowEntry
    bool valid = false;
  };

  struct BufEntry {
    VkBuffer buffer = VK_NULL_HANDLE;
    VmaAllocation alloc = VK_NULL_HANDLE;
    void *mapped = nullptr;
    bool valid = false;
  };

  struct MeshEntry {
    uint32_t vbo = 0;
    VkBuffer indexBuffer = VK_NULL_HANDLE;
    VmaAllocation indexAlloc = VK_NULL_HANDLE;
    bool valid = false;
  };

  struct ShadowEntry {
    bool cube = false;
    int w = 0, h = 0;
    VkImage image = VK_NULL_HANDLE;
    VmaAllocation alloc = VK_NULL_HANDLE;
    VkImageView attachmentView = VK_NULL_HANDLE; // 2D or 2D_ARRAY(6)
    uint32_t tex = 0;                            // sampled-view handle
    VkImageLayout layout = VK_IMAGE_LAYOUT_UNDEFINED;
    bool valid = false;
  };

  struct FrameData {
    VkCommandBuffer cb = VK_NULL_HANDLE;
    VkFence fence = VK_NULL_HANDLE;
    VkSemaphore acquireSem = VK_NULL_HANDLE;
    VkBuffer ring = VK_NULL_HANDLE;
    VmaAllocation ringAlloc = VK_NULL_HANDLE;
    void *ringMapped = nullptr;
    VkDeviceAddress ringAddr = 0;
    VkDeviceSize ringOffset = 0;
  };

  // --- setup ---
  void createInstance();
  void pickPhysicalDevice();
  void createDevice();
  void createSwapchain();
  void destroySwapchain();
  void recreateSwapchain();
  void createFrameData();
  void createSamplers();
  void createDescriptors();
  void createGlobalPipelineLayout();
  void createDefaultTextures();

  // --- helpers ---
  void immediateSubmit(const std::function<void(VkCommandBuffer)> &record);
  void imageBarrier(VkCommandBuffer cb, VkImage image,
                    VkImageAspectFlags aspect, uint32_t layerCount,
                    VkImageLayout from, VkImageLayout to,
                    VkPipelineStageFlags2 srcStage, VkAccessFlags2 srcAccess,
                    VkPipelineStageFlags2 dstStage, VkAccessFlags2 dstAccess);
  uint32_t registerTexture(bool cube, VkImage image, VmaAllocation alloc,
                           VkImageView view, VkSampler sampler, bool ownsImage);
  // Writes a single combined image/sampler into a dedicated (non-bindless)
  // descriptor binding (the shadow maps, bindings 2 and 3). Binding 3 is an
  // array (one cube per point-shadow caster); arrayElement selects the slot.
  void writeDedicatedTexture(uint32_t binding, uint32_t arrayElement,
                             VkImageView view, VkSampler sampler);
  uint32_t uploadTexture(const unsigned char *pixels, int w, int h, int layers,
                         bool cube, VkSampler sampler);
  uint32_t createBufferInternal(const void *data, size_t byteSize,
                                VkBufferUsageFlags usage);
  VkPipeline getPipeline(VKShader &shader, VKPass pass, VKVertexLayout layout);
  void recordDraw(VKVertexLayout layout);
  void waitAllFrames();

  // --- core objects ---
  GLFWwindow *window = nullptr;
  VkInstance instance = VK_NULL_HANDLE;
  VkDebugUtilsMessengerEXT debugMessenger = VK_NULL_HANDLE;
  VkSurfaceKHR surface = VK_NULL_HANDLE;
  VkPhysicalDevice physicalDevice = VK_NULL_HANDLE;
  VkDevice device = VK_NULL_HANDLE;
  uint32_t queueFamily = 0;
  VkQueue queue = VK_NULL_HANDLE;
  VmaAllocator allocator = VK_NULL_HANDLE;

  // --- swapchain ---
  VkSwapchainKHR swapchain = VK_NULL_HANDLE;
  VkFormat swapFormat = VK_FORMAT_B8G8R8A8_UNORM;
  VkExtent2D swapExtent{};
  std::vector<VkImage> swapImages;
  std::vector<VkImageView> swapViews;
  std::vector<VkSemaphore> renderSems; // one per swapchain image
  VkImage depthImage = VK_NULL_HANDLE;
  VmaAllocation depthAlloc = VK_NULL_HANDLE;
  VkImageView depthView = VK_NULL_HANDLE;

  // --- frame state ---
  VkCommandPool commandPool = VK_NULL_HANDLE;
  FrameData frames[kFramesInFlight];
  int frameIndex = 0;
  uint32_t imageIndex = 0;
  bool frameActive = false;

  // --- descriptors / pipeline layout ---
  VkDescriptorSetLayout setLayout = VK_NULL_HANDLE;
  VkDescriptorPool descriptorPool = VK_NULL_HANDLE;
  VkDescriptorSet descriptorSet = VK_NULL_HANDLE;
  VkPipelineLayout pipelineLayout = VK_NULL_HANDLE;
  // Texture handles currently mirrored into the dedicated shadow bindings (2/3),
  // so the descriptors are only rewritten when the caster's map changes.
  uint32_t shadow2DHandle = UINT32_MAX;
  uint32_t shadowCubeHandles[MAX_SHADOW_CUBES] = {UINT32_MAX, UINT32_MAX,
                                                  UINT32_MAX, UINT32_MAX};

  // --- samplers ---
  VkSampler samplerRepeat = VK_NULL_HANDLE;     // material textures
  VkSampler samplerCubeLinear = VK_NULL_HANDLE; // skybox
  VkSampler samplerShadow2D = VK_NULL_HANDLE;   // nearest, white border
  VkSampler samplerShadowCube = VK_NULL_HANDLE; // nearest, clamp to edge

  // --- resources (handle == index, entry 0 reserved) ---
  std::vector<TexEntry> textures;
  std::vector<BufEntry> buffers;
  std::vector<MeshEntry> meshes;
  std::vector<ShadowEntry> shadowTargets;
  uint32_t next2DSlot = 0;
  uint32_t nextCubeSlot = 0;

  // --- draw-time state ---
  VKShader *currentShader = nullptr;
  VKPass currentPass = VKPassMain;
  uint32_t currentShadowTarget = 0; // 0 = backbuffer pass
  VkPipeline boundPipeline = VK_NULL_HANDLE;
  int32_t boundTex2D[8] = {};
  int32_t boundCube[8] = {};
  bool cullFront = false;
  bool depthLequal = false;
};
