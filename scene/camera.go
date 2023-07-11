package scene

import (
	"github.com/go-gl/mathgl/mgl32"
)

type Camera struct {
  Pos mgl32.Vec3
  Front mgl32.Vec3
  Up mgl32.Vec3
  Yaw float32
  Pitch float32
  Fov float32
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


  
