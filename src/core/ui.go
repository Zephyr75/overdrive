package core

import (
	"image"

	"github.com/disintegration/imaging"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/scene"
	"github.com/Zephyr75/overdrive/settings"
)

// The overlay's geometry: two triangles in clip space, position(3) | uv(2), so
// it draws like any other mesh
var quadVertices = []float32{
	-1, 1, 0, 0, 1,
	-1, -1, 0, 0, 0,
	1, 1, 0, 1, 1,

	1, 1, 0, 1, 1,
	-1, -1, 0, 0, 0,
	1, -1, 0, 1, 0,
}

// Everything the UI overlay owns on the GPU, plus the hover state that decides
// whether the widget tree is rasterised again this frame
type overlay struct {
	backend       renderer.Backend
	pipeline      renderer.PipelineHandle
	mesh          renderer.MeshHandle
	image         renderer.ImageHandle
	slot          int32
	width, height int

	lastInstance string
	lastMap      map[string]bool
	areas        []ui.Area
}

// Builds the overlay's quad, pipeline and first canvas image
func newOverlay(backend renderer.Backend) (*overlay, error) { // TODO: review
	ovl := &overlay{backend: backend, lastMap: map[string]bool{}}

	buf, _ := backend.CreateBuffer(renderer.BufferSpec{
		Name: "overlayQuad", Usage: renderer.BufferVertex,
		Location: renderer.LocationHost, Data: quadVertices,
	})
	ovl.mesh = backend.CreateMesh(renderer.MeshSpec{Name: "overlayQuad", Vertices: buf, Stride: 5 * 4})

	// Tests depth but does not write it: the overlay composites over the
	// finished scene from the near plane
	pass, err := backend.CreatePipeline(renderer.PipelineSpec{
		Name: "ui", Shader: "ui",
		Vertex: renderer.VertexLayout{
			Stride: 5 * 4,
			Attrs: []renderer.VertexAttr{
				{Location: 0, Format: renderer.FormatRGB32F, Offset: 0},
				{Location: 1, Format: renderer.FormatRG32F, Offset: 3 * 4},
			},
		},
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: renderer.CompareLess, DepthWrite: false,
		Blend:        renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatDepth32F,
		Samples:      backend.Capacities().BackbufferSamples,
	})
	if err != nil {
		return nil, err
	}
	ovl.pipeline = pass
	ovl.resize(settings.WindowWidth, settings.WindowHeight)
	return ovl, nil
}

// Replaces the canvas image when the window size changed
//
// The old one goes through Destroy, which retires it behind the frames in
// flight and only then gives its bindless slot back
func (ovl *overlay) resize(width, height int) { // TODO: review
	if ovl.image != 0 && ovl.width == width && ovl.height == height {
		return
	}
	if ovl.image != 0 {
		ovl.backend.Destroy(ovl.image)
	}
	ovl.image = ovl.backend.CreateImage(renderer.ImageSpec{
		Name: "uiOverlay", Width: width, Height: height, Format: renderer.FormatRGBA8,
		Usage: renderer.ImageSampled | renderer.ImageCopyDst,
	})
	ovl.slot = int32(ovl.backend.Slot(ovl.image))
	ovl.width, ovl.height = width, height
	// Fill it once outside any frame, so the first pass samples an image in a
	// layout it has actually been transitioned into rather than Undefined
	ovl.backend.UpdateImage(ovl.image, renderer.ImageData{Pixels: make([]byte, width*height*4), Width: width, Height: height})
}

// Rasterises the widget tree into the canvas, uploads it and draws it as a
// fullscreen quad, inside the main pass
func (ovl *overlay) draw(frame renderer.Frame, pass renderer.Pass, app App, widget func(app App) ui.UIElement) { // TODO: review
	window := app.Window
	ovl.resize(settings.WindowWidth, settings.WindowHeight)

	img := image.NewRGBA(image.Rect(0, 0, ovl.width, ovl.height))
	var instance ui.UIElement
	if widget != nil {
		instance = widget(app)
	}
	equal := true
	for _, area := range ovl.areas {
		if ui.MouseInBounds(window, area) != ovl.lastMap[area.ToString()] {
			equal = false
		}
		if ui.MouseInBounds(window, area) && window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
			area.Function()
		}
	}

	if instance != nil {
		// Redraw only when the widget tree or the hover state changed
		if ovl.lastInstance != instance.ToString() || !equal {
			ovl.lastInstance = instance.ToString()
			ovl.areas = instance.Draw(img, window)

			newAreas := []ui.Area{}
			for _, area := range ovl.areas {
				if area.Left != 0 || area.Right != 0 || area.Top != 0 || area.Bottom != 0 {
					newAreas = append(newAreas, area)
				}
			}
			ovl.areas = newAreas
		}
		for _, area := range ovl.areas {
			ovl.lastMap[area.ToString()] = ui.MouseInBounds(window, area)
		}
	}

	flipped := imaging.FlipV(img)
	// Called from inside the pass, so the backend stages this and records the
	// copy at the start of the next frame
	ovl.backend.UpdateImage(ovl.image, renderer.ImageData{Pixels: flipped.Pix, Width: ovl.width, Height: ovl.height})

	// The overlay is an ordinary mesh with an ordinary material, so it needs no
	// special draw path — only a texture slot and an identity transform
	uniforms := renderer.DrawUniforms{Model: mgl32.Ident4(), TexDiffuse: ovl.slot}
	var push [4]renderer.Address
	push[scene.PushDraw] = frame.Upload(&uniforms)
	pass.Draw(renderer.DrawCall{Pipeline: ovl.pipeline, Mesh: ovl.mesh, Push: push})
}
