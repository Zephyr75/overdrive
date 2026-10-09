package scene

import (
	"fmt"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// The scene's <environment>: an equirectangular .hdr in textures/ beside the scene
type EnvironmentXml struct {
	File string `xml:"file"`
	// Multiplies the whole environment, Blender's Background strength; 0 reads as 1
	Strength float32 `xml:"strength"`
	// Turn about the vertical axis in radians, Blender's Mapping Z rotation
	Rotation float32 `xml:"rotation"`
	// Caps the radiance the image lights with, 0 for none: set it below the sun
	// when a scene light stands in for the sun, or the sun is counted twice
	Clamp float32 `xml:"clamp"`
}

// The sky behind the scene and the image-based light it gives, both from one environment
type Skybox struct {
	mesh renderer.MeshHandle
	// Read from the XML before setup; an empty path lights the scene with a flat grey
	path               string
	Strength, Rotation float32
	clamp              float32
	// The background, the prefiltered specular chain and the irradiance, all
	// equirectangular RGBA16F; the shader samples them by these 2D slots
	SkySlot, SpecularSlot, IrradianceSlot int32
}

// The radiance of the flat environment a scene without one gets
const flatEnvironment = 0.05

// Scales the light the environment gives, never the sky drawn behind the scene
//
// Stands in for the sky occlusion the engine lacks: unoccluded, Blender's
// environment at full strength lights the inside of every shadow and buries
// the scene lights; 0.25 is tuned by eye on the showcase against them
const environmentLightScale = 0.25

// Uploads the skybox cube, then bakes and uploads the environment maps
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
	buf, _ := backend.CreateBuffer(renderer.BufferSpec{
		Name: "skyboxCube", Usage: renderer.BufferVertex, Location: renderer.LocationHost, InitialData: vertices,
	})
	skybox.mesh = backend.CreateMesh(renderer.MeshSpec{Name: "skyboxCube", Vertices: buf, Stride: positionStride})

	source := flatEquirect(flatEnvironment)
	if skybox.path != "" {
		var err error
		if source, err = loadHDR(skybox.path); err != nil {
			return fmt.Errorf("environment %s: %w", skybox.path, err)
		}
	}
	if skybox.clamp > 0 {
		source.clamp(skybox.clamp)
	}
	chain := buildChain(source)

	// Wraps around the horizon, clamps at the poles, and mips for the specular chain
	sampler := backend.CreateSampler(renderer.SamplerSpec{
		Name: "environment", Mag: renderer.FilterLinear, Min: renderer.FilterLinear,
		Mipmap:   renderer.FilterLinear,
		OutsideU: renderer.OutsideRepeat,
		OutsideV: renderer.OutsideClampToEdge,
		OutsideW: renderer.OutsideClampToEdge,
		MaxLod:   specularLevels,
	})
	skybox.SkySlot = uploadEquirect(backend, sampler, "environmentSky", []*equirect{chain.atWidth(skyWidth)})
	specular := bakeSpecular(chain)
	irradiance := bakeIrradiance(chain)
	// After every bake: specular[0] is the chain's own 512 level, scaled in place
	for _, level := range append(specular, irradiance) {
		level.scale(environmentLightScale)
	}
	skybox.SpecularSlot = uploadEquirect(backend, sampler, "environmentSpecular", specular)
	skybox.IrradianceSlot = uploadEquirect(backend, sampler, "environmentIrradiance", []*equirect{irradiance})
	return nil
}

// Uploads one RGBA16F image, levels[k] as mip k, and returns its 2D slot
func uploadEquirect(backend renderer.Backend, sampler renderer.SamplerHandle, name string, levels []*equirect) int32 {
	img := backend.CreateImage(renderer.ImageSpec{
		Name: name, Width: levels[0].width, Height: levels[0].height, Format: renderer.FormatRGBA16F,
		Usage: renderer.ImageSampled | renderer.ImageCopyDst, MipLevels: len(levels), Sampler: sampler,
	})
	for mip, level := range levels {
		backend.UpdateImage(img, renderer.ImageData{Pixels: level.rgba16f(), Width: level.width, Height: level.height, Mip: mip})
	}
	return int32(backend.Slot(img))
}

// Draws the skybox first in the main pass, with a depth test that lets it fill the far plane
func (scene *Scene) RenderSkybox(frame renderer.Frame, pass renderer.Pass, pipes Pipelines, base *renderer.FrameUniforms) {
	sky := *base
	view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
	sky.View = view.Mat3().Mat4()
	sky.Projection = mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov),
		float32(settings.Current.Window.Width)/float32(settings.Current.Window.Height), 0.1, 100.0)

	ctx := &drawContext{frame: frame, pass: pass, pipeline: pipes.Skybox}
	ctx.push.frame = frame.Upload(&sky)

	uniforms := renderer.DrawUniforms{Model: mgl32.Ident4()}
	ctx.draw(scene.Skybox.mesh, &uniforms)
}
