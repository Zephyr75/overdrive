// Package opengl implements renderer.Backend on OpenGL 4.1 core. Every gl.*
// call in the engine lives in this package
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

// One drawable: its vertex array, an optional index buffer, and everything Draw
// needs to know about it
//
// count and indexed are recorded at creation because they are intrinsic to the
// geometry. That is what lets one Draw serve meshes, the skybox and the overlay.
type meshEntry struct {
	vao     uint32
	vbo     uint32 // owned only by meshes that build their own, others share the caller's
	ebo     uint32
	count   int32 // index count when indexed, vertex count otherwise
	indexed bool
}

type GLBackend struct {
	window *glfw.Window
	// Built-in fallbacks: the "no texture" white pixel, and a black cubemap for
	// cube sampler units the scene leaves unbound.
	whiteTex  uint32
	blackCube uint32

	meshes map[renderer.MeshHandle]meshEntry

	// Float count of every vertex buffer, which is how a non-indexed mesh
	// derives its vertex count from its layout's stride. Vulkan reads the same
	// thing off its VMA allocation size.
	bufferFloats map[renderer.BufferHandle]int

	// Depth renderbuffers owned by colour render targets, keyed by their FBO.
	// Depth targets attach a sampled texture instead and so appear here not at all.
	targetDepthRBOs map[renderer.RenderTargetHandle]uint32

	// Two std140 uniform buffers shared by every program: the frame block at
	// binding point 0, rewritten once per pass, and the draw block at binding
	// point 1, rewritten per draw. Both take the Go struct verbatim — see
	// uniforms.go for why no staging copy is needed.
	frameUBO uint32
	drawUBO  uint32
}

// Builds an empty OpenGL backend, before any GL context exists
func New() *GLBackend {
	return &GLBackend{
		meshes:          make(map[renderer.MeshHandle]meshEntry),
		bufferFloats:    make(map[renderer.BufferHandle]int),
		targetDepthRBOs: make(map[renderer.RenderTargetHandle]uint32),
	}
}

// --- lifecycle ---------------------------------------------------------------

// Asks GLFW for a 4.1 core forward-compatible context with 4x multisampling
func (b *GLBackend) ConfigureWindow() {
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Samples, 4)
}

// Makes the context current and creates the built-in textures and shared uniform buffer
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

	// Create the 1x1 white pixel that stands in for "no texture" (handle 0 maps here)
	gl.GenTextures(1, &b.whiteTex)
	gl.BindTexture(gl.TEXTURE_2D, b.whiteTex)
	white := []uint8{255, 255, 255, 255}
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(white))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)

	// Create the 1x1 black cubemap bound to cube sampler units the scene leaves unused
	gl.GenTextures(1, &b.blackCube)
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, b.blackCube)
	black := []uint8{0, 0, 0, 255}
	for i := 0; i < 6; i++ {
		gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X+uint32(i), 0, gl.RGBA, 1, 1,
			0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(black))
	}
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MAG_FILTER, gl.NEAREST)

	// Create the two shared uniform blocks, permanently bound to binding points
	// 0 and 1. setupProgramInterface points every program's blocks at these
	gl.GenBuffers(1, &b.frameUBO)
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.frameUBO)
	gl.BufferData(gl.UNIFORM_BUFFER, frameBlockSize, nil, gl.DYNAMIC_DRAW)
	gl.BindBufferBase(gl.UNIFORM_BUFFER, bindingFrame, b.frameUBO)

	gl.GenBuffers(1, &b.drawUBO)
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.drawUBO)
	gl.BufferData(gl.UNIFORM_BUFFER, drawBlockSize, nil, gl.DYNAMIC_DRAW)
	gl.BindBufferBase(gl.UNIFORM_BUFFER, bindingDraw, b.drawUBO)

	gl.BindBuffer(gl.UNIFORM_BUFFER, 0)

	return nil
}

// Does nothing, since GL objects die with the context, which dies with the window
func (b *GLBackend) Shutdown() {
}

// --- frame -------------------------------------------------------------------

// Does nothing, as GL has no per-frame setup to record
func (b *GLBackend) BeginFrame() {}

// Presents the frame by swapping the window's buffers
func (b *GLBackend) EndFrame() {
	b.window.SwapBuffers()
}

// Binds the target framebuffer, sets the viewport and clears depth, plus color when asked
func (b *GLBackend) BeginPass(target renderer.RenderTargetHandle, w, h int, clear *[4]float32) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, uint32(target))
	gl.Viewport(0, 0, int32(w), int32(h))
	bits := uint32(gl.DEPTH_BUFFER_BIT)
	if clear != nil {
		gl.ClearColor(clear[0], clear[1], clear[2], clear[3])
		bits |= gl.COLOR_BUFFER_BIT
	}
	gl.Clear(bits)
}

// Rebinds the backbuffer, so a target is never left bound between passes
func (b *GLBackend) EndPass() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

// Culls front faces during the sun's shadow pass and back faces everywhere else
func (b *GLBackend) SetCullFace(front bool) {
	if front {
		gl.CullFace(gl.FRONT)
	} else {
		gl.CullFace(gl.BACK)
	}
}

// Switches the depth test to LEQUAL for the skybox and back to LESS afterwards
func (b *GLBackend) SetDepthFunc(lequal bool) {
	if lequal {
		gl.DepthFunc(gl.LEQUAL)
	} else {
		gl.DepthFunc(gl.LESS)
	}
}

// --- shaders -----------------------------------------------------------------

// Compiles and links one GLSL program from shaders/gl, the handle being the program name
func (b *GLBackend) CreateShader(name string, hasGeometry bool) (renderer.ShaderHandle, error) {
	program, err := createProgram(name, hasGeometry)
	if err != nil {
		return 0, err
	}
	b.setupProgramInterface(program)
	return renderer.ShaderHandle(program), nil
}

// --- textures ----------------------------------------------------------------

// Decodes an image file into a repeat-wrapped, linearly filtered 2D texture
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

// Decodes six face images into one clamped cubemap texture
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

// Returns the built-in white pixel, which in GL is a real texture name
func (b *GLBackend) WhiteTexture() renderer.TextureHandle {
	return renderer.TextureHandle(b.whiteTex)
}

// Reuploads the UI overlay's pixels immediately, allocating the texture when handle 0 is passed
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

// Deletes a texture, the driver deferring the free until no draw still reads it
func (b *GLBackend) DestroyTexture(h renderer.TextureHandle) {
	tex := uint32(h)
	if tex != 0 {
		gl.DeleteTextures(1, &tex)
	}
}

// --- buffers and meshes ------------------------------------------------------

// Uploads float data into a new vertex buffer, hinting STATIC or DYNAMIC draw
func (b *GLBackend) CreateBuffer(data []float32, dynamic bool) renderer.BufferHandle {
	usage := uint32(gl.STATIC_DRAW)
	if dynamic {
		usage = gl.DYNAMIC_DRAW
	}
	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(data)*4, gl.Ptr(data), usage)
	h := renderer.BufferHandle(vbo)
	b.bufferFloats[h] = len(data)
	return h
}

// Respecifies a buffer's contents, the driver ghosting the old storage if a draw still reads it
func (b *GLBackend) UpdateBuffer(h renderer.BufferHandle, data []float32) {
	gl.BindBuffer(gl.ARRAY_BUFFER, uint32(h))
	gl.BufferData(gl.ARRAY_BUFFER, len(data)*4, gl.Ptr(data), gl.DYNAMIC_DRAW)
	b.bufferFloats[h] = len(data)
}

// Deletes a buffer object
func (b *GLBackend) DestroyBuffer(h renderer.BufferHandle) {
	vbo := uint32(h)
	gl.DeleteBuffers(1, &vbo)
	delete(b.bufferFloats, h)
}

// Builds a VAO recording the fixed vertex layout plus this face group's index buffer
func (b *GLBackend) CreateMesh(vbo renderer.BufferHandle, indices []uint32, layout renderer.VertexLayout) renderer.MeshHandle {
	var vao uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, uint32(vbo))

	var ebo uint32
	count := int32(len(indices))
	indexed := len(indices) > 0
	if indexed {
		gl.GenBuffers(1, &ebo)
		gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, ebo)
		gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices)*4, gl.Ptr(indices), gl.STATIC_DRAW)
	} else {
		// No index list, so the draw is a vertex sweep of the whole buffer
		count = int32(b.bufferFloats[vbo] / layout.Floats())
	}

	setAttribPointers(layout)
	gl.BindVertexArray(0)

	h := renderer.MeshHandle(vao)
	b.meshes[h] = meshEntry{vao: vao, ebo: ebo, count: count, indexed: indexed}
	return h
}

// Records the layout's attribute pointers into the bound VAO
//
// This is the OpenGL half of renderer.VertexLayout. The Vulkan half is
// vertexInputState in vulkan/shader.go, which bakes the same description into a
// pipeline instead.
func setAttribPointers(layout renderer.VertexLayout) {
	stride := int32(layout.Floats() * 4)
	switch layout {
	case renderer.LayoutPosition:
		gl.EnableVertexAttribArray(0)
		gl.VertexAttribPointer(0, 3, gl.FLOAT, false, stride, gl.PtrOffset(0))
	case renderer.LayoutPositionUV:
		gl.EnableVertexAttribArray(0)
		gl.VertexAttribPointer(0, 3, gl.FLOAT, false, stride, gl.PtrOffset(0))
		gl.EnableVertexAttribArray(1)
		gl.VertexAttribPointer(1, 2, gl.FLOAT, false, stride, gl.PtrOffset(3*4))
	default: // LayoutMesh
		gl.EnableVertexAttribArray(0)
		gl.VertexAttribPointer(0, 3, gl.FLOAT, false, stride, gl.PtrOffset(0))
		gl.EnableVertexAttribArray(1)
		gl.VertexAttribPointer(1, 3, gl.FLOAT, false, stride, gl.PtrOffset(3*4))
		gl.EnableVertexAttribArray(2)
		gl.VertexAttribPointer(2, 2, gl.FLOAT, false, stride, gl.PtrOffset(6*4))
	}
}

// Deletes a mesh's VAO and the buffers it owns
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

// --- shadow targets ----------------------------------------------------------

// Builds an offscreen FBO and the texture it renders into
//
// Depth targets are the shadow maps: a depth texture with a white border, so
// outside the light frustum reads "fully lit". Colour targets additionally get
// a depth renderbuffer, since anything drawing a scene offscreen still needs
// depth testing.
func (b *GLBackend) CreateRenderTarget(spec renderer.RenderTargetSpec) (renderer.RenderTargetHandle, renderer.TextureHandle) {
	var fbo, tex uint32
	gl.GenFramebuffers(1, &fbo)
	gl.GenTextures(1, &tex)

	target := uint32(gl.TEXTURE_2D)
	if spec.Cube {
		target = gl.TEXTURE_CUBE_MAP
	}
	gl.BindTexture(target, tex)

	format, typ := int32(gl.DEPTH_COMPONENT), uint32(gl.DEPTH_COMPONENT)
	filter := int32(gl.NEAREST)
	if spec.Format == renderer.TargetColor {
		format, typ = gl.RGBA, gl.RGBA
		filter = gl.LINEAR
	}

	if spec.Cube {
		for i := 0; i < 6; i++ {
			gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X+uint32(i), 0, format,
				int32(spec.Width), int32(spec.Height), 0, typ, gl.FLOAT, nil)
		}
		gl.TexParameteri(target, gl.TEXTURE_WRAP_R, gl.CLAMP_TO_EDGE)
	} else {
		gl.TexImage2D(gl.TEXTURE_2D, 0, format, int32(spec.Width), int32(spec.Height),
			0, typ, gl.FLOAT, nil)
	}
	gl.TexParameteri(target, gl.TEXTURE_MIN_FILTER, filter)
	gl.TexParameteri(target, gl.TEXTURE_MAG_FILTER, filter)

	if spec.Format == renderer.TargetDepth && !spec.Cube {
		// Make everything outside the light frustum read as "fully lit"
		gl.TexParameteri(target, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_BORDER)
		gl.TexParameteri(target, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_BORDER)
		borderColor := []float32{1.0, 1.0, 1.0, 1.0}
		gl.TexParameterfv(target, gl.TEXTURE_BORDER_COLOR, &borderColor[0])
	} else {
		gl.TexParameteri(target, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(target, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	}

	gl.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	if spec.Format == renderer.TargetColor {
		// Colour targets carry their own depth, which nothing samples
		var rbo uint32
		gl.GenRenderbuffers(1, &rbo)
		gl.BindRenderbuffer(gl.RENDERBUFFER, rbo)
		gl.RenderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT24, int32(spec.Width), int32(spec.Height))
		gl.FramebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, rbo)
		if spec.Cube {
			gl.FramebufferTexture(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, tex, 0)
		} else {
			gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
		}
		b.targetDepthRBOs[renderer.RenderTargetHandle(fbo)] = rbo
	} else if spec.Cube {
		// Attach all six faces at once, the geometry shader routing triangles to them
		gl.FramebufferTexture(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, tex, 0)
		gl.DrawBuffer(gl.NONE)
		gl.ReadBuffer(gl.NONE)
	} else {
		gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, tex, 0)
		gl.DrawBuffer(gl.NONE)
		gl.ReadBuffer(gl.NONE)
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	return renderer.RenderTargetHandle(fbo), renderer.TextureHandle(tex)
}

// Deletes a framebuffer object and any depth renderbuffer it owns, leaving its texture to DestroyTexture
func (b *GLBackend) DestroyRenderTarget(f renderer.RenderTargetHandle) {
	fbo := uint32(f)
	if fbo == 0 {
		return
	}
	if rbo, ok := b.targetDepthRBOs[f]; ok {
		gl.DeleteRenderbuffers(1, &rbo)
		delete(b.targetDepthRBOs, f)
	}
	gl.DeleteFramebuffers(1, &fbo)
}

// ---- draws ------------------------------------------------------------------

// ---- draws ------------------------------------------------------------------

// Binds the program, uploads the draw block and issues one mesh
//
// The mesh carries its own count and indexed-ness, so this is the only draw
// entry point: a face group, the skybox cube and the overlay quad differ in
// what was recorded at creation, not in how they are issued.
func (b *GLBackend) Draw(s renderer.ShaderHandle, m renderer.MeshHandle, u *renderer.DrawUniforms) {
	e, ok := b.meshes[m]
	if !ok {
		return
	}
	gl.UseProgram(uint32(s))
	b.bindDrawUniforms(u)
	gl.BindVertexArray(e.vao)
	if e.indexed {
		gl.DrawElements(gl.TRIANGLES, e.count, gl.UNSIGNED_INT, gl.PtrOffset(0))
	} else {
		gl.DrawArrays(gl.TRIANGLES, 0, e.count)
	}
	gl.BindVertexArray(0)
}

// ---- capabilities -----------------------------------------------------------

// Reports no optional capability, since ray tracing and compute are Vulkan-only
func (b *GLBackend) Supports(renderer.Feature) bool { return false }

// ---- helpers ----------------------------------------------------------------

// Decodes an image file into a tightly packed RGBA8 buffer
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
