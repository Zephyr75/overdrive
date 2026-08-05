// Package renderer defines the backend abstraction: one Backend interface,
// opaque resource handles, and one typed Uniforms struct. Nothing in this
// package (or above it) imports a graphics API. See notes/ENGINE_FLOW.md
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
// and later sampled.
type TargetFormat int

const (
	// Sampled depth: shadow maps
	TargetDepth TargetFormat = iota
	// Sampled colour, high dynamic range: offscreen scene targets, post-processing
	TargetColor
)

// VertexLayout is how a mesh's vertex buffer is laid out, and therefore which
// pipeline variant the backend binds it with.
//
// It is a property of the geometry, recorded once when the mesh is created, so
// that one Draw serves every kind of drawable. A new kind of geometry means a
// new layout here, not a new interface method.
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

// RenderTargetSpec describes an offscreen target by what it *is*, not by what
// it is used for.
//
// The two shadow maps were once dedicated interface methods, which meant the
// engine could express a depth target and nothing else — no HDR buffer, no
// G-buffer, no reflection probe. Adding a use now means filling in this struct
// rather than widening the Backend interface.
type RenderTargetSpec struct {
	Width, Height int
	Format        TargetFormat
	// Cube allocates 6 layers, attached as an array (a geometry stage routes
	// triangles to faces) and sampled as a cubemap.
	Cube bool
}

type Backend interface {
	// Sets the API-specific window hints (GL context version, or NoAPI for Vulkan), between glfw.Init and glfw.CreateWindow
	ConfigureWindow()
	// Sets up the context/device/swapchain, once, after window creation
	Init(window *glfw.Window) error
	// Destroys everything the backend owns
	Shutdown()

	// Opens a frame, before any pass is begun
	BeginFrame()
	// Closes the frame and presents it
	EndFrame()

	// Begins a pass on target (0 = backbuffer): binds it, sets the viewport to w×h, always clears depth, clears color only when clear is non-nil
	BeginPass(target RenderTargetHandle, w, h int, clear *[4]float32)
	// Ends the pass, after which nothing may be drawn until the next BeginPass
	EndPass()

	// Publishes the pass-scoped uniforms, from a snapshot of *f taken at call time
	//
	// Must be called after BeginPass and before the pass's first draw. Every
	// draw in the pass then reads this same block, which is what keeps the
	// ~1.2 KB of camera and light data off the per-draw path.
	BindFrameUniforms(f *FrameUniforms)

	// Selects the culled face as immediate state, which is dynamic state on the VK backend
	SetCullFace(front bool)
	// Selects the depth compare op (LEQUAL for the skybox, LESS otherwise)
	SetDepthFunc(lequal bool)

	// Loads the shader set named e.g. "forward", each backend resolving its own per-stage files
	CreateShader(name string, hasGeometry bool) (ShaderHandle, error)

	// Loads an RGBA image file as a sampled 2D texture
	LoadTexture(path string) (TextureHandle, error)
	// Loads six face images as one cubemap texture
	LoadCubemap(faces [6]string) (TextureHandle, error)
	// Returns the built-in 1x1 white pixel, the "no texture" texture
	WhiteTexture() TextureHandle
	// (Re)uploads RGBA8 pixels of a w×h texture, handle 0 allocating one
	UpdateTexture2D(h TextureHandle, w, hgt int, pixels []byte) TextureHandle
	// Destroys a texture
	DestroyTexture(h TextureHandle)

	// Creates a vertex buffer from float data, dynamic hinting at later rewrites
	CreateBuffer(data []float32, dynamic bool) BufferHandle
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

	// Draws a mesh from a snapshot of *u taken at call time, leaving u reusable
	//
	// One entry point for every drawable. Vertex layout, vertex or index count
	// and indexed-ness are properties of the mesh, recorded when it was created,
	// not of the call site — so a new kind of drawable needs a new way to build
	// a mesh, not a new way to draw one.
	Draw(s ShaderHandle, m MeshHandle, u *DrawUniforms)

	// Reports whether an optional capability is available
	Supports(f Feature) bool
}
