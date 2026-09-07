package scene

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

type SceneXml struct {
	CamXml    CameraXml  `xml:"camera"`
	MeshesXml []MeshXml  `xml:"mesh"`
	LightsXml []LightXml `xml:"light"`
}

type Scene struct {
	Meshes []Mesh
	Lights []Light
	Skybox Skybox
	Cam    Camera

	// The one depth texture every shadow in the scene is a sub-rect of, plus the
	// records describing the tiles handed out this frame
	atlas       shadowAtlas
	shadowTiles []renderer.ShadowTile

	// This frame's bake work, as indices into Lights, and the tiles it drew into
	//
	// A light is queued whole: a point light's six faces bake together or not at
	// all, five of them leaving a lit wedge.
	staticQueue, dynamicQueue []int32
	staticBakes, dynamicBakes int

	// Meshes a move touched since the last shadow update, as indices into Meshes
	//
	// Filled by UpdateMeshes and consumed by UpdateShadows, rather than a second
	// mechanism watching the same thing.
	movedMeshes []int

	backend renderer.Backend
}

// Loads a scene from XML and uploads its meshes, shadow maps and skybox through the backend
func NewScene(path string, backend renderer.Backend) (Scene, error) { // TODO: review
	scene, err := LoadScene(path)
	if err != nil {
		return Scene{}, err
	}
	scene.backend = backend
	for i := range scene.Meshes {
		if err := scene.Meshes[i].setup(backend); err != nil {
			return Scene{}, fmt.Errorf("mesh %s: %w", scene.Meshes[i].Name, err)
		}
	}
	// One atlas for every light, allocated here rather than per casting light.
	// Who gets a tile of it is a per-frame decision, not a load-time one
	scene.atlas.setup(backend)
	if err := scene.Skybox.setup(backend); err != nil {
		return Scene{}, err
	}
	return scene, nil
}

// Returns a scene with nothing in it, for running the app with UI only
func EmptyScene() Scene { // TODO: review
	var scene Scene
	scene.Meshes = make([]Mesh, 0)
	scene.Lights = make([]Light, 0)
	scene.Skybox = Skybox{}
	scene.Cam = Camera{}
	return scene
}

// Reuploads the vertices of every mesh a physics step moved this frame
func (scene *Scene) UpdateMeshes() { // TODO: review
	if scene == nil {
		return
	}
	for i := range scene.Meshes {
		if scene.Meshes[i].needsUpdate {
			scene.movedMeshes = append(scene.movedMeshes, i)
		}
		scene.Meshes[i].updateVertices()
	}
}

// Finds a mesh by name, returning nil when the scene has none
func (scene *Scene) Mesh(name string) *Mesh { // TODO: review
	for i, mesh := range scene.Meshes {
		if mesh.Name == name {
			return &scene.Meshes[i]
		}
	}
	return nil
}

// Finds a light by name, returning nil when the scene has none
func (scene *Scene) Light(name string) *Light { // TODO: review
	for i, light := range scene.Lights {
		if light.Name == name {
			return &scene.Lights[i]
		}
	}
	return nil
}

// Returns the scene's camera
func (scene *Scene) Camera() *Camera { // TODO: review
	return &scene.Cam
}

// Parses a scene XML file into meshes, lights and a camera, with no GPU work
func LoadScene(path string) (Scene, error) { // TODO: review
	xmlFile, err := os.Open(path)
	if err != nil {
		return Scene{}, fmt.Errorf("open scene: %w", err)
	}
	defer xmlFile.Close()

	xmlData, err := io.ReadAll(xmlFile)
	if err != nil {
		return Scene{}, fmt.Errorf("read scene: %w", err)
	}

	var sceneXml SceneXml
	if err := xml.Unmarshal(xmlData, &sceneXml); err != nil {
		return Scene{}, fmt.Errorf("parse scene XML: %w", err)
	}

	var scene Scene

	scene.Meshes = make([]Mesh, len(sceneXml.MeshesXml))
	scene.Lights = make([]Light, len(sceneXml.LightsXml))

	scene.Cam = sceneXml.CamXml.toCamera()

	for i, meshXml := range sceneXml.MeshesXml {
		scene.Meshes[i], err = meshXml.toMesh()
		if err != nil {
			return Scene{}, fmt.Errorf("mesh %s: %w", meshXml.Name, err)
		}
	}

	for i, lightXml := range sceneXml.LightsXml {
		scene.Lights[i] = lightXml.toLight()
	}

	return scene, nil
}

// Writes the per-frame values into u: camera matrices, the light array, and the scene-wide texture handles
func (scene *Scene) FillFrameUniforms(uniforms *renderer.FrameUniforms) { // TODO: review
	uniforms.View = mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
	uniforms.Projection = mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
	uniforms.ViewPos = scene.Cam.Pos

	count := len(scene.Lights)
	if count > renderer.MaxLights {
		count = renderer.MaxLights
	}
	uniforms.LightCount = int32(count)
	for i := 0; i < count; i++ {
		light := &scene.Lights[i]
		uniforms.Lights[i] = renderer.LightData{
			Type:        int32(light.Type),
			Constant:    lightConstant,
			Color:       light.Color,
			Intensity:   light.Intensity,
			Diffuse:     light.Diffuse,
			Position:    light.Pos,
			Direction:   light.Dir,
			Cutoff:      light.Cutoff,
			OuterCutoff: light.OuterCutoff,
			Radius:      light.Radius,
			// Set by UpdateShadows, which must therefore run first
			ShadowIndex: light.shadowIndex,
			ShadowCount: light.shadowCount,
		}
		// Read here rather than cached at init: settings.Load runs after this
		// package's variables are initialised, so a snapshot would be the default
		if settings.NoShadows {
			uniforms.Lights[i].ShadowIndex, uniforms.Lights[i].ShadowCount = -1, 0
		}
	}

	uniforms.TexSkybox = scene.Skybox.Slot

	// The two atlases are dedicated descriptors the shader reaches by a literal
	// index, so nothing about them travels in this block: which of the two a
	// record samples is its Flags bit 0
	uniforms.ShadowNormalScale = settings.ShadowNormalScale()
}

// Opens the depth prepass, draws every mesh depth-only and closes it
//
// Run rather than Render because it owns its pass, as BakeShadows does:
// RenderScene and RenderSkybox instead draw inside a pass the caller opened.
//
// It is handed the same uploaded frame block the forward pass gets, so the two
// read the same bytes rather than two rebuilt copies of them. An EQUAL depth
// test rejects a difference down to the last bit, and prepass.slang combines the
// matrices in exactly the order forward.slang's vsMain does
func (scene *Scene) RunDepthPrepass(frame renderer.Frame, pipes Pipelines, frameAddr renderer.Address) { // TODO: review
	clear := [4]float32{1, 0, 0, 0}
	frame.Pass(renderer.PassSpec{
		Name:  "depthPrepass",
		Depth: &renderer.Attachment{View: renderer.BackbufferDepth, Clear: &clear, Store: true},
		FlipY: true,
	}, func(pass renderer.Pass) {
		ctx := &drawContext{frame: frame, pass: pass, pipeline: pipes.Prepass}
		ctx.push[PushFrame] = frameAddr

		uniforms := renderer.DrawUniforms{Model: mgl32.Ident4()}
		for i := range scene.Meshes {
			scene.Meshes[i].draw(ctx, &uniforms)
		}
	})
}

// Draws every mesh of the scene with the forward pipeline, inside the main pass
func (scene *Scene) RenderScene(frame renderer.Frame, pass renderer.Pass, pipes Pipelines,
	frameAddr, recordAddr renderer.Address) { // TODO: review

	ctx := &drawContext{frame: frame, pass: pass, pipeline: pipes.Forward}
	ctx.push[PushFrame] = frameAddr
	ctx.push[PushRecords] = recordAddr

	// Static mesh geometry is baked into the OBJ vertices, so the model matrix
	// is identity and only the material fields vary between draws
	uniforms := renderer.DrawUniforms{Model: mgl32.Ident4()}
	for i := range scene.Meshes {
		scene.Meshes[i].draw(ctx, &uniforms)
	}
}

// The images the main pass samples, which it must declare so they are
// transitioned out of the layout the bake left them in
func (scene *Scene) ShadowImages() []renderer.Handle { // TODO: review
	return []renderer.Handle{scene.atlas.staticImage, scene.atlas.dynamicImage}
}
