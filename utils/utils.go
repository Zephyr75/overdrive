package utils

import (
  "github.com/go-gl/mathgl/mgl32"
  "fmt"
  "math"
  "github.com/go-gl/gl/v4.1-core/gl"
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

var (
	quadVAO uint32
	quadVBO uint32
)

func RenderQuad() {
	if quadVAO == 0 {
		quadVertices := []float32{
			// positions     // texCoords
			-1.0, 1.0, 0.0, 0.0, 1.0,
			-1.0, -1.0, 0.0, 0.0, 0.0,
			1.0, 1.0, 0.0, 1.0, 1.0,
			1.0, -1.0, 0.0, 1.0, 0.0,
		}
		gl.GenVertexArrays(1, &quadVAO)
		gl.GenBuffers(1, &quadVBO)
		gl.BindVertexArray(quadVAO)
		gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
		gl.BufferData(gl.ARRAY_BUFFER, len(quadVertices)*4, gl.Ptr(quadVertices), gl.STATIC_DRAW)
		var stride int32 = 5 * 4
		gl.EnableVertexAttribArray(0)
		gl.VertexAttribPointer(0, 3, gl.FLOAT, false, stride, gl.PtrOffset(0))
		gl.EnableVertexAttribArray(1)
		gl.VertexAttribPointer(1, 2, gl.FLOAT, false, stride, gl.PtrOffset(3*4))
	}
	gl.BindVertexArray(quadVAO)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
	gl.BindVertexArray(0)
}
