package input

import (
  "overdrive/scene"

	"github.com/go-gl/glfw/v3.3/glfw"
)


func ProcessInput(window *glfw.Window, deltaTime float32) {
  if window.GetKey(glfw.KeyEscape) == glfw.Press {
    window.SetShouldClose(true)
  }
  var cameraSpeed float32 = 2.5 * deltaTime
  if window.GetKey(glfw.KeyW) == glfw.Press {
    scene.Cam.Pos = scene.Cam.Pos.Add(scene.Cam.Front.Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyS) == glfw.Press {
    scene.Cam.Pos = scene.Cam.Pos.Sub(scene.Cam.Front.Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyA) == glfw.Press {
    scene.Cam.Pos = scene.Cam.Pos.Sub((scene.Cam.Front.Cross(scene.Cam.Up).Normalize()).Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyD) == glfw.Press {
    scene.Cam.Pos = scene.Cam.Pos.Add((scene.Cam.Front.Cross(scene.Cam.Up).Normalize()).Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyE) == glfw.Press {
    scene.Cam.Pos = scene.Cam.Pos.Add(scene.Cam.Up.Mul(cameraSpeed))
  }
  if window.GetKey(glfw.KeyQ) == glfw.Press {
    scene.Cam.Pos = scene.Cam.Pos.Sub(scene.Cam.Up.Mul(cameraSpeed))
  }
}

