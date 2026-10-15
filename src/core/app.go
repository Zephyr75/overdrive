package core

import (
	"fmt"
	"runtime"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/gameobject"
	"github.com/Zephyr75/overdrive/input"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/scene"
	"github.com/Zephyr75/overdrive/settings"
	"github.com/Zephyr75/overdrive/utils"
	"github.com/Zephyr75/overdrive/vulkan"
)

type App struct {
	Name           string
	Width          int
	Height         int
	Window         *glfw.Window
	Backend        renderer.Backend
	ScreenshotFile string // when set, one frame is captured and saved to a file with the given name
	InputHandler   func(window *glfw.Window, deltaTime float32)
	MouseCallback  func(window *glfw.Window, x float64, y float64)
	// Seconds physics and game objects hold still after the first frame, so a
	// showcase can be seen before it moves; a game sets 0
	StartDelay float64
}

// Pins the package to the main OS thread, where GLFW event handling must run
func init() {
	runtime.LockOSThread()
}

// Asks the window to close, ending the frame loop after the current iteration
func (app App) Quit() {
	app.Window.SetShouldClose(true)
}

// Creates the backend, the window and its input callbacks, then initialises the backend on that window
func NewApp(name string, width int, height int, inputHandler func(window *glfw.Window, deltaTime float32), mouseCallback func(window *glfw.Window, x float64, y float64)) App {
	app := App{
		Name:          name,
		Width:         width,
		Height:        height,
		MouseCallback: mouseCallback,
		InputHandler:  inputHandler,
		StartDelay:    physicsStartDelay,
	}

	// Create the backend before the window, so it can set its own hints
	app.Backend = vulkan.New()

	// Initialize GLFW and verify Vulkan support
	glfw.Init()
	if !glfw.VulkanSupported() {
		utils.HandleError(fmt.Errorf("GLFW reports no Vulkan loader"))
	}
	// Set the GLFW window to use no API, indicating that we will handle rendering ourselves
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)

	// Create the GLFW window with specified width, height, and title
	window, err := glfw.CreateWindow(settings.Current.Window.Width, settings.Current.Window.Height, name, nil, nil)
	if err != nil {
		glfw.Terminate()
	}
	app.Window = window

	// Wire the input callbacks, falling back to the built-in handlers
	window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
	window.SetScrollCallback(input.ScrollCallback)
	if !settings.Current.Debug.LockCamera {
		if app.MouseCallback != nil {
			window.SetCursorPosCallback(app.MouseCallback)
		} else {
			window.SetCursorPosCallback(input.DefaultMouseCallback)
		}
		window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
	}

	// The backbuffer sample count is resolved against the device's limits and
	// read back through Capacities: the images at that sample count are built
	// here, not in the backend
	samples := 1
	if settings.IsMSAAEnabled() {
		samples = settings.Current.AntiAliasing.Samples
	}

	utils.HandleError(app.Backend.Init(window, renderer.Request{
		Features: []renderer.Feature{renderer.FeatureCompute},
		Samples:  samples,
	}))

	return app
}

const (
	// The most steps one frame runs to catch up; a longer stall is skipped, not replayed
	maxPhysicsSteps = 8
	// StartDelay's default, so the scene can be seen before anything moves
	physicsStartDelay = 5
)

// Builds the pipelines and runs the frame loop until the window closes; the
// physics world and the game objects are optional, either may be nil
func (app App) Run(loadedScene *scene.Scene, widget func(app App) ui.UIElement, bodies *physics.World, objects *gameobject.Game) {
	backend := app.Backend

	pipelines, err := scene.NewPipelines(backend)
	utils.HandleError(err)
	overlay, err := newOverlay(backend)
	utils.HandleError(err)
	occlusion, err := newOcclusionPasses(backend)
	utils.HandleError(err)

	if loadedScene != nil {
		input.SetScene(loadedScene)
	} else {
		emptyScene := scene.EmptyScene()
		input.SetScene(&emptyScene)
	}

	// Init the frame timing
	frames := 0
	// Tiles the last frame baked into each atlas, printed beside the FPS: a
	// static scene must settle at zero, which is what the static/dynamic split is for
	staticBakes, dynamicBakes := 0, 0
	curTime := glfw.GetTime()
	var deltaTime float32 = 0.0
	lastFrame := float64(0.0)

	clearColor := [4]float32{0.1, 0.1, 0.1, 1.0}
	depthClear := [4]float32{1, 0, 0, 0}

	// The depth buffer and, when multisampling, the colour image the main pass
	// resolves out of. Rebuilt whenever the swapchain has resized under them
	var targets screenTargets

	// Late enough for the shadow allocator to have settled; physics counts
	// 1/60 s a frame meanwhile, so two runs photograph the same moment
	var shot *screenshot
	if app.ScreenshotFile != "" {
		shot = &screenshot{path: app.ScreenshotFile, frame: 90}
	}
	frameNo := 0
	captured := false

	// From the first frame rather than from startup, whose clock already
	// includes the whole scene load
	physicsStart := glfw.GetTime() + app.StartDelay
	stepsRun := 0
	// The world's own rate, which the game objects are stepped at too
	rate := physics.DefaultRate
	if bodies != nil {
		rate = bodies.StepRate()
	}
	// Before the first frame, so it draws every object where its Start put it
	if objects != nil {
		objects.Start()
	}

	// Run one iteration per frame until the window closes
	for !app.Window.ShouldClose() {

		// Before Frame, which is where a resize is discovered: the frame that
		// discovers one records nothing, so this is what catches up to it
		targets.rebuildOnResize(backend)

		// The steps that should have run by now, negative while waiting. A
		// screenshot counts 1/60 s a frame instead of the clock, so frame N is
		// the same moment of the simulation on every run and every display
		stepsDue := int((glfw.GetTime() - physicsStart) * float64(rate))
		if shot != nil {
			stepsDue = (frameNo - int(app.StartDelay*60)) * rate / 60
		}
		if stepsDue > stepsRun {
			for range min(stepsDue-stepsRun, maxPhysicsSteps) {
				if objects != nil {
					objects.Step()
				}
				if bodies != nil {
					bodies.Step()
				}
			}
			stepsRun = stepsDue
		}
		if bodies != nil {
			bodies.PlaceAndRotateMeshes()
		}

		loadedScene.UpdateMeshes()

		// Process input before anything is recorded, so the camera is current
		if settings.Current.Debug.LockCamera {
			// Nothing moves the camera
		} else if app.InputHandler != nil {
			app.InputHandler(app.Window, deltaTime)
		} else {
			input.DefaultInput(app.Window, deltaTime)
		}

		// Before the frame: textures the UI introduces are uploaded outside it
		overlay.prepare(app, widget)

		backend.Frame(func(frame renderer.Frame) {
			var frameUniforms renderer.FrameUniforms
			var frameAddr, recordAddr renderer.Address
			var reads []renderer.Handle

			if loadedScene != nil {
				// Allocate this frame's tiles first: FillFrameUniforms copies each
				// light's record index out of it, and the bake walks the same tiles
				loadedScene.UpdateShadows(settings.Current.Shadows.NearPlane, settings.Current.Shadows.FarPlane)
				loadedScene.FillFrameUniforms(&frameUniforms)
				reads = loadedScene.ShadowImages()
			}
			// Without the prepass there is no depth to occlude from before the main pass
			prepass := loadedScene != nil && !settings.Current.Debug.NoPrepass
			ao := settings.Current.AmbientOcclusion
			runAO := prepass && ao.Enabled
			occlusion.fillUniforms(backend, &targets, &frameUniforms, runAO, ao.Radius, ao.Intensity)

			// One upload for the whole frame: the prepass and the forward pass
			// read the same bytes, which is what an EQUAL depth test needs
			frameAddr = frame.Upload(&frameUniforms)

			if loadedScene != nil {
				recordAddr = frame.Upload(loadedScene.ShadowTiles())

				// A settled scene runs neither bake pass. The check is not an
				// optimisation: the static pass clears its atlas, so opening it
				// with nothing to draw would erase every static tile
				if loadedScene.HasStaticBakes() {
					frame.Pass(renderer.PassSpec{
						Name:  "shadowStatic",
						Depth: &renderer.Attachment{View: loadedScene.StaticAtlasView(), Clear: &depthClear, Store: true},
					}, func(pass renderer.Pass) {
						loadedScene.RenderStaticBakes(frame, pass, pipelines)
					})
				}
				// Between the two passes: each dynamic tile starts as the static
				// tile just baked, and the dynamic pass draws over it
				loadedScene.InitDynamicTiles(frame)
				// No clear, or it would erase the tiles just copied in
				if loadedScene.HasDynamicBakes() {
					frame.Pass(renderer.PassSpec{
						Name:  "shadowDynamic",
						Depth: &renderer.Attachment{View: loadedScene.DynamicAtlasView(), Store: true},
					}, func(pass renderer.Pass) {
						loadedScene.RenderDynamicBakes(frame, pass, pipelines)
					})
				}
				staticBakes, dynamicBakes = loadedScene.BakeCounts()
			}

			// Depth first, so the forward pass shades each visible fragment once
			// rather than once per surface drawn over it
			if prepass {
				frame.Pass(renderer.PassSpec{
					Name:  "depthPrepass",
					Depth: targets.prepassDepth(&depthClear),
					FlipY: true,
				}, func(pass renderer.Pass) {
					loadedScene.RenderDepth(frame, pass, pipelines, frameAddr)
				})
			}
			// Between the prepass and the main pass, which samples the result
			if runAO {
				occlusion.record(frame, &targets, frameAddr)
				reads = append(reads, targets.ao)
			}

			// The main pass keeps the depth the prepass left, which is what the
			// EQUAL test in the forward pipeline compares against
			depth := renderer.Attachment{View: targets.depthView}
			if !prepass {
				depth.Clear = &depthClear
			}
			frame.Pass(renderer.PassSpec{
				Name:  "main",
				Color: []renderer.Attachment{targets.colorAttachment(&clearColor)},
				Depth: &depth,
				Reads: reads,
				FlipY: true,
			}, func(pass renderer.Pass) {
				if loadedScene != nil {
					loadedScene.RenderSkybox(frame, pass, pipelines, &frameUniforms)
					loadedScene.RenderScene(frame, pass, pipelines, frameAddr, recordAddr)
				}
				overlay.draw(frame, pass)
			})

			captured = shot.record(backend, frame, frameNo)
		})
		frameNo++

		if captured {
			utils.HandleError(shot.write(backend))
			fmt.Printf("\nwrote %s\n", shot.path)
			app.Window.SetShouldClose(true)
		}

		// Advance the clock and print the FPS once a second
		frames++
		deltaTime = float32(glfw.GetTime()) - float32(lastFrame)
		lastFrame = glfw.GetTime()
		if glfw.GetTime()-curTime > 1 {
			fmt.Printf("\rFPS: %d  bakes: %d static %d dynamic   ", frames, staticBakes, dynamicBakes)
			frames = 0
			curTime = glfw.GetTime()
		}

		glfw.PollEvents()
	}
	targets.destroy(backend)
	backend.Shutdown()
	glfw.Terminate()
}
