package scene

import (
  "github.com/go-gl/mathgl/mgl32"
  "overdrive/utils"
)

type LightXml struct {
  Type string `xml:"type,attr"`
  Pos string `xml:"position"`
	Dir string `xml:"direction"`
  Color string `xml:"color"`
  Diffuse float32 `xml:"diffuse"`
  Specular float32 `xml:"specular"`
  Intensity float32 `xml:"intensity"`
}

type Light struct {
  Type int
  Pos mgl32.Vec3 
  Dir mgl32.Vec3 
	CutOff float32
  Color mgl32.Vec3
  Diffuse float32
  Specular float32
  Intensity float32
}

func (l LightXml) ToLight() Light {
	t := 0
  pos := utils.ParseVec3(l.Pos)
  dir := utils.ParseVec3(l.Dir)
  color := utils.ParseVec3(l.Color)

  pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
	dir = mgl32.Vec3{dir[0], dir[2], -dir[1]}
	// dir = dir.Add(mgl32.Vec3{0, 1, 0})
	// dir = mgl32.Vec3{0, 1, 0}
	// dir = dir.Mul(180.0 / 3.14)
	switch l.Type {
	case "sun":
		t = 0
	case "point":
		t = 1
	}
  return Light{
    Type: t,
    Pos: pos,
		Dir: dir,
    Color: color,
		Diffuse: l.Diffuse,
		Specular: l.Specular,
    Intensity: l.Intensity,
  }
}
