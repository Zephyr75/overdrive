package core

import (
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/scene"
)

// The overlay's geometry: a unit quad, position(3) | uv(2), with uv equal to
// position. Each gutter Cmd stretches it onto its rect through the model matrix
var quadVertices = []float32{
	0, 0, 0, 0, 0,
	1, 0, 0, 1, 0,
	1, 1, 0, 1, 1,

	0, 0, 0, 0, 0,
	1, 1, 0, 1, 1,
	0, 1, 0, 0, 1,
}

// A texture gutter handed over, uploaded once and kept while it is drawn
type uiTexture struct {
	image    renderer.ImageHandle
	slot     int32
	lastUsed int
}

// A texture not drawn for this many frames is destroyed, giving its slot back:
// a label whose text changes, like an FPS counter, mints a new one each time
const textureLifetime = 120

// Everything the UI overlay owns on the GPU, plus the draw list prepared for
// this frame
type overlay struct {
	backend  renderer.Backend
	pipeline renderer.PipelineHandle
	mesh     renderer.MeshHandle

	drawList      ui.DrawList
	width, height int // the backbuffer the draw list was laid out for
	textures      map[uint64]*uiTexture
	frame         int
	mouseDown     bool
}

// Builds the overlay's quad and pipeline
func newOverlay(backend renderer.Backend) (*overlay, error) {
	ovl := &overlay{backend: backend, textures: map[uint64]*uiTexture{}}

	buf, _ := backend.CreateBuffer(renderer.BufferSpec{
		Name: "overlayQuad", Usage: renderer.BufferVertex,
		Location: renderer.LocationHost, InitialData: quadVertices,
	})
	ovl.mesh = backend.CreateMesh(renderer.MeshSpec{Name: "overlayQuad", Vertices: buf, Stride: 5 * 4})

	// No culling, since the model matrix flips Y and so the winding; no depth
	// test, since gutter's list is already in painter's order over the scene
	pipeline, err := backend.CreatePipeline(renderer.PipelineSpec{
		Name: "ui", Shader: "ui",
		Vertex: renderer.VertexLayout{
			Stride: 5 * 4,
			Attrs: []renderer.VertexAttr{
				{Location: 0, Format: renderer.FormatRGB32F, Offset: 0},
				{Location: 1, Format: renderer.FormatRG32F, Offset: 3 * 4},
			},
		},
		Cull:         renderer.CullNone,
		DepthCompare: renderer.CompareAlways, DepthWrite: false,
		Blend:        renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatDepth32F,
		Samples:      backend.Capacities().BackbufferSamples,
	})
	if err != nil {
		return nil, err
	}
	ovl.pipeline = pipeline
	return ovl, nil
}

// Builds the widget tree, fires a click, and uploads any texture the draw list
// introduces. Runs before the frame is recorded: an upload outside a frame
// lands immediately, while one inside a frame would only be copied at the start
// of the next, after this frame had already sampled the image
func (ovl *overlay) prepare(app App, widget func(app App) ui.UIElement) {
	ovl.drawList.Reset()
	if widget == nil {
		return
	}
	ovl.frame++
	ovl.width, ovl.height = ovl.backend.BackbufferSize()

	// Mouse-look captures the cursor, and a captured cursor reports a virtual
	// position, so the UI only sees the mouse while the cursor is free
	input := ui.Input{CursorX: -1, CursorY: -1, Width: ovl.width, Height: ovl.height}
	free := app.Window.GetInputMode(glfw.CursorMode) != glfw.CursorDisabled
	if free {
		// The cursor is in window coordinates, the draw list in backbuffer pixels
		x, y := app.Window.GetCursorPos()
		if w, h := app.Window.GetSize(); w > 0 && h > 0 {
			input.CursorX = x * float64(ovl.width) / float64(w)
			input.CursorY = y * float64(ovl.height) / float64(h)
		}
	}

	areas := widget(app).Draw(&ovl.drawList, input)

	// Fire on the press edge, topmost area first, so a held button acts once
	down := free && app.Window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press
	if down && !ovl.mouseDown {
		for i := len(areas) - 1; i >= 0; i-- {
			if areas[i].Function != nil && ui.MouseInBounds(input, areas[i]) {
				areas[i].Function()
				break
			}
		}
	}
	ovl.mouseDown = down

	// gutter hands back the same Key for the same content every frame, so a
	// texture is uploaded the first time its Key shows up and reused after
	for i := range ovl.drawList.Cmds {
		t := ovl.drawList.Cmds[i].Tex
		if t == nil {
			continue
		}
		tex, ok := ovl.textures[t.Key]
		if !ok {
			image := ovl.backend.CreateImage(renderer.ImageSpec{
				Name: "uiTexture", Width: t.W, Height: t.H, Format: renderer.FormatRGBA8,
				Usage: renderer.ImageSampled | renderer.ImageCopyDst,
			})
			// gutter's textures are tightly packed RGBA8, straight alpha
			ovl.backend.UpdateImage(image, renderer.ImageData{Pixels: t.Pixels.Pix, Width: t.W, Height: t.H})
			tex = &uiTexture{image: image, slot: int32(ovl.backend.Slot(image))}
			ovl.textures[t.Key] = tex
		}
		tex.lastUsed = ovl.frame
	}

	for key, tex := range ovl.textures {
		if ovl.frame-tex.lastUsed > textureLifetime {
			ovl.backend.Destroy(tex.image)
			delete(ovl.textures, key)
		}
	}
}

// Draws the prepared list inside the main pass: one quad per Cmd, in order,
// each stretched onto its rect and tinted by its colour
func (ovl *overlay) draw(frame renderer.Frame, pass renderer.Pass) {
	if ovl.width == 0 || ovl.height == 0 {
		return
	}
	sw, sh := float32(ovl.width), float32(ovl.height)
	for i := range ovl.drawList.Cmds {
		cmd := &ovl.drawList.Cmds[i]
		r := cmd.Rect
		if cmd.Color.A == 0 || r.W <= 0 || r.H <= 0 {
			continue
		}

		// Pixels, top-left origin, Y down, to clip space, Y up: the main pass
		// flips Y, as the old fullscreen canvas's UVs relied on
		model := mgl32.Translate3D(2*float32(r.X)/sw-1, 1-2*float32(r.Y)/sh, 0).
			Mul4(mgl32.Scale3D(2*float32(r.W)/sw, -2*float32(r.H)/sh, 1))

		// DrawUniforms is size-locked to common.slang, so the tint rides in the
		// material colour and metallic slots rather than a field of its own.
		// Slot 0 is the backend's white pixel, so an untextured Cmd is a fill
		uniforms := renderer.DrawUniforms{
			Model:       model,
			MatDiffuse:  [3]float32{float32(cmd.Color.R) / 255, float32(cmd.Color.G) / 255, float32(cmd.Color.B) / 255},
			MatMetallic: float32(cmd.Color.A) / 255,
		}
		if cmd.Tex != nil {
			if tex, ok := ovl.textures[cmd.Tex.Key]; ok {
				uniforms.TexDiffuse = tex.slot
			}
		}
		push := scene.DrawOnlyAddressArray(frame.Upload(&uniforms))
		pass.Draw(renderer.DrawCall{Pipeline: ovl.pipeline, Mesh: ovl.mesh, Push: push})
	}
}
