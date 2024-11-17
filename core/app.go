package core

import (
	"fmt"
	"runtime"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/ecs"
	"github.com/Zephyr75/overdrive/input"
	"github.com/Zephyr75/overdrive/opengl"
	"github.com/Zephyr75/overdrive/scene"
	"github.com/Zephyr75/overdrive/settings"
	"github.com/Zephyr75/overdrive/utils"
)

type App struct {
	Name          string
	Width         int
	Height        int
	Debug         bool
	Window        *glfw.Window
	InputHandler  func(window *glfw.Window, deltaTime float32)
	MouseCallback func(window *glfw.Window, x float64, y float64)
}

func init() {
	// GLFW event handling must run on the main OS thread
	runtime.LockOSThread()
}

func (app App) Quit() {
	app.Window.SetShouldClose(true)
}

func NewApp(name string, width int, height int, debug bool, inputHandler func(window *glfw.Window, deltaTime float32), mouseCallback func(window *glfw.Window, x float64, y float64)) App {

	app := App{
		Name:          name,
		Width:         width,
		Height:        height,
		Debug:         debug,
		MouseCallback: mouseCallback,
		InputHandler:  inputHandler,
	}

	// GLFW setup
	glfw.Init()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Samples, 4)

	// Window creation
	window, err := glfw.CreateWindow(settings.WindowWidth, settings.WindowHeight, "Cube", nil, nil)
	if err != nil {
		glfw.Terminate()
	}
	window.MakeContextCurrent()
	app.Window = window

	// Callbacks
	window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
	window.SetScrollCallback(input.ScrollCallback)
	if app.MouseCallback != nil {
		window.SetCursorPosCallback(app.MouseCallback)
	} else {
		window.SetCursorPosCallback(input.DefaultMouseCallback)
	}
	window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)

	// OpenGL setup
	gl.Init()
	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.CULL_FACE)
	gl.Enable(gl.BLEND)
	// Anti-aliasing
	// gl.Enable(gl.MULTISAMPLE)

	return app
}

func (app App) Run(s *scene.Scene, widget func(app App) ui.UIElement, world *ecs.World) {

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
	curTime := glfw.GetTime()
	var deltaTime float32 = 0.0
	lastFrame := float64(0.0)

	// Window lifecycle
	for !app.Window.ShouldClose() {

		world.Update(time.Second / 60)

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
		if app.InputHandler != nil {
			app.InputHandler(app.Window, deltaTime)
		} else {
			input.DefaultInput(app.Window, deltaTime)
		}
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

			// gl.PolygonMode(gl.FRONT_AND_BACK, gl.LINE)

			s.RenderSkybox(skyboxProgram)

			s.RenderScene(cubesProgram, lightSpaceMatrix, farPlane)

			// draw a circle

		}

		var window *glfw.Window = app.Window
		renderUI(app, window, widget, uiProgram)

		// Time management
		i++
		deltaTime = float32(glfw.GetTime()) - float32(lastFrame)
		lastFrame = glfw.GetTime()
		if glfw.GetTime()-curTime > 1 {
			fmt.Printf("\rFPS: %d", i)
			i = 0
			curTime = glfw.GetTime()
		}

		// Swap buffers
		app.Window.SwapBuffers()
		glfw.PollEvents()
	}
	glfw.Terminate()
}
