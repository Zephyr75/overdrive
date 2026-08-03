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
	"github.com/Zephyr75/overdrive/vulkan"
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

// Pins the package to the main OS thread, where GLFW event handling must run
func init() {
	runtime.LockOSThread()
}

// Asks the window to close, ending the frame loop after the current iteration
func (app App) Quit() {
	app.Window.SetShouldClose(true)
}

// Selects the graphics backend from OVERDRIVE_BACKEND, defaulting to "gl"
//
// It lives here rather than in renderer/ because the backend packages import
// renderer, which would otherwise be an import cycle.
func createBackend() renderer.Backend {
	switch os.Getenv("OVERDRIVE_BACKEND") {
	case "", "gl", "opengl":
		return opengl.New()
	case "vulkan", "vk":
		return vulkan.New()
	default:
		panic("unknown OVERDRIVE_BACKEND value")
	}
}

// Creates the backend, the window and its input callbacks, then initialises the backend on that window
func NewApp(name string, width int, height int, debug bool, inputHandler func(window *glfw.Window, deltaTime float32), mouseCallback func(window *glfw.Window, x float64, y float64)) App {

	app := App{
		Name:          name,
		Width:         width,
		Height:        height,
		Debug:         debug,
		MouseCallback: mouseCallback,
		InputHandler:  inputHandler,
	}

	// Create the backend before the window, so it can set its own hints
	app.Backend = createBackend()

	glfw.Init()
	app.Backend.ConfigureWindow()

	window, err := glfw.CreateWindow(settings.WindowWidth, settings.WindowHeight, name, nil, nil)
	if err != nil {
		glfw.Terminate()
	}
	app.Window = window

	// Wire the input callbacks, falling back to the built-in handlers
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

// Loads the shader sets and runs the frame loop until the window closes
func (app App) Run(s *scene.Scene, widget func(app App) ui.UIElement, world *ecs.World) {
	b := app.Backend

	forwardShader, err := b.CreateShader("forward", false)
	utils.HandleError(err)
	depthShader, err := b.CreateShader("depth", false)
	utils.HandleError(err)
	depthCubeShader, err := b.CreateShader("depth_cube", true)
	utils.HandleError(err)
	uiShader, err := b.CreateShader("ui", false)
	utils.HandleError(err)
	skyboxShader, err := b.CreateShader("skybox", false)
	utils.HandleError(err)

	// The overlay's quad is built once, like any other mesh
	uiQuad := createOverlayQuad(b)

	if s != nil {
		input.SetScene(s)
	} else {
		emptyScene := scene.EmptyScene()
		input.SetScene(&emptyScene)
	}

	// Init the frame timing
	frames := 0
	curTime := glfw.GetTime()
	var deltaTime float32 = 0.0
	lastFrame := float64(0.0)

	const nearPlane = float32(1.0)
	const farPlane = float32(50.0)

	// Run one iteration per frame until the window closes
	for !app.Window.ShouldClose() {

		world.Update(time.Second / 60)

		s.UpdateMeshes()

		// Process input before anything is recorded, so the camera is current
		if app.InputHandler != nil {
			app.InputHandler(app.Window, deltaTime)
		} else {
			input.DefaultInput(app.Window, deltaTime)
		}

		b.BeginFrame()

		// One pass-scoped block, refilled and rebound as each pass begins
		var f renderer.FrameUniforms
		f.FarPlane = farPlane

		if s != nil {
			s.FillFrameUniforms(&f)

			// Bake one shadow pass per casting light. Non-casters own no depth
			// target and are lit unshadowed in the main pass, and the sun's
			// pass leaves its light-space matrix in f for the main pass
			dirCaster, pointCaster := s.ShadowCasters()
			for _, i := range [2]int32{dirCaster, pointCaster} {
				if i >= 0 {
					s.Lights[i].RenderShadowMap(nearPlane, farPlane, depthShader, depthCubeShader, s, &f)
				}
			}
		}

		// Run the main pass, the only one that clears color
		b.BeginPass(0, settings.WindowWidth, settings.WindowHeight,
			&[4]float32{0.1, 0.1, 0.1, 1.0})

		if s != nil {
			s.RenderSkybox(skyboxShader, &f)
			s.RenderScene(forwardShader, &f)
		} else {
			// No scene binds the block, but the overlay's draw still pushes its
			// address, so it has to point at something valid
			b.BindFrameUniforms(&f)
		}

		renderUI(app, widget, uiShader, uiQuad)

		b.EndPass()
		b.EndFrame()

		// Advance the clock and print the FPS once a second
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
