package renderer

import "github.com/go-gl/glfw/v3.3/glfw"

// Backend owns the device and every resource on it. Nothing here names a
// technique: it knows images, buffers, pipelines, passes and dispatches, and
// what those are used for is decided above this package.
type Backend interface {
	// --- lifetime

	// Sets up the device and swapchain, once, after window creation
	Init(window *glfw.Window, req Request) error
	// Destroys everything the backend owns
	Shutdown()
	// Reports what the device can do and what Request actually got
	Capacities() Capacities

	// --- resources

	// Creates an image, sampled, stored into, attached, or any mix
	CreateImage(ImageSpec) ImageHandle
	// Creates a view of one slice and one aspect of an image
	CreateView(ImageHandle, ViewSpec) ViewHandle
	// Uploads CPU pixels into an image, whole or a region
	UpdateImage(ImageHandle, ImageData)
	// Creates a buffer and returns its device address
	CreateBuffer(BufferSpec) (BufferHandle, Address)
	// Rewrites part of a buffer from a pointer to a value or a slice
	UpdateBuffer(handle BufferHandle, offset uint64, data any)
	// Copies a buffer back to the CPU
	//
	// Stalls: it waits on the frames in flight before mapping. Right for a
	// screenshot or an image test, wrong inside a frame loop
	ReadBuffer(BufferHandle) []byte
	// Pairs a vertex buffer with one face group's indices
	CreateMesh(MeshSpec) MeshHandle
	CreateSampler(SamplerSpec) SamplerHandle
	// Builds a pipeline object from its whole state, shaders included
	CreatePipeline(PipelineSpec) (PipelineHandle, error)

	// The shader-visible index of an image, allocated on first call
	//
	// The caller writes it into its own uniform block; the backend never reads
	// that block, so this is the only translation there is
	Slot(Handle) uint32
	// Destroys a resource once the frames that could reference it have retired
	Destroy(Handle)
	// Rebuilds every pipeline from its spec, re-reading the SPIR-V on disk
	ReloadPipelines() error

	// --- frames

	// Records and submits one frame. Nothing outside the closure may record
	Frame(record func(Frame))
}

// Frame is one recorded frame. Its methods are the operations legal between
// passes; a Frame value cannot exist outside Backend.Frame, so the ordering
// rules that used to be runtime guards are scope now.
type Frame interface {
	// Copies a block into this frame's arena and returns its device address
	//
	// Frame-scoped: the arena resets every frame, so an address stored across
	// frames points at another frame's data. data is a pointer to a value or a
	// slice, and is memcpyd, never reinterpreted
	Upload(data any) Address

	// Runs one render pass. Attachments and Reads are transitioned first
	Pass(PassSpec, func(Pass))
	// Runs one compute pass, outside any render pass
	Compute(ComputeSpec, func(Compute))

	// Copies between images and buffers, outside any pass
	Copy(CopySpec)
	// Clears a storage image, outside any pass
	Clear(ClearSpec)
}

// Pass is one open render pass: the only place a draw is legal
type Pass interface {
	// Narrows the viewport and scissor to a rect of the pass's target
	Viewport(x, y, w, handle int)
	Draw(DrawCall)
}

// Compute is one open compute pass: the only place a dispatch is legal
type Compute interface {
	Dispatch(DispatchCall)
}
