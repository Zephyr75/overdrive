package input

import (

	"github.com/go-gl/glfw/v3.3/glfw"
)

var (
  inGame bool = true
)

func ProcessInput(window *glfw.Window, deltaTime float32, cameraControl bool) {
  if window.GetKey(glfw.KeyEscape) == glfw.Press {
    window.SetShouldClose(true)
  }
  var cameraSpeed float32 = 10 * deltaTime
  if window.GetKey(glfw.KeyLeftShift) == glfw.Press {
    cameraSpeed *= 4
  }

  if cameraControl {
    if window.GetKey(glfw.KeyW) == glfw.Press {
      S.Cam.Pos = S.Cam.Pos.Add(S.Cam.Front.Mul(cameraSpeed))
    }
    if window.GetKey(glfw.KeyS) == glfw.Press {
      S.Cam.Pos = S.Cam.Pos.Sub(S.Cam.Front.Mul(cameraSpeed))
    }
    if window.GetKey(glfw.KeyA) == glfw.Press {
      S.Cam.Pos = S.Cam.Pos.Sub((S.Cam.Front.Cross(S.Cam.Up).Normalize()).Mul(cameraSpeed))
    }
    if window.GetKey(glfw.KeyD) == glfw.Press {
      S.Cam.Pos = S.Cam.Pos.Add((S.Cam.Front.Cross(S.Cam.Up).Normalize()).Mul(cameraSpeed))
    }
    if window.GetKey(glfw.KeyQ) == glfw.Press {
      S.Cam.Pos = S.Cam.Pos.Add(S.Cam.Up.Mul(cameraSpeed))
    }
    if window.GetKey(glfw.KeyE) == glfw.Press {
      S.Cam.Pos = S.Cam.Pos.Sub(S.Cam.Up.Mul(cameraSpeed))
    }
  }

  if window.GetKey(glfw.KeyTab) == glfw.Press {
    if inGame {
      window.SetCursorPosCallback(nil)
      window.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
      inGame = false
    } else {
      window.SetCursorPosCallback(MouseCallback)
      window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
      inGame = true
    }
  }
}

