// Package vulkan implements renderer.Backend on Vulkan 1.3, using the
// hand-written bindings in the go-vulkan repo. Every vk.* call in the
// engine lives in this package.
package vulkan

import (
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

const (
	framesInFlight = 2
	// Per-frame uniform arena: a bump allocator, reset to 0 at BeginFrame and
	// reused only because the fence wait there proves the GPU is done with it.
	// Sized by the shadow pass, not by the draw count: the bake rebinds the whole
	// 4848-byte FrameUniforms once per tile, because three of its fields describe
	// the tile being baked, so a full 337-slot atlas costs ~1.6 MiB before a
	// single scene draw. 1 MiB overflowed at 259 tiles, and an overflow restarts
	// at offset 0 over blocks the GPU still needs — silent wrong depth, one
	// warning line on stderr.
	//
	// The real fix is to split those three fields into their own small block so a
	// tile costs ~100 bytes instead of 4848; see notes/TODO.md. This is the size
	// that makes the current shape safe.
	arenaSize = 4 << 20
	// Bindless array sizes, which must match the descriptor set layout the
	// shaders were compiled against.
	max2DTextures   = 256
	maxCubeTextures = 64

	depthFormat = vk.FormatD32Sfloat
	// Wants to be R16G16B16A16_SFLOAT for HDR, but no half-float format is bound
	// yet. Changing this constant is the whole HDR change on this side
	offscreenColorFormat = vk.FormatR8G8B8A8Unorm
)

// Every stage reads the push constant: vertex for matrices, geometry for the cube shadow matrices, fragment for materials and lights
const pushStages = vk.ShaderStageVertex | vk.ShaderStageGeometry | vk.ShaderStageFragment

// A pipeline is built per (shader, pass kind, vertex layout): the pass decides winding, formats and blending, the layout decides vertex input
type passKind int

const (
	passMain passKind = iota
	passShadow2D
	passShadowCube
	passOffscreenColor
	// The depth prepass: the backbuffer's depth attachment, no colour at all
	passDepthPrepass
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

// One drawable: a vertex buffer, an optional index buffer, and everything Draw needs
//
// layout and count are recorded at creation, which is what lets one Draw serve
// meshes, the skybox and the overlay alike.
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
	width, height  int
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

// Everything one in-flight frame owns: its command buffer, sync objects and uniform arena
type frameData struct {
	cb          vk.CommandBuffer
	fence       vk.Fence
	acquireSem  vk.Semaphore
	arena       vk.Buffer
	arenaAlloc  vk.VmaAllocation
	arenaMapped unsafe.Pointer
	arenaAddr   uint64
	arenaUsed   uint64
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

	// The multisampled colour image the main pass renders into, resolved into the
	// swapchain image at EndPass. Zero when MSAA is off, which is the "is MSAA on" test
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
	shadowStaticHandle  renderer.TextureHandle
	shadowDynamicHandle renderer.TextureHandle

	// Device address of this pass's frame block, re-pushed by every draw.
	// BindFrameUniforms writes it; the arena entry lives until the frame ends.
	frameUniformAddr uint64
	// Device address of this frame's shadow record array, likewise re-pushed by
	// every draw. Frame-scoped, so BeginFrame seeds it with one empty record and
	// no draw can push a null pointer.
	recordAddr uint64

	// draw-time state
	currentPass   passKind
	currentTarget renderer.RenderTargetHandle // 0 = backbuffer pass
	// True between BeginPass and EndPass. currentTarget cannot stand in for it,
	// 0 meaning both "backbuffer pass" and "no pass"; the copy path needs to tell
	// those apart, a copy being illegal inside dynamic rendering
	passActive    bool
	boundPipeline vk.Pipeline
	cullMode      renderer.CullMode
	depthCompare  renderer.CompareOp
	// What the backbuffer's depth image holds, so a second pass on it can load
	// rather than discard. Reset every frame, the image not surviving one
	depthLayout vk.ImageLayout
	// The shader set BindShader selected, used by every following Draw
	boundShader renderer.ShaderHandle
}

// Builds an empty Vulkan backend, before any Vulkan object exists
func New() *VKBackend {
	b := &VKBackend{
		swapFormat:          vk.FormatB8G8R8A8Unorm,
		samples:             vk.SampleCount1Bit, // Init narrows this once the device is known
		shadowStaticHandle:  invalidHandle,
		shadowDynamicHandle: invalidHandle,
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

// Brings up the whole device stack: instance, surface, device, allocator, swapchain, frames, descriptors and default textures
func (backend *VKBackend) Init(window *glfw.Window) error {
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
	backend.createSamplers()
	backend.createDescriptors()
	backend.createGlobalPipelineLayout()
	backend.createDefaultTextures()
	return nil
}

// Creates the instance with the extensions GLFW requires, and the validation layers when [debug] validation is set
func (backend *VKBackend) createInstance() error {
	// Keep validation opt-in, as the layers are a separate package on most
	// distributions and instance creation fails outright when one is missing
	var layers []string
	if settings.Validation {
		layers = append(layers, "VK_LAYER_KHRONOS_validation")
	}

	inst, err := vk.CreateInstance(vk.InstanceCreateInfo{
		AppName:    "Overdrive", // TODO: use parameter
		APIVersion: vk.ApiVersion13,
		Extensions: backend.window.GetRequiredInstanceExtensions(),
		Layers:     layers,
	})
	if err != nil { // TODO: replace with chk
		return err
	}
	backend.instance = inst
	return nil
}

// Creates the surface, picks a graphics-and-present queue family, and creates the logical device with the features the engine needs
func (backend *VKBackend) createSurfaceAndDevice() error {
	devices, err := vk.EnumeratePhysicalDevices(backend.instance)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Vulkan physical devices")
	}
	backend.physicalDevice = devices[0]
	name := vk.GetPhysicalDeviceProperties2(backend.physicalDevice).DeviceName
	fmt.Printf("Vulkan device: %s\n", name)

	found := false
	for i, qf := range vk.GetPhysicalDeviceQueueFamilyProperties(backend.physicalDevice) {
		if qf.QueueFlags&vk.QueueGraphics != 0 {
			backend.queueFamily = uint32(i)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no queue family supports both graphics and present")
	}

	// DescriptorIndexing makes the bindless arrays legal, BufferDeviceAddress the
	// uniform pointers, GeometryShader the point shadow pass, ScalarBlockLayout
	// the -fvk-use-scalar-layout SPIR-V
	dev, err := vk.CreateDevice(backend.physicalDevice, vk.DeviceCreateInfo{
		QueueCreateInfos: []vk.DeviceQueueCreateInfo{
			{QueueFamilyIndex: backend.queueFamily, Priorities: []float32{1}},
		},
		Extensions: []string{"VK_KHR_swapchain"},
		Features: vk.Features{
			DescriptorIndexing:                        true,
			ShaderSampledImageArrayNonUniformIndexing: true,
			RuntimeDescriptorArray:                    true,
			BufferDeviceAddress:                       true,
			SamplerAnisotropy:                         true,
			Synchronization2:                          true,
			DynamicRendering:                          true,
			// Added on top of reference
			GeometryShader:                               true,
			ScalarBlockLayout:                            true,
			DescriptorBindingPartiallyBound:              true,
			DescriptorBindingSampledImageUpdateAfterBind: true,
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

// Allocates the per-frame command buffer, fence, semaphore and mapped uniform arena, one set per frame in flight
func (backend *VKBackend) createFrameData() error {
	cbs, err := vk.AllocateCommandBuffers(backend.device, backend.commandPool, framesInFlight)
	if err != nil {
		return err
	}
	for i := range backend.frames {
		f := &backend.frames[i]
		f.cb = cbs[i]
		// Create the fence signalled, so the first frame does not block on a
		// fence no submit will ever signal
		if f.fence, err = vk.CreateFence(backend.device, vk.FenceCreateSignaled); err != nil {
			return err
		}
		if f.acquireSem, err = vk.CreateSemaphore(backend.device); err != nil {
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
		f.arena, f.arenaAlloc, f.arenaMapped = buf, alloc, info.MappedData
		f.arenaAddr = vk.GetBufferDeviceAddress(backend.device, buf)
	}
	return nil
}

// Creates the four samplers the engine binds: repeating material, clamped cube, and the two nearest-filtered shadow samplers
func (backend *VKBackend) createSamplers() {
	var err error
	base := vk.SamplerCreateInfo{
		MagFilter: vk.FilterLinear, MinFilter: vk.FilterLinear,
		// Linear rather than nearest between mip levels. Inert while MaxLod is 1
		// and nothing generates mips, but the right default for when they do
		MipmapMode:   vk.SamplerMipmapModeLinear,
		AddressModeU: vk.SamplerAddressModeRepeat,
		AddressModeV: vk.SamplerAddressModeRepeat,
		AddressModeW: vk.SamplerAddressModeRepeat,
		MaxLod:       1,
	}

	// Material sampler only: a skybox is never viewed at a grazing angle, and the
	// shadow samplers filter NEAREST, where anisotropy means nothing
	material := base
	if settings.AnisotropyEnabled() {
		material.AnisotropyEnable = true
		material.MaxAnisotropy = float32(settings.Anisotropy)
		// Lower a request the device cannot meet rather than failing: the config
		// file is written once, the GPU it runs on is not
		if limit := vk.GetPhysicalDeviceProperties2(backend.physicalDevice).MaxSamplerAnisotropy; limit < material.MaxAnisotropy {
			material.MaxAnisotropy = limit
		}
	}
	backend.samplerRepeat, err = vk.CreateSampler(backend.device, material)
	fatal(err, "create repeat sampler")

	// Derived from base, not material, so none of these inherit the anisotropy
	clamp := base
	clamp.AddressModeU = vk.SamplerAddressModeClampToEdge
	clamp.AddressModeV = vk.SamplerAddressModeClampToEdge
	clamp.AddressModeW = vk.SamplerAddressModeClampToEdge
	backend.samplerCubeLinear, err = vk.CreateSampler(backend.device, clamp)
	fatal(err, "create cube sampler")

	cubeShadow := clamp
	cubeShadow.MagFilter = vk.FilterNearest
	cubeShadow.MinFilter = vk.FilterNearest
	backend.samplerShadowCube, err = vk.CreateSampler(backend.device, cubeShadow)
	fatal(err, "create cube shadow sampler")

	// Give the sun's map an opaque-white border, so outside its light frustum
	// reads "fully lit"
	shadow2D := cubeShadow
	shadow2D.AddressModeU = vk.SamplerAddressModeClampToBorder
	shadow2D.AddressModeV = vk.SamplerAddressModeClampToBorder
	shadow2D.BorderColor = vk.BorderColorOpaqueWhiteFloat
	backend.samplerShadow2D, err = vk.CreateSampler(backend.device, shadow2D)
	fatal(err, "create 2D shadow sampler")
}

// Creates the one descriptor set the engine binds: two bindless texture arrays plus dedicated shadow-map descriptors
func (backend *VKBackend) createDescriptors() {
	// 0/1 bindless material arrays, 2/3 the static and dynamic shadow atlases.
	// Dedicated because PCF taps them 9x per fragment and some drivers re-fetch a
	// dynamically-indexed descriptor per tap — measured at ~1.7x frame time.
	// Matches common.slang
	const bindless = vk.DescriptorBindingPartiallyBound | vk.DescriptorBindingUpdateAfterBind
	bindings := []vk.DescriptorSetLayoutBinding{
		{Binding: 0, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: max2DTextures, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
		{Binding: 1, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: maxCubeTextures, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
		{Binding: 2, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: 1, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
		{Binding: 3, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: 1, StageFlags: vk.ShaderStageFragment, BindingFlags: bindless},
	}

	layout, err := vk.CreateDescriptorSetLayout(backend.device, vk.DescriptorSetLayoutCreateInfo{
		Flags:           vk.DescriptorSetLayoutCreateUpdateAfterBindPool,
		Bindings:        bindings,
		UseBindingFlags: true,
	})
	fatal(err, "create descriptor set layout")
	backend.setLayout = layout

	total := uint32(max2DTextures + maxCubeTextures + 2)
	pool, err := vk.CreateDescriptorPool(backend.device, vk.DescriptorPoolCreateInfo{
		Flags:     vk.DescriptorPoolCreateUpdateAfterBind,
		MaxSets:   1,
		PoolSizes: []vk.DescriptorPoolSize{{Type: vk.DescriptorTypeCombinedImageSampler, DescriptorCount: total}},
	})
	fatal(err, "create descriptor pool")
	backend.descriptorPool = pool

	sets, err := vk.AllocateDescriptorSets(backend.device, vk.DescriptorSetAllocateInfo{
		Pool:    pool,
		Layouts: []vk.DescriptorSetLayout{layout},
	})
	fatal(err, "allocate descriptor set")
	backend.descriptorSet = sets[0]
}

func (backend *VKBackend) createGlobalPipelineLayout() {
	layout, err := vk.CreatePipelineLayout(backend.device, vk.PipelineLayoutCreateInfo{
		SetLayouts:         []vk.DescriptorSetLayout{backend.setLayout},
		PushConstantRanges: []vk.PushConstantRange{{StageFlags: pushStages, Size: pushConstantSize}},
	})
	fatal(err, "create pipeline layout")
	backend.pipelineLayout = layout
}

// Uploads the white pixel and black cube that occupy slot 0 of each bindless array, and seeds the shadow descriptors with them
func (backend *VKBackend) createDefaultTextures() {
	// Fill 2D slot 0 (handle 0), the white pixel the engine uses for "no texture"
	backend.uploadTexture([]byte{255, 255, 255, 255}, 1, 1, 1, false, backend.samplerRepeat)
	// Fill cube slot 0 with a black dummy, sampled when no cubemap was ever set
	backend.uploadTexture(make([]byte, 4*6), 1, 1, 6, true, backend.samplerCubeLinear)

	// Seed both atlas descriptors with the white pixel: partially-bound tolerates
	// holes, but a draw sampling one would still read undefined data. Both are 2D
	// now that a cube face is an ordinary atlas tile
	backend.writeDedicatedTexture(2, 0, backend.textures[0].view, backend.samplerShadow2D)
	backend.writeDedicatedTexture(3, 0, backend.textures[0].view, backend.samplerShadow2D)
}

// Waits for the GPU to go idle, then destroys every Vulkan object the backend owns, in reverse creation order
func (backend *VKBackend) Shutdown() {
	if backend.device == 0 {
		return
	}
	// Wait first, as nothing may be destroyed while the GPU might still read it
	_ = vk.DeviceWaitIdle(backend.device)

	// Age out the deferred-destruction queue, since an idle GPU references none
	// of it any more
	backend.frameCounter += framesInFlight + 1
	backend.drainRetired()

	for i := range backend.shaders {
		s := &backend.shaders[i]
		for p := range s.pipelines {
			for l := range s.pipelines[p] {
				if s.pipelines[p][l] != 0 {
					vk.DestroyPipeline(backend.device, s.pipelines[p][l])
				}
			}
		}
		for _, m := range []vk.ShaderModule{s.vert, s.geo, s.frag} {
			if m != 0 {
				vk.DestroyShaderModule(backend.device, m)
			}
		}
	}
	for _, e := range backend.textures {
		if !e.valid {
			continue
		}
		vk.DestroyImageView(backend.device, e.view)
		if e.ownsImage {
			backend.allocator.VmaDestroyImage(e.image, e.alloc)
		}
		// Free the UI overlay's persistently mapped staging buffer
		if e.staging != 0 {
			backend.allocator.VmaDestroyBuffer(e.staging, e.stagingAlloc)
		}
	}
	for _, e := range backend.targets {
		if e.valid {
			vk.DestroyImageView(backend.device, e.attachmentView)
			backend.allocator.VmaDestroyImage(e.image, e.alloc)
		}
	}
	for _, e := range backend.meshes {
		if e.valid {
			backend.allocator.VmaDestroyBuffer(e.indexBuffer, e.indexAlloc)
		}
	}
	for _, e := range backend.buffers {
		if e.valid {
			backend.allocator.VmaDestroyBuffer(e.buffer, e.alloc)
		}
	}
	for i := range backend.frames {
		f := &backend.frames[i]
		vk.DestroyFence(backend.device, f.fence)
		vk.DestroySemaphore(backend.device, f.acquireSem)
		backend.allocator.VmaDestroyBuffer(f.arena, f.arenaAlloc)
	}
	for _, s := range []vk.Sampler{backend.samplerRepeat, backend.samplerCubeLinear, backend.samplerShadow2D, backend.samplerShadowCube} {
		vk.DestroySampler(backend.device, s)
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

// --- frame -------------------------------------------------------------------

// Waits for this frame slot to be free, acquires a swapchain image, resets the command buffer and records the pending uploads
func (backend *VKBackend) BeginFrame() {
	if backend.device == 0 {
		return
	}
	frame := &backend.frames[backend.frameIndex]
	// The depth image is not preserved between frames, so every frame's first
	// pass on it discards rather than loads
	backend.depthLayout = vk.ImageLayoutUndefined

	// Throttle the CPU here, as without it frame N+2 would overwrite the arena
	// and command buffer while the GPU still reads them
	fatal(vk.WaitForFences(backend.device, []vk.Fence{frame.fence}, true, math.MaxUint64), "wait frame fence")

	for {
		idx, err := vk.AcquireNextImageKHR(backend.device, backend.swapchain, math.MaxUint64, frame.acquireSem, 0)
		if err == vk.ErrOutOfDateKHR {
			backend.recreateSwapchain()
			continue
		}
		if err != nil && err != vk.SuboptimalKHR {
			fmt.Fprintf(os.Stderr, "vulkan: acquire failed: %v\n", err)
		}
		backend.imageIndex = idx
		break
	}

	fatal(vk.ResetFences(backend.device, []vk.Fence{frame.fence}), "reset frame fence")
	frame.arenaUsed = 0
	backend.frameCounter++
	backend.drainRetired()

	fatal(vk.ResetCommandBuffer(frame.cb), "reset command buffer")
	fatal(vk.BeginCommandBuffer(frame.cb, vk.CommandBufferUsageOneTimeSubmit), "begin command buffer")
	// Bind one descriptor set for the whole frame, only its contents changing
	vk.CmdBindDescriptorSets(frame.cb, vk.PipelineBindPointGraphics, backend.pipelineLayout, 0,
		[]vk.DescriptorSet{backend.descriptorSet})

	// Flush anything staged during the previous frame's passes, copies being
	// legal only outside a render pass
	backend.flushPendingUploads(frame.cb)

	// One empty record at the head of the arena, so a draw pushes a valid pointer
	// even in a frame where nothing called BindShadowRecords. The shader only
	// indexes it through a non-negative LightData.ShadowIndex, which no light has
	// in that case, but the address itself is dereferenced by the pipeline setup
	backend.recordAddr = writeArena(backend, renderer.ShadowTile{})

	backend.boundPipeline = 0
	backend.frameActive = true
}

// Transitions the swapchain image to present layout, submits the frame's command buffer and presents it
func (backend *VKBackend) EndFrame() {
	if !backend.frameActive {
		return
	}
	f := &backend.frames[backend.frameIndex]

	// Transition to present layout, the explicit version of what SwapBuffers hides
	backend.imageBarrier(f.cb, backend.swapImages[backend.imageIndex], vk.ImageAspectColor, 1,
		vk.ImageLayoutColorAttachmentOptimal, vk.ImageLayoutPresentSrcKHR,
		vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite,
		vk.PipelineStage2None, vk.Access2None)

	fatal(vk.EndCommandBuffer(f.cb), "end command buffer")

	// Wait on the frame's semaphore, signal the image's: present waits on the
	// image's own, and the two index spaces are not interchangeable
	fatal(vk.QueueSubmit2(backend.queue, []vk.SubmitInfo2{{
		WaitSemaphores:   []vk.SemaphoreSubmitInfo{{Semaphore: f.acquireSem, StageMask: vk.PipelineStage2ColorAttachmentOutput}},
		CommandBuffers:   []vk.CommandBuffer{f.cb},
		SignalSemaphores: []vk.SemaphoreSubmitInfo{{Semaphore: backend.renderSems[backend.imageIndex], StageMask: vk.PipelineStage2AllCommands}},
	}}, f.fence), "queue submit")

	if err := vk.QueuePresentKHR(backend.queue, backend.renderSems[backend.imageIndex], backend.swapchain, backend.imageIndex); err != nil {
		if err == vk.ErrOutOfDateKHR || err == vk.SuboptimalKHR {
			backend.recreateSwapchain()
		} else {
			fmt.Fprintf(os.Stderr, "vulkan: present failed: %v\n", err)
		}
	}

	backend.frameIndex = (backend.frameIndex + 1) % framesInFlight
	backend.frameActive = false
}

// Transitions the target into attachment layout and begins dynamic rendering on it, with the viewport, scissor and dynamic state this pass needs
// Begins a depth-only pass on the backbuffer's depth attachment
//
// No colour attachment at all, so nothing is shaded, nothing is blended and —
// under MSAA — nothing is resolved; the prepass costs a geometry pass and a
// depth write, not a second pass over the framebuffer. StoreOp is Store because
// the whole point is that the main pass loads what this leaves.
func (backend *VKBackend) BeginDepthPrepass() {
	if !backend.frameActive {
		return
	}
	cb := backend.frames[backend.frameIndex].cb
	backend.passActive = true
	backend.currentPass = passDepthPrepass
	backend.currentTarget = 0

	backend.imageBarrier(cb, backend.depthImage, vk.ImageAspectDepth, 1,
		backend.depthLayout, vk.ImageLayoutDepthAttachmentOptimal,
		vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite,
		vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite)
	backend.depthLayout = vk.ImageLayoutDepthAttachmentOptimal

	info := vk.RenderingInfo{
		LayerCount: 1,
		RenderArea: vk.Rect2D{Extent: backend.swapExtent},
		DepthAttachment: &vk.RenderingAttachmentInfo{
			ImageView:   backend.depthView,
			ImageLayout: vk.ImageLayoutDepthAttachmentOptimal,
			LoadOp:      vk.AttachmentLoadOpClear,
			StoreOp:     vk.AttachmentStoreOpStore,
			ClearValue:  vk.ClearDepthStencil(1, 0),
		},
	}
	viewport := backend.viewportFor(passDepthPrepass, 0, 0,
		int(backend.swapExtent.Width), int(backend.swapExtent.Height))

	vk.CmdBeginRendering(cb, info)
	vk.CmdSetViewport(cb, viewport)
	vk.CmdSetScissor(cb, info.RenderArea)
	backend.applyDynamicState(cb)
}

func (backend *VKBackend) BeginPass(target renderer.RenderTargetHandle, clear *[4]float32, keepDepth bool) {
	// The target knows its own extent, so a pass cannot be given one that
	// disagrees with its attachments
	w, h := int(backend.swapExtent.Width), int(backend.swapExtent.Height)
	if target != 0 {
		if t := &backend.targets[target]; t.valid {
			w, h = t.width, t.height
		}
	}
	if !backend.frameActive {
		return
	}
	cb := backend.frames[backend.frameIndex].cb
	backend.passActive = true

	depthAtt := vk.RenderingAttachmentInfo{
		ImageLayout: vk.ImageLayoutDepthAttachmentOptimal,
		LoadOp:      vk.AttachmentLoadOpClear,
		ClearValue:  vk.ClearDepthStencil(1, 0),
	}
	if keepDepth {
		depthAtt.LoadOp = vk.AttachmentLoadOpLoad
		depthAtt.ClearValue = vk.ClearValue{}
	}
	info := vk.RenderingInfo{LayerCount: 1}
	var viewport vk.Viewport

	if target == 0 {
		backend.imageBarrier(cb, backend.swapImages[backend.imageIndex], vk.ImageAspectColor, 1,
			vk.ImageLayoutUndefined, vk.ImageLayoutColorAttachmentOptimal,
			vk.PipelineStage2ColorAttachmentOutput, vk.Access2None,
			vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite)
		// From whatever the prepass left, not from Undefined: Undefined is a
		// discard, and keepDepth on the backbuffer exists to read it back
		backend.imageBarrier(cb, backend.depthImage, vk.ImageAspectDepth, 1,
			backend.depthLayout, vk.ImageLayoutDepthAttachmentOptimal,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite)
		backend.depthLayout = vk.ImageLayoutDepthAttachmentOptimal

		colorAtt := vk.RenderingAttachmentInfo{
			ImageView:   backend.swapViews[backend.imageIndex],
			ImageLayout: vk.ImageLayoutColorAttachmentOptimal,
			LoadOp:      vk.AttachmentLoadOpDontCare,
			StoreOp:     vk.AttachmentStoreOpStore,
		}
		if clear != nil {
			colorAtt.LoadOp = vk.AttachmentLoadOpClear
			colorAtt.ClearValue = vk.ClearColor(clear[0], clear[1], clear[2], clear[3])
		}
		// The pass draws into the multisampled image and resolves into the
		// swapchain image, so the samples themselves never need storing
		if backend.msaaView != 0 {
			colorAtt.ResolveImageView = colorAtt.ImageView
			colorAtt.ResolveImageLayout = vk.ImageLayoutColorAttachmentOptimal
			colorAtt.ResolveMode = vk.ResolveModeAverage
			colorAtt.ImageView = backend.msaaView
			colorAtt.StoreOp = vk.AttachmentStoreOpDontCare

			backend.imageBarrier(cb, backend.msaaImage, vk.ImageAspectColor, 1,
				vk.ImageLayoutUndefined, vk.ImageLayoutColorAttachmentOptimal,
				vk.PipelineStage2ColorAttachmentOutput, vk.Access2None,
				vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite)
		}
		depthAtt.ImageView = backend.depthView
		depthAtt.StoreOp = vk.AttachmentStoreOpDontCare

		info.RenderArea = vk.Rect2D{Extent: backend.swapExtent}
		info.ColorAttachments = []vk.RenderingAttachmentInfo{colorAtt}

		viewport = backend.viewportFor(passMain, 0, 0, w, h)

		backend.currentPass = passMain
		backend.currentTarget = 0
	} else {
		t := &backend.targets[target]
		layers := t.layers()

		info.RenderArea = vk.Rect2D{Extent: vk.Extent2D{Width: uint32(w), Height: uint32(h)}}
		info.LayerCount = layers
		backend.currentPass = t.pass()
		backend.currentTarget = target

		if t.format == renderer.TargetColor {
			backend.imageBarrier(cb, t.image, vk.ImageAspectColor, layers,
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

			viewport = backend.viewportFor(passOffscreenColor, 0, 0, w, h)

			vk.CmdBeginRendering(cb, info)
			vk.CmdSetViewport(cb, viewport)
			vk.CmdSetScissor(cb, info.RenderArea)
			backend.applyDynamicState(cb)
			return
		}

		backend.imageBarrier(cb, t.image, vk.ImageAspectDepth, layers,
			t.layout, vk.ImageLayoutDepthAttachmentOptimal,
			vk.PipelineStage2AllCommands, vk.Access2MemoryRead|vk.Access2MemoryWrite,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite)
		t.layout = vk.ImageLayoutDepthAttachmentOptimal

		depthAtt.ImageView = t.attachmentView
		depthAtt.StoreOp = vk.AttachmentStoreOpStore

		viewport = backend.viewportFor(backend.currentPass, 0, 0, w, h)
	}
	info.DepthAttachment = &depthAtt

	vk.CmdBeginRendering(cb, info)
	vk.CmdSetViewport(cb, viewport)
	vk.CmdSetScissor(cb, info.RenderArea)
	backend.applyDynamicState(cb)
}

// Ends dynamic rendering and, for a shadow pass, transitions the depth target into shader-read layout
func (backend *VKBackend) EndPass() {
	if !backend.frameActive {
		return
	}
	cb := backend.frames[backend.frameIndex].cb
	vk.CmdEndRendering(cb)
	backend.passActive = false

	// Offscreen targets move to shader-read before a later pass samples them; the
	// swapchain image keeps its attachment layout until EndFrame
	if backend.currentTarget != 0 {
		t := &backend.targets[backend.currentTarget]
		srcStage := vk.PipelineStage2LateFragmentTests
		srcAccess := vk.Access2DepthStencilAttachmentWrite
		from := vk.ImageLayoutDepthAttachmentOptimal
		if t.format == renderer.TargetColor {
			srcStage = vk.PipelineStage2ColorAttachmentOutput
			srcAccess = vk.Access2ColorAttachmentWrite
			from = vk.ImageLayoutColorAttachmentOptimal
		}
		backend.imageBarrier(cb, t.image, t.aspect(), t.layers(),
			from, vk.ImageLayoutShaderReadOnlyOptimal,
			srcStage, srcAccess,
			vk.PipelineStage2FragmentShader, vk.Access2ShaderSampledRead)
		t.layout = vk.ImageLayoutShaderReadOnlyOptimal
		backend.currentTarget = 0
	}
}

// --- viewport and transfers --------------------------------------------------

// Builds the viewport covering a rect of a pass's target, in target texels from the top left
func (backend *VKBackend) viewportFor(pass passKind, x, y, w, h int) vk.Viewport {
	vp := vk.Viewport{
		X: float32(x), Y: float32(y),
		Width: float32(w), Height: float32(h),
		MaxDepth: 1,
	}
	if pass != passShadow2D && pass != passShadowCube {
		vp.Y = float32(y + h)
		vp.Height = -float32(h)
	}
	return vp
}

// Narrows the viewport and scissor to one rect of the current pass's target
func (backend *VKBackend) SetViewportScissor(x, y, w, h int) {
	if !backend.frameActive || !backend.passActive {
		fmt.Fprintln(os.Stderr, "vulkan: SetViewportScissor outside a pass, ignored")
		return
	}
	cb := backend.frames[backend.frameIndex].cb
	vk.CmdSetViewport(cb, backend.viewportFor(backend.currentPass, x, y, w, h))
	vk.CmdSetScissor(cb, vk.Rect2D{
		Offset: vk.Offset2D{X: int32(x), Y: int32(y)},
		Extent: vk.Extent2D{Width: uint32(w), Height: uint32(h)},
	})
}

// Copies a depth rect between two targets, both of which end up back in shader-read layout
//
// Records into this frame's command buffer, so it is ordered against the passes
// around it. The two round trips through TRANSFER_SRC/DST are why it returns
// both images to ShaderReadOnlyOptimal rather than leaving them in a transfer
// layout: the source atlas stays sampleable, and the destination's next
// BeginPass barrier starts from a layout it can name.
func (backend *VKBackend) CopyDepthRegion(src, dst renderer.RenderTargetHandle, srcX, srcY, dstX, dstY, w, h int) {
	if !backend.frameActive {
		return
	}
	// A copy inside CmdBeginRendering is invalid, and the attachment it would
	// write is the one being rendered into
	if backend.passActive {
		fmt.Fprintln(os.Stderr, "vulkan: CopyDepthRegion inside a pass, ignored")
		return
	}
	s, d := backend.target(src), backend.target(dst)
	if s == nil || d == nil || s == d {
		fmt.Fprintln(os.Stderr, "vulkan: CopyDepthRegion with an invalid target, ignored")
		return
	}
	if s.format != renderer.TargetDepth || d.format != renderer.TargetDepth {
		fmt.Fprintln(os.Stderr, "vulkan: CopyDepthRegion on a colour target, ignored")
		return
	}
	// Out of bounds is a validation error and a device loss, not a clipped copy
	if w <= 0 || h <= 0 ||
		srcX < 0 || srcY < 0 || srcX+w > s.width || srcY+h > s.height ||
		dstX < 0 || dstY < 0 || dstX+w > d.width || dstY+h > d.height {
		fmt.Fprintf(os.Stderr, "vulkan: CopyDepthRegion %dx%d out of bounds, ignored\n", w, h)
		return
	}

	cb := backend.frames[backend.frameIndex].cb
	// Every stage/access on the source side, since the layout it is coming from
	// depends on whether it was baked this frame or is a cached one from an
	// earlier frame
	backend.imageBarrier(cb, s.image, vk.ImageAspectDepth, s.layers(),
		s.layout, vk.ImageLayoutTransferSrcOptimal,
		vk.PipelineStage2AllCommands, vk.Access2MemoryRead|vk.Access2MemoryWrite,
		vk.PipelineStage2Copy, vk.Access2TransferRead)
	backend.imageBarrier(cb, d.image, vk.ImageAspectDepth, d.layers(),
		d.layout, vk.ImageLayoutTransferDstOptimal,
		vk.PipelineStage2AllCommands, vk.Access2MemoryRead|vk.Access2MemoryWrite,
		vk.PipelineStage2Copy, vk.Access2TransferWrite)

	vk.CmdCopyImage(cb,
		s.image, vk.ImageLayoutTransferSrcOptimal,
		d.image, vk.ImageLayoutTransferDstOptimal,
		[]vk.ImageCopy{{
			AspectMask: vk.ImageAspectDepth,
			SrcOffset:  vk.Offset2D{X: int32(srcX), Y: int32(srcY)},
			DstOffset:  vk.Offset2D{X: int32(dstX), Y: int32(dstY)},
			Extent:     vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		}})

	backend.imageBarrier(cb, s.image, vk.ImageAspectDepth, s.layers(),
		vk.ImageLayoutTransferSrcOptimal, vk.ImageLayoutShaderReadOnlyOptimal,
		vk.PipelineStage2Copy, vk.Access2TransferRead,
		vk.PipelineStage2FragmentShader, vk.Access2ShaderSampledRead)
	s.layout = vk.ImageLayoutShaderReadOnlyOptimal
	backend.imageBarrier(cb, d.image, vk.ImageAspectDepth, d.layers(),
		vk.ImageLayoutTransferDstOptimal, vk.ImageLayoutShaderReadOnlyOptimal,
		vk.PipelineStage2Copy, vk.Access2TransferWrite,
		vk.PipelineStage2FragmentShader, vk.Access2ShaderSampledRead)
	d.layout = vk.ImageLayoutShaderReadOnlyOptimal
}

// Resolves a render-target handle, or nil when it names no live target
func (backend *VKBackend) target(h renderer.RenderTargetHandle) *targetEntry {
	if h == 0 || int(h) >= len(backend.targets) || !backend.targets[h].valid {
		return nil
	}
	return &backend.targets[h]
}

// --- dynamic state -----------------------------------------------------------

// Cull mode and depth compare are Vulkan 1.3 dynamic state, so these stay
// immediate calls instead of forcing a separate pipeline per combination.

// Records the cull mode, remembering it for the next pass that starts
func (backend *VKBackend) SetCullMode(m renderer.CullMode) {
	backend.cullMode = m
	if backend.frameActive {
		vk.CmdSetCullMode(backend.frames[backend.frameIndex].cb, cullMode(m))
	}
}

// Records the depth compare op, remembering it for the next pass that starts
func (backend *VKBackend) SetDepthCompare(op renderer.CompareOp) {
	backend.depthCompare = op
	if backend.frameActive {
		vk.CmdSetDepthCompareOp(backend.frames[backend.frameIndex].cb, compareOp(op))
	}
}

// Re-issues the immediate state at pass start, the engine setting it between passes as often as inside them
func (backend *VKBackend) applyDynamicState(cb vk.CommandBuffer) {
	vk.CmdSetCullMode(cb, cullMode(backend.cullMode))
	vk.CmdSetDepthCompareOp(cb, compareOp(backend.depthCompare))
}

// Translates the engine's cull mode into Vulkan's
func cullMode(m renderer.CullMode) vk.CullModeFlags {
	switch m {
	case renderer.CullFront:
		return vk.CullModeFront
	case renderer.CullNone:
		return vk.CullModeNone
	default:
		return vk.CullModeBack
	}
}

// Translates the engine's depth compare op into Vulkan's
func compareOp(op renderer.CompareOp) vk.CompareOp {
	switch op {
	case renderer.CompareLessEqual:
		return vk.CompareOpLessOrEqual
	case renderer.CompareEqual:
		return vk.CompareOpEqual
	case renderer.CompareAlways:
		return vk.CompareOpAlways
	default:
		return vk.CompareOpLess
	}
}

// --- capabilities ------------------------------------------------------------

// Reports no optional capability, ray tracing and compute not being wired up yet (notes/FEATURES.md §3)
func (backend *VKBackend) Supports(renderer.Feature) bool { return false }

// --- helpers -----------------------------------------------------------------

// Records one sync2 layout transition over every layer of an image
func (backend *VKBackend) imageBarrier(cb vk.CommandBuffer, image vk.Image,
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
func (backend *VKBackend) immediateSubmit(record func(cb vk.CommandBuffer)) {
	cbs, err := vk.AllocateCommandBuffers(backend.device, backend.commandPool, 1)
	fatal(err, "allocate one-time command buffer")
	cb := cbs[0]

	fatal(vk.BeginCommandBuffer(cb, vk.CommandBufferUsageOneTimeSubmit), "begin one-time command buffer")
	record(cb)
	fatal(vk.EndCommandBuffer(cb), "end one-time command buffer")

	fatal(vk.QueueSubmit2(backend.queue, []vk.SubmitInfo2{{CommandBuffers: []vk.CommandBuffer{cb}}}, 0), "submit one-time")
	fatal(vk.QueueWaitIdle(backend.queue), "wait one-time")
}

// Drains the frames in flight, required before destroying a resource an already-submitted frame might read
//
// Skips the frame being recorded: its fence was reset in BeginFrame and is only
// signalled by EndFrame, so waiting on it from inside the frame would deadlock.
func (backend *VKBackend) waitAllFrames() {
	fences := make([]vk.Fence, 0, framesInFlight)
	for i := range backend.frames {
		if backend.frameActive && i == backend.frameIndex {
			continue
		}
		fences = append(fences, backend.frames[i].fence)
	}
	if len(fences) == 0 {
		return
	}
	_ = vk.WaitForFences(backend.device, fences, true, math.MaxUint64)
}
