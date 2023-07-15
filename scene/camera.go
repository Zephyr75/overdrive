package scene

import (
	"github.com/go-gl/mathgl/mgl32"
  "overdrive/utils"
)

type Camera struct {
  Type string
  Pos mgl32.Vec3
  Front mgl32.Vec3
  Up mgl32.Vec3
  Yaw float32
  Pitch float32
  Fov float32
}

type CameraXml struct {
  Type string `xml:"type,attr"`
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
  return Camera{
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
    Pos: mgl32.Vec3{0.0, 0.0, 3.0},
    Front: mgl32.Vec3{0.0, 0.0, -1.0},
    Up: mgl32.Vec3{0.0, 1.0, 0.0},
    Yaw: -90.0,
    Pitch: 0.0,
    Fov: 45.0,
  }
}


  
