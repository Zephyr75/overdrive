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
    return
  }
  defer xmlFile.Close()

  xmlData, err := ioutil.ReadAll(xmlFile)
  if err != nil {
    fmt.Println("Error reading file:", err)
    return
  }

  var scene SceneXml

  xml.Unmarshal(xmlData, &scene)

  for _, mesh := range scene.Meshes {
    fmt.Println(mesh.Filename)
  }

  fmt.Println("------------------")

  for _, light := range scene.Lights {
    fmt.Println(light.Type)
    fmt.Println(light.Pos)
    fmt.Println(light.Color)
    fmt.Println(light.Intensity)
  }

  
  fmt.Println("------------------")

  fmt.Println(scene.Cam.Pos)


}
