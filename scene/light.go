package scene

import "github.com/go-gl/mathgl/mgl32"

type LightXml struct {
  Type string `xml:"type,attr"`
  Pos string `xml:"position"`
  Color string `xml:"color"`
  Intensity float32 `xml:"intensity"`
}

type Light struct {
  Type string
  Pos mgl32.Vec3 
  Color mgl32.Vec3
  Intensity float32
}
