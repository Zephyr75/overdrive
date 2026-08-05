// Package vulkan implements renderer.Backend on Vulkan 1.3, using the
// hand-written bindings in the sibling go-vulkan repo. Every vk.* call in the
// engine lives in this package.
//
// Structure and conventions mirror the debugged C++ backend (cpp_deprecated/vulkan/) and
// the techniques in notes/VULKAN.md: dynamic rendering (no render-pass
// objects), buffer device address for uniforms, bindless descriptor indexing
// for material textures, synchronization2 barriers, and 2 frames in flight.
package vulkan

import (
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

const (
	framesInFlight = 2
	// Per-frame uniform ring. Each draw snapshots one Uniforms block (1312
	// bytes) into it, so this holds a few hundred draws per frame.
	ringSize = 1 << 20
	// Bindless array sizes, which must match the descriptor set layout the
	// shaders were compiled against.
	max2DTextures   = 256
	maxCubeTextures = 64

	depthFormat = vk.FormatD32Sfloat
	// Offscreen colour targets. This wants to be R16G16B16A16_SFLOAT — the
	// motivating use (tone mapping, bloom) needs values above 1.0 — but the
	// go-vulkan bindings expose no half-float format yet (notes/FEATURES.md §2).
	// Changing this one constant is the whole HDR change on this side.
	offscreenColorFormat = vk.FormatR8G8B8A8Unorm
)

// The push constant (the two uniform blocks' device addresses) is read by every
// stage: the vertex stage for matrices, geometry for the cube shadow
// matrices, fragment for materials and lights.
const pushStages = vk.ShaderStageVertex | vk.ShaderStageGeometry | vk.ShaderStageFragment

// A pipeline is built per (shader, pass kind, vertex layout). The pass kind
// decides winding, attachment formats and blending, the layout decides the
// vertex input state.
type passKind int

const (
	passMain passKind = iota
	passShadow2D
	passShadowCube
	passOffscreenColor
	passCount
)

// Pipelines are keyed by layout, so the table needs a count the enum itself
// does not carry. renderer.VertexLayout is the shared declaration.
const layoutCount = int(renderer.LayoutPositionUV) + 1

// One texture: its image, view, and the bindless slot shaders reach it through
type texEntry struct {
	cube bool
	// Index into the bindless array of its kind (binding 0 for 2D, 1 for cube).
	slot      uint32
	image     vk.Image
	alloc     vk.VmaAllocation
	view      vk.ImageView
	ownsImage bool // false for shadow-map views, whose image the shadowEntry owns
	valid     bool

	// Set only on textures the CPU rewrites every frame (the UI overlay): a
	// persistently mapped staging buffer plus the deferred-copy bookkeeping.
	staging       vk.Buffer
	stagingAlloc  vk.VmaAllocation
	stagingMapped unsafe.Pointer
	stagingSize   uint64
	width, height int
	pending       bool // staged pixels not yet copied into the image
}

// One buffer and its persistent mapping
type bufEntry struct {
	buffer vk.Buffer
	alloc  vk.VmaAllocation
	mapped unsafe.Pointer
	size   uint64
	valid  bool
}

// One drawable: a vertex buffer, an optional index buffer, and everything Draw
// needs to know about it
//
// layout and count are recorded at creation because they are intrinsic to the
// geometry. That is what lets one Draw serve meshes, the skybox and the overlay.
type meshEntry struct {
	vbo         renderer.BufferHandle
	indexBuffer vk.Buffer
	indexAlloc  vk.VmaAllocation
	layout      renderer.VertexLayout
	count       uint32 // index count when indexed, vertex count otherwise
	indexed     bool
	valid       bool
}

// One offscreen render target, holding both views of the same image
type targetEntry struct {
	format         renderer.TargetFormat
	cube           bool
	image          vk.Image
	alloc          vk.VmaAllocation
	attachmentView vk.ImageView           // 2D, or 2D_ARRAY(6) for cubes
	tex            renderer.TextureHandle // the sampled view, as a texture handle
	// Tracked so BeginPass knows which transition to record, an image having
	// no implicit "ready to render into" state the way a GL texture does.
	layout vk.ImageLayout
	valid  bool
}

// Reports the pass kind a target is rendered with, which selects the pipeline
func (t *targetEntry) pass() passKind {
	switch {
	case t.format == renderer.TargetColor:
		return passOffscreenColor
	case t.cube:
		return passShadowCube
	default:
		return passShadow2D
	}
}

// Reports the aspect its attachment and barriers refer to
func (t *targetEntry) aspect() vk.ImageAspectFlags {
	if t.format == renderer.TargetColor {
		return vk.ImageAspectColor
	}
	return vk.ImageAspectDepth
}

// Reports the number of array layers, 6 for a cube
func (t *targetEntry) layers() uint32 {
	if t.cube {
		return 6
	}
	return 1
}

// A texture's GPU objects awaiting deferred destruction
type retiredTexture struct {
	frame        uint64
	view         vk.ImageView
	image        vk.Image
	alloc        vk.VmaAllocation
	staging      vk.Buffer
	stagingAlloc vk.VmaAllocation
}

// Everything one in-flight frame owns: its command buffer, sync objects and uniform ring
type frameData struct {
	cb         vk.CommandBuffer
	fence      vk.Fence
	acquireSem vk.Semaphore
	ring       vk.Buffer
	ringAlloc  vk.VmaAllocation
	ringMapped unsafe.Pointer
	ringAddr   uint64
	ringOffset uint64
}

type VKBackend struct {
	window *glfw.Window

	instance       vk.Instance
	surface        vk.SurfaceKHR
	physicalDevice vk.PhysicalDevice
	device         vk.Device
	queueFamily    uint32
	queue          vk.Queue
	allocator      *vk.VmaAllocator

	// swapchain
	swapchainCI vk.SwapchainCreateInfo // kept for recreation on resize
	swapchain   vk.SwapchainKHR
	swapFormat  vk.Format
	swapExtent  vk.Extent2D
	swapImages  []vk.Image
	swapViews   []vk.ImageView
	renderSems  []vk.Semaphore // one per swapchain image
	depthImage  vk.Image
	depthAlloc  vk.VmaAllocation
	depthView   vk.ImageView

	// The multisampled colour image the main pass renders into, resolved into
	// the swapchain image at the end of the pass. Zero when MSAA is off, which
	// is what every "is MSAA on" test in this package checks
	msaaImage vk.Image
	msaaAlloc vk.VmaAllocation
	msaaView  vk.ImageView
	// Sample count of the main pass, decided once in Init from
	// settings.MSAASamples and the device's limits
	samples vk.SampleCountFlags

	// frame state
	commandPool vk.CommandPool
	frames      [framesInFlight]frameData
	frameIndex  int
	imageIndex  uint32
	frameActive bool

	// descriptors / pipeline layout
	setLayout      vk.DescriptorSetLayout
	descriptorPool vk.DescriptorPool
	descriptorSet  vk.DescriptorSet
	pipelineLayout vk.PipelineLayout

	// samplers
	samplerRepeat     vk.Sampler // material textures
	samplerCubeLinear vk.Sampler // skybox
	samplerShadow2D   vk.Sampler // nearest, clamp-to-border, white border
	samplerShadowCube vk.Sampler // nearest, clamp-to-edge

	// Resource tables, the handle being the index. Entry 0 is reserved in every
	// table except textures, where handle 0 is the built-in white pixel.
	textures     []texEntry
	buffers      []bufEntry
	meshes       []meshEntry
	targets      []targetEntry
	shaders      []shaderEntry
	next2DSlot   uint32
	nextCubeSlot uint32

	// Textures with staged pixels waiting to be copied at the next BeginFrame.
	pendingUploads []renderer.TextureHandle

	// Resources replaced mid-frame, waiting for the frames that reference them
	// to finish. frameCounter is the monotonic frame number they are aged against.
	retired      []retiredTexture
	frameCounter uint64

	// Which texture handles are currently mirrored into the dedicated shadow
	// descriptors (bindings 2 and 3), so they are only rewritten on change.
	shadow2DHandle   renderer.TextureHandle
	shadowCubeHandle [renderer.MaxShadowCubes]renderer.TextureHandle

	// Device address of this pass's frame block, re-pushed by every draw.
	// BindFrameUniforms writes it; the ring entry lives until the frame ends.
	frameUniformAddr uint64

	// draw-time state
	currentPass   passKind
	currentTarget renderer.RenderTargetHandle // 0 = backbuffer pass
	boundPipeline vk.Pipeline
	cullFront     bool
	depthLequal   bool
}

// Builds an empty Vulkan backend, before any Vulkan object exists
func New() *VKBackend {
	b := &VKBackend{
		swapFormat:     vk.FormatB8G8R8A8Unorm,
		samples:        vk.SampleCount1Bit, // Init narrows this once the device is known
		shadow2DHandle: invalidHandle,
	}
	for i := range b.shadowCubeHandle {
		b.shadowCubeHandle[i] = invalidHandle
	}
	// Reserve index 0 in the tables whose handle 0 means "none"
	b.buffers = append(b.buffers, bufEntry{})
	b.meshes = append(b.meshes, meshEntry{})
	b.targets = append(b.targets, targetEntry{})
	return b
}

// Marks "no texture mirrored yet" in the dedicated-binding caches, at a value
// no real handle (a table index) can collide with.
const invalidHandle = renderer.TextureHandle(math.MaxUint32)

// Aborts on a failed Vulkan call, since resource creation failing mid-run is not recoverable and error plumbing at every call site would bury the code
func fatal(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("vulkan: %s: %v", what, err))
	}
}

// --- lifecycle ---------------------------------------------------------------

// Asks GLFW for a window with no client API, Vulkan reaching it through the surface created in Init
func (b *VKBackend) ConfigureWindow() {
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
}

// Brings up the whole device stack: instance, surface, device, allocator, swapchain, frames, descriptors and default textures
func (b *VKBackend) Init(window *glfw.Window) error {
	b.window = window
	if !glfw.VulkanSupported() {
		return fmt.Errorf("GLFW reports no Vulkan loader")
	}

	if err := b.createInstance(); err != nil {
		return err
	}
	if err := b.createSurfaceAndDevice(); err != nil {
		return err
	}

	b.allocator = vk.VmaCreateAllocator(vk.VmaAllocatorCreateInfo{
		Flags:          vk.VmaAllocatorCreateBufferDeviceAddressBit,
		PhysicalDevice: b.physicalDevice,
		Device:         b.device,
		Instance:       b.instance,
	})

	// Before the swapchain, which sizes its colour and depth images to it
	b.samples = b.pickSampleCount()

	if err := b.createSwapchain(); err != nil {
		return err
	}

	pool, err := vk.CreateCommandPool(b.device, b.queueFamily, vk.CommandPoolCreateResetCommandBuffer)
	if err != nil {
		return err
	}
	b.commandPool = pool

	if err := b.createFrameData(); err != nil {
		return err
	}
	b.createSamplers()
	b.createDescriptors()
	b.createGlobalPipelineLayout()
	b.createDefaultTextures()
	return nil
}

// Creates the instance with the extensions GLFW requires, and the validation layers when OVERDRIVE_VK_VALIDATION is set
func (b *VKBackend) createInstance() error {
	// Keep validation opt-in, as the layers are a separate package on most
	// distributions and instance creation fails outright when one is missing
	var layers []string
	if os.Getenv("OVERDRIVE_VK_VALIDATION") != "" {
		layers = append(layers, "VK_LAYER_KHRONOS_validation")
	}

	inst, err := vk.CreateInstance(vk.InstanceCreateInfo{
		AppName:    "Overdrive",
		APIVersion: vk.ApiVersion13,
		Extensions: b.window.GetRequiredInstanceExtensions(),
		Layers:     layers,
	})
	if err != nil {
		return err
	}
	b.instance = inst
	return nil
}

// Creates the surface, picks a graphics-and-present queue family, and creates the logical device with the features the engine needs
func (b *VKBackend) createSurfaceAndDevice() error {
	devices, err := vk.EnumeratePhysicalDevices(b.instance)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Vulkan physical devices")
	}
	b.physicalDevice = devices[0]

	// Create the surface before the device, so present support can be verified
	// on the queue family we are about to request
	surfRaw, err := b.window.CreateWindowSurface((*byte)(unsafe.Pointer(b.instance)), nil)
	if err != nil {
		return err
	}
	b.surface = vk.SurfaceKHR(*(*uintptr)(unsafe.Pointer(surfRaw)))

	found := false
	for i, qf := range vk.GetPhysicalDeviceQueueFamilyProperties(b.physicalDevice) {
		if qf.QueueFlags&vk.QueueGraphics == 0 {
			continue
		}
		ok, err := vk.GetPhysicalDeviceSurfaceSupportKHR(b.physicalDevice, uint32(i), b.surface)
		if err != nil {
			return err
		}
		if ok {
			b.queueFamily = uint32(i)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no queue family supports both graphics and present")
	}

	name := vk.GetPhysicalDeviceProperties2(b.physicalDevice).DeviceName
	fmt.Printf("Vulkan device: %s\n", name)

	// Enable the features the engine's shaders and backend rely on.
	// ScalarBlockLayout matches the -fvk-use-scalar-layout SPIR-V,
	// GeometryShader is the point shadow pass, and the descriptor-indexing
	// group is what makes the bindless texture arrays legal
	dev, err := vk.CreateDevice(b.physicalDevice, vk.DeviceCreateInfo{
		QueueCreateInfos: []vk.DeviceQueueCreateInfo{
			{QueueFamilyIndex: b.queueFamily, Priorities: []float32{1}},
		},
		Extensions: []string{"VK_KHR_swapchain"},
		Features: vk.Features{
			SamplerAnisotropy:                            true,
			GeometryShader:                               true,
			ScalarBlockLayout:                            true,
			BufferDeviceAddress:                          true,
			DescriptorIndexing:                           true,
			RuntimeDescriptorArray:                       true,
			DescriptorBindingPartiallyBound:              true,
			DescriptorBindingVariableDescriptorCount:     true,
			ShaderSampledImageArrayNonUniformIndexing:    true,
			DescriptorBindingSampledImageUpdateAfterBind: true,
			DynamicRendering:                             true,
			Synchronization2:                             true,
		},
	})
	if err != nil {
		return err
	}
	b.device = dev
	b.queue = vk.GetDeviceQueue(dev, b.queueFamily, 0)
	return nil
}

// Allocates the per-frame command buffer, fence, semaphore and mapped uniform ring, one set per frame in flight
func (b *VKBackend) createFrameData() error {
	cbs, err := vk.AllocateCommandBuffers(b.device, b.commandPool, framesInFlight)
	if err != nil {
		return err
	}
	for i := range b.frames {
		f := &b.frames[i]
		f.cb = cbs[i]
		// Create the fence signalled, so the first frame does not block on a
		// fence no submit will ever signal
		if f.fence, err = vk.CreateFence(b.device, vk.FenceCreateSignaled); err != nil {
			return err
		}
		if f.acquireSem, err = vk.CreateSemaphore(b.device); err != nil {
			return err
		}

		buf, alloc, info, err := b.allocator.VmaCreateBuffer(
			vk.BufferCreateInfo{Size: ringSize, Usage: vk.BufferUsageShaderDeviceAddress},
			vk.VmaAllocationCreateInfo{
				Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
				Usage: vk.VmaMemoryUsageAuto,
			})
		if err != nil {
			return err
		}
		f.ring, f.ringAlloc, f.ringMapped = buf, alloc, info.MappedData
		f.ringAddr = vk.GetBufferDeviceAddress(b.device, buf)
	}
	return nil
}

// Creates the four samplers the engine binds: repeating material, clamped cube, and the two nearest-filtered shadow samplers
func (b *VKBackend) createSamplers() {
	var err error
	base := vk.SamplerCreateInfo{
		MagFilter: vk.FilterLinear, MinFilter: vk.FilterLinear,
		MipmapMode:   vk.SamplerMipmapModeNearest,
		AddressModeU: vk.SamplerAddressModeRepeat,
		AddressModeV: vk.SamplerAddressModeRepeat,
		AddressModeW: vk.SamplerAddressModeRepeat,
		MaxLod:       1,
	}
	b.samplerRepeat, err = vk.CreateSampler(b.device, base)
	fatal(err, "create repeat sampler")

	clamp := base
	clamp.AddressModeU = vk.SamplerAddressModeClampToEdge
	clamp.AddressModeV = vk.SamplerAddressModeClampToEdge
	clamp.AddressModeW = vk.SamplerAddressModeClampToEdge
	b.samplerCubeLinear, err = vk.CreateSampler(b.device, clamp)
	fatal(err, "create cube sampler")

	cubeShadow := clamp
	cubeShadow.MagFilter = vk.FilterNearest
	cubeShadow.MinFilter = vk.FilterNearest
	b.samplerShadowCube, err = vk.CreateSampler(b.device, cubeShadow)
	fatal(err, "create cube shadow sampler")

	// Give the sun's map an opaque-white border, so outside its light frustum
	// reads "fully lit" (the GL backend's TEXTURE_BORDER_COLOR)
	shadow2D := cubeShadow
	shadow2D.AddressModeU = vk.SamplerAddressModeClampToBorder
	shadow2D.AddressModeV = vk.SamplerAddressModeClampToBorder
	shadow2D.BorderColor = vk.BorderColorOpaqueWhiteFloat
	b.samplerShadow2D, err = vk.CreateSampler(b.device, shadow2D)
	fatal(err, "create 2D shadow sampler")
}

// Creates the one descriptor set the engine binds: two bindless texture arrays plus dedicated shadow-map descriptors
func (b *VKBackend) createDescriptors() {
	// Bindings 0/1 are the bindless material texture arrays. Bindings 2/3 are
	// dedicated single descriptors for the shadow maps, because the PCF kernels
	// tap them 9x/20x per fragment and sampling through a dynamically-indexed
	// bindless array makes some drivers re-fetch the descriptor per tap. The
	// layout matches common.slang.
	const bindless = vk.DescriptorBindingPartiallyBound | vk.DescriptorBindingUpdateAfterBind
	bindings := []vk.DescriptorSetLayoutBinding{
		{Binding: 0, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: max2DTextures, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
		{Binding: 1, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: maxCubeTextures, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
		{Binding: 2, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: 1, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
		{Binding: 3, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: renderer.MaxShadowCubes, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
	}

	layout, err := vk.CreateDescriptorSetLayout(b.device, vk.DescriptorSetLayoutCreateInfo{
		Flags:           vk.DescriptorSetLayoutCreateUpdateAfterBindPool,
		Bindings:        bindings,
		UseBindingFlags: true,
	})
	fatal(err, "create descriptor set layout")
	b.setLayout = layout

	total := uint32(max2DTextures + maxCubeTextures + 1 + renderer.MaxShadowCubes)
	pool, err := vk.CreateDescriptorPool(b.device, vk.DescriptorPoolCreateInfo{
		Flags:     vk.DescriptorPoolCreateUpdateAfterBind,
		MaxSets:   1,
		PoolSizes: []vk.DescriptorPoolSize{{Type: vk.DescriptorTypeCombinedImageSampler, DescriptorCount: total}},
	})
	fatal(err, "create descriptor pool")
	b.descriptorPool = pool

	sets, err := vk.AllocateDescriptorSets(b.device, vk.DescriptorSetAllocateInfo{
		Pool:    pool,
		Layouts: []vk.DescriptorSetLayout{layout},
	})
	fatal(err, "allocate descriptor set")
	b.descriptorSet = sets[0]
}

// Creates the one layout every pipeline shares: the bindless set plus an 8-byte push constant holding this draw's uniform address
func (b *VKBackend) createGlobalPipelineLayout() {
	layout, err := vk.CreatePipelineLayout(b.device, vk.PipelineLayoutCreateInfo{
		SetLayouts:         []vk.DescriptorSetLayout{b.setLayout},
		PushConstantRanges: []vk.PushConstantRange{{StageFlags: pushStages, Size: 16}},
	})
	fatal(err, "create pipeline layout")
	b.pipelineLayout = layout
}

// Uploads the white pixel and black cube that occupy slot 0 of each bindless array, and seeds the shadow descriptors with them
func (b *VKBackend) createDefaultTextures() {
	// Fill 2D slot 0 (handle 0), the white pixel the engine uses for "no texture"
	b.uploadTexture([]byte{255, 255, 255, 255}, 1, 1, 1, false, b.samplerRepeat)
	// Fill cube slot 0 with a black dummy, sampled when no cubemap was ever set
	b.uploadTexture(make([]byte, 4*6), 1, 1, 6, true, b.samplerCubeLinear)

	// Seed the dedicated shadow descriptors so they are valid before the first
	// shadow map exists. Partially-bound would tolerate holes, but every draw
	// that samples them would still read undefined data
	b.writeDedicatedTexture(2, 0, b.textures[0].view, b.samplerShadow2D)
	for i := uint32(0); i < renderer.MaxShadowCubes; i++ {
		b.writeDedicatedTexture(3, i, b.textures[1].view, b.samplerShadowCube)
	}
}

// Waits for the GPU to go idle, then destroys every Vulkan object the backend owns, in reverse creation order
func (b *VKBackend) Shutdown() {
	if b.device == 0 {
		return
	}
	// Wait first, as nothing may be destroyed while the GPU might still read it
	_ = vk.DeviceWaitIdle(b.device)

	// Age out the deferred-destruction queue, since an idle GPU references none
	// of it any more
	b.frameCounter += framesInFlight + 1
	b.drainRetired()

	for i := range b.shaders {
		s := &b.shaders[i]
		for p := range s.pipelines {
			for l := range s.pipelines[p] {
				if s.pipelines[p][l] != 0 {
					vk.DestroyPipeline(b.device, s.pipelines[p][l])
				}
			}
		}
		for _, m := range []vk.ShaderModule{s.vert, s.geo, s.frag} {
			if m != 0 {
				vk.DestroyShaderModule(b.device, m)
			}
		}
	}
	for _, e := range b.textures {
		if !e.valid {
			continue
		}
		vk.DestroyImageView(b.device, e.view)
		if e.ownsImage {
			b.allocator.VmaDestroyImage(e.image, e.alloc)
		}
		// Free the UI overlay's persistently mapped staging buffer
		if e.staging != 0 {
			b.allocator.VmaDestroyBuffer(e.staging, e.stagingAlloc)
		}
	}
	for _, e := range b.targets {
		if e.valid {
			vk.DestroyImageView(b.device, e.attachmentView)
			b.allocator.VmaDestroyImage(e.image, e.alloc)
		}
	}
	for _, e := range b.meshes {
		if e.valid {
			b.allocator.VmaDestroyBuffer(e.indexBuffer, e.indexAlloc)
		}
	}
	for _, e := range b.buffers {
		if e.valid {
			b.allocator.VmaDestroyBuffer(e.buffer, e.alloc)
		}
	}
	for i := range b.frames {
		f := &b.frames[i]
		vk.DestroyFence(b.device, f.fence)
		vk.DestroySemaphore(b.device, f.acquireSem)
		b.allocator.VmaDestroyBuffer(f.ring, f.ringAlloc)
	}
	for _, s := range []vk.Sampler{b.samplerRepeat, b.samplerCubeLinear, b.samplerShadow2D, b.samplerShadowCube} {
		vk.DestroySampler(b.device, s)
	}
	b.destroySwapchain()
	vk.DestroyPipelineLayout(b.device, b.pipelineLayout)
	vk.DestroyDescriptorPool(b.device, b.descriptorPool)
	vk.DestroyDescriptorSetLayout(b.device, b.setLayout)
	vk.DestroyCommandPool(b.device, b.commandPool)
	vk.VmaDestroyAllocator(b.allocator)
	vk.DestroySurfaceKHR(b.instance, b.surface)
	vk.DestroyDevice(b.device)
	vk.DestroyInstance(b.instance)
	b.device = 0
}

// --- frame -------------------------------------------------------------------

// Waits for this frame slot to be free, acquires a swapchain image, resets the command buffer and records the pending uploads
func (b *VKBackend) BeginFrame() {
	if b.device == 0 {
		return
	}
	f := &b.frames[b.frameIndex]
	// Throttle the CPU here, as without it frame N+2 would overwrite the ring
	// and command buffer while the GPU still reads them
	fatal(vk.WaitForFences(b.device, []vk.Fence{f.fence}, true, math.MaxUint64), "wait frame fence")

	for {
		idx, err := vk.AcquireNextImageKHR(b.device, b.swapchain, math.MaxUint64, f.acquireSem, 0)
		if err == vk.ErrOutOfDateKHR {
			b.recreateSwapchain()
			continue
		}
		if err != nil && err != vk.SuboptimalKHR {
			fmt.Fprintf(os.Stderr, "vulkan: acquire failed: %v\n", err)
		}
		b.imageIndex = idx
		break
	}

	fatal(vk.ResetFences(b.device, []vk.Fence{f.fence}), "reset frame fence")
	f.ringOffset = 0
	b.frameCounter++
	b.drainRetired()

	fatal(vk.ResetCommandBuffer(f.cb), "reset command buffer")
	fatal(vk.BeginCommandBuffer(f.cb, vk.CommandBufferUsageOneTimeSubmit), "begin command buffer")
	// Bind one descriptor set for the whole frame, only its contents changing
	vk.CmdBindDescriptorSets(f.cb, vk.PipelineBindPointGraphics, b.pipelineLayout, 0,
		[]vk.DescriptorSet{b.descriptorSet})

	// Flush anything staged during the previous frame's passes, copies being
	// legal only outside a render pass
	b.flushPendingUploads(f.cb)

	b.boundPipeline = 0
	b.frameActive = true
}

// Transitions the swapchain image to present layout, submits the frame's command buffer and presents it
func (b *VKBackend) EndFrame() {
	if !b.frameActive {
		return
	}
	f := &b.frames[b.frameIndex]

	// Transition to present layout, the explicit version of what SwapBuffers hides
	b.imageBarrier(f.cb, b.swapImages[b.imageIndex], vk.ImageAspectColor, 1,
		vk.ImageLayoutColorAttachmentOptimal, vk.ImageLayoutPresentSrcKHR,
		vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite,
		vk.PipelineStage2None, vk.Access2None)

	fatal(vk.EndCommandBuffer(f.cb), "end command buffer")

	// Wait on the frame's acquire semaphore but signal the image's render
	// semaphore, as present waits on the image's own semaphore and the two
	// index spaces are not interchangeable
	fatal(vk.QueueSubmit2(b.queue, []vk.SubmitInfo2{{
		WaitSemaphores:   []vk.SemaphoreSubmitInfo{{Semaphore: f.acquireSem, StageMask: vk.PipelineStage2ColorAttachmentOutput}},
		CommandBuffers:   []vk.CommandBuffer{f.cb},
		SignalSemaphores: []vk.SemaphoreSubmitInfo{{Semaphore: b.renderSems[b.imageIndex], StageMask: vk.PipelineStage2AllCommands}},
	}}, f.fence), "queue submit")

	if err := vk.QueuePresentKHR(b.queue, b.renderSems[b.imageIndex], b.swapchain, b.imageIndex); err != nil {
		if err == vk.ErrOutOfDateKHR || err == vk.SuboptimalKHR {
			b.recreateSwapchain()
		} else {
			fmt.Fprintf(os.Stderr, "vulkan: present failed: %v\n", err)
		}
	}

	b.frameIndex = (b.frameIndex + 1) % framesInFlight
	b.frameActive = false
}

// Transitions the target into attachment layout and begins dynamic rendering on it, with the viewport, scissor and dynamic state this pass needs
func (b *VKBackend) BeginPass(target renderer.RenderTargetHandle, w, h int, clear *[4]float32) {
	if !b.frameActive {
		return
	}
	cb := b.frames[b.frameIndex].cb

	depthAtt := vk.RenderingAttachmentInfo{
		ImageLayout: vk.ImageLayoutDepthAttachmentOptimal,
		LoadOp:      vk.AttachmentLoadOpClear,
		ClearValue:  vk.ClearDepthStencil(1, 0),
	}
	info := vk.RenderingInfo{LayerCount: 1}
	var viewport vk.Viewport
	viewport.MaxDepth = 1

	if target == 0 {
		b.imageBarrier(cb, b.swapImages[b.imageIndex], vk.ImageAspectColor, 1,
			vk.ImageLayoutUndefined, vk.ImageLayoutColorAttachmentOptimal,
			vk.PipelineStage2ColorAttachmentOutput, vk.Access2None,
			vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite)
		b.imageBarrier(cb, b.depthImage, vk.ImageAspectDepth, 1,
			vk.ImageLayoutUndefined, vk.ImageLayoutDepthAttachmentOptimal,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite)

		colorAtt := vk.RenderingAttachmentInfo{
			ImageView:   b.swapViews[b.imageIndex],
			ImageLayout: vk.ImageLayoutColorAttachmentOptimal,
			LoadOp:      vk.AttachmentLoadOpDontCare,
			StoreOp:     vk.AttachmentStoreOpStore,
		}
		if clear != nil {
			colorAtt.LoadOp = vk.AttachmentLoadOpClear
			colorAtt.ClearValue = vk.ClearColor(clear[0], clear[1], clear[2], clear[3])
		}
		// Under MSAA the pass draws into the multisampled image and resolves it
		// into the swapchain image on EndPass, so the samples themselves never
		// need storing — which is what makes the image transient
		if b.msaaView != 0 {
			colorAtt.ResolveImageView = colorAtt.ImageView
			colorAtt.ResolveImageLayout = vk.ImageLayoutColorAttachmentOptimal
			colorAtt.ResolveMode = vk.ResolveModeAverage
			colorAtt.ImageView = b.msaaView
			colorAtt.StoreOp = vk.AttachmentStoreOpDontCare

			b.imageBarrier(cb, b.msaaImage, vk.ImageAspectColor, 1,
				vk.ImageLayoutUndefined, vk.ImageLayoutColorAttachmentOptimal,
				vk.PipelineStage2ColorAttachmentOutput, vk.Access2None,
				vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite)
		}
		depthAtt.ImageView = b.depthView
		depthAtt.StoreOp = vk.AttachmentStoreOpDontCare

		info.RenderArea = vk.Rect2D{Extent: b.swapExtent}
		info.ColorAttachments = []vk.RenderingAttachmentInfo{colorAtt}

		// Flip the viewport height, turning Vulkan's y-down clip space y-up,
		// which is what the projection matrices in scene/ assume. That also
		// cancels the winding flip, so the scene's CCW front faces stay correct
		// without touching any geometry
		viewport.Y = float32(b.swapExtent.Height)
		viewport.Width = float32(b.swapExtent.Width)
		viewport.Height = -float32(b.swapExtent.Height)

		b.currentPass = passMain
		b.currentTarget = 0
	} else {
		t := &b.targets[target]
		layers := t.layers()

		info.RenderArea = vk.Rect2D{Extent: vk.Extent2D{Width: uint32(w), Height: uint32(h)}}
		info.LayerCount = layers
		b.currentPass = t.pass()
		b.currentTarget = target

		if t.format == renderer.TargetColor {
			b.imageBarrier(cb, t.image, vk.ImageAspectColor, layers,
				t.layout, vk.ImageLayoutColorAttachmentOptimal,
				vk.PipelineStage2AllCommands, vk.Access2MemoryRead|vk.Access2MemoryWrite,
				vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite)
			t.layout = vk.ImageLayoutColorAttachmentOptimal

			colorAtt := vk.RenderingAttachmentInfo{
				ImageView:   t.attachmentView,
				ImageLayout: vk.ImageLayoutColorAttachmentOptimal,
				LoadOp:      vk.AttachmentLoadOpDontCare,
				StoreOp:     vk.AttachmentStoreOpStore,
			}
			if clear != nil {
				colorAtt.LoadOp = vk.AttachmentLoadOpClear
				colorAtt.ClearValue = vk.ClearColor(clear[0], clear[1], clear[2], clear[3])
			}
			info.ColorAttachments = []vk.RenderingAttachmentInfo{colorAtt}

			// Match the backbuffer's flipped viewport, so an offscreen colour
			// pass and the main pass agree on which way is up
			viewport.Y = float32(h)
			viewport.Width = float32(w)
			viewport.Height = -float32(h)

			vk.CmdBeginRendering(cb, info)
			vk.CmdSetViewport(cb, viewport)
			vk.CmdSetScissor(cb, info.RenderArea)
			b.applyDynamicState(cb)
			return
		}

		b.imageBarrier(cb, t.image, vk.ImageAspectDepth, layers,
			t.layout, vk.ImageLayoutDepthAttachmentOptimal,
			vk.PipelineStage2AllCommands, vk.Access2MemoryRead|vk.Access2MemoryWrite,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite)
		t.layout = vk.ImageLayoutDepthAttachmentOptimal

		depthAtt.ImageView = t.attachmentView
		depthAtt.StoreOp = vk.AttachmentStoreOpStore

		// Keep the viewport positive here, unlike the main pass: the shadow map
		// is sampled as a texture rather than presented, so it wants the y-down
		// memory layout the depth comparison in the shaders expects. The cost is
		// inverted winding, which the pipeline declares as CW front faces
		viewport.Width = float32(w)
		viewport.Height = float32(h)
	}
	info.DepthAttachment = &depthAtt

	vk.CmdBeginRendering(cb, info)
	vk.CmdSetViewport(cb, viewport)
	vk.CmdSetScissor(cb, info.RenderArea)
	b.applyDynamicState(cb)
}

// Ends dynamic rendering and, for a shadow pass, transitions the depth target into shader-read layout
func (b *VKBackend) EndPass() {
	if !b.frameActive {
		return
	}
	cb := b.frames[b.frameIndex].cb
	vk.CmdEndRendering(cb)

	// Move an offscreen target to shader-read layout before a later pass
	// samples it. The swapchain image instead keeps its attachment layout until
	// EndFrame's present barrier
	if b.currentTarget != 0 {
		t := &b.targets[b.currentTarget]
		srcStage := vk.PipelineStage2LateFragmentTests
		srcAccess := vk.Access2DepthStencilAttachmentWrite
		from := vk.ImageLayoutDepthAttachmentOptimal
		if t.format == renderer.TargetColor {
			srcStage = vk.PipelineStage2ColorAttachmentOutput
			srcAccess = vk.Access2ColorAttachmentWrite
			from = vk.ImageLayoutColorAttachmentOptimal
		}
		b.imageBarrier(cb, t.image, t.aspect(), t.layers(),
			from, vk.ImageLayoutShaderReadOnlyOptimal,
			srcStage, srcAccess,
			vk.PipelineStage2FragmentShader, vk.Access2ShaderSampledRead)
		t.layout = vk.ImageLayoutShaderReadOnlyOptimal
		b.currentTarget = 0
	}
}

// --- dynamic state -----------------------------------------------------------

// Cull mode and depth compare are Vulkan 1.3 dynamic state, so these stay
// immediate calls instead of forcing a separate pipeline per combination.

// Records the cull mode, remembering it for the next pass that starts
func (b *VKBackend) SetCullFace(front bool) {
	b.cullFront = front
	if b.frameActive {
		vk.CmdSetCullMode(b.frames[b.frameIndex].cb, cullMode(front))
	}
}

// Records the depth compare op, remembering it for the next pass that starts
func (b *VKBackend) SetDepthFunc(lequal bool) {
	b.depthLequal = lequal
	if b.frameActive {
		vk.CmdSetDepthCompareOp(b.frames[b.frameIndex].cb, compareOp(lequal))
	}
}

// Re-issues the immediate state at pass start, the engine setting it between passes as often as inside them
func (b *VKBackend) applyDynamicState(cb vk.CommandBuffer) {
	vk.CmdSetCullMode(cb, cullMode(b.cullFront))
	vk.CmdSetDepthCompareOp(cb, compareOp(b.depthLequal))
}

// Translates the engine's front/back flag into a Vulkan cull mode
func cullMode(front bool) vk.CullModeFlags {
	if front {
		return vk.CullModeFront
	}
	return vk.CullModeBack
}

// Translates the engine's lequal flag into a Vulkan compare op
func compareOp(lequal bool) vk.CompareOp {
	if lequal {
		return vk.CompareOpLessOrEqual
	}
	return vk.CompareOpLess
}

// --- capabilities ------------------------------------------------------------

// Reports no optional capability, ray tracing and compute not being wired up yet (notes/FEATURES.md §3)
func (b *VKBackend) Supports(renderer.Feature) bool { return false }

// --- helpers -----------------------------------------------------------------

// Records one sync2 layout transition over every layer of an image
func (b *VKBackend) imageBarrier(cb vk.CommandBuffer, image vk.Image,
	aspect vk.ImageAspectFlags, layerCount uint32,
	from, to vk.ImageLayout,
	srcStage vk.PipelineStageFlags2, srcAccess vk.AccessFlags2,
	dstStage vk.PipelineStageFlags2, dstAccess vk.AccessFlags2) {

	vk.CmdPipelineBarrier2(cb, []vk.ImageMemoryBarrier2{{
		SrcStageMask: srcStage, SrcAccessMask: srcAccess,
		DstStageMask: dstStage, DstAccessMask: dstAccess,
		OldLayout: from, NewLayout: to,
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored, DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Image: image,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: aspect, LevelCount: 1, LayerCount: layerCount,
		},
	}})
}

// Records a one-off command buffer and blocks until the GPU has run it, used by the load-time upload paths
func (b *VKBackend) immediateSubmit(record func(cb vk.CommandBuffer)) {
	cbs, err := vk.AllocateCommandBuffers(b.device, b.commandPool, 1)
	fatal(err, "allocate one-time command buffer")
	cb := cbs[0]

	fatal(vk.BeginCommandBuffer(cb, vk.CommandBufferUsageOneTimeSubmit), "begin one-time command buffer")
	record(cb)
	fatal(vk.EndCommandBuffer(cb), "end one-time command buffer")

	fatal(vk.QueueSubmit2(b.queue, []vk.SubmitInfo2{{CommandBuffers: []vk.CommandBuffer{cb}}}, 0), "submit one-time")
	fatal(vk.QueueWaitIdle(b.queue), "wait one-time")
}

// Drains the frames in flight, required before mutating or destroying a resource an already-submitted frame might still read
//
// The frame currently being recorded is skipped. Its fence was reset in
// BeginFrame and is only signalled by EndFrame's submit, so waiting on it from
// inside the frame would deadlock. Skipping it is also correct — nothing it
// records has reached the GPU yet.
func (b *VKBackend) waitAllFrames() {
	fences := make([]vk.Fence, 0, framesInFlight)
	for i := range b.frames {
		if b.frameActive && i == b.frameIndex {
			continue
		}
		fences = append(fences, b.frames[i].fence)
	}
	if len(fences) == 0 {
		return
	}
	_ = vk.WaitForFences(b.device, fences, true, math.MaxUint64)
}
