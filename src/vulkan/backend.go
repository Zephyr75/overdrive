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

	instance                 vk.Instance
	surface                  vk.SurfaceKHR
	physicalDevice           vk.PhysicalDevice
	device                   vk.Device
	queueFamily              uint32
	queue                    vk.Queue
	allocator                *vk.VmaAllocator
	physicalDeviceProperties vk.PhysicalDeviceProperties
	capacities               renderer.Capacities

	// swapchain and everything sized to it
	swapchainCI     vk.SwapchainCreateInfo
	swapchain       vk.SwapchainKHR
	swapFormat      vk.Format
	swapExtent      vk.Extent2D
	swapchainImages []imageInfo
	renderSems      []vk.Semaphore
	// What the backbuffer rasterises at, resolved against the device's limits
	// once at Init. The images sized to it are the caller's
	samples vk.SampleCountFlags

	// frame state
	commandPool  vk.CommandPool
	frames       [framesInFlight]frameData
	frameIndex   int
	imageIndex   uint32
	frameCounter uint64
	// True while Backend.Frame's closure runs, which is what tells an image
	// upload to stage rather than submit
	recording     bool
	boundPipeline vk.Pipeline

	// descriptors
	setLayout      vk.DescriptorSetLayout
	descriptorPool vk.DescriptorPool
	descriptorSet  vk.DescriptorSet
	pipelineLayout vk.PipelineLayout
	slotFree       [bindingKinds][]uint32
	slotNext       [bindingKinds]uint32

	// Resource tables, the handle being the index. Entry 0 is reserved in the
	// tables whose handle 0 means "none"
	images    []*imageInfo
	views     []*viewInfo
	buffers   []*bufferInfo
	meshes    []*meshInfo
	pipelines []*pipelineInfo
	samplers  []vk.Sampler
	modules   map[string]vk.ShaderModule

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
		swapFormat: vk.FormatB8G8R8A8Unorm, // no gamma encoding : already done in shader
		samples:    vk.SampleCount1Bit,     // 1 sample per pixel : MSAA off
		modules:    map[string]vk.ShaderModule{},
	}
	// Reserve handle 0 for "none"
	// Reserve handle 1 for backbuffer (redirects to swapchainImages)
	backend.images = append(backend.images, &imageInfo{binding: -1}, &imageInfo{binding: -1})
	// Reserve handle 0 for "none"
	backend.buffers = append(backend.buffers, &bufferInfo{})
	// Reserve handle 0 for "none"
	backend.meshes = append(backend.meshes, &meshInfo{})
	return backend
}

// Aborts on a failed Vulkan call
func fatal(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("vulkan: %s: %v", what, err))
	}
}

// --- lifecycle ---------------------------------------------------------------

// Brings up the whole device stack
func (backend *VKBackend) Init(window *glfw.Window, req renderer.Request) error { // TODO: review
	backend.window = window

	if err := backend.createInstance(); err != nil {
		return err
	}
	if err := backend.createSurfaceAndDevice(); err != nil {
		return err
	}

	backend.allocator = vk.VmaCreateAllocator(vk.VmaAllocatorCreateInfo{
		Flags:          vk.VmaAllocatorCreateBufferDeviceAddressBit,
		PhysicalDevice: backend.physicalDevice,
		Device:         backend.device,
		Instance:       backend.instance,
	})

	backend.samples = backend.pickSampleCount(req.Samples)

	if err := backend.createSwapchain(); err != nil {
		return err
	}

	pool, err := vk.CreateCommandPool(backend.device, backend.queueFamily, vk.CommandPoolCreateResetCommandBuffer)
	if err != nil {
		return err
	}
	backend.commandPool = pool

	if err := backend.createFrameData(); err != nil {
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
	backend.instance = inst

	// Load debug-utils on instance
	if backend.hasLabels {
		vk.LoadDebugUtils(inst)
	}
	return nil
}

// Creates the window surface and the logical device with the features the engine needs
func (backend *VKBackend) createSurfaceAndDevice() error { 
	// List all GPUs
	devices, err := vk.EnumeratePhysicalDevices(backend.instance)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Vulkan physical devices")
	}
	backend.physicalDevice = devices[0]
	backend.physicalDeviceProperties = vk.GetPhysicalDeviceProperties2(backend.physicalDevice)
	fmt.Printf("Vulkan device: %s\n", backend.physicalDeviceProperties.DeviceName)

	// Find compatible command queue
	foundQueue := false
	for i, queueFamily := range vk.GetPhysicalDeviceQueueFamilyProperties(backend.physicalDevice) {
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
	device, err := vk.CreateDevice(backend.physicalDevice, vk.DeviceCreateInfo{
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
	backend.device = device
	backend.queue = vk.GetDeviceQueue(device, backend.queueFamily, 0)

	// Get window surface
	surface, err := backend.window.CreateWindowSurface((*byte)(unsafe.Pointer(backend.instance)), nil)
	if err != nil {
		return err
	}
	backend.surface = vk.SurfaceKHR(*(*uintptr)(unsafe.Pointer(surface)))

	// Check ig queue family supports presenting to a surface
	ok, err := vk.GetPhysicalDeviceSurfaceSupportKHR(backend.physicalDevice, backend.queueFamily, backend.surface)
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
func (backend *VKBackend) createFrameData() error { // TODO: review
	cbs, err := vk.AllocateCommandBuffers(backend.device, backend.commandPool, framesInFlight)
	if err != nil {
		return err
	}
	for i := range backend.frames {
		frame := &backend.frames[i]
		frame.commandBuffer = cbs[i]
		// Signalled, so the first frame does not block on a fence no submit
		// will ever signal
		if frame.fence, err = vk.CreateFence(backend.device, vk.FenceCreateSignaled); err != nil {
			return err
		}
		if frame.acquireSem, err = vk.CreateSemaphore(backend.device); err != nil {
			return err
		}

		buf, alloc, info, err := backend.allocator.VmaCreateBuffer(
			vk.BufferCreateInfo{Size: arenaSize, Usage: vk.BufferUsageShaderDeviceAddress},
			vk.VmaAllocationCreateInfo{
				Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
				Usage: vk.VmaMemoryUsageAuto,
			})
		if err != nil {
			return err
		}
		frame.arena, frame.arenaAlloc, frame.arenaMapped = buf, alloc, info.MappedData
		frame.arenaAddr = vk.GetBufferDeviceAddress(backend.device, buf)
	}
	return nil
}

// Creates the sampler handle 0 names, which every image gets unless it asks for
// another
func (backend *VKBackend) createDefaultSampler() { // TODO: review
	aniso := float32(0)
	if settings.AnisotropyEnabled() {
		aniso = float32(settings.Anisotropy)
	}
	backend.samplers = append(backend.samplers, 0) // placeholder, replaced below
	sampler, err := vk.CreateSampler(backend.device, vk.SamplerCreateInfo{
		MagFilter: vk.FilterLinear, MinFilter: vk.FilterLinear,
		MipmapMode:       vk.SamplerMipmapModeLinear,
		AddressModeU:     vk.SamplerAddressModeRepeat,
		AddressModeV:     vk.SamplerAddressModeRepeat,
		AddressModeW:     vk.SamplerAddressModeRepeat,
		AnisotropyEnable: aniso > 1,
		MaxAnisotropy:    minF32(aniso, backend.physicalDeviceProperties.MaxSamplerAnisotropy),
		MaxLod:           16,
	})
	fatal(err, "create default sampler")
	backend.samplers[0] = sampler
}

func minF32(first, second float32) float32 { // TODO: review
	if first < second {
		return first
	}
	return second
}

func (backend *VKBackend) createGlobalPipelineLayout() { // TODO: review
	layout, err := vk.CreatePipelineLayout(backend.device, vk.PipelineLayoutCreateInfo{
		SetLayouts:         []vk.DescriptorSetLayout{backend.setLayout},
		PushConstantRanges: []vk.PushConstantRange{{StageFlags: pushStages, Size: pushConstantSize}},
	})
	fatal(err, "create pipeline layout")
	backend.pipelineLayout = layout
}

// Uploads the white pixel and black cube that occupy slot 0 of the two sampled
// arrays, which is what an unset texture falls back to, and seeds the dedicated
// descriptors so none of them is ever read unwritten
func (backend *VKBackend) createDefaultImages() { // TODO: review
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

	// Partially-bound tolerates a hole, but a draw sampling one would still read
	// undefined data
	info := backend.image(white)
	for i := uint32(0); i < maxHotTextures; i++ {
		vk.UpdateDescriptorSets(backend.device, []vk.WriteDescriptorSet{{
			DstSet: backend.descriptorSet, DstBinding: bindHot, DstArrayElement: i,
			DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			ImageInfo: []vk.DescriptorImageInfo{{
				Sampler: info.sampler, ImageView: info.view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
			}},
		}})
	}
}

// Records what the device can do and what the request actually got
func (backend *VKBackend) buildCaps(req renderer.Request) { // TODO: review
	features := renderer.Features{
		renderer.FeatureCompute: true,
	}
	for _, frame := range req.Features {
		if !features[frame] {
			features[frame] = false
		}
	}
	backend.capacities = renderer.Capacities{
		MaxAnisotropy:     backend.physicalDeviceProperties.MaxSamplerAnisotropy,
		SampleCounts:      int(backend.physicalDeviceProperties.FramebufferColorSampleCounts & backend.physicalDeviceProperties.FramebufferDepthSampleCounts),
		BackbufferSamples: samplesToInt(backend.samples),
		Features:          features,
		Formats: func(format renderer.Format) bool {
			props := vk.GetPhysicalDeviceFormatProperties2(backend.physicalDevice, backend.format(format))
			return props.OptimalTilingFeatures&(vk.FormatFeatureSampledImage|vk.FormatFeatureColorAttachment|vk.FormatFeatureDepthStencilAttachment) != 0
		},
	}
}

func (backend *VKBackend) Capacities() renderer.Capacities { return backend.capacities } 

// The size the swapchain currently is, which the caller's window-sized images
// have to match
func (backend *VKBackend) BackbufferSize() (int, int) {
	return int(backend.swapExtent.Width), int(backend.swapExtent.Height)
}

// Waits for the GPU to go idle, then destroys every Vulkan object the backend
// owns, in reverse creation order
func (backend *VKBackend) Shutdown() { // TODO: review
	if backend.device == 0 {
		return
	}
	_ = vk.DeviceWaitIdle(backend.device)

	// An idle GPU references none of the retire queue any more
	backend.frameCounter += framesInFlight + 1
	backend.drainRetired()

	for _, info := range backend.pipelines {
		if info.valid {
			vk.DestroyPipeline(backend.device, info.pipeline)
		}
	}
	for _, module := range backend.modules {
		vk.DestroyShaderModule(backend.device, module)
	}
	for _, info := range backend.views {
		if info.valid {
			vk.DestroyImageView(backend.device, info.view)
		}
	}
	for _, info := range backend.images {
		if !info.valid {
			continue
		}
		vk.DestroyImageView(backend.device, info.view)
		if info.ownsImage {
			backend.allocator.VmaDestroyImage(info.image, info.alloc)
		}
		if info.staging != 0 {
			backend.allocator.VmaDestroyBuffer(info.staging, info.stagingAlloc)
		}
	}
	for _, info := range backend.meshes {
		if info.valid {
			backend.allocator.VmaDestroyBuffer(info.indexBuffer, info.indexAlloc)
		}
	}
	for _, info := range backend.buffers {
		if info.valid {
			backend.allocator.VmaDestroyBuffer(info.buffer, info.alloc)
		}
	}
	for i := range backend.frames {
		frame := &backend.frames[i]
		vk.DestroyFence(backend.device, frame.fence)
		vk.DestroySemaphore(backend.device, frame.acquireSem)
		backend.allocator.VmaDestroyBuffer(frame.arena, frame.arenaAlloc)
	}
	for _, sampler := range backend.samplers {
		vk.DestroySampler(backend.device, sampler)
	}
	backend.destroySwapchain()
	vk.DestroyPipelineLayout(backend.device, backend.pipelineLayout)
	vk.DestroyDescriptorPool(backend.device, backend.descriptorPool)
	vk.DestroyDescriptorSetLayout(backend.device, backend.setLayout)
	vk.DestroyCommandPool(backend.device, backend.commandPool)
	vk.VmaDestroyAllocator(backend.allocator)
	vk.DestroySurfaceKHR(backend.instance, backend.surface)
	vk.DestroyDevice(backend.device)
	vk.DestroyInstance(backend.instance)
	backend.device = 0
}

// --- one-off submits ---------------------------------------------------------

// Records a one-off command buffer and blocks until the GPU has run it, which is
// what the load-time upload paths use
func (backend *VKBackend) immediateSubmit(record func(commandBuffer vk.CommandBuffer)) { // TODO: review
	cbs, err := vk.AllocateCommandBuffers(backend.device, backend.commandPool, 1)
	fatal(err, "allocate one-time command buffer")
	commandBuffer := cbs[0]

	fatal(vk.BeginCommandBuffer(commandBuffer, vk.CommandBufferUsageOneTimeSubmit), "begin one-time command buffer")
	record(commandBuffer)
	fatal(vk.EndCommandBuffer(commandBuffer), "end one-time command buffer")

	fatal(vk.QueueSubmit2(backend.queue, []vk.SubmitInfo2{{CommandBuffers: []vk.CommandBuffer{commandBuffer}}}, 0), "submit one-time")
	fatal(vk.QueueWaitIdle(backend.queue), "wait one-time")
}

// Drains the frames in flight, required before touching a resource an
// already-submitted frame might read
//
// Skips the frame being recorded: its fence was reset at the start of the frame
// and is only signalled at the end, so waiting on it from inside would deadlock
func (backend *VKBackend) waitAllFrames() { // TODO: review
	fences := make([]vk.Fence, 0, framesInFlight)
	for i := range backend.frames {
		if backend.recording && i == backend.frameIndex {
			continue
		}
		fences = append(fences, backend.frames[i].fence)
	}
	if len(fences) == 0 {
		return
	}
	_ = vk.WaitForFences(backend.device, fences, true, math.MaxUint64)
}
