package utils

import (
  "github.com/go-gl/mathgl/mgl32"
  "fmt"
)

// Parses a string of the form "x,y,z" into a mgl32.Vec3
func ParseVec3(s string) mgl32.Vec3 {
  var x, y, z float32
  fmt.Sscanf(s, "%f,%f,%f", &x, &y, &z)
  return mgl32.Vec3{x, y, z}
}
