// Package renderer defines the backend abstraction: one Backend interface,
// opaque resource handles, and one typed Uniforms struct
package renderer

import "github.com/go-gl/glfw/v3.3/glfw"

// Opaque handles, each backend keeping its own table. TextureHandle 0 is the
// built-in white pixel, RenderTargetHandle 0 the backbuffer.
type (
	TextureHandle      uint32
	BufferHandle       uint32
	MeshHandle         uint32
	RenderTargetHandle uint32
	ShaderHandle       uint32
)

// Feature is an optional capability a backend may support. Call
// Backend.Supports before using the matching optional interface.
type Feature int

const (
	FeatureRayTracing Feature = iota
	FeatureCompute
)

// TargetFormat is what a render target stores, and therefore how it is attached
// and later sampled
type TargetFormat int

const (
	// Sampled depth: shadow maps
	TargetDepth TargetFormat = iota
	// Sampled colour, high dynamic range: offscreen scene targets, post-processing
	TargetColor
)

// VertexLayout is how a mesh's vertex buffer is laid out, and therefore which
// pipeline variant the backend binds it with.
type VertexLayout int

const (
	// position(3)|normal(3)|uv(2): scene meshes
	LayoutMesh VertexLayout = iota
	// position(3): the skybox cube
	LayoutPosition
	// position(3)|uv(2): fullscreen quads, the UI overlay
	LayoutPositionUV
)

// Floats returns how many float32s one vertex of this layout occupies
func (l VertexLayout) Floats() int {
	switch l {
	case LayoutPosition:
		return 3
	case LayoutPositionUV:
		return 5
	default:
		return 8
	}
}

// CullMode is which face the rasteriser discards
type CullMode int

const (
	// Back faces discarded, the scene default
	CullBack CullMode = iota
	// Front faces discarded, which the sun's shadow pass uses to avoid peter-panning
	CullFront
	// Nothing discarded, for two-sided geometry
	CullNone
)

// CompareOp is the depth test a draw is subject to
type CompareOp int

const (
	// Nearer fragments win, the scene default
	CompareLess CompareOp = iota
	// Ties win too, so the skybox can sit exactly on the far plane
	CompareLessEqual
	// No depth rejection
	CompareAlways
)

// RenderTargetSpec describes an offscreen target by what it is, not by what
// it is used for
type RenderTargetSpec struct {
	Width, Height int
	Format        TargetFormat
	// Cube allocates 6 layers, attached as an array (a geometry stage routes
	// triangles to faces) and sampled as a cubemap
	Cube bool
}

type Backend interface {
	// Sets up the context/device/swapchain, once, after window creation
	Init(window *glfw.Window) error
	// Destroys everything the backend owns
	Shutdown()

	// Opens a frame, before any pass is begun
	BeginFrame()
	// Closes the frame and presents it
	EndFrame()

	// Begins a pass on target (0 = backbuffer): binds it, sets the viewport to
	// the target's own size, always clears depth, clears color only when clear
	// is non-nil
	BeginPass(target RenderTargetHandle, clear *[4]float32)
	// Ends the pass, after which nothing may be drawn until the next BeginPass
	EndPass()

	// Publishes the pass-scoped uniforms, from a snapshot of *f taken at call time
	//
	// Must be called after BeginPass and before the pass's first draw. Every
	// draw in the pass then reads this same block, which is what keeps the
	// ~1.2 KB of camera and light data off the per-draw path.
	BindFrameUniforms(f *FrameUniforms)

	// Selects which face is culled, as pass-scoped state
	SetCullMode(m CullMode)
	// Selects the depth compare op, as pass-scoped state
	SetDepthCompare(op CompareOp)

	// Loads the shader set named e.g. "forward"
	//
	// Vertex and fragment stages are required; a geometry stage is picked up
	// when the set has one, so the caller does not have to know which do.
	CreateShader(name string) (ShaderHandle, error)

	// Uploads tightly packed RGBA8 pixels as a sampled 2D texture
	//
	// Decoding is the caller's job: a backend that opened files could only ever
	// accept what image.Decode accepts, which rules out mipmaps, compressed
	// formats and HDR floats.
	CreateTexture(pixels []byte, w, h int) TextureHandle
	// Uploads six same-sized RGBA8 faces as one cubemap texture
	CreateCubemap(faces [6][]byte, w, h int) TextureHandle
	// (Re)uploads RGBA8 pixels of a w×h texture, handle 0 allocating one
	UpdateTexture2D(h TextureHandle, w, hgt int, pixels []byte) TextureHandle
	// Destroys a texture
	DestroyTexture(h TextureHandle)

	// Creates a vertex buffer from float data
	CreateBuffer(data []float32) BufferHandle
	// Rewrites a buffer's contents in place
	UpdateBuffer(h BufferHandle, data []float32)
	// Destroys a buffer
	DestroyBuffer(h BufferHandle)

	// Pairs a vertex buffer with a vertex layout and an optional index list, one handle per material face group
	//
	// A nil index list makes the mesh non-indexed, its vertex count derived from
	// the buffer's size and the layout's stride. Several meshes may share one
	// vertex buffer, which is how a multi-material OBJ becomes one buffer plus
	// one mesh per face group.
	CreateMesh(vertexBuf BufferHandle, indices []uint32, layout VertexLayout) MeshHandle
	// Destroys a mesh, leaving the vertex buffer it borrowed alone
	DestroyMesh(m MeshHandle)

	// Creates an offscreen target and its sampled view, returning both handles
	CreateRenderTarget(spec RenderTargetSpec) (RenderTargetHandle, TextureHandle)
	// Destroys a render target
	DestroyRenderTarget(f RenderTargetHandle)

	// Selects the shader set every following Draw uses, until the next call
	//
	// Split from Draw so a run of draws sharing a shader states it once. It is
	// also the shape PipelineSpec wants (BACKEND_DECISION.md §6), so the call
	// sites do not move again when pipelines land.
	BindShader(s ShaderHandle)

	// Draws a mesh with the bound shader, from a snapshot of *u taken at call
	// time, leaving u reusable
	//
	// One entry point for every drawable. Vertex layout, vertex or index count
	// and indexed-ness are properties of the mesh, recorded when it was created,
	// not of the call site — so a new kind of drawable needs a new way to build
	// a mesh, not a new way to draw one.
	Draw(m MeshHandle, u *DrawUniforms)

	// Reports whether an optional capability is available
	Supports(f Feature) bool
}
