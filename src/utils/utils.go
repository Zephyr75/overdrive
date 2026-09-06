package utils

import (
	"fmt"

	"github.com/go-gl/mathgl/mgl32"
)

// Parses a string of the form "x,y,z" into a mgl32.Vec3
func ParseVec3(text string) mgl32.Vec3 {
	var x, y, z float32
	fmt.Sscanf(text, "%f,%f,%f", &x, &y, &z)
	return mgl32.Vec3{x, y, z}
}

// Panics on a non-nil error, startup failures not being recoverable
func HandleError(err error) {
	if err != nil {
		panic(err)
	}
}
