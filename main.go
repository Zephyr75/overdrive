package main

import (
	// "fmt"
	// "go/build"
	// "image"
	// "image/draw"
	_ "image/png"
	// "log"
	// "os"
	"runtime"
	// "strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	// "github.com/go-gl/mathgl/mgl32"
)

const windowWidth = 800
const windowHeight = 600

func init() {
	// GLFW event handling must run on the main OS thread
	runtime.LockOSThread()
}

func main() {

  // FULL SETUP
  glfw.Init()
  glfw.WindowHint(glfw.ContextVersionMajor, 4)
  glfw.WindowHint(glfw.ContextVersionMinor, 1)
  glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
  glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)

  // FULL WINDOW DEFINITION
  window, err := glfw.CreateWindow(windowWidth, windowHeight, "Cube", nil, nil)
  if err != nil {
      glfw.Terminate()
  }
  window.MakeContextCurrent()

  // FULL GL SETUP
  gl.Init()

 
  // FULL WINDOW LIFECYCLE
  for !window.ShouldClose() {
    processInput(window)

    gl.ClearColor(0.2, 0.3, 0.3, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

    window.SwapBuffers()
    glfw.PollEvents()
  }

}

func processInput(window *glfw.Window) {
  if window.GetKey(glfw.KeyEscape) == glfw.Press {
    window.SetShouldClose(true)
  }
}

