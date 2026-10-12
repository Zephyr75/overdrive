package scene

import (
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// A scene file as the Blender add-on writes it: [camera], [environment], and a
// [[mesh]] and [[light]] block per object, positions in Blender's z-up axes
type SceneToml struct {
	Camera      CameraToml       `toml:"camera"`
	Meshes      []MeshToml       `toml:"mesh"`
	Lights      []LightToml      `toml:"light"`
	Environment *EnvironmentToml `toml:"environment"`
}

type Scene struct {
	Meshes []Mesh
	Lights []Light
	Skybox Skybox
	Cam    Camera

	// The one depth texture every shadow in the scene is a sub-rect of
	atlas       shadowAtlas
	// The records describing the tiles handed out this frame
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

// Loads a scene file and uploads its meshes, shadow maps and skybox through the backend
func NewScene(path string, backend renderer.Backend) (Scene, error) { 
	// Parse the file first: everything below needs the mesh and light counts
	scene, err := LoadScene(path)
	if err != nil {
		return Scene{}, err
	}
	// Stored so later moves and uploads need no backend argument
	scene.backend = backend
	// Uploads each mesh's vertices, indices and material textures, all decoded
	// in parallel first, since the backend takes uploads from one thread only
	textures := decodeTextures(scene.Meshes)
	for i := range scene.Meshes {
		if err := scene.Meshes[i].setup(backend, textures); err != nil {
			return Scene{}, fmt.Errorf("mesh %s: %w", scene.Meshes[i].Name, err)
		}
	}
	// One atlas for every light, allocated here rather than per casting light.
	// Who gets a tile of it is a per-frame decision, not a load-time one
	scene.atlas.setup(backend)
	// Last because it owns the environment maps the main pass samples, and needs nothing above
	if err := scene.Skybox.setup(backend); err != nil {
		return Scene{}, err
	}
	return scene, nil
}

// Returns a scene with nothing in it, for running the app with UI only
func EmptyScene() Scene { 
	var scene Scene
	scene.Meshes = make([]Mesh, 0)
	scene.Lights = make([]Light, 0)
	scene.Skybox = Skybox{}
	scene.Cam = Camera{}
	return scene
}

// Records every mesh Place moved this frame, for the shadow update to re-bake
func (scene *Scene) UpdateMeshes() {
	if scene == nil {
		return
	}
	for i := range scene.Meshes {
		if scene.Meshes[i].moved {
			scene.movedMeshes = append(scene.movedMeshes, i)
			scene.Meshes[i].moved = false
		}
	}
}

// Finds a mesh by name, returning nil when the scene has none
func (scene *Scene) FindMesh(name string) *Mesh { 
	for i, mesh := range scene.Meshes {
		if mesh.Name == name {
			return &scene.Meshes[i]
		}
	}
	return nil
}

// Finds a light by name, returning nil when the scene has none
func (scene *Scene) Light(name string) *Light { 
	for i, light := range scene.Lights {
		if light.Name == name {
			return &scene.Lights[i]
		}
	}
	return nil
}

// Returns the scene's camera
func (scene *Scene) Camera() *Camera { 
	return &scene.Cam
}

// Parses a scene file into meshes, lights and a camera, with no GPU work
func LoadScene(path string) (Scene, error) {
	var file SceneToml
	meta, err := toml.DecodeFile(path, &file)
	if err != nil {
		return Scene{}, fmt.Errorf("scene %s: %w", path, err)
	}
	// A misspelt key would otherwise be dropped and its default used in silence
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return Scene{}, fmt.Errorf("scene %s: unknown key %q", path, undecoded[0].String())
	}

	var scene Scene

	scene.Meshes = make([]Mesh, len(file.Meshes))
	scene.Lights = make([]Light, len(file.Lights))

	scene.Cam = file.Camera.toCamera()

	for i, entry := range file.Meshes {
		scene.Meshes[i], err = entry.toMesh(filepath.Dir(path))
		if err != nil {
			return Scene{}, fmt.Errorf("mesh %s: %w", entry.Name, err)
		}
	}

	for i, entry := range file.Lights {
		scene.Lights[i] = entry.toLight()
	}

	// Without an [environment] the sky is a flat grey, which Skybox.setup builds
	scene.Skybox.Strength = 1
	if env := file.Environment; env != nil {
		if env.File != "" {
			scene.Skybox.path = filepath.Join(filepath.Dir(path), "textures", env.File)
		}
		if env.Strength != 0 {
			scene.Skybox.Strength = env.Strength
		}
		scene.Skybox.Rotation = mgl32.DegToRad(env.Rotation)
	}

	return scene, nil
}

// Writes the per-frame values into u: camera matrices, the light array, and the scene-wide texture handles
func (scene *Scene) FillFrameUniforms(uniforms *renderer.FrameUniforms) { 
	uniforms.View = mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
	uniforms.Projection = mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov),
		float32(settings.Current.Window.Width)/float32(settings.Current.Window.Height), 0.1, 100.0)
	uniforms.InvProjection = uniforms.Projection.Inv()
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
		if settings.Current.Debug.NoShadows {
			uniforms.Lights[i].ShadowIndex, uniforms.Lights[i].ShadowCount = -1, 0
		}
	}

	uniforms.TexSky = scene.Skybox.SkySlot
	uniforms.TexSpecular = scene.Skybox.SpecularSlot
	uniforms.TexIrradiance = scene.Skybox.IrradianceSlot
	uniforms.EnvStrength = scene.Skybox.Strength
	uniforms.EnvRotation = scene.Skybox.Rotation

	// The two atlases are dedicated descriptors the shader reaches by a literal
	// index, so nothing about them travels in this block: which of the two a
	// record samples is its Flags bit 0
	uniforms.ShadowNormalScale = settings.ShadowNormalScale()
}

// Draws every mesh of the scene depth-only, inside the depth prepass
func (scene *Scene) RenderDepth(frame renderer.Frame, pass renderer.Pass, pipes Pipelines,
	frameAddr renderer.Address) { 

	ctx := &drawContext{frame: frame, pass: pass, pipeline: pipes.Prepass}
	ctx.push.frame = frameAddr

	// Each mesh writes its own model matrix, the same one the forward pass gets
	var uniforms renderer.DrawUniforms
	for i := range scene.Meshes {
		scene.Meshes[i].draw(ctx, &uniforms)
	}
}

// Draws every mesh of the scene with the forward pipeline, inside the main pass
func (scene *Scene) RenderScene(frame renderer.Frame, pass renderer.Pass, pipes Pipelines,
	frameAddr, recordAddr renderer.Address) { 

	ctx := &drawContext{frame: frame, pass: pass, pipeline: pipes.Forward}
	ctx.push.frame = frameAddr
	ctx.push.records = recordAddr

	// Each mesh writes its model matrix and material fields
	var uniforms renderer.DrawUniforms
	for i := range scene.Meshes {
		scene.Meshes[i].draw(ctx, &uniforms)
	}
}

// The images the main pass samples, which it must declare so they are
// transitioned out of the layout the bake left them in
func (scene *Scene) ShadowImages() []renderer.Handle { 
	return []renderer.Handle{scene.atlas.staticImage, scene.atlas.dynamicImage}
}
