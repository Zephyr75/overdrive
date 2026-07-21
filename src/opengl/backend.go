// Package opengl implements renderer.Backend on OpenGL 4.1 core. Every gl.*
// call in the engine lives in this package.
package opengl

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/overdrive/renderer"
)

type meshEntry struct {
	vao uint32
	vbo uint32 // owned only by skybox meshes; regular meshes share the caller's
	ebo uint32
}

type GLBackend struct {
	window *glfw.Window
	// Built-in fallbacks: the "no texture" white pixel, and a black cubemap for
	// cube sampler units the scene leaves unbound.
	whiteTex  uint32
	blackCube uint32

	meshes map[renderer.MeshHandle]meshEntry

	// Fullscreen-quad state for DrawFullscreenQuad (was utils.RenderQuad).
	quadVAO uint32
	quadVBO uint32

	// One std140 uniform buffer shared by every program, bound at binding
	// point 0, rewritten per draw. blockScratch is the CPU staging copy.
	ubo          uint32
	blockScratch []byte
}

func New() *GLBackend {
	return &GLBackend{
		meshes:       make(map[renderer.MeshHandle]meshEntry),
		blockScratch: make([]byte, blockSize),
	}
}

// --- lifecycle ---------------------------------------------------------------

func (b *GLBackend) ConfigureWindow() {
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Samples, 4)
}

func (b *GLBackend) Init(window *glfw.Window) error {
	b.window = window
	window.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		return err
	}

	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.CULL_FACE)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.Enable(gl.BLEND)

	// Built-in 1x1 white pixel: the "no texture" texture (handle 0 maps here).
	gl.GenTextures(1, &b.whiteTex)
	gl.BindTexture(gl.TEXTURE_2D, b.whiteTex)
	white := []uint8{255, 255, 255, 255}
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(white))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)

	// Built-in 1x1 black cubemap, for cube sampler units with nothing bound.
	gl.GenTextures(1, &b.blackCube)
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, b.blackCube)
	black := []uint8{0, 0, 0, 255}
	for i := 0; i < 6; i++ {
		gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X+uint32(i), 0, gl.RGBA, 1, 1,
			0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(black))
	}
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MAG_FILTER, gl.NEAREST)

	// The shared uniform block, permanently bound to binding point 0; every
	// program's block is pointed at the same buffer in setupProgramInterface.
	gl.GenBuffers(1, &b.ubo)
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.ubo)
	gl.BufferData(gl.UNIFORM_BUFFER, blockSize, nil, gl.DYNAMIC_DRAW)
	gl.BindBuffer(gl.UNIFORM_BUFFER, 0)
	gl.BindBufferBase(gl.UNIFORM_BUFFER, 0, b.ubo)

	return nil
}

func (b *GLBackend) Shutdown() {
	// GL objects die with the context, which dies with the window.
}

// --- frame -------------------------------------------------------------------

func (b *GLBackend) BeginFrame() {}

func (b *GLBackend) EndFrame() {
	b.window.SwapBuffers()
}

func (b *GLBackend) BeginPass(target renderer.FramebufferHandle, w, h int, clear *[4]float32) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, uint32(target))
	gl.Viewport(0, 0, int32(w), int32(h))
	bits := uint32(gl.DEPTH_BUFFER_BIT)
	if clear != nil {
		gl.ClearColor(clear[0], clear[1], clear[2], clear[3])
		bits |= gl.COLOR_BUFFER_BIT
	}
	gl.Clear(bits)
}

func (b *GLBackend) EndPass() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

func (b *GLBackend) SetCullFace(front bool) {
	if front {
		gl.CullFace(gl.FRONT)
	} else {
		gl.CullFace(gl.BACK)
	}
}

func (b *GLBackend) SetDepthFunc(lequal bool) {
	if lequal {
		gl.DepthFunc(gl.LEQUAL)
	} else {
		gl.DepthFunc(gl.LESS)
	}
}

// --- shaders -----------------------------------------------------------------

func (b *GLBackend) CreateShader(name string, hasGeometry bool) (renderer.ShaderHandle, error) {
	program, err := createProgram(name, hasGeometry)
	if err != nil {
		return 0, err
	}
	b.setupProgramInterface(program)
	return renderer.ShaderHandle(program), nil
}

// --- textures ----------------------------------------------------------------

func (b *GLBackend) LoadTexture(path string) (renderer.TextureHandle, error) {
	rgba, err := loadRGBA(path)
	if err != nil {
		return 0, err
	}
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.REPEAT)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.REPEAT)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA,
		int32(rgba.Rect.Size().X), int32(rgba.Rect.Size().Y),
		0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(rgba.Pix))
	return renderer.TextureHandle(tex), nil
}

func (b *GLBackend) LoadCubemap(faces [6]string) (renderer.TextureHandle, error) {
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, tex)
	for i, path := range faces {
		rgba, err := loadRGBA(path)
		if err != nil {
			return 0, fmt.Errorf("cubemap face %s: %w", path, err)
		}
		gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X+uint32(i), 0, gl.RGBA,
			int32(rgba.Rect.Size().X), int32(rgba.Rect.Size().Y),
			0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(rgba.Pix))
	}
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_R, gl.CLAMP_TO_EDGE)
	return renderer.TextureHandle(tex), nil
}

func (b *GLBackend) WhiteTexture() renderer.TextureHandle {
	return renderer.TextureHandle(b.whiteTex)
}

func (b *GLBackend) UpdateTexture2D(h renderer.TextureHandle, w, hgt int, pixels []byte) renderer.TextureHandle {
	tex := uint32(h)
	if tex == 0 {
		gl.GenTextures(1, &tex)
		gl.BindTexture(gl.TEXTURE_2D, tex)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	}
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(hgt),
		0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
	return renderer.TextureHandle(tex)
}

func (b *GLBackend) DestroyTexture(h renderer.TextureHandle) {
	tex := uint32(h)
	if tex != 0 {
		gl.DeleteTextures(1, &tex)
	}
}

// --- buffers and meshes ------------------------------------------------------

func (b *GLBackend) CreateBuffer(data []float32, dynamic bool) renderer.BufferHandle {
	usage := uint32(gl.STATIC_DRAW)
	if dynamic {
		usage = gl.DYNAMIC_DRAW
	}
	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(data)*4, gl.Ptr(data), usage)
	return renderer.BufferHandle(vbo)
}

func (b *GLBackend) UpdateBuffer(h renderer.BufferHandle, data []float32) {
	gl.BindBuffer(gl.ARRAY_BUFFER, uint32(h))
	gl.BufferData(gl.ARRAY_BUFFER, len(data)*4, gl.Ptr(data), gl.DYNAMIC_DRAW)
}

func (b *GLBackend) DestroyBuffer(h renderer.BufferHandle) {
	vbo := uint32(h)
	gl.DeleteBuffers(1, &vbo)
}

func (b *GLBackend) CreateMesh(vbo renderer.BufferHandle, indices []uint32) renderer.MeshHandle {
	var vao, ebo uint32
	gl.GenVertexArrays(1, &vao)
	gl.GenBuffers(1, &ebo)

	gl.BindVertexArray(vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, uint32(vbo))
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, ebo)
	gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices)*4, gl.Ptr(indices), gl.STATIC_DRAW)

	// position(3) | normal(3) | uv(2), 32-byte stride
	gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(3*4))
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))
	gl.EnableVertexAttribArray(2)
	gl.BindVertexArray(0)

	h := renderer.MeshHandle(vao)
	b.meshes[h] = meshEntry{vao: vao, ebo: ebo}
	return h
}

func (b *GLBackend) DestroyMesh(m renderer.MeshHandle) {
	e, ok := b.meshes[m]
	if !ok {
		return
	}
	gl.DeleteVertexArrays(1, &e.vao)
	if e.ebo != 0 {
		gl.DeleteBuffers(1, &e.ebo)
	}
	if e.vbo != 0 {
		gl.DeleteBuffers(1, &e.vbo)
	}
	delete(b.meshes, m)
}

func (b *GLBackend) CreateSkyboxMesh(verts []float32) renderer.MeshHandle {
	var vao, vbo uint32
	gl.GenVertexArrays(1, &vao)
	gl.GenBuffers(1, &vbo)
	gl.BindVertexArray(vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, gl.Ptr(verts), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 3*4, gl.PtrOffset(0))
	gl.BindVertexArray(0)

	h := renderer.MeshHandle(vao)
	b.meshes[h] = meshEntry{vao: vao, vbo: vbo}
	return h
}

// --- shadow targets ----------------------------------------------------------

func (b *GLBackend) CreateShadowMap2D(w, h int) (renderer.FramebufferHandle, renderer.TextureHandle) {
	var fbo, tex uint32
	gl.GenFramebuffers(1, &fbo)
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT, int32(w), int32(h),
		0, gl.DEPTH_COMPONENT, gl.FLOAT, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_BORDER)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_BORDER)
	// Outside the light frustum reads "fully lit".
	borderColor := []float32{1.0, 1.0, 1.0, 1.0}
	gl.TexParameterfv(gl.TEXTURE_2D, gl.TEXTURE_BORDER_COLOR, &borderColor[0])

	gl.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, tex, 0)
	gl.DrawBuffer(gl.NONE)
	gl.ReadBuffer(gl.NONE)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	return renderer.FramebufferHandle(fbo), renderer.TextureHandle(tex)
}

func (b *GLBackend) CreateShadowCubemap(w, h int) (renderer.FramebufferHandle, renderer.TextureHandle) {
	var fbo, tex uint32
	gl.GenFramebuffers(1, &fbo)
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, tex)
	for i := 0; i < 6; i++ {
		gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X+uint32(i), 0, gl.DEPTH_COMPONENT,
			int32(w), int32(h), 0, gl.DEPTH_COMPONENT, gl.FLOAT, nil)
	}
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_R, gl.CLAMP_TO_EDGE)

	gl.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	// Layered attachment: the geometry shader routes triangles to faces.
	gl.FramebufferTexture(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, tex, 0)
	gl.DrawBuffer(gl.NONE)
	gl.ReadBuffer(gl.NONE)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	return renderer.FramebufferHandle(fbo), renderer.TextureHandle(tex)
}

func (b *GLBackend) DestroyFramebuffer(f renderer.FramebufferHandle) {
	fbo := uint32(f)
	if fbo != 0 {
		gl.DeleteFramebuffers(1, &fbo)
	}
}

// ---- draws ------------------------------------------------------------------

func (b *GLBackend) DrawMesh(s renderer.ShaderHandle, m renderer.MeshHandle, indexCount int, u *renderer.Uniforms) {
	gl.UseProgram(uint32(s))
	b.applyUniforms(u)
	gl.BindVertexArray(uint32(m))
	gl.DrawElements(gl.TRIANGLES, int32(indexCount), gl.UNSIGNED_INT, gl.PtrOffset(0))
	gl.BindVertexArray(0)
}

func (b *GLBackend) DrawSkybox(s renderer.ShaderHandle, m renderer.MeshHandle, u *renderer.Uniforms) {
	gl.UseProgram(uint32(s))
	b.applyUniforms(u)
	gl.BindVertexArray(uint32(m))
	gl.DrawArrays(gl.TRIANGLES, 0, 36)
	gl.BindVertexArray(0)
}

func (b *GLBackend) DrawFullscreenQuad(s renderer.ShaderHandle, tex renderer.TextureHandle) {
	if b.quadVAO == 0 {
		quadVertices := []float32{
			// positions     // texCoords
			-1.0, 1.0, 0.0, 0.0, 1.0,
			-1.0, -1.0, 0.0, 0.0, 0.0,
			1.0, 1.0, 0.0, 1.0, 1.0,
			1.0, -1.0, 0.0, 1.0, 0.0,
		}
		gl.GenVertexArrays(1, &b.quadVAO)
		gl.GenBuffers(1, &b.quadVBO)
		gl.BindVertexArray(b.quadVAO)
		gl.BindBuffer(gl.ARRAY_BUFFER, b.quadVBO)
		gl.BufferData(gl.ARRAY_BUFFER, len(quadVertices)*4, gl.Ptr(quadVertices), gl.STATIC_DRAW)
		gl.EnableVertexAttribArray(0)
		gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 5*4, gl.PtrOffset(0))
		gl.EnableVertexAttribArray(1)
		gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 5*4, gl.PtrOffset(3*4))
		gl.BindVertexArray(0)
	}
	gl.UseProgram(uint32(s))
	// The UI shader samples through ourTexture, which is pinned to its own unit
	// like every other sampler.
	b.bind2D(unitOurTexture, tex)
	gl.BindVertexArray(b.quadVAO)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
	gl.BindVertexArray(0)
}

// ---- capabilities -----------------------------------------------------------

func (b *GLBackend) Supports(renderer.Feature) bool { return false }

// ---- helpers ----------------------------------------------------------------

func loadRGBA(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)
	return rgba, nil
}
