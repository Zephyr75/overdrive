package input

import (
	_ "image/png"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
  "math"

  "github.com/Zephyr75/overdrive/settings"

  "github.com/Zephyr75/overdrive/scene"
)

var (
  firstMouse bool = true
  lastX float64 = float64(settings.WindowWidth) / 2.0
  lastY float64 = float64(settings.WindowHeight) / 2.0
  S *scene.Scene
)


func FramebufferSizeCallback(window *glfw.Window, width int, height int) {
  gl.Viewport(0, 0, int32(width), int32(height))
  settings.WindowWidth = width
  settings.WindowHeight = height
}

func MouseCallback(window *glfw.Window, xPos, yPos float64) {
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
  S.Cam.Yaw -= float32(xOffset)
  S.Cam.Pitch -= float32(yOffset)
  if S.Cam.Pitch > 89.0 {
    S.Cam.Pitch = 89.0
  }
  if S.Cam.Pitch < -89.0 {
    S.Cam.Pitch = -89.0
  }
  var direction mgl32.Vec3
  direction[2] = -float32(math.Cos(float64(mgl32.DegToRad(S.Cam.Pitch))) * math.Cos(float64(mgl32.DegToRad(S.Cam.Yaw))))
  direction[1] = -float32(math.Sin(float64(mgl32.DegToRad(S.Cam.Pitch))))
  direction[0] = -float32(math.Cos(float64(mgl32.DegToRad(S.Cam.Pitch))) * math.Sin(float64(mgl32.DegToRad(S.Cam.Yaw))))
  S.Cam.Front = direction.Normalize()

}

func ScrollCallback(window *glfw.Window, xOffset, yOffset float64) {
  S.Cam.Fov -= float32(yOffset) 
  if S.Cam.Fov < 1.0 {
    S.Cam.Fov = 1.0
  }
  if S.Cam.Fov > 90.0 {
    S.Cam.Fov = 90.0
  }

}
