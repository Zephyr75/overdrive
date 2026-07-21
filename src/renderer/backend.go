// Package renderer defines the backend abstraction: one Backend interface,
// opaque resource handles, and one typed Uniforms struct. Nothing in this
// package (or above it) imports a graphics API; see GO_BACKEND.md.
package renderer

import "github.com/go-gl/glfw/v3.3/glfw"

// Opaque handles; each backend keeps its own table.
// TextureHandle 0 = the built-in white pixel. FramebufferHandle 0 = backbuffer.
type (
	TextureHandle     uint32
	BufferHandle      uint32
	MeshHandle        uint32
	FramebufferHandle uint32
	ShaderHandle      uint32
)

// Feature identifies an optional capability a backend may support.
// Call Backend.Supports before using the matching optional interface.
type Feature int

const (
	FeatureRayTracing Feature = iota
	FeatureCompute
)

type Backend interface {
	// Called after glfw.Init but before glfw.CreateWindow: sets the
	// API-specific window hints (GL context version, or NoAPI for Vulkan).
	ConfigureWindow()
	// Called once after window creation: context/device/swapchain setup.
	Init(window *glfw.Window) error
	Shutdown()

	// Frame lifecycle: call BeginFrame once, then one or more
	// BeginPass/EndPass pairs, then EndFrame (which presents).
	BeginFrame()
	EndFrame()

	// Begins a render pass on the given target (0 = backbuffer): binds it,
	// sets the viewport to w×h, always clears depth, clears color only when
	// clear is non-nil. Clears happen only here, never mid-pass.
	BeginPass(target FramebufferHandle, w, h int, clear *[4]float32)
	EndPass()

	// Immediate state (Vulkan 1.3 dynamic state on the VK backend).
	SetCullFace(front bool)
	SetDepthFunc(lequal bool)

	// Loads the shader set named e.g. "forward"; each backend resolves its
	// own per-stage files.
	CreateShader(name string, hasGeometry bool) (ShaderHandle, error)

	LoadTexture(path string) (TextureHandle, error)
	LoadCubemap(faces [6]string) (TextureHandle, error)
	WhiteTexture() TextureHandle
	// (Re)uploads RGBA8 pixels of a w×h texture; pass 0 to allocate one.
	UpdateTexture2D(h TextureHandle, w, hgt int, pixels []byte) TextureHandle
	DestroyTexture(h TextureHandle)

	CreateBuffer(data []float32, dynamic bool) BufferHandle
	UpdateBuffer(h BufferHandle, data []float32)
	DestroyBuffer(h BufferHandle)

	// A mesh = a vertex buffer + an index slice. Vertex layout is fixed:
	// position(3) | normal(3) | uv(2), 32-byte stride. One handle per
	// material face group, all groups sharing one vertex buffer.
	CreateMesh(vertexBuf BufferHandle, indices []uint32) MeshHandle
	DestroyMesh(m MeshHandle)
	// Skybox: 36 non-indexed vertices, position(3) only.
	CreateSkyboxMesh(verts []float32) MeshHandle

	// Shadow render targets. The returned TextureHandle goes into
	// Uniforms.TexShadowMap / Uniforms.TexShadowCubeMap.
	CreateShadowMap2D(w, h int) (FramebufferHandle, TextureHandle)
	CreateShadowCubemap(w, h int) (FramebufferHandle, TextureHandle)
	DestroyFramebuffer(f FramebufferHandle)

	// Draws snapshot *u at call time; the caller may reuse u afterwards.
	DrawMesh(s ShaderHandle, m MeshHandle, indexCount int, u *Uniforms)
	DrawSkybox(s ShaderHandle, m MeshHandle, u *Uniforms)
	DrawFullscreenQuad(s ShaderHandle, tex TextureHandle)

	Supports(f Feature) bool
}
