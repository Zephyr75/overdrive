package scene

import (
	"github.com/go-gl/mathgl/mgl32"
  "overdrive/utils"
  "math"
)

type Camera struct {
  Name string
  Type string
  Pos mgl32.Vec3
  Front mgl32.Vec3
  Up mgl32.Vec3
  Yaw float32
  Pitch float32
  Fov float32
}

type CameraXml struct {
  Name string `xml:"name,attr"`
  Type string `xml:"type"`
  Pos string `xml:"position"`
  Front string `xml:"front"`
  Up string `xml:"up"`
  Yaw float32 `xml:"yaw"`
  Pitch float32 `xml:"pitch"`
  Fov float32 `xml:"fov"`
}

func (c CameraXml) ToCamera() Camera {
  pos := utils.ParseVec3(c.Pos)
  front := utils.ParseVec3(c.Front)
  up := utils.ParseVec3(c.Up)
  pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
  // front = mgl32.Vec3{front[0], front[2], front[1]}
  // up = mgl32.Vec3{up[0], up[2], up[1]}
  up = mgl32.Vec3{0.0, 1.0, 0.0}

  var direction mgl32.Vec3
  direction[2] = -float32(math.Cos(float64(mgl32.DegToRad(c.Pitch))) * math.Cos(float64(mgl32.DegToRad(c.Yaw))))
  direction[1] = -float32(math.Sin(float64(mgl32.DegToRad(c.Pitch))))
  direction[0] = -float32(math.Cos(float64(mgl32.DegToRad(c.Pitch))) * math.Sin(float64(mgl32.DegToRad(c.Yaw))))
  front = direction.Normalize()

  return Camera{
    Name: c.Name,
    Type: c.Type,
    Pos: pos,
    Front: front,
    Up: up,
    Yaw: c.Yaw,
    Pitch: c.Pitch,
    Fov: c.Fov,
  }
}

func NewCamera() Camera {
  return Camera{
    Pos: mgl32.Vec3{0.0, 20.0, 15.0},
    Front: mgl32.Vec3{0.0, -1.0, 1.0},
    Up: mgl32.Vec3{0.0, 1.0, 1.0},
    Yaw: 0.0,
    Pitch: 0.0,
    Fov: 45.0,
  }
}


  
