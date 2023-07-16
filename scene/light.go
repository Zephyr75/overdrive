package scene

import (
  "github.com/go-gl/mathgl/mgl32"
  "overdrive/utils"
)

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

func (l LightXml) ToLight() Light {
  pos := utils.ParseVec3(l.Pos)
  color := utils.ParseVec3(l.Color)


  pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
  return Light{
    Type: l.Type,
    Pos: pos,
    Color: color,
    Intensity: l.Intensity,
  }
}
