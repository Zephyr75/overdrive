package core

import (
	"os"
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"

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

func (app App) Run(widget func(app App) ui.UIElement) {

	// GLFW setup
	glfw.Init()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Samples, 4)

	// Window creation
	window, err := glfw.CreateWindow(settings.WindowWidth, settings.WindowHeight, "Cube", nil, nil)
  app.Window = window
	utils.HandleError(err)
	window.MakeContextCurrent()

	// Callbacks
	window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
  window.SetScrollCallback(input.ScrollCallback)
	// window.SetCursorPosCallback(input.MouseCallback)
	// window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)

	// OpenGL setup
	gl.Init()
	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.CULL_FACE)
	gl.Enable(gl.MULTISAMPLE)
	gl.Enable(gl.BLEND)

	// Declare main shader programs
	vertexShaderFile, err := os.ReadFile("shaders/cubes.vert.glsl")
	utils.HandleError(err)
	vertexShaderSource := string(vertexShaderFile) + "\x00"

	fragmentShaderFile, err := os.ReadFile("shaders/cubes.frag.glsl")
	utils.HandleError(err)
	fragmentShaderSource := string(fragmentShaderFile) + "\x00"

	cubesProgram, err := opengl.CreateProgram(vertexShaderSource, fragmentShaderSource, "")
	utils.HandleError(err)

	// Declare directional depth shader programs
	// depthVertexShaderFile, err := os.ReadFile("shaders/depth.vert.glsl")
	// if err != nil { panic(err) }
	// depthVertexShaderSource := string(depthVertexShaderFile) + "\x00"

	// depthFragmentShaderFile, err := os.ReadFile("shaders/depth.frag.glsl")
	// if err != nil { panic(err) }
	// depthFragmentShaderSource := string(depthFragmentShaderFile) + "\x00"

	// depthProgram, err := opengl.CreateProgram(depthVertexShaderSource, depthFragmentShaderSource)
	// if err != nil { panic(err) }

	// Declare point depth shader programs
	depthVertexShaderFile, err := os.ReadFile("shaders/depth_cube.vert.glsl")
	utils.HandleError(err)
	depthVertexShaderSource := string(depthVertexShaderFile) + "\x00"

	depthFragmentShaderFile, err := os.ReadFile("shaders/depth_cube.frag.glsl")
	utils.HandleError(err)
	depthFragmentShaderSource := string(depthFragmentShaderFile) + "\x00"

	geometryShaderFile, err := os.ReadFile("shaders/depth_cube.geo.glsl")
	utils.HandleError(err)
	geometryShaderSource := string(geometryShaderFile) + "\x00"

	depthCubeProgram, err := opengl.CreateProgram(depthVertexShaderSource, depthFragmentShaderSource, geometryShaderSource)
	utils.HandleError(err)

	// Declare debug shader programs
	// depthDebugVertexShaderFile, err := os.ReadFile("shaders/depth_debug.vert.glsl")
	//  utils.HandleError(err)
	// depthDebugVertexShaderSource := string(depthDebugVertexShaderFile) + "\x00"

	// depthDebugFragmentShaderFile, err := os.ReadFile("shaders/depth_debug.frag.glsl")
	//  utils.HandleError(err)
	// depthDebugFragmentShaderSource := string(depthDebugFragmentShaderFile) + "\x00"

	// depthDebugProgram, err := opengl.CreateProgram(depthDebugVertexShaderSource, depthDebugFragmentShaderSource, "")
	//  utils.HandleError(err)

	// Declare UI shader programs
	uiVertexShaderFile, err := os.ReadFile("shaders/ui.vert.glsl")
	utils.HandleError(err)
	uiVertexShaderSource := string(uiVertexShaderFile) + "\x00"

	uiFragmentShaderFile, err := os.ReadFile("shaders/ui.frag.glsl")
	utils.HandleError(err)
	uiFragmentShaderSource := string(uiFragmentShaderFile) + "\x00"

	uiProgram, err := opengl.CreateProgram(uiVertexShaderSource, uiFragmentShaderSource, "")
	utils.HandleError(err)

	// Declare skybox shader programs
	skyboxVertexShaderFile, err := os.ReadFile("shaders/skybox.vert.glsl")
	utils.HandleError(err)
	skyboxVertexShaderSource := string(skyboxVertexShaderFile) + "\x00"

	skyboxFragmentShaderFile, err := os.ReadFile("shaders/skybox.frag.glsl")
	utils.HandleError(err)
	skyboxFragmentShaderSource := string(skyboxFragmentShaderFile) + "\x00"

	skyboxProgram, err := opengl.CreateProgram(skyboxVertexShaderSource, skyboxFragmentShaderSource, "")
	utils.HandleError(err)

	// Load scene
	var s scene.Scene = scene.LoadScene("assets/untitled.xml")
	for i := 0; i < len(s.Meshes); i++ {
		s.Meshes[i].Setup()
	}
	for i := 0; i < len(s.Lights); i++ {
		s.Lights[i].Setup()
	}

	// Gutter init
	

	// Time init
	i := 0
	time := glfw.GetTime()
	var deltaTime float32 = 0.0
	lastFrame := float64(0.0)

	// Window lifecycle
	for !window.ShouldClose() {
		input.ProcessInput(window, deltaTime)
		gl.ClearColor(0.1, 0.1, 0.1, 1.0)
		gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

		// Render scene from directional light's perspective
		// gl.CullFace(gl.FRONT)
		nearPlane := float32(1.0)
		farPlane := float32(50.0)
		// increase 10 to 20 for a wider angle
		lightProjection := mgl32.Ortho(-10.0, 10.0, -10.0, 10.0, nearPlane, farPlane)
		lightView := mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Sub(s.Lights[0].Dir), mgl32.Vec3{0.0, 1.0, 0.0})
		lightSpaceMatrix := lightProjection.Mul4(lightView)

		// gl.UseProgram(depthProgram)

		// model := mgl32.Scale3D(1.0, 1.0, 1.0)
		// modelLoc := gl.GetUniformLocation(depthProgram, gl.Str("model\x00"))
		// gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

		// lightSpaceMatrixLoc := gl.GetUniformLocation(depthProgram, gl.Str("lightSpaceMatrix\x00"))
		// gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

		// gl.Viewport(0, 0, int32(settings.ShadowWidth), int32(settings.ShadowHeight))
		// gl.BindFramebuffer(gl.FRAMEBUFFER, s.Lights[0].DepthMapFBO)
		// gl.Clear(gl.DEPTH_BUFFER_BIT)
		// for i := 0; i < len(s.Meshes); i++ {
		//   s.Meshes[i].Draw(depthProgram, &s)
		// }
		// gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		// gl.CullFace(gl.BACK)

		// Render scene from point light's perspective
		aspect := float32(settings.ShadowWidth) / float32(settings.ShadowHeight)
		nearPlane = float32(1.0)
		farPlane = float32(25.0)
		shadowProjection := mgl32.Perspective(mgl32.DegToRad(90.0), aspect, nearPlane, farPlane)
		shadowTransforms := []mgl32.Mat4{
			shadowProjection.Mul4(mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Add(mgl32.Vec3{1.0, 0.0, 0.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Add(mgl32.Vec3{-1.0, 0.0, 0.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Add(mgl32.Vec3{0.0, 1.0, 0.0}), mgl32.Vec3{0.0, 0.0, 1.0})),
			shadowProjection.Mul4(mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Add(mgl32.Vec3{0.0, -1.0, 0.0}), mgl32.Vec3{0.0, 0.0, -1.0})),
			shadowProjection.Mul4(mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Add(mgl32.Vec3{0.0, 0.0, 1.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Add(mgl32.Vec3{0.0, 0.0, -1.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
		}

		gl.Viewport(0, 0, int32(settings.ShadowWidth), int32(settings.ShadowHeight))
		gl.BindFramebuffer(gl.FRAMEBUFFER, s.Lights[0].DepthMapFBO)
		gl.Clear(gl.DEPTH_BUFFER_BIT)

		gl.UseProgram(depthCubeProgram)

		farPlaneLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("farPlane\x00"))
		gl.Uniform1f(farPlaneLoc, farPlane)

		lightPosLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("lightPos\x00"))
		gl.Uniform3fv(lightPosLoc, 1, &s.Lights[0].Pos[0])

		model := mgl32.Scale3D(1.0, 1.0, 1.0)
		modelLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("model\x00"))
		gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

		for i := 0; i < 6; i++ {
			shadowTransformLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str(fmt.Sprintf("shadowMatrices[%d]\x00", i)))
			gl.UniformMatrix4fv(shadowTransformLoc, 1, false, &shadowTransforms[i][0])
		}

		skyboxLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("skybox\x00"))
		gl.Uniform1i(skyboxLoc, 3)

		gl.ActiveTexture(gl.TEXTURE3)
		gl.BindTexture(gl.TEXTURE_CUBE_MAP, s.Skybox.Texture)

		for i := 0; i < len(s.Meshes); i++ {
			s.Meshes[i].Draw(depthCubeProgram, &s)
		}

		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)

		// Clear buffers
		gl.Viewport(0, 0, int32(settings.WindowWidth), int32(settings.WindowHeight))
		gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

		///////////////////////
		// Render shadow map //
		///////////////////////

		// gl.UseProgram(depthDebugProgram)
		// nearPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("near_plane\x00"))
		// gl.Uniform1f(nearPlaneLoc, nearPlane)
		// farPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("farPlane\x00"))
		// gl.Uniform1f(farPlaneLoc, farPlane)
		// depthMapLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("depthMap\x00"))
		// gl.Uniform1i(depthMapLoc, 0)
		// gl.ActiveTexture(gl.TEXTURE0)
		// gl.BindTexture(gl.TEXTURE_2D, s.Lights[0].DepthMap)
		// renderQuad()

		//////////////////
		// Render scene //
		//////////////////
		gl.UseProgram(cubesProgram)

		view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
		viewLoc := gl.GetUniformLocation(cubesProgram, gl.Str("view\x00"))
		gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

		projection := mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
		projectionLoc := gl.GetUniformLocation(cubesProgram, gl.Str("projection\x00"))
		gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

		model = mgl32.Scale3D(1.0, 1.0, 1.0)
		modelLoc = gl.GetUniformLocation(cubesProgram, gl.Str("model\x00"))
		gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

		lightSpaceMatrixLoc := gl.GetUniformLocation(cubesProgram, gl.Str("lightSpaceMatrix\x00"))
		gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

		farPlaneLoc = gl.GetUniformLocation(cubesProgram, gl.Str("farPlane\x00"))
		gl.Uniform1f(farPlaneLoc, farPlane)

		for i := 0; i < len(s.Meshes); i++ {
			s.Meshes[i].Draw(cubesProgram, &s)
		}

		///////////////////
		// Render skybox //
		///////////////////
		gl.DepthFunc(gl.LEQUAL)
		gl.UseProgram(skyboxProgram)

		view = view.Mat3().Mat4()
		viewLoc = gl.GetUniformLocation(skyboxProgram, gl.Str("view\x00"))
		gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

		projection = mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
		projectionLoc = gl.GetUniformLocation(skyboxProgram, gl.Str("projection\x00"))
		gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

		skyboxLoc = gl.GetUniformLocation(skyboxProgram, gl.Str("skybox\x00"))
		gl.Uniform1i(skyboxLoc, 0)

		gl.BindVertexArray(s.Skybox.Vao)
		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_CUBE_MAP, s.Skybox.Texture)
		gl.DrawArrays(gl.TRIANGLES, 0, 36)
		gl.BindVertexArray(0)
		gl.DepthFunc(gl.LESS)

    renderUI(app, window, widget, uiProgram)
		

		/////////////////////
		// Time management //
		/////////////////////
		i++
		deltaTime = float32(glfw.GetTime()) - float32(lastFrame)
		lastFrame = glfw.GetTime()
		if glfw.GetTime()-time > 1 {
			fmt.Printf("\rFPS: %d", i)
			i = 0
			time = glfw.GetTime()
		}

		//////////////////
		// Swap buffers //
		//////////////////
		window.SwapBuffers()
		glfw.PollEvents()
	}
}



