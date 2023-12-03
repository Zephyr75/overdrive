package utils

import (
  "github.com/go-gl/mathgl/mgl32"
  "fmt"
  "math"
)

// Parses a string of the form "x,y,z" into a mgl32.Vec3
func ParseVec3(s string) mgl32.Vec3 {
  var x, y, z float32
  fmt.Sscanf(s, "%f,%f,%f", &x, &y, &z)
  return mgl32.Vec3{x, y, z}
}

func EulerToDirection(pitch, yaw, roll float32) mgl32.Vec3 {
    // Convert degrees to radians for trigonometric functions
    pitchRad := float64(mgl32.DegToRad(pitch))
    yawRad := float64(mgl32.DegToRad(yaw))
    // rollRad := float64(mgl32.DegToRad(roll))

    // Calculate the direction vector components
    x := float32(math.Cos(yawRad) * math.Cos(pitchRad))
    y := float32(math.Sin(pitchRad))
    z := float32(math.Sin(yawRad) * math.Cos(pitchRad))

    // Create and return the resulting direction vector
    return mgl32.Vec3{x, y, z}
}

func HandleError(err error) {
  if err != nil {
    panic(err)
  }
}
