package core

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/ecs"
	"github.com/Zephyr75/overdrive/input"
	"github.com/Zephyr75/overdrive/opengl"
	"github.com/Zephyr75/overdrive/renderer"
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
	Backend       renderer.Backend
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

// createBackend selects the graphics backend. It lives here rather than in
// renderer/ because the backend packages import renderer (an import cycle
// otherwise). Selection: OVERDRIVE_BACKEND env var, default "gl".
func createBackend() renderer.Backend {
	switch os.Getenv("OVERDRIVE_BACKEND") {
	case "", "gl", "opengl":
		return opengl.New()
	case "vulkan", "vk":
		panic("vulkan backend not implemented yet (GO_BACKEND.md Phase 4)")
	default:
		panic("unknown OVERDRIVE_BACKEND value")
	}
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

	// The backend is created before the window so it can set its own hints.
	app.Backend = createBackend()

	glfw.Init()
	app.Backend.ConfigureWindow()

	window, err := glfw.CreateWindow(settings.WindowWidth, settings.WindowHeight, name, nil, nil)
	if err != nil {
		glfw.Terminate()
	}
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

	utils.HandleError(app.Backend.Init(window))

	return app
}

func (app App) Run(s *scene.Scene, widget func(app App) ui.UIElement, world *ecs.World) {
	b := app.Backend

	// The main program is still "clouds" for parity with the old engine;
	// the Slang migration (GO_BACKEND.md Phase 3) switches it to "forward".
	forwardShader, err := b.CreateShader("clouds", false)
	utils.HandleError(err)
	depthShader, err := b.CreateShader("depth", false)
	utils.HandleError(err)
	depthCubeShader, err := b.CreateShader("depth_cube", true)
	utils.HandleError(err)
	uiShader, err := b.CreateShader("ui", false)
	utils.HandleError(err)
	skyboxShader, err := b.CreateShader("skybox", false)
	utils.HandleError(err)

	if s != nil {
		input.SetScene(s)
	} else {
		emptyScene := scene.EmptyScene()
		input.SetScene(&emptyScene)
	}

	// Time init
	frames := 0
	curTime := glfw.GetTime()
	var deltaTime float32 = 0.0
	lastFrame := float64(0.0)

	const nearPlane = float32(1.0)
	const farPlane = float32(50.0)

	// Window lifecycle
	for !app.Window.ShouldClose() {

		world.Update(time.Second / 60)

		s.UpdateMeshes()

		// Process input
		if app.InputHandler != nil {
			app.InputHandler(app.Window, deltaTime)
		} else {
			input.DefaultInput(app.Window, deltaTime)
		}

		b.BeginFrame()

		var u renderer.Uniforms
		u.FarPlane = farPlane

		if s != nil {
			s.FillFrameUniforms(&u)

			// Shadow passes — one pass per light's depth target. The sun's
			// pass leaves its light-space matrix in u for the main pass.
			for i := range s.Lights {
				s.Lights[i].RenderLight(nearPlane, farPlane, depthShader, depthCubeShader, s, &u)
			}
		}

		// Main pass — the only pass that clears color.
		b.BeginPass(0, settings.WindowWidth, settings.WindowHeight,
			&[4]float32{0.1, 0.1, 0.1, 1.0})

		if s != nil {
			s.RenderSkybox(skyboxShader, &u)
			s.RenderScene(forwardShader, &u)
		}

		renderUI(app, widget, uiShader)

		b.EndPass()
		b.EndFrame()

		// Time management
		frames++
		deltaTime = float32(glfw.GetTime()) - float32(lastFrame)
		lastFrame = glfw.GetTime()
		if glfw.GetTime()-curTime > 1 {
			fmt.Printf("\rFPS: %d", frames)
			frames = 0
			curTime = glfw.GetTime()
		}

		glfw.PollEvents()
	}
	b.Shutdown()
	glfw.Terminate()
}
