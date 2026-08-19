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
	// tiles handed out this frame and the records that describe them
	atlas         shadowAtlas
	tiles         []shadowTile
	shadowRecords []renderer.ShadowRecord

	backend renderer.Backend
}

// Loads a scene from XML and uploads its meshes, shadow maps and skybox through the backend
func NewScene(path string, b renderer.Backend) Scene {
	s := LoadScene(path)
	s.backend = b
	for i := range s.Meshes {
		s.Meshes[i].setup(b)
	}
	// One atlas for every light, allocated here rather than per casting light.
	// Who gets a tile of it is a per-frame decision, not a load-time one
	s.atlas.setup(b)
	s.Skybox.setup(b)
	return s
}

// Returns a scene with nothing in it, for running the app with UI only
func EmptyScene() Scene {
	var s Scene
	s.Meshes = make([]Mesh, 0)
	s.Lights = make([]Light, 0)
	s.Skybox = Skybox{}
	s.Cam = Camera{}
	return s
}

// Reuploads the vertices of every mesh a physics step moved this frame
func (s *Scene) UpdateMeshes() {
	if s != nil {
		for i := range s.Meshes {
			s.Meshes[i].updateVertices()
		}
	}
}

// Finds a mesh by name, returning nil when the scene has none
func (s *Scene) Mesh(name string) *Mesh {
	for i, mesh := range s.Meshes {
		if mesh.Name == name {
			return &s.Meshes[i]
		}
	}
	return nil
}

// Finds a light by name, returning nil when the scene has none
func (s *Scene) Light(name string) *Light {
	for i, light := range s.Lights {
		if light.Name == name {
			return &s.Lights[i]
		}
	}
	return nil
}

// Returns the scene's camera
func (s *Scene) Camera() *Camera {
	return &s.Cam
}

// Parses a scene XML file into meshes, lights and a camera, with no GPU work
func LoadScene(path string) Scene {
	xmlFile, err := os.Open(path)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return Scene{}
	}
	defer xmlFile.Close()

	xmlData, err := io.ReadAll(xmlFile)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return Scene{}
	}

	var sceneXml SceneXml

	if err := xml.Unmarshal(xmlData, &sceneXml); err != nil {
		fmt.Println("Error parsing scene XML:", err)
		return Scene{}
	}

	var s Scene

	s.Meshes = make([]Mesh, len(sceneXml.MeshesXml))
	s.Lights = make([]Light, len(sceneXml.LightsXml))

	s.Cam = sceneXml.CamXml.toCamera()

	for i, meshXml := range sceneXml.MeshesXml {
		s.Meshes[i] = meshXml.toMesh()
	}

	for i, lightXml := range sceneXml.LightsXml {
		s.Lights[i] = lightXml.toLight()
	}

	return s
}

// Writes the per-frame values into u: camera matrices, the light array, and the scene-wide texture handles
func (s *Scene) FillFrameUniforms(u *renderer.FrameUniforms) {
	u.View = mgl32.LookAtV(s.Cam.Pos, s.Cam.Pos.Add(s.Cam.Front), s.Cam.Up)
	u.Projection = mgl32.Perspective(mgl32.DegToRad(s.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
	u.ViewPos = s.Cam.Pos

	count := len(s.Lights)
	if count > renderer.MaxLights {
		count = renderer.MaxLights
	}
	u.LightCount = int32(count)
	for i := 0; i < count; i++ {
		l := &s.Lights[i]
		u.Lights[i] = renderer.LightData{
			Type:        int32(l.Type),
			Constant:    lightConstant,
			Color:       l.Color,
			Intensity:   l.Intensity,
			Diffuse:     l.Diffuse,
			Position:    l.Pos,
			Direction:   l.Dir,
			Cutoff:      l.Cutoff,
			OuterCutoff: l.OuterCutoff,
			Radius:      l.Radius,
			// Set by UpdateShadows, which must therefore run first
			ShadowIndex: l.shadowIndex,
			ShadowCount: l.shadowCount,
		}
		// Read here rather than cached at init: settings.Load runs after this
		// package's variables are initialised, so a snapshot would be the default
		if settings.NoShadows {
			u.Lights[i].ShadowIndex, u.Lights[i].ShadowCount = -1, 0
		}
	}

	u.TexSkybox = s.Skybox.Texture

	// Both bindings point at the one atlas until Part E splits static from
	// dynamic; no record sets the flag that would select the second
	u.TexShadowStatic = s.atlas.tex
	u.TexShadowDynamic = s.atlas.tex
}

// Draws every mesh of the scene with the forward shader, inside the main pass
func (s *Scene) RenderScene(shader renderer.ShaderHandle, f *renderer.FrameUniforms) {
	// Restore the full view matrix, the skybox pass having stripped its
	// translation in its own copy of the block
	f.View = mgl32.LookAtV(s.Cam.Pos, s.Cam.Pos.Add(s.Cam.Front), s.Cam.Up)
	f.Projection = mgl32.Perspective(mgl32.DegToRad(s.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
	s.backend.BindFrameUniforms(f)

	// Static mesh geometry is baked into the OBJ vertices, so the model matrix
	// is identity and only the material fields vary between draws
	u := renderer.DrawUniforms{Model: mgl32.Ident4()}
	s.backend.BindShader(shader)
	for i := range s.Meshes {
		s.Meshes[i].draw(&u)
	}
}
