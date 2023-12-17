package scene

import (
  "fmt"
  "os"
  "io/ioutil"
  "encoding/xml"
  "github.com/go-gl/gl/v4.1-core/gl"
  "github.com/go-gl/mathgl/mgl32"
  "github.com/Zephyr75/overdrive/settings"


)


type SceneXml struct {
  CamXml CameraXml `xml:"camera"`
  MeshesXml []MeshXml `xml:"mesh"`
  LightsXml []LightXml `xml:"light"`
}

type Scene struct {
  Meshes []Mesh
  Lights []Light
  Skybox Skybox
  Cam Camera
}

func NewScene(path string) Scene {
  // Load scene
	var s Scene = LoadScene(path)
	for i := 0; i < len(s.Meshes); i++ {
		s.Meshes[i].setup()
	}
	for i := 0; i < len(s.Lights); i++ {
		s.Lights[i].setup()
	}
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
    for _, mesh := range s.Meshes {
      mesh.updateVertices()
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

  xmlData, err := ioutil.ReadAll(xmlFile)
  if err != nil {
    fmt.Println("Error reading file:", err)
    return Scene{}
  }

  var sceneXml SceneXml

  xml.Unmarshal(xmlData, &sceneXml)

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

  // fmt.Println(scene.Meshes[0].Vertices)

  s.Skybox = Skybox{}
  s.Skybox.setup()

  return s
}

func (s Scene) RenderScene(cubesProgram uint32, lightSpaceMatrix mgl32.Mat4, farPlane float32) {
  gl.UseProgram(cubesProgram)

  view := mgl32.LookAtV(s.Cam.Pos, s.Cam.Pos.Add(s.Cam.Front), s.Cam.Up)
  viewLoc := gl.GetUniformLocation(cubesProgram, gl.Str("view\x00"))
  gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

  projection := mgl32.Perspective(mgl32.DegToRad(s.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
  projectionLoc := gl.GetUniformLocation(cubesProgram, gl.Str("projection\x00"))
  gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

  model := mgl32.Scale3D(1.0, 1.0, 1.0)
  modelLoc := gl.GetUniformLocation(cubesProgram, gl.Str("model\x00"))
  gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

  lightSpaceMatrixLoc := gl.GetUniformLocation(cubesProgram, gl.Str("lightSpaceMatrix\x00"))
  gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

  farPlaneLoc := gl.GetUniformLocation(cubesProgram, gl.Str("farPlane\x00"))
  gl.Uniform1f(farPlaneLoc, farPlane)

  for i := 0; i < len(s.Meshes); i++ {
    s.Meshes[i].draw(cubesProgram, &s)
  }

}


