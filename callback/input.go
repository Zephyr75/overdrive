package callback

import (
  "overdrive/opengl"

	"github.com/go-gl/glfw/v3.3/glfw"
)


func ProcessInput(window *glfw.Window, deltaTime float32) {
  if window.GetKey(glfw.KeyEscape) == glfw.Press {
    window.SetShouldClose(true)
  }
  var cameraSpeed float32 = 2.5 * deltaTime
  if window.GetKey(glfw.KeyW) == glfw.Press {
    opengl.CameraPos = opengl.CameraPos.Add(opengl.CameraFront.Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyS) == glfw.Press {
    opengl.CameraPos = opengl.CameraPos.Sub(opengl.CameraFront.Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyA) == glfw.Press {
    opengl.CameraPos = opengl.CameraPos.Sub((opengl.CameraFront.Cross(opengl.CameraUp).Normalize()).Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyD) == glfw.Press {
    opengl.CameraPos = opengl.CameraPos.Add((opengl.CameraFront.Cross(opengl.CameraUp).Normalize()).Mul(cameraSpeed))
  }
}

