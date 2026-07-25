package utils

import (
	"fmt"
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

// Parses a string of the form "x,y,z" into a mgl32.Vec3
func ParseVec3(s string) mgl32.Vec3 {
	var x, y, z float32
	fmt.Sscanf(s, "%f,%f,%f", &x, &y, &z)
	return mgl32.Vec3{x, y, z}
}

// Turns Euler angles in degrees into a unit direction vector, roll being unused
func EulerToDirection(pitch, yaw, roll float32) mgl32.Vec3 {
	pitchRad := float64(mgl32.DegToRad(pitch))
	yawRad := float64(mgl32.DegToRad(yaw))

	x := float32(math.Cos(yawRad) * math.Cos(pitchRad))
	y := float32(math.Sin(pitchRad))
	z := float32(math.Sin(yawRad) * math.Cos(pitchRad))

	return mgl32.Vec3{x, y, z}
}

// Panics on a non-nil error, startup failures not being recoverable
func HandleError(err error) {
	if err != nil {
		panic(err)
	}
}
