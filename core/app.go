package core

import (
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"overdrive/input"

	"fmt"
	"overdrive/opengl"
	"overdrive/scene"
	"overdrive/settings"
	"overdrive/utils"

	"github.com/Zephyr75/gutter/ui"
)

type App struct {
  Name string
  Width int
  Height int
  Window *glfw.Window
}

func init() {
	// GLFW event handling must run on the main OS thread
	runtime.LockOSThread()
}

func (app App) Quit() {
	app.Window.SetShouldClose(true)
}

func NewApp(name string, width int, height int) App {
  
	// GLFW setup
	glfw.Init()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Samples, 4)

	// Window creation
	window, err := glfw.CreateWindow(settings.WindowWidth, settings.WindowHeight, "Cube", nil, nil)
	utils.HandleError(err)
	window.MakeContextCurrent()

	// Callbacks
	window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
  window.SetScrollCallback(input.ScrollCallback)
	window.SetCursorPosCallback(input.MouseCallback)
	window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)

	// OpenGL setup
	gl.Init()
	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.CULL_FACE)
  gl.Enable(gl.BLEND)
  // Anti-aliasing
	// gl.Enable(gl.MULTISAMPLE)	

  
  return App{
    Name: name,
    Width: width,
    Height: height,
    Window: window,
  }
}

func (app App) Run(s *scene.Scene, widget func(app App) ui.UIElement) {

	// Declare main shader programs
  cubesProgram, err := opengl.CreateProgram("cubes", false)
	utils.HandleError(err)

	// Declare directional depth shader programs
	depthProgram, err := opengl.CreateProgram("depth", false)
  utils.HandleError(err)

	// Declare point depth shader programs
	depthCubeProgram, err := opengl.CreateProgram("depth_cube", true)
	utils.HandleError(err)

	// Declare debug shader programs
  // depthDebugProgram, err := opengl.CreateProgram("depth_debug", false)
  // utils.HandleError(err)

	// Declare UI shader programs
	uiProgram, err := opengl.CreateProgram("ui", false)
	utils.HandleError(err)

	// Declare skybox shader programs
	skyboxProgram, err := opengl.CreateProgram("skybox", false)
	utils.HandleError(err)


  
  if s != nil {
    input.S = s
  } else {
    emptyScene := scene.EmptyScene()
    input.S = &emptyScene
  }
	

	// Time init
	i := 0
	time := glfw.GetTime()
	var deltaTime float32 = 0.0
	lastFrame := float64(0.0)

	// Window lifecycle
	for !app.Window.ShouldClose() {

    // update every mesh
    s.UpdateMeshes()
    // println(scene.GetMesh("Suzanne").Positions[0].X())
    // var mesh *scene.Mesh

    // mesh = app.Scene.GetMesh("Suzanne")
    // mesh.Move(0.01, 0, 0)
    // println("1", scene)
    // scene.GetMesh(("Suzanne")).Move(0.01, 0, 0)
    // println(app.Scene.GetLight("Light.003").Pos.X())

    // Process input
    input.ProcessInput(app.Window, deltaTime)
    gl.ClearColor(0.1, 0.1, 0.1, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

    if s != nil {
      // Settings
      nearPlane := float32(1.0)
      farPlane := float32(50.0)

      // Render depth map and depth cube map
      s.Lights[0].RenderLight(nearPlane, farPlane, depthProgram, depthCubeProgram, s)
      lightSpaceMatrix := s.Lights[1].RenderLight(nearPlane, farPlane, depthProgram, depthCubeProgram, s)

      // println(s.Lights[0].Type)
      // println(s.Lights[1].Type)
      // println()

      // Clear buffers
      gl.Viewport(0, 0, int32(settings.WindowWidth), int32(settings.WindowHeight))
      gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

      // Render shadow map

      // gl.UseProgram(depthDebugProgram)
      // nearPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("near_plane\x00"))
      // gl.Uniform1f(nearPlaneLoc, nearPlane)
      // farPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("farPlane\x00"))
      // gl.Uniform1f(farPlaneLoc, farPlane)
      // depthMapLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("depthMap\x00"))
      // gl.Uniform1i(depthMapLoc, 0)
      // gl.ActiveTexture(gl.TEXTURE0)
      // gl.BindTexture(gl.TEXTURE_2D, s.Lights[1].DepthMap)
      // utils.RenderQuad()

      s.RenderScene(cubesProgram, lightSpaceMatrix, farPlane)
      
      s.RenderSkybox(skyboxProgram)
    }

    if widget != nil {
      renderUI(app, app.Window, widget, uiProgram)
    }

		// Time management
		i++
		deltaTime = float32(glfw.GetTime()) - float32(lastFrame)
		lastFrame = glfw.GetTime()
		if glfw.GetTime()-time > 1 {
			fmt.Printf("\rFPS: %d", i)
			i = 0
			time = glfw.GetTime()
		}

		// Swap buffers
		app.Window.SwapBuffers()
		glfw.PollEvents()
	}
}
