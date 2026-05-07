package input

import (
	"github.com/go-gl/glfw/v3.3/glfw"
)

var (
	inGame bool = true
)

func DefaultInput(window *glfw.Window, deltaTime float32) {
	if window.GetKey(glfw.KeyEscape) == glfw.Press {
		window.SetShouldClose(true)
	}
	var cameraSpeed float32 = 10 * deltaTime
	if window.GetKey(glfw.KeyLeftShift) == glfw.Press {
		cameraSpeed *= 4
	}

	if window.GetKey(glfw.KeyW) == glfw.Press {
		s.Cam.Pos = s.Cam.Pos.Add(s.Cam.Front.Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyS) == glfw.Press {
		s.Cam.Pos = s.Cam.Pos.Sub(s.Cam.Front.Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyA) == glfw.Press {
		s.Cam.Pos = s.Cam.Pos.Sub((s.Cam.Front.Cross(s.Cam.Up).Normalize()).Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyD) == glfw.Press {
		s.Cam.Pos = s.Cam.Pos.Add((s.Cam.Front.Cross(s.Cam.Up).Normalize()).Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyQ) == glfw.Press {
		s.Cam.Pos = s.Cam.Pos.Add(s.Cam.Up.Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyE) == glfw.Press {
		s.Cam.Pos = s.Cam.Pos.Sub(s.Cam.Up.Mul(cameraSpeed))
	}

	if window.GetKey(glfw.KeyTab) == glfw.Press {
		if inGame {
			window.SetCursorPosCallback(nil)
			window.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
			inGame = false
		} else {
			window.SetCursorPosCallback(DefaultMouseCallback)
			window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
			inGame = true
		}
	}
}
