package scene

import (
  "fmt"
  "os"
  "io/ioutil"
  "encoding/xml"
  "github.com/go-gl/gl/v4.1-core/gl"
  "github.com/go-gl/mathgl/mgl32"
  "overdrive/settings"


)

var (
  // TODO: move this to a better place
  Cam Camera = NewCamera()
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

  Cam = sceneXml.CamXml.ToCamera()

  for i, meshXml := range sceneXml.MeshesXml {
    s.Meshes[i] = meshXml.toMesh()
  }

  for i, lightXml := range sceneXml.LightsXml {
    s.Lights[i] = lightXml.ToLight()
  }

  // fmt.Println(scene.Meshes[0].Vertices)

  s.Skybox = Skybox{}
  s.Skybox.Setup()

  return s
}

func (s Scene) RenderScene(cubesProgram uint32, lightSpaceMatrix mgl32.Mat4, farPlane float32) {
  gl.UseProgram(cubesProgram)

  view := mgl32.LookAtV(Cam.Pos, Cam.Pos.Add(Cam.Front), Cam.Up)
  viewLoc := gl.GetUniformLocation(cubesProgram, gl.Str("view\x00"))
  gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

  projection := mgl32.Perspective(mgl32.DegToRad(Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
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
    s.Meshes[i].Draw(cubesProgram, &s)
  }

}


