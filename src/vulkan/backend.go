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
	// Bindless array sizes; must match the descriptor set layout the shaders
	// were compiled against.
	max2DTextures   = 256
	maxCubeTextures = 64

	depthFormat = vk.FormatD32Sfloat
)

// The push constant (the uniform block's device address) is read by every
// stage: the vertex stage for matrices, geometry for the cube shadow
// matrices, fragment for materials and lights.
const pushStages = vk.ShaderStageVertex | vk.ShaderStageGeometry | vk.ShaderStageFragment

// A pipeline is built per (shader, pass kind, vertex layout). The pass kind
// decides winding, attachment formats and blending; the layout decides the
// vertex input state.
type passKind int

const (
	passMain passKind = iota
	passShadow2D
	passShadowCube
	passCount
)

type vertexLayout int

const (
	layoutMesh       vertexLayout = iota // position(3)|normal(3)|uv(2), 32-byte stride
	layoutSkybox                         // position(3) only, 12-byte stride
	layoutFullscreen                     // no vertex input; the UI triangle
	layoutCount
)

type texEntry struct {
	cube bool
	// Index into the bindless array of its kind (binding 0 for 2D, 1 for cube).
	slot      uint32
	image     vk.Image
	alloc     vk.VmaAllocation
	view      vk.ImageView
	ownsImage bool // false for shadow-map views: the shadowEntry owns the image
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

type bufEntry struct {
	buffer vk.Buffer
	alloc  vk.VmaAllocation
	mapped unsafe.Pointer
	size   uint64
	valid  bool
}

type meshEntry struct {
	vbo         renderer.BufferHandle
	indexBuffer vk.Buffer
	indexAlloc  vk.VmaAllocation
	valid       bool
}

type shadowEntry struct {
	cube           bool
	image          vk.Image
	alloc          vk.VmaAllocation
	attachmentView vk.ImageView           // 2D, or 2D_ARRAY(6) for cubes
	tex            renderer.TextureHandle // the sampled view, as a texture handle
	// Tracked so BeginPass knows which transition to record; unlike OpenGL,
	// an image has no implicit "ready to render into" state.
	layout vk.ImageLayout
	valid  bool
}

// retiredTexture is a texture's GPU objects awaiting deferred destruction.
type retiredTexture struct {
	frame        uint64
	view         vk.ImageView
	image        vk.Image
	alloc        vk.VmaAllocation
	staging      vk.Buffer
	stagingAlloc vk.VmaAllocation
}

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

	// Resource tables; the handle is the index. Entry 0 is reserved in every
	// table except textures, where handle 0 is the built-in white pixel.
	textures      []texEntry
	buffers       []bufEntry
	meshes        []meshEntry
	shadowTargets []shadowEntry
	shaders       []shaderEntry
	next2DSlot    uint32
	nextCubeSlot  uint32

	// Textures with staged pixels waiting to be copied at the next BeginFrame.
	pendingUploads []renderer.TextureHandle

	// The UI overlay's quad, created on first use.
	quadBuffer renderer.BufferHandle

	// Resources replaced mid-frame, waiting for the frames that reference them
	// to finish. frameCounter is the monotonic frame number they are aged against.
	retired      []retiredTexture
	frameCounter uint64

	// Which texture handles are currently mirrored into the dedicated shadow
	// descriptors (bindings 2 and 3), so they are only rewritten on change.
	shadow2DHandle   renderer.TextureHandle
	shadowCubeHandle [renderer.MaxShadowCubes]renderer.TextureHandle

	// draw-time state
	currentPass         passKind
	currentShadowTarget renderer.FramebufferHandle // 0 = backbuffer pass
	boundPipeline       vk.Pipeline
	cullFront           bool
	depthLequal         bool
}

func New() *VKBackend {
	b := &VKBackend{
		swapFormat:     vk.FormatB8G8R8A8Unorm,
		shadow2DHandle: invalidHandle,
	}
	for i := range b.shadowCubeHandle {
		b.shadowCubeHandle[i] = invalidHandle
	}
	// Reserve index 0 in the tables whose handle 0 means "none".
	b.buffers = append(b.buffers, bufEntry{})
	b.meshes = append(b.meshes, meshEntry{})
	b.shadowTargets = append(b.shadowTargets, shadowEntry{})
	return b
}

// invalidHandle marks "no texture mirrored yet" in the dedicated-binding
// caches. It cannot collide with a real handle (a table index).
const invalidHandle = renderer.TextureHandle(math.MaxUint32)

// fatal aborts on a failed Vulkan call. Resource creation failing mid-run is
// not recoverable here, and the tutorial-style linear code stays readable
// without error plumbing at every call site (the C++ backend's VK_CHECK).
func fatal(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("vulkan: %s: %v", what, err))
	}
}

// --- lifecycle ---------------------------------------------------------------

func (b *VKBackend) ConfigureWindow() {
	// No OpenGL context: Vulkan reaches the window through a VkSurfaceKHR
	// created in Init.
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
}

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

func (b *VKBackend) createInstance() error {
	// Validation is opt-in because the layers are a separate package on most
	// distributions and instance creation fails outright when a requested layer
	// is missing. Set OVERDRIVE_VK_VALIDATION=1 while developing.
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

func (b *VKBackend) createSurfaceAndDevice() error {
	devices, err := vk.EnumeratePhysicalDevices(b.instance)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Vulkan physical devices")
	}
	b.physicalDevice = devices[0]

	// The surface must exist before the device so present support can be
	// verified on the queue family we are about to request.
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

	// The feature set the engine's shaders and backend rely on. ScalarBlockLayout
	// matches the -fvk-use-scalar-layout SPIR-V; GeometryShader is the point
	// shadow pass; the descriptor-indexing group is the bindless texture arrays.
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

func (b *VKBackend) createFrameData() error {
	cbs, err := vk.AllocateCommandBuffers(b.device, b.commandPool, framesInFlight)
	if err != nil {
		return err
	}
	for i := range b.frames {
		f := &b.frames[i]
		f.cb = cbs[i]
		// Created signalled so the first frame does not block on a fence that
		// no submit will ever signal.
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

	// Outside the sun's light frustum must read "fully lit", which is what an
	// opaque-white border gives (the GL backend's TEXTURE_BORDER_COLOR).
	shadow2D := cubeShadow
	shadow2D.AddressModeU = vk.SamplerAddressModeClampToBorder
	shadow2D.AddressModeV = vk.SamplerAddressModeClampToBorder
	shadow2D.BorderColor = vk.BorderColorOpaqueWhiteFloat
	b.samplerShadow2D, err = vk.CreateSampler(b.device, shadow2D)
	fatal(err, "create 2D shadow sampler")
}

func (b *VKBackend) createDescriptors() {
	// Bindings 0/1 are the bindless material texture arrays. Bindings 2/3 are
	// dedicated single descriptors for the shadow maps: the PCF kernels tap
	// them 9x/20x per fragment, and sampling through a dynamically-indexed
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

func (b *VKBackend) createGlobalPipelineLayout() {
	// One layout for every pipeline: the bindless set, plus an 8-byte push
	// constant holding the address of this draw's uniform block.
	layout, err := vk.CreatePipelineLayout(b.device, vk.PipelineLayoutCreateInfo{
		SetLayouts:         []vk.DescriptorSetLayout{b.setLayout},
		PushConstantRanges: []vk.PushConstantRange{{StageFlags: pushStages, Size: 8}},
	})
	fatal(err, "create pipeline layout")
	b.pipelineLayout = layout
}

func (b *VKBackend) createDefaultTextures() {
	// 2D slot 0 / handle 0: the white pixel the engine uses for "no texture".
	b.uploadTexture([]byte{255, 255, 255, 255}, 1, 1, 1, false, b.samplerRepeat)
	// Cube slot 0: a black dummy, sampled when no cubemap was ever set.
	b.uploadTexture(make([]byte, 4*6), 1, 1, 6, true, b.samplerCubeLinear)

	// Seed the dedicated shadow descriptors so they are valid before the first
	// shadow map exists; partially-bound would tolerate holes, but every draw
	// that samples them would still read undefined data.
	b.writeDedicatedTexture(2, 0, b.textures[0].view, b.samplerShadow2D)
	for i := uint32(0); i < renderer.MaxShadowCubes; i++ {
		b.writeDedicatedTexture(3, i, b.textures[1].view, b.samplerShadowCube)
	}
}

func (b *VKBackend) Shutdown() {
	if b.device == 0 {
		return
	}
	// Nothing may be destroyed while the GPU might still read it.
	_ = vk.DeviceWaitIdle(b.device)

	// The GPU is idle, so everything still queued for deferred destruction is
	// now unreferenced.
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
		// The UI overlay's persistently mapped staging buffer.
		if e.staging != 0 {
			b.allocator.VmaDestroyBuffer(e.staging, e.stagingAlloc)
		}
	}
	for _, e := range b.shadowTargets {
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

func (b *VKBackend) BeginFrame() {
	if b.device == 0 {
		return
	}
	f := &b.frames[b.frameIndex]
	// The CPU throttle: without it frame N+2 would overwrite the ring and
	// command buffer while the GPU still reads them.
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
	// One descriptor set for the whole frame; only its contents change.
	vk.CmdBindDescriptorSets(f.cb, vk.PipelineBindPointGraphics, b.pipelineLayout, 0,
		[]vk.DescriptorSet{b.descriptorSet})

	// Copies must be recorded outside a render pass, so anything staged during
	// the previous frame's passes is flushed here.
	b.flushPendingUploads(f.cb)

	b.boundPipeline = 0
	b.frameActive = true
}

func (b *VKBackend) EndFrame() {
	if !b.frameActive {
		return
	}
	f := &b.frames[b.frameIndex]

	// The explicit version of what SwapBuffers hides.
	b.imageBarrier(f.cb, b.swapImages[b.imageIndex], vk.ImageAspectColor, 1,
		vk.ImageLayoutColorAttachmentOptimal, vk.ImageLayoutPresentSrcKHR,
		vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite,
		vk.PipelineStage2None, vk.Access2None)

	fatal(vk.EndCommandBuffer(f.cb), "end command buffer")

	// The acquire semaphore is per in-flight frame, the render semaphore per
	// swapchain image: present waits on the image's own semaphore, and the two
	// index spaces are not interchangeable.
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

func (b *VKBackend) BeginPass(target renderer.FramebufferHandle, w, h int, clear *[4]float32) {
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
		depthAtt.ImageView = b.depthView
		depthAtt.StoreOp = vk.AttachmentStoreOpDontCare

		info.RenderArea = vk.Rect2D{Extent: b.swapExtent}
		info.ColorAttachments = []vk.RenderingAttachmentInfo{colorAtt}

		// Negative height flips Vulkan's y-down clip space back to OpenGL's
		// y-up, which also cancels the winding flip, so the scene's CCW front
		// faces stay correct without touching any geometry.
		viewport.Y = float32(b.swapExtent.Height)
		viewport.Width = float32(b.swapExtent.Width)
		viewport.Height = -float32(b.swapExtent.Height)

		b.currentPass = passMain
		b.currentShadowTarget = 0
	} else {
		t := &b.shadowTargets[target]
		layers := uint32(1)
		if t.cube {
			layers = 6
		}
		b.imageBarrier(cb, t.image, vk.ImageAspectDepth, layers,
			t.layout, vk.ImageLayoutDepthAttachmentOptimal,
			vk.PipelineStage2AllCommands, vk.Access2MemoryRead|vk.Access2MemoryWrite,
			vk.PipelineStage2EarlyFragmentTests|vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite)
		t.layout = vk.ImageLayoutDepthAttachmentOptimal

		depthAtt.ImageView = t.attachmentView
		depthAtt.StoreOp = vk.AttachmentStoreOpStore

		info.RenderArea = vk.Rect2D{Extent: vk.Extent2D{Width: uint32(w), Height: uint32(h)}}
		info.LayerCount = layers

		// Positive viewport: the shadow map's memory layout then matches
		// OpenGL's, so the sampling math in the shaders is unchanged. The cost
		// is inverted winding, which the pipeline declares as CW front faces.
		viewport.Width = float32(w)
		viewport.Height = float32(h)

		b.currentPass = passShadow2D
		if t.cube {
			b.currentPass = passShadowCube
		}
		b.currentShadowTarget = target
	}
	info.DepthAttachment = &depthAtt

	vk.CmdBeginRendering(cb, info)
	vk.CmdSetViewport(cb, viewport)
	vk.CmdSetScissor(cb, info.RenderArea)
	b.applyDynamicState(cb)
}

func (b *VKBackend) EndPass() {
	if !b.frameActive {
		return
	}
	cb := b.frames[b.frameIndex].cb
	vk.CmdEndRendering(cb)

	// A shadow target has to reach shader-read layout before the main pass
	// samples it. The swapchain image keeps its attachment layout until
	// EndFrame's present barrier.
	if b.currentShadowTarget != 0 {
		t := &b.shadowTargets[b.currentShadowTarget]
		layers := uint32(1)
		if t.cube {
			layers = 6
		}
		b.imageBarrier(cb, t.image, vk.ImageAspectDepth, layers,
			vk.ImageLayoutDepthAttachmentOptimal, vk.ImageLayoutShaderReadOnlyOptimal,
			vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite,
			vk.PipelineStage2FragmentShader, vk.Access2ShaderSampledRead)
		t.layout = vk.ImageLayoutShaderReadOnlyOptimal
		b.currentShadowTarget = 0
	}
}

// --- dynamic state -----------------------------------------------------------

// Cull mode and depth compare are Vulkan 1.3 dynamic state, so these stay
// immediate calls like their OpenGL counterparts instead of forcing a separate
// pipeline per combination.

func (b *VKBackend) SetCullFace(front bool) {
	b.cullFront = front
	if b.frameActive {
		vk.CmdSetCullMode(b.frames[b.frameIndex].cb, cullMode(front))
	}
}

func (b *VKBackend) SetDepthFunc(lequal bool) {
	b.depthLequal = lequal
	if b.frameActive {
		vk.CmdSetDepthCompareOp(b.frames[b.frameIndex].cb, compareOp(lequal))
	}
}

// applyDynamicState re-issues the immediate state at pass start, since the
// engine sets it between passes as often as inside them.
func (b *VKBackend) applyDynamicState(cb vk.CommandBuffer) {
	vk.CmdSetCullMode(cb, cullMode(b.cullFront))
	vk.CmdSetDepthCompareOp(cb, compareOp(b.depthLequal))
}

func cullMode(front bool) vk.CullModeFlags {
	if front {
		return vk.CullModeFront
	}
	return vk.CullModeBack
}

func compareOp(lequal bool) vk.CompareOp {
	if lequal {
		return vk.CompareOpLessOrEqual
	}
	return vk.CompareOpLess
}

// --- capabilities ------------------------------------------------------------

// Ray tracing and compute are not wired up yet (GO_BACKEND.md Phase 6); when
// they are, this reports what the device actually exposes.
func (b *VKBackend) Supports(renderer.Feature) bool { return false }

// --- helpers -----------------------------------------------------------------

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

// immediateSubmit records a one-off command buffer and blocks until the GPU
// has run it. Used by the upload paths, which happen at load time only.
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

// waitAllFrames drains the frames in flight. Required before mutating or
// destroying a resource an already-submitted frame might still be reading.
//
// The frame currently being recorded is skipped: its fence was reset in
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
