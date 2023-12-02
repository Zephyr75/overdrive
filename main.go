package main

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
	"image"
	"image/color"

	"github.com/Zephyr75/gutter/ui"
	// "github.com/Zephyr75/gutter/utils"
	"github.com/disintegration/imaging"
)

type App struct {
	Name   string
	Width  int
	Height int
	Window *glfw.Window
}

func init() {
	// GLFW event handling must run on the main OS thread
	runtime.LockOSThread()
}

func main() {

  app := App {
    Name: "Gutter",
    Width: 1920,
    Height: 1080,
  }

	Run(MainWindow, app)

}

func Run(widget func(app App) ui.UIElement, app App) {

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

	// Callbacks
	window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
	window.SetCursorPosCallback(input.MouseCallback)
	window.SetScrollCallback(input.ScrollCallback)
	window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)

	// OpenGL setup
	gl.Init()
	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.CULL_FACE)
	gl.Enable(gl.MULTISAMPLE)
  gl.Enable(gl.BLEND)

	// gl.Enable(gl.FRAMEBUFFER_SRGB)

	// Declare main shader programs
	vertexShaderFile, err := os.ReadFile("shaders/cubes.vert.glsl")
	if err != nil {
		panic(err)
	}
	vertexShaderSource := string(vertexShaderFile) + "\x00"

	fragmentShaderFile, err := os.ReadFile("shaders/cubes.frag.glsl")
	if err != nil {
		panic(err)
	}
	fragmentShaderSource := string(fragmentShaderFile) + "\x00"

	cubesProgram, err := opengl.CreateProgram(vertexShaderSource, fragmentShaderSource, "")
	if err != nil {
		panic(err)
	}

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
	if err != nil {
		panic(err)
	}
	depthVertexShaderSource := string(depthVertexShaderFile) + "\x00"

	depthFragmentShaderFile, err := os.ReadFile("shaders/depth_cube.frag.glsl")
	if err != nil {
		panic(err)
	}
	depthFragmentShaderSource := string(depthFragmentShaderFile) + "\x00"

	geometryShaderFile, err := os.ReadFile("shaders/depth_cube.geo.glsl")
	if err != nil {
		panic(err)
	}
	geometryShaderSource := string(geometryShaderFile) + "\x00"

	depthCubeProgram, err := opengl.CreateProgram(depthVertexShaderSource, depthFragmentShaderSource, geometryShaderSource)
	if err != nil {
		panic(err)
	}

	// Declare debug shader programs
	depthDebugVertexShaderFile, err := os.ReadFile("shaders/depth_debug.vert.glsl")
	if err != nil {
		panic(err)
	}
	depthDebugVertexShaderSource := string(depthDebugVertexShaderFile) + "\x00"

	depthDebugFragmentShaderFile, err := os.ReadFile("shaders/depth_debug.frag.glsl")
	if err != nil {
		panic(err)
	}
	depthDebugFragmentShaderSource := string(depthDebugFragmentShaderFile) + "\x00"

	depthDebugProgram, err := opengl.CreateProgram(depthDebugVertexShaderSource, depthDebugFragmentShaderSource, "")
	if err != nil {
		panic(err)
	}

	gl.UseProgram(depthDebugProgram)
	depthMapLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("depthMap\x00"))
	gl.Uniform1i(depthMapLoc, 0)

	// Declare skybox shader programs
	skyboxVertexShaderFile, err := os.ReadFile("shaders/skybox.vert.glsl")
	if err != nil {
		panic(err)
	}
	skyboxVertexShaderSource := string(skyboxVertexShaderFile) + "\x00"

	skyboxFragmentShaderFile, err := os.ReadFile("shaders/skybox.frag.glsl")
	if err != nil {
		panic(err)
	}
	skyboxFragmentShaderSource := string(skyboxFragmentShaderFile) + "\x00"

	skyboxProgram, err := opengl.CreateProgram(skyboxVertexShaderSource, skyboxFragmentShaderSource, "")
	if err != nil {
		panic(err)
	}

	// Create debug plane
	planeVertices := []float32{
		// positions         // normals      // texcoords
		25.0, -0.5, 25.0, 0.0, 1.0, 0.0, 25.0, 0.0,
		-25.0, -0.5, 25.0, 0.0, 1.0, 0.0, 0.0, 0.0,
		-25.0, -0.5, -25.0, 0.0, 1.0, 0.0, 0.0, 25.0,

		25.0, -0.5, 25.0, 0.0, 1.0, 0.0, 25.0, 0.0,
		-25.0, -0.5, -25.0, 0.0, 1.0, 0.0, 0.0, 25.0,
		25.0, -0.5, -25.0, 0.0, 1.0, 0.0, 25.0, 25.0,
	}
	var planeVAO uint32
	var planeVBO uint32
	gl.GenVertexArrays(1, &planeVAO)
	gl.GenBuffers(1, &planeVBO)
	gl.BindVertexArray(planeVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, planeVBO)
	gl.BufferData(gl.ARRAY_BUFFER, len(planeVertices), gl.Ptr(planeVertices), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(3*4))
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))
	gl.BindVertexArray(0)

	// Load scene
	var s scene.Scene = scene.LoadScene("assets/untitled.xml")

	// fmt.Println(s.Meshes[0].Vertices)

	for i := 0; i < len(s.Meshes); i++ {
		s.Meshes[i].Setup()
	}
	for i := 0; i < len(s.Lights); i++ {
		s.Lights[i].Setup()
	}

	

	// Window lifecycle

	lastInstance := ""
	var flippedImg *image.NRGBA

	lastMap := map[string]bool{}
	areas := []ui.Area{}

	i := 0
	time := glfw.GetTime()
	lastFrame := float64(0.0)
	var deltaTime float32 = 0.0
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

		farPlaneLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("far_plane\x00"))
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

		// Render debug plane
		// gl.UseProgram(depthDebugProgram)
		// nearPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("near_plane\x00"))
		// gl.Uniform1f(nearPlaneLoc, nearPlane)
		// farPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("far_plane\x00"))
		// gl.Uniform1f(farPlaneLoc, farPlane)
		// gl.ActiveTexture(gl.TEXTURE0)
		// gl.BindTexture(gl.TEXTURE_2D, s.Lights[0].DepthMap)
		// gl.BindVertexArray(planeVAO)
		// renderQuad()

    // Render UI


    var texture uint32
    {
      gl.GenTextures(1, &texture)

      gl.BindTexture(gl.TEXTURE_2D, texture)
      gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
      gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
      gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
      gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)

      gl.BindImageTexture(0, texture, 0, false, 0, gl.WRITE_ONLY, gl.RGBA8)
    }

    // var framebuffer uint32
    // {
    //   gl.GenFramebuffers(1, &framebuffer)
    //   gl.BindFramebuffer(gl.FRAMEBUFFER, framebuffer)
    //   gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0)

    //   gl.BindFramebuffer(gl.READ_FRAMEBUFFER, framebuffer)
    //   gl.BindFramebuffer(gl.DRAW_FRAMEBUFFER, 0)
    // }

    w := settings.WindowWidth
    h := settings.WindowHeight

    var img = image.NewRGBA(image.Rect(0, 0, w, h))
    instance := widget(app)

    equal := true
    for _, area := range areas {
      if ui.MouseInBounds(window, area) != lastMap[area.ToString()] {
        equal = false
      }
      if ui.MouseInBounds(window, area) && window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
        area.Function()
      }
    }

    if lastInstance != instance.ToString() || !equal {
      lastInstance = instance.ToString()
      areas = instance.Draw(img, window)
      // Remove all empty areas
      newAreas := []ui.Area{}
      for _, area := range areas {
        if area.Left != 0 || area.Right != 0 || area.Top != 0 || area.Bottom != 0 {
          newAreas = append(newAreas, area)
        }
      }
      areas = newAreas
      flippedImg = imaging.FlipV(img)
    }
    for _, area := range areas {
      lastMap[area.ToString()] = ui.MouseInBounds(window, area)
    }

    gl.BindTexture(gl.TEXTURE_2D, texture)
    gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(h), 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(flippedImg.Pix))

    // gl.BlitFramebuffer(0, 0, int32(w), int32(h), 0, 0, int32(w), int32(h), gl.COLOR_BUFFER_BIT, gl.LINEAR)

    gl.UseProgram(depthDebugProgram)
		nearPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("near_plane\x00"))
		gl.Uniform1f(nearPlaneLoc, nearPlane)
		// farPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("far_plane\x00"))
		// gl.Uniform1f(farPlaneLoc, farPlane)
		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, texture)
		gl.BindVertexArray(planeVAO)
    renderQuad()
		// Render scene as normal
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

		farPlaneLoc = gl.GetUniformLocation(cubesProgram, gl.Str("far_plane\x00"))
		gl.Uniform1f(farPlaneLoc, farPlane)

		for i := 0; i < len(s.Meshes); i++ {
			s.Meshes[i].Draw(cubesProgram, &s)
		}

		// Render skybox
		gl.DepthFunc(gl.LEQUAL)
		gl.UseProgram(skyboxProgram)

		view = mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
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





    // Compute delta time
		currentFrame := glfw.GetTime()
		deltaTime = float32(currentFrame - lastFrame)
		lastFrame = currentFrame

		i++
		if glfw.GetTime()-time > 1 {
			fmt.Printf("\rFPS: %d", i)
			i = 0
			time = glfw.GetTime()
		}
		window.SwapBuffers()
		glfw.PollEvents()
	}
}

var (
	quadVAO uint32
	quadVBO uint32
)

func renderQuad() {
	if quadVAO == 0 {
		quadVertices := []float32{
			// positions     // texCoords
			-1.0, 1.0, 0.0, 0.0, 1.0,
			-1.0, -1.0, 0.0, 0.0, 0.0,
			1.0, 1.0, 0.0, 1.0, 1.0,
			1.0, -1.0, 0.0, 1.0, 0.0,
		}
		gl.GenVertexArrays(1, &quadVAO)
		gl.GenBuffers(1, &quadVBO)
		gl.BindVertexArray(quadVAO)
		gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
		gl.BufferData(gl.ARRAY_BUFFER, len(quadVertices)*4, gl.Ptr(quadVertices), gl.STATIC_DRAW)
		var stride int32 = 5 * 4
		gl.EnableVertexAttribArray(0)
		gl.VertexAttribPointer(0, 3, gl.FLOAT, false, stride, gl.PtrOffset(0))
		gl.EnableVertexAttribArray(1)
		gl.VertexAttribPointer(1, 2, gl.FLOAT, false, stride, gl.PtrOffset(3*4))
	}
	gl.BindVertexArray(quadVAO)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
	gl.BindVertexArray(0)

}

/////////////

var (
	counter int = 10
)

func MainWindow(app App) ui.UIElement {
	return ui.Row{
		Style: ui.Style{
			Color: color.Transparent,
		},
		Children: []ui.UIElement{
			ui.Button{
				Properties: ui.Properties{
					Alignment: ui.AlignmentTop,
					Size: ui.Size{
						Scale:  ui.ScalePixel,
						Width:  100,
						Height: 100,
					},
				},
				Function: func() {
					Quit()
				},
				Style: ui.Style{
					Color: green,
				},
			},
			ui.Column{
				Properties: ui.Properties{
					Padding: ui.PaddingSideBySide(ui.ScaleRelative, 0, 25, 25, 0),
				},
				Style: ui.Style{
					Color: color.White,
				},
				Children: []ui.UIElement{
					ui.Button{
						Properties: ui.Properties{
							Size: ui.Size{
								Scale:  ui.ScaleRelative,
								Width:  50,
								Height: 50,
							},
						},
						Function: func() {
							counter += 1
						},
						Style: ui.Style{
							Color: green,
						},
						// Image:      "white_on_black.png",
						// HoverImage: "black_on_white.png",
					},
					ui.Button{
						Properties: ui.Properties{
							Size: ui.Size{
								Scale:  ui.ScaleRelative,
								Width:  50,
								Height: 100,
							},
						},
						Function: func() {
							counter -= 1
						},
						Style: ui.Style{
							Color: red,
							// BorderColor: white,
							// BorderWidth: 10,
							// CornerRadius: 25,
						},
						Child: ui.Text{
							Properties: ui.Properties{
								Alignment: ui.AlignmentTopLeft,
								//Padding:   ui.PaddingEqual(ui.ScalePixel, 100),
								Size: ui.Size{
									Scale:  ui.ScalePixel,
									Width:  100,
									Height: 50,
								},
							},
							StyleText: ui.StyleText{
								// Font:      "Comfortaa.ttf",
								FontSize:  counter,
								FontColor: black,
							},
						},
						// Image:      "white_on_black.png",
						// HoverImage: "black_on_white.png",
					},
					ui.Container{
						Properties: ui.Properties{
							Size: ui.Size{
								Scale:  ui.ScaleRelative,
								Width:  50,
								Height: 50,
							},
						},
						Style: ui.Style{
							// BorderWidth: 10,
							// BorderColor: white,
							// CornerRadius: 25,
							Color: color.Transparent,
							// ShadowWidth: 10,
							// ShadowAlignment: ui.AlignmentBottom,
						},
						// Image: "white_on_black.png",
					},
				},
			},
			ui.Container{
				Style: ui.Style{
					Color: red,
				},
				Child: ui.Text{
					Properties: ui.Properties{
						Alignment: ui.AlignmentTopLeft,
						//Padding:   ui.PaddingEqual(ui.ScalePixel, 100),
						Size: ui.Size{
							Scale:  ui.ScalePixel,
							Width:  100,
							Height: 50,
						},
					},
					StyleText: ui.StyleText{
						// Font:      "Comfortaa.ttf",
						FontSize:  counter,
						FontColor: black,
					},
				},
			},
		},
	}
}

func Quit() {
	// app.Window.SetShouldClose(true)
}

var (
	green = color.RGBA{158, 206, 106, 255}
	white = color.RGBA{192, 202, 245, 255}
	blue  = color.RGBA{122, 162, 247, 255}
	red   = color.RGBA{247, 118, 142, 255}
	black = color.RGBA{26, 27, 38, 255}
  transparent = color.RGBA{0, 0, 0, 0}
)
