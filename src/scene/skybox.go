package scene

import (
	"fmt"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/paths"
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

type Skybox struct {
	mesh    renderer.MeshHandle
	Texture renderer.ImageHandle
	// The cube slot the shader samples, resolved once at load
	Slot int32
}

// Uploads the skybox cube and loads its six face images as a cubemap
func (skybox *Skybox) setup(backend renderer.Backend) error {
	vertices := []float32{
		// positions
		-1.0, 1.0, -1.0,
		-1.0, -1.0, -1.0,
		1.0, -1.0, -1.0,
		1.0, -1.0, -1.0,
		1.0, 1.0, -1.0,
		-1.0, 1.0, -1.0,

		-1.0, -1.0, 1.0,
		-1.0, -1.0, -1.0,
		-1.0, 1.0, -1.0,
		-1.0, 1.0, -1.0,
		-1.0, 1.0, 1.0,
		-1.0, -1.0, 1.0,

		1.0, -1.0, -1.0,
		1.0, -1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, -1.0,
		1.0, -1.0, -1.0,

		-1.0, -1.0, 1.0,
		-1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, -1.0, 1.0,
		-1.0, -1.0, 1.0,

		-1.0, 1.0, -1.0,
		1.0, 1.0, -1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		-1.0, 1.0, 1.0,
		-1.0, 1.0, -1.0,

		-1.0, -1.0, -1.0,
		-1.0, -1.0, 1.0,
		1.0, -1.0, -1.0,
		1.0, -1.0, -1.0,
		-1.0, -1.0, 1.0,
		1.0, -1.0, 1.0,
	}

	// The cube owns its own buffer and carries no indices, unlike scene meshes
	// which share one buffer across their face groups
	buf, _ := backend.CreateBuffer(renderer.BufferInfo{
		Name: "skyboxCube", Usage: renderer.BufferVertex, Location: renderer.LocationHost, Data: vertices,
	})
	skybox.mesh = backend.CreateMesh(renderer.MeshInfo{Name: "skyboxCube", Vertices: buf, Stride: positionStride})
	faces, width, height, err := loadCubeFaces([6]string{
		paths.Texture("skybox/right.png"),
		paths.Texture("skybox/left.png"),
		paths.Texture("skybox/top.png"),
		paths.Texture("skybox/bottom.png"),
		paths.Texture("skybox/front.png"),
		paths.Texture("skybox/back.png"),
	})
	if err != nil {
		return fmt.Errorf("skybox: %w", err)
	}
	// A skybox is never viewed at a grazing angle, so no anisotropy; clamped so
	// a face's edge texels do not wrap into the opposite side
	sampler := backend.CreateSampler(renderer.SamplerInfo{
		Name: "skybox", Mag: renderer.FilterLinear, Min: renderer.FilterLinear,
		Mipmap:   renderer.FilterLinear,
		OutsideU: renderer.OutsideClampToEdge,
		OutsideV: renderer.OutsideClampToEdge,
		OutsideW: renderer.OutsideClampToEdge,
		MaxLod:   1,
	})
	skybox.Texture = backend.CreateImage(renderer.ImageInfo{
		Name: "skybox", Width: width, Height: height, Layers: 6, Kind: renderer.ImageCube,
		Format:  renderer.FormatRGBA8,
		Usage:   renderer.ImageSampled | renderer.ImageCopyDst,
		Sampler: sampler,
	})
	// Six same-sized faces concatenated, so one copy fills the whole image
	pixels := make([]byte, 0, len(faces[0])*6)
	for _, face := range faces {
		pixels = append(pixels, face...)
	}
	backend.UpdateImage(skybox.Texture, renderer.ImageData{Pixels: pixels, Width: width, Height: height, LayerCount: 6})
	skybox.Slot = int32(backend.Slot(skybox.Texture))
	return nil
}

// Draws the skybox first in the main pass, with a depth test that lets it fill the far plane
//
// It uploads its own copy of the frame block with the view translation stripped,
// so the cube follows the camera. The forward pass keeps using the address of
// the untouched one, which is why nothing has to be restored afterwards
func (scene *Scene) RenderSkybox(frame renderer.Frame, pass renderer.Pass, pipes Pipelines, base *renderer.FrameUniforms) {
	sky := *base
	view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
	sky.View = view.Mat3().Mat4()
	sky.Projection = mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)

	ctx := &drawContext{frame: frame, pass: pass, pipeline: pipes.Skybox}
	ctx.push[PushFrame] = frame.Upload(&sky)

	uniforms := renderer.DrawUniforms{Model: mgl32.Ident4()}
	ctx.draw(scene.Skybox.mesh, &uniforms)
}
