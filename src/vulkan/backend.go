// Package vulkan implements renderer.Backend on Vulkan 1.3, using the
// hand-written bindings in the go-vulkan repo. Every vk.* call in the engine
// lives in this package, and nothing in it names a rendering technique.
package vulkan

import (
	"fmt"
	"math"
	"slices"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

const (
	framesInFlight = 2
	// Per-frame upload arena. Sized by the shadow bake, which uploads one
	// ~80-byte block per tile: a full 337-slot atlas costs ~40 KiB here, where
	// republishing the whole frame block per tile used to cost ~1.6 MiB
	arenaSize = 2 << 20
	// Views 0 and 1 are reserved: none, and the backbuffer
	firstUserView = 2

	depthFormat = vk.FormatD32Sfloat
)

// Every stage reads the push constant: vertex for matrices, geometry for a cube
// pass, fragment for materials and lights, compute for whatever a dispatch was
// handed
const pushStages = vk.ShaderStageVertex | vk.ShaderStageGeometry | vk.ShaderStageFragment | vk.ShaderStageCompute

// The same stages reach the descriptor set
const descriptorStages = pushStages

// Four device addresses, well inside the 128-byte guaranteed minimum
const pushConstantSize = uint32(unsafe.Sizeof([4]renderer.Address{}))

// The backend implements the whole interface, checked here rather than at the
// one call site that assigns it
var _ renderer.Backend = (*VKBackend)(nil)

type VKBackend struct {
	window *glfw.Window

	vkInstance        vk.Instance
	vkSurface         vk.SurfaceKHR
	vkPhysDevice      vk.PhysicalDevice
	vkDevice          vk.Device
	queueFamily       uint32
	vkQueue           vk.Queue
	vmaAllocator      *vk.VmaAllocator
	vkPhysDeviceProps vk.PhysicalDeviceProperties
	capacities        renderer.Capacities

	// swapchain and everything sized to it
	vkSwapchainCI      vk.SwapchainCreateInfo
	vkSwapchain        vk.SwapchainKHR
	vkSwapFormat       vk.Format
	vkSwapExtent       vk.Extent2D
	swapchainImages    []image
	vkRenderSemaphores []vk.Semaphore
	// What the backbuffer rasterises at, resolved against the device's limits
	// once at Init. The images sized to it are the caller's
	vkSamples vk.SampleCountFlags

	// frame state
	vkCommandPool vk.CommandPool
	frames        [framesInFlight]frame
	frameIndex    int
	imageIndex    uint32
	frameCounter  uint64
	// True while Backend.Frame's closure runs, which is what tells an image
	// upload to stage rather than submit
	recording       bool
	vkBoundPipeline vk.Pipeline

	// descriptors
	vkSetLayout      vk.DescriptorSetLayout
	vkDescriptorPool vk.DescriptorPool
	vkDescriptorSet  vk.DescriptorSet
	vkPipelineLayout vk.PipelineLayout
	slotFree         [bindingKinds][]uint32
	slotNext         [bindingKinds]uint32

	// Resource tables, the handle being the index. Entry 0 is reserved in the
	// tables whose handle 0 means "none"
	images          []*image
	views           []*view
	buffers         []*bufferInfo
	meshes          []*meshInfo
	pipelines       []*pipelineInfo
	vkSamplers      []vk.Sampler
	vkShaderModules map[string]vk.ShaderModule

	// Images with staged pixels waiting to be copied at the next frame's start
	pendingUploads []renderer.ImageHandle
	// Resources waiting for the frames that reference them to finish
	retired []retired

	// Whether VK_EXT_debug_utils was there to enable, which is what makes a
	// pass label reach a capture
	hasLabels bool
}

// Builds an empty Vulkan backend, before any Vulkan object exists
func New() *VKBackend {
	backend := &VKBackend{
		vkSwapFormat:    vk.FormatB8G8R8A8Unorm, // no gamma encoding : already done in shader
		vkSamples:       vk.SampleCount1Bit,     // 1 sample per pixel : MSAA off
		vkShaderModules: map[string]vk.ShaderModule{},
	}
	// Reserve handle 0 for "none"
	// Reserve handle 1 for backbuffer (redirects to swapchainImages)
	backend.images = append(backend.images, &image{binding: -1}, &image{binding: -1})
	// Reserve handle 0 for "none"
	backend.buffers = append(backend.buffers, &bufferInfo{})
	// Reserve handle 0 for "none"
	backend.meshes = append(backend.meshes, &meshInfo{})
	return backend
}

// Aborts on a failed Vulkan call
func fatalVk(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("vulkan: %s: %v", what, err))
	}
}

// --- lifecycle ---------------------------------------------------------------

// Brings up the whole device stack
func (backend *VKBackend) Init(window *glfw.Window, req renderer.Request) error { 
	backend.window = window

	if err := backend.createInstance(); err != nil {
		return err
	}
	if err := backend.createSurfaceAndDevice(); err != nil {
		return err
	}

	backend.vmaAllocator = vk.VmaCreateAllocator(vk.VmaAllocatorCreateInfo{
		Flags:          vk.VmaAllocatorCreateBufferDeviceAddressBit,
		PhysicalDevice: backend.vkPhysDevice,
		Device:         backend.vkDevice,
		Instance:       backend.vkInstance,
	})

	backend.vkSamples = backend.pickSampleCount(req.Samples)

	if err := backend.createSwapchain(); err != nil {
		return err
	}

	pool, err := vk.CreateCommandPool(backend.vkDevice, backend.queueFamily, vk.CommandPoolCreateResetCommandBuffer)
	if err != nil {
		return err
	}
	backend.vkCommandPool = pool

	if err := backend.createFrames(); err != nil {
		return err
	}
	backend.createDefaultSampler()
	backend.createDescriptors()
	backend.createGlobalPipelineLayout()
	backend.createDefaultImages()
	backend.buildCaps(req)
	return nil
}

// Creates the instance with the extensions GLFW requires, the validation layers
// when [debug] validation is set, and debug-utils when the loader has it
func (backend *VKBackend) createInstance() error {
	// Add validation layers if required to help catch misuse in driver API calls
	var layers []string
	if settings.Validation {
		layers = append(layers, "VK_LAYER_KHRONOS_validation")
	}

	// Add debug-utils extensions if available to label command buffers and queues for RenderDoc debugging
	extensions := backend.window.GetRequiredInstanceExtensions()
	if available, err := vk.EnumerateInstanceExtensionProperties(); err == nil {
		if slices.Contains(available, vk.ExtDebugUtils) {
			extensions = append(extensions, vk.ExtDebugUtils)
			backend.hasLabels = true
		}
	}

	// Create vulkan instance
	inst, err := vk.CreateInstance(vk.InstanceCreateInfo{
		AppName:    "Overdrive",
		APIVersion: vk.ApiVersion13,
		Extensions: extensions,
		Layers:     layers,
	})
	if err != nil {
		return err
	}
	backend.vkInstance = inst

	// Load debug-utils on instance
	if backend.hasLabels {
		vk.LoadDebugUtils(inst)
	}
	return nil
}

// Creates the window surface and the logical device with the features the engine needs
func (backend *VKBackend) createSurfaceAndDevice() error {
	// List all GPUs
	devices, err := vk.EnumeratePhysicalDevices(backend.vkInstance)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Vulkan physical devices")
	}
	backend.vkPhysDevice = devices[0]
	backend.vkPhysDeviceProps = vk.GetPhysicalDeviceProperties2(backend.vkPhysDevice)
	fmt.Printf("Vulkan device: %s\n", backend.vkPhysDeviceProps.DeviceName)

	// Find compatible command queue
	foundQueue := false
	for i, queueFamily := range vk.GetPhysicalDeviceQueueFamilyProperties(backend.vkPhysDevice) {
		// QueueFlags is all 1s masked by QueueGraphics to ensure graphics support
		if queueFamily.QueueFlags&vk.QueueGraphics != 0 {
			backend.queueFamily = uint32(i)
			foundQueue = true
			break
		}
	}
	if !foundQueue {
		return fmt.Errorf("no queue family supports both graphics and present")
	}

	// Create device with useful features
	device, err := vk.CreateDevice(backend.vkPhysDevice, vk.DeviceCreateInfo{
		QueueCreateInfos: []vk.DeviceQueueCreateInfo{
			{QueueFamilyIndex: backend.queueFamily, Priorities: []float32{1}},
		},
		Extensions: []string{"VK_KHR_swapchain"},
		Features: vk.Features{
			// Enables the six descriptor features below
			DescriptorIndexing: true,
			// Lets the texture array index vary per fragment, not just per draw
			ShaderSampledImageArrayNonUniformIndexing: true,
			// The same, for the storage images a dispatch writes
			ShaderStorageImageArrayNonUniformIndexing: true,
			// Lets a shader declare an unsized array, sized at set-layout creation instead
			RuntimeDescriptorArray: true,
			// Gives a buffer a raw GPU pointer, which is what renderer.Address is
			BufferDeviceAddress: true,
			// Lets a sampler ask for anisotropy above 1
			SamplerAnisotropy: true,
			// The VkSubmitInfo2 and split stage/access masks barrier.go builds
			Synchronization2: true,

			// Names attachments inline, so no VkRenderPass or VkFramebuffer object exists
			DynamicRendering: true,
			// Declared but unused: no shader in the tree has a geometry entry point
			GeometryShader: true,
			// Permits the scalar block layout Slang emits, which Go's struct packing matches
			ScalarBlockLayout: true,
			// Tolerates unwritten slots in a descriptor array, as long as no shader reads one
			DescriptorBindingPartiallyBound: true,
			// Lets Slot rewrite a descriptor while a frame using the set is still in flight
			DescriptorBindingSampledImageUpdateAfterBind: true,
			// The same, for the storage images a dispatch writes
			DescriptorBindingStorageImageUpdateAfterBind: true,
		},
	})
	if err != nil {
		return err
	}
	backend.vkDevice = device
	backend.vkQueue = vk.GetDeviceQueue(device, backend.queueFamily, 0)

	// Get window surface
	surface, err := backend.window.CreateWindowSurface((*byte)(unsafe.Pointer(backend.vkInstance)), nil)
	if err != nil {
		return err
	}
	backend.vkSurface = vk.SurfaceKHR(*(*uintptr)(unsafe.Pointer(surface)))

	// Check ig queue family supports presenting to a surface
	ok, err := vk.GetPhysicalDeviceSurfaceSupportKHR(backend.vkPhysDevice, backend.queueFamily, backend.vkSurface)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("selected queue family cannot present to the surface")
	}
	return nil
}

// Allocates the per-frame command buffer, fence, semaphore, mapped arena and
// query pool, one set per frame in flight
func (backend *VKBackend) createFrames() error {
	commandBuffers, err := vk.AllocateCommandBuffers(backend.vkDevice, backend.vkCommandPool, framesInFlight)
	if err != nil {
		return err
	}
	for i := range backend.frames {
		info := &backend.frames[i]
		info.vkCommandBuffer = commandBuffers[i]
		// Start in Signaled state so the first frame does not block on a fence that will never be signaled
		info.vkFence, err = vk.CreateFence(backend.vkDevice, vk.FenceCreateSignaled)
		if err != nil {
			return err
		}
		info.vkAcquireSemaphore, err = vk.CreateSemaphore(backend.vkDevice)
		if err != nil {
			return err
		}

		vkBuffer, vkAlloc, vkAllocInfo, err := backend.vmaAllocator.VmaCreateBuffer(
			vk.BufferCreateInfo{Size: arenaSize, Usage: vk.BufferUsageShaderDeviceAddress},
			vk.VmaAllocationCreateInfo{
				Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
				Usage: vk.VmaMemoryUsageAuto,
			})
		if err != nil {
			return err
		}
		info.vkArenaBuffer, info.vmaArenaAlloc, info.arenaMapped = vkBuffer, vkAlloc, vkAllocInfo.MappedData
		info.arenaAddr = vk.GetBufferDeviceAddress(backend.vkDevice, vkBuffer)
	}
	return nil
}

// Creates the sampler handle 0 names, which every image gets unless it asks for
// another
func (backend *VKBackend) createDefaultSampler() { 
	anisotropy := float32(0)
	if settings.AnisotropyEnabled() {
		anisotropy = float32(settings.Anisotropy)
	}
	sampler, err := vk.CreateSampler(backend.vkDevice, vk.SamplerCreateInfo{
		MagFilter: vk.FilterLinear, MinFilter: vk.FilterLinear,
		MipmapMode:       vk.SamplerMipmapModeLinear,
		AddressModeU:     vk.SamplerAddressModeRepeat,
		AddressModeV:     vk.SamplerAddressModeRepeat,
		AddressModeW:     vk.SamplerAddressModeRepeat,
		AnisotropyEnable: anisotropy > 1,
		MaxAnisotropy: func(a, b float32) float32 { if a < b { return a }; return b }(anisotropy, backend.vkPhysDeviceProps.MaxSamplerAnisotropy),
		MaxLod:           16,
	})
	fatalVk(err, "create default sampler")
	backend.vkSamplers = append(backend.vkSamplers, sampler)
}

func (backend *VKBackend) createGlobalPipelineLayout() { 
	layout, err := vk.CreatePipelineLayout(backend.vkDevice, vk.PipelineLayoutCreateInfo{
		SetLayouts:         []vk.DescriptorSetLayout{backend.vkSetLayout},
		PushConstantRanges: []vk.PushConstantRange{{StageFlags: pushStages, Size: pushConstantSize}},
	})
	fatalVk(err, "create pipeline layout")
	backend.vkPipelineLayout = layout
}

// Uploads the white pixel and black cube that occupy slot 0 of the two corresponding arrays
// and pre-fills hot slots with a default white texture
func (backend *VKBackend) createDefaultImages() {
	white := backend.CreateImage(renderer.ImageSpec{
		Name: "white", Width: 1, Height: 1, Format: renderer.FormatRGBA8,
		Usage: renderer.ImageSampled | renderer.ImageCopyDst,
	})
	backend.UpdateImage(white, renderer.ImageData{Pixels: []byte{255, 255, 255, 255}})
	backend.Slot(white)

	black := backend.CreateImage(renderer.ImageSpec{
		Name: "blackCube", Width: 1, Height: 1, Layers: 6, Kind: renderer.ImageCube,
		Format: renderer.FormatRGBA8, Usage: renderer.ImageSampled | renderer.ImageCopyDst,
	})
	backend.UpdateImage(black, renderer.ImageData{Pixels: make([]byte, 4*6), LayerCount: 6})
	backend.Slot(black)

	// Pre-fill all hot slots with a white texture
	image := backend.image(white)
	for i := uint32(0); i < maxHotTextures; i++ {
		vk.UpdateDescriptorSets(backend.vkDevice, []vk.WriteDescriptorSet{{
			DstSet: backend.vkDescriptorSet, DstBinding: bindHot, DstArrayElement: i,
			DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			ImageInfo: []vk.DescriptorImageInfo{{
				Sampler: image.vkSampler, ImageView: image.vkView, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
			}},
		}})
	}
}

// Records what the device can do and what the request actually got
func (backend *VKBackend) buildCaps(req renderer.Request) { 
	// Initialize the feature set with default Compute support
	features := renderer.Features{
		renderer.FeatureCompute: true,
	}
	// Populate the feature map with request’s feature flags, defaulting missing ones to false until we know they are supported
	for _, frame := range req.Features {
		if !features[frame] {
			features[frame] = false
		}
	}
	// Build backend capabilities from physical device limits and format support
	backend.capacities = renderer.Capacities{
		MaxAnisotropy:     backend.vkPhysDeviceProps.MaxSamplerAnisotropy,
		SampleCounts:      int(backend.vkPhysDeviceProps.FramebufferColorSampleCounts & backend.vkPhysDeviceProps.FramebufferDepthSampleCounts),
		BackbufferSamples: samplesToInt(backend.vkSamples),
		Features:          features,
		Formats: func(format renderer.Format) bool {
			props := vk.GetPhysicalDeviceFormatProperties2(backend.vkPhysDevice, backend.toVkFormat(format))
			return props.OptimalTilingFeatures&(vk.FormatFeatureSampledImage|vk.FormatFeatureColorAttachment|vk.FormatFeatureDepthStencilAttachment) != 0
		},
	}
}

func (backend *VKBackend) Capacities() renderer.Capacities { return backend.capacities }

// The size the swapchain currently is, which the caller's window-sized images
// have to match
func (backend *VKBackend) BackbufferSize() (int, int) {
	return int(backend.vkSwapExtent.Width), int(backend.vkSwapExtent.Height)
}

// Waits for the GPU to go idle, then destroys every Vulkan object the backend
// owns, in reverse creation order
func (backend *VKBackend) Shutdown() { // TODO: review
	if backend.vkDevice == 0 {
		return
	}
	_ = vk.DeviceWaitIdle(backend.vkDevice)

	// An idle GPU references none of the retire queue any more
	backend.frameCounter += framesInFlight + 1
	backend.drainRetired()

	for _, info := range backend.pipelines {
		if info.valid {
			vk.DestroyPipeline(backend.vkDevice, info.pipeline)
		}
	}
	for _, module := range backend.vkShaderModules {
		vk.DestroyShaderModule(backend.vkDevice, module)
	}
	for _, info := range backend.views {
		if info.valid {
			vk.DestroyImageView(backend.vkDevice, info.vkView)
		}
	}
	for _, info := range backend.images {
		if !info.valid {
			continue
		}
		vk.DestroyImageView(backend.vkDevice, info.vkView)
		if info.ownsImage {
			backend.vmaAllocator.VmaDestroyImage(info.vkImage, info.vmaAllocation)
		}
		if info.stagingBuffer != 0 {
			backend.vmaAllocator.VmaDestroyBuffer(info.stagingBuffer, info.stagingAlloc)
		}
	}
	for _, info := range backend.meshes {
		if info.valid {
			backend.vmaAllocator.VmaDestroyBuffer(info.vkIndexBuffer, info.vmaIndexAlloc)
		}
	}
	for _, info := range backend.buffers {
		if info.valid {
			backend.vmaAllocator.VmaDestroyBuffer(info.vkBuffer, info.vmaAlloc)
		}
	}
	for i := range backend.frames {
		info := &backend.frames[i]
		vk.DestroyFence(backend.vkDevice, info.vkFence)
		vk.DestroySemaphore(backend.vkDevice, info.vkAcquireSemaphore)
		backend.vmaAllocator.VmaDestroyBuffer(info.vkArenaBuffer, info.vmaArenaAlloc)
	}
	for _, sampler := range backend.vkSamplers {
		vk.DestroySampler(backend.vkDevice, sampler)
	}
	backend.destroySwapchain()
	vk.DestroyPipelineLayout(backend.vkDevice, backend.vkPipelineLayout)
	vk.DestroyDescriptorPool(backend.vkDevice, backend.vkDescriptorPool)
	vk.DestroyDescriptorSetLayout(backend.vkDevice, backend.vkSetLayout)
	vk.DestroyCommandPool(backend.vkDevice, backend.vkCommandPool)
	vk.VmaDestroyAllocator(backend.vmaAllocator)
	vk.DestroySurfaceKHR(backend.vkInstance, backend.vkSurface)
	vk.DestroyDevice(backend.vkDevice)
	vk.DestroyInstance(backend.vkInstance)
	backend.vkDevice = 0
}

// --- one-off submits ---------------------------------------------------------

// Records a one-off command buffer and blocks until the GPU has run it, 
// which is what the load-time upload paths use
func (backend *VKBackend) immediateSubmit(record func(commandBuffer vk.CommandBuffer)) { 
	commandBuffers, err := vk.AllocateCommandBuffers(backend.vkDevice, backend.vkCommandPool, 1)
	fatalVk(err, "allocate one-time command buffer")
	commandBuffer := commandBuffers[0]

	fatalVk(vk.BeginCommandBuffer(commandBuffer, vk.CommandBufferUsageOneTimeSubmit), "begin one-time command buffer")
	record(commandBuffer)
	fatalVk(vk.EndCommandBuffer(commandBuffer), "end one-time command buffer")

	fatalVk(vk.QueueSubmit2(backend.vkQueue, []vk.SubmitInfo2{{CommandBuffers: []vk.CommandBuffer{commandBuffer}}}, 0), "submit one-time")
	fatalVk(vk.QueueWaitIdle(backend.vkQueue), "wait one-time")
}

// Blocks until all frames except the one that is currently
// being recorded are finished: this guarantees that no GPU command is
// still reading from a buffer that the CPU is about to modify
func (backend *VKBackend) waitAllFrames() { 
	fences := make([]vk.Fence, 0, framesInFlight)
	for i := range backend.frames {
		// Skip the frame that is currently being recorded
		if backend.recording && i == backend.frameIndex {
			continue
		}
		fences = append(fences, backend.frames[i].vkFence)
	}
	if len(fences) == 0 {
		return
	}
	_ = vk.WaitForFences(backend.vkDevice, fences, true, math.MaxUint64)
}
