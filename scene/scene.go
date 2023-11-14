package scene

import (
  "fmt"
  "os"
  "io/ioutil"
  "encoding/xml"
)

var (
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


  fmt.Println(Cam)

  Cam = sceneXml.CamXml.ToCamera()

  fmt.Println(Cam)

  for i, meshXml := range sceneXml.MeshesXml {
    s.Meshes[i] = meshXml.ToMesh()
  }

  for i, lightXml := range sceneXml.LightsXml {
    s.Lights[i] = lightXml.ToLight()
  }

  // fmt.Println(scene.Meshes[0].Vertices)



  return s
}



