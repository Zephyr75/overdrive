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

	backend renderer.Backend
}

func NewScene(path string, b renderer.Backend) Scene {
	s := LoadScene(path)
	s.backend = b
	for i := range s.Meshes {
		s.Meshes[i].setup(b)
	}
	for i := range s.Lights {
		s.Lights[i].setup(b)
	}
	s.Skybox.setup(b)
	return s
}

func EmptyScene() Scene {
	var s Scene
	s.Meshes = make([]Mesh, 0)
	s.Lights = make([]Light, 0)
	s.Skybox = Skybox{}
	s.Cam = Camera{}
	return s
}

func (s *Scene) UpdateMeshes() {
	if s != nil {
		for i := range s.Meshes {
			s.Meshes[i].updateVertices()
		}
	}
}

func (s *Scene) GetMesh(name string) *Mesh {
	for i, mesh := range s.Meshes {
		if mesh.Name == name {
			return &s.Meshes[i]
		}
	}
	return nil
}

func (s *Scene) GetLight(name string) *Light {
	for i, light := range s.Lights {
		if light.Name == name {
			return &s.Lights[i]
		}
	}
	return nil
}

func (s *Scene) GetCamera() *Camera {
	return &s.Cam
}

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

// FillFrameUniforms writes the per-frame values into u: camera matrices,
// the light array, and the scene-wide texture handles (shadow maps, skybox).
func (s *Scene) FillFrameUniforms(u *renderer.Uniforms) {
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
			Type:      int32(l.Type),
			Constant:  1.0,
			Linear:    0.09,
			Quadratic: 0.032,
			Cutoff:    cos45,
			Color:     l.Color,
			Intensity: l.Intensity,
			Diffuse:   l.Diffuse,
			Specular:  l.Specular,
			Position:  l.Pos,
			Direction: l.Dir,
		}
	}

	u.TexSkybox = s.Skybox.Texture
	for i := range s.Lights {
		l := &s.Lights[i]
		if l.Type == renderer.LightSun && l.depthMap != 0 {
			u.TexShadowMap = l.depthMap
		}
		if l.Type == renderer.LightPoint && l.depthCubeMap != 0 {
			u.TexShadowCubeMap = l.depthCubeMap
		}
	}
}

// cos(45°), the spot cutoff the old per-draw uniform code hardcoded.
const cos45 = float32(0.7071067811865476)

func (s *Scene) RenderScene(shader renderer.ShaderHandle, u *renderer.Uniforms) {
	// Restore the full view matrix (the skybox pass strips its translation).
	u.View = mgl32.LookAtV(s.Cam.Pos, s.Cam.Pos.Add(s.Cam.Front), s.Cam.Up)
	u.Projection = mgl32.Perspective(mgl32.DegToRad(s.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
	u.Model = mgl32.Scale3D(1.0, 1.0, 1.0)

	for i := range s.Meshes {
		s.Meshes[i].draw(shader, u)
	}
}
