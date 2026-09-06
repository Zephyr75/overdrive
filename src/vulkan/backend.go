// Package vulkan implements renderer.Backend on Vulkan 1.3, using the
// hand-written bindings in the go-vulkan repo. Every vk.* call in the engine
// lives in this package, and nothing in it names a rendering technique.
package vulkan

import (
	"fmt"
	"math"
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
	// Views 0-2 are reserved: none, the backbuffer, and its depth buffer
	firstUserView = 3

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

	instance       vk.Instance
	surface        vk.SurfaceKHR
	physicalDevice vk.PhysicalDevice
	device         vk.Device
	queueFamily    uint32
	queue          vk.Queue
	allocator      *vk.VmaAllocator
	props          vk.PhysicalDeviceProperties
	caps           renderer.Capacities

	// swapchain and everything sized to it
	swapchainCI     vk.SwapchainCreateInfo
	swapchain       vk.SwapchainKHR
	swapFormat      vk.Format
	swapExtent      vk.Extent2D
	swapchainImages []imageEntry
	renderSems      []vk.Semaphore
	// The depth buffer and, when the backend multisamples, the colour image the
	// backbuffer view resolves out of. Both are the backend's because both are
	// sized to a swapchain only it sees resize
	depth   imageEntry
	msaa    imageEntry
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
	images    []*imageEntry
	views     []*viewEntry
	buffers   []*bufEntry
	meshes    []*meshEntry
	pipelines []*pipelineEntry
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
		swapFormat: vk.FormatB8G8R8A8Unorm,
		samples:    vk.SampleCount1Bit, // Init narrows this once the device is known
		modules:    map[string]vk.ShaderModule{},
	}
	// Reserve index 0 in the tables whose handle 0 means "none", and index 1 in
	// the image table for renderer.BackbufferImage, which image() resolves to
	// whichever swapchain image this frame acquired
	backend.images = append(backend.images, &imageEntry{binding: -1}, &imageEntry{binding: -1})
	backend.buffers = append(backend.buffers, &bufEntry{})
	backend.meshes = append(backend.meshes, &meshEntry{})
	return backend
}

// Aborts on a failed Vulkan call: resource creation failing mid-run is not
// recoverable, and error plumbing at every call site would bury the code
func fatal(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("vulkan: %s: %v", what, err))
	}
}

// --- lifecycle ---------------------------------------------------------------

// Brings up the whole device stack: instance, surface, device, allocator,
// swapchain, frames, descriptors and the default textures
func (backend *VKBackend) Init(window *glfw.Window, req renderer.Request) error {
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

	// Before the swapchain, which sizes its colour and depth images to it
	backend.samples = backend.pickSampleCount()

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
	// Keep validation opt-in: the layers are a separate package on most
	// distributions and instance creation fails outright when one is missing
	var layers []string
	if settings.Validation {
		layers = append(layers, "VK_LAYER_KHRONOS_validation")
	}

	// Ask before enabling: an extension the loader does not have fails instance
	// creation outright, and this one is absent on a machine with neither the
	// validation layers nor RenderDoc installed
	extensions := backend.window.GetRequiredInstanceExtensions()
	if available, err := vk.EnumerateInstanceExtensionProperties(); err == nil {
		for _, entry := range available {
			if entry == vk.ExtDebugUtils {
				extensions = append(extensions, vk.ExtDebugUtils)
				backend.hasLabels = true
				break
			}
		}
	}

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
	// The extension's entry points are not exported by the loader, so they are
	// fetched here; without this every label call is a no-op
	vk.LoadDebugUtils(inst)
	return nil
}

// Creates the surface, picks a graphics-and-present queue family, and creates
// the logical device with the features the engine needs
func (backend *VKBackend) createSurfaceAndDevice() error {
	devices, err := vk.EnumeratePhysicalDevices(backend.instance)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Vulkan physical devices")
	}
	backend.physicalDevice = devices[0]
	backend.props = vk.GetPhysicalDeviceProperties2(backend.physicalDevice)
	fmt.Printf("Vulkan device: %s\n", backend.props.DeviceName)

	found := false
	for i, queueFamily := range vk.GetPhysicalDeviceQueueFamilyProperties(backend.physicalDevice) {
		if queueFamily.QueueFlags&vk.QueueGraphics != 0 {
			backend.queueFamily = uint32(i)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no queue family supports both graphics and present")
	}

	// DescriptorIndexing makes the bindless arrays legal, BufferDeviceAddress
	// the uniform pointers, ScalarBlockLayout the -fvk-use-scalar-layout SPIR-V,
	// the storage-image pair the compute output targets
	dev, err := vk.CreateDevice(backend.physicalDevice, vk.DeviceCreateInfo{
		QueueCreateInfos: []vk.DeviceQueueCreateInfo{
			{QueueFamilyIndex: backend.queueFamily, Priorities: []float32{1}},
		},
		Extensions: []string{"VK_KHR_swapchain"},
		Features: vk.Features{
			DescriptorIndexing:                           true,
			ShaderSampledImageArrayNonUniformIndexing:    true,
			ShaderStorageImageArrayNonUniformIndexing:    true,
			RuntimeDescriptorArray:                       true,
			BufferDeviceAddress:                          true,
			SamplerAnisotropy:                            true,
			Synchronization2:                             true,
			DynamicRendering:                             true,
			GeometryShader:                               true,
			ScalarBlockLayout:                            true,
			DescriptorBindingPartiallyBound:              true,
			DescriptorBindingSampledImageUpdateAfterBind: true,
			DescriptorBindingStorageImageUpdateAfterBind: true,
		},
	})
	if err != nil {
		return err
	}
	backend.device = dev
	backend.queue = vk.GetDeviceQueue(dev, backend.queueFamily, 0)

	surfRaw, err := backend.window.CreateWindowSurface((*byte)(unsafe.Pointer(backend.instance)), nil)
	if err != nil {
		return err
	}
	backend.surface = vk.SurfaceKHR(*(*uintptr)(unsafe.Pointer(surfRaw)))
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
func (backend *VKBackend) createFrameData() error {
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
func (backend *VKBackend) createDefaultSampler() {
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
		MaxAnisotropy:    minF32(aniso, backend.props.MaxSamplerAnisotropy),
		MaxLod:           16,
	})
	fatal(err, "create default sampler")
	backend.samplers[0] = sampler
}

func minF32(first, second float32) float32 {
	if first < second {
		return first
	}
	return second
}

func (backend *VKBackend) createGlobalPipelineLayout() {
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
func (backend *VKBackend) createDefaultImages() {
	white := backend.CreateImage(renderer.ImageInfo{
		Name: "white", Width: 1, Height: 1, Format: renderer.FormatRGBA8,
		Usage: renderer.ImageSampled | renderer.ImageCopyDst,
	})
	backend.UpdateImage(white, renderer.ImageData{Pixels: []byte{255, 255, 255, 255}})
	backend.Slot(white)

	black := backend.CreateImage(renderer.ImageInfo{
		Name: "blackCube", Width: 1, Height: 1, Layers: 6, Kind: renderer.ImageCube,
		Format: renderer.FormatRGBA8, Usage: renderer.ImageSampled | renderer.ImageCopyDst,
	})
	backend.UpdateImage(black, renderer.ImageData{Pixels: make([]byte, 4*6), LayerCount: 6})
	backend.Slot(black)

	// Partially-bound tolerates a hole, but a draw sampling one would still read
	// undefined data
	entry := backend.image(white)
	for i := uint32(0); i < maxHotTextures; i++ {
		vk.UpdateDescriptorSets(backend.device, []vk.WriteDescriptorSet{{
			DstSet: backend.descriptorSet, DstBinding: bindHot, DstArrayElement: i,
			DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			ImageInfo: []vk.DescriptorImageInfo{{
				Sampler: entry.sampler, ImageView: entry.view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
			}},
		}})
	}
}

// Records what the device can do and what the request actually got
func (backend *VKBackend) buildCaps(req renderer.Request) {
	features := renderer.Features{
		renderer.FeatureCompute: true,
	}
	for _, frame := range req.Features {
		if !features[frame] {
			features[frame] = false
		}
	}
	backend.caps = renderer.Capacities{
		MaxAnisotropy:     backend.props.MaxSamplerAnisotropy,
		SampleCounts:      int(backend.props.FramebufferColorSampleCounts & backend.props.FramebufferDepthSampleCounts),
		BackbufferSamples: samplesToInt(backend.samples),
		Features:          features,
		Formats: func(format renderer.Format) bool {
			props := vk.GetPhysicalDeviceFormatProperties2(backend.physicalDevice, backend.format(format))
			return props.OptimalTilingFeatures&(vk.FormatFeatureSampledImage|vk.FormatFeatureColorAttachment|vk.FormatFeatureDepthStencilAttachment) != 0
		},
	}
}

func (backend *VKBackend) Capacities() renderer.Capacities { return backend.caps }

// Waits for the GPU to go idle, then destroys every Vulkan object the backend
// owns, in reverse creation order
func (backend *VKBackend) Shutdown() {
	if backend.device == 0 {
		return
	}
	_ = vk.DeviceWaitIdle(backend.device)

	// An idle GPU references none of the retire queue any more
	backend.frameCounter += framesInFlight + 1
	backend.drainRetired()

	for _, entry := range backend.pipelines {
		if entry.valid {
			vk.DestroyPipeline(backend.device, entry.pipeline)
		}
	}
	for _, module := range backend.modules {
		vk.DestroyShaderModule(backend.device, module)
	}
	for _, entry := range backend.views {
		if entry.valid {
			vk.DestroyImageView(backend.device, entry.view)
		}
	}
	for _, entry := range backend.images {
		if !entry.valid {
			continue
		}
		vk.DestroyImageView(backend.device, entry.view)
		if entry.ownsImage {
			backend.allocator.VmaDestroyImage(entry.image, entry.alloc)
		}
		if entry.staging != 0 {
			backend.allocator.VmaDestroyBuffer(entry.staging, entry.stagingAlloc)
		}
	}
	for _, entry := range backend.meshes {
		if entry.valid {
			backend.allocator.VmaDestroyBuffer(entry.indexBuffer, entry.indexAlloc)
		}
	}
	for _, entry := range backend.buffers {
		if entry.valid {
			backend.allocator.VmaDestroyBuffer(entry.buffer, entry.alloc)
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
func (backend *VKBackend) immediateSubmit(record func(commandBuffer vk.CommandBuffer)) {
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
func (backend *VKBackend) waitAllFrames() {
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
