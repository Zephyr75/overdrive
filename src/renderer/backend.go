// Package renderer defines the backend abstraction: one Backend interface,
// opaque resource handles, and one typed Uniforms struct. Nothing in this
// package (or above it) imports a graphics API. See notes/GO_BACKEND.md
package renderer

import "github.com/go-gl/glfw/v3.3/glfw"

// Opaque handles, each backend keeping its own table. TextureHandle 0 is the
// built-in white pixel, FramebufferHandle 0 the backbuffer.
type (
	TextureHandle     uint32
	BufferHandle      uint32
	MeshHandle        uint32
	FramebufferHandle uint32
	ShaderHandle      uint32
)

// Feature is an optional capability a backend may support. Call
// Backend.Supports before using the matching optional interface.
type Feature int

const (
	FeatureRayTracing Feature = iota
	FeatureCompute
)

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
	BeginPass(target FramebufferHandle, w, h int, clear *[4]float32)
	// Ends the pass, after which nothing may be drawn until the next BeginPass
	EndPass()

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

	// Pairs a vertex buffer with one index list, in the fixed position(3)|normal(3)|uv(2) layout of 32-byte stride, one handle per material face group
	CreateMesh(vertexBuf BufferHandle, indices []uint32) MeshHandle
	// Destroys a mesh
	DestroyMesh(m MeshHandle)
	// Creates the skybox cube: 36 non-indexed vertices, position(3) only
	CreateSkyboxMesh(verts []float32) MeshHandle

	// Creates a 2D depth target and its sampled view, for Uniforms.TexShadowMap
	CreateShadowMap2D(w, h int) (FramebufferHandle, TextureHandle)
	// Creates a cube depth target and its sampled view, for Uniforms.TexShadowCubeMap
	CreateShadowCubemap(w, h int) (FramebufferHandle, TextureHandle)
	// Destroys a render target
	DestroyFramebuffer(f FramebufferHandle)

	// Draws an indexed mesh from a snapshot of *u taken at call time, leaving u reusable
	DrawMesh(s ShaderHandle, m MeshHandle, indexCount int, u *Uniforms)
	// Draws the skybox cube non-indexed
	DrawSkybox(s ShaderHandle, m MeshHandle, u *Uniforms)
	// Draws the UI overlay quad, sampling tex
	DrawFullscreenQuad(s ShaderHandle, tex TextureHandle)

	// Reports whether an optional capability is available
	Supports(f Feature) bool
}
