package scene

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"
)


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
  s.Cam = sceneXml.CamXml.ToCamera()


  for i, meshXml := range sceneXml.MeshesXml {
    s.Meshes[i] = meshXml.ToMesh()
  }

  for i, lightXml := range sceneXml.LightsXml {
    s.Lights[i] = lightXml.ToLight()
    fmt.Println(s.Lights[i].Pos)
  }

  // fmt.Println(scene.Meshes[0].Vertices)

  return s
}
