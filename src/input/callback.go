package input

import (
	_ "image/png"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"math"

	"github.com/Zephyr75/overdrive/settings"

	"github.com/Zephyr75/overdrive/scene"
)

var (
	firstMouse bool    = true
	lastX      float64 = float64(settings.WindowWidth) / 2.0
	lastY      float64 = float64(settings.WindowHeight) / 2.0
	s          *scene.Scene
)

// SetScene provides the active scene to the input handlers.
func SetScene(scene *scene.Scene) {
	s = scene
}

// The viewport is set per pass by Backend.BeginPass, so a resize only needs
// to update the dimensions the next frame's passes will use.
func FramebufferSizeCallback(window *glfw.Window, width int, height int) {
	settings.WindowWidth = width
	settings.WindowHeight = height
}

func DefaultMouseCallback(window *glfw.Window, xPos, yPos float64) {
	if firstMouse {
		lastX = xPos
		lastY = yPos
		firstMouse = false
	}
	xOffset := xPos - lastX
	yOffset := lastY - yPos
	lastX = xPos
	lastY = yPos
	sensitivity := 0.1
	xOffset *= sensitivity
	yOffset *= sensitivity
	s.Cam.Yaw -= float32(xOffset)
	s.Cam.Pitch -= float32(yOffset)
	if s.Cam.Pitch > 89.0 {
		s.Cam.Pitch = 89.0
	}
	if s.Cam.Pitch < -89.0 {
		s.Cam.Pitch = -89.0
	}
	var direction mgl32.Vec3
	direction[2] = -float32(math.Cos(float64(mgl32.DegToRad(s.Cam.Pitch))) * math.Cos(float64(mgl32.DegToRad(s.Cam.Yaw))))
	direction[1] = -float32(math.Sin(float64(mgl32.DegToRad(s.Cam.Pitch))))
	direction[0] = -float32(math.Cos(float64(mgl32.DegToRad(s.Cam.Pitch))) * math.Sin(float64(mgl32.DegToRad(s.Cam.Yaw))))
	s.Cam.Front = direction.Normalize()

}

func ScrollCallback(window *glfw.Window, xOffset, yOffset float64) {
	s.Cam.Fov -= float32(yOffset)
	if s.Cam.Fov < 1.0 {
		s.Cam.Fov = 1.0
	}
	if s.Cam.Fov > 90.0 {
		s.Cam.Fov = 90.0
	}

}
