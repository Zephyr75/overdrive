package opengl

import (
	"github.com/go-gl/mathgl/mgl32"
)

var (
  CameraPos mgl32.Vec3 = mgl32.Vec3{0.0, 0.0, 3.0}
  CameraFront mgl32.Vec3 = mgl32.Vec3{0.0, 0.0, -1.0}
  CameraUp mgl32.Vec3 = mgl32.Vec3{0.0, 1.0, 0.0}

  Yaw float32 = -90.0
  Pitch float32 = 0.0
  Fov float32 = 45.0
)

