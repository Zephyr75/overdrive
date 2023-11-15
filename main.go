package main

import (
	"os"
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"

	"overdrive/input"

	"overdrive/opengl"
	"overdrive/scene"
	"overdrive/settings"
  // "math"
)


func init() {
	// GLFW event handling must run on the main OS thread
	runtime.LockOSThread()
}

func main() {

  // GLFW setup
  glfw.Init()
  glfw.WindowHint(glfw.ContextVersionMajor, 4)
  glfw.WindowHint(glfw.ContextVersionMinor, 1)
  glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
  glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)

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

  // Declare shader programs
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

  cubesProgram, err := opengl.CreateProgram(vertexShaderSource, fragmentShaderSource)
  if err != nil {
    panic(err)
  }

  // depthVertexShaderFile, err := os.ReadFile("shaders/depth.vert.glsl")
  // if err != nil {
  //   panic(err)
  // }
  // depthVertexShaderSource := string(depthVertexShaderFile) + "\x00"

  // depthFragmentShaderFile, err := os.ReadFile("shaders/depth.frag.glsl")
  // if err != nil {
  //   panic(err)
  // }
  // depthFragmentShaderSource := string(depthFragmentShaderFile) + "\x00"

  // depthProgram, err := opengl.CreateProgram(depthVertexShaderSource, depthFragmentShaderSource)
  // if err != nil {
  //   panic(err)
  // }

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
  lastFrame := 0.0
  var deltaTime float32 = 0.0
  for !window.ShouldClose() {
    input.ProcessInput(window, deltaTime)

    // Render scene from light's perspective
    // nearPlane := float32(1.0)
    // farPlane := float32(7.5)
    // lightProjection := mgl32.Ortho(-10.0, 10.0, -10.0, 10.0, nearPlane, farPlane)
    // lightView := mgl32.LookAtV(s.Lights[0].Pos, mgl32.Vec3{0.0, 0.0, 0.0}, mgl32.Vec3{0.0, 1.0, 0.0})
    // lightSpaceMatrix := lightProjection.Mul4(lightView)

    // gl.UseProgram(depthProgram)
    // lightSpaceMatrixLoc := gl.GetUniformLocation(depthProgram, gl.Str("lightSpaceMatrix\x00"))
    // gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

    // gl.Viewport(0, 0, int32(settings.ShadowWidth), int32(settings.ShadowHeight))
    // gl.BindFramebuffer(gl.FRAMEBUFFER, s.Lights[0].DepthMapFBO)
    // gl.Clear(gl.DEPTH_BUFFER_BIT)
    // for i := 0; i < len(s.Meshes); i++ {
    //   s.Meshes[i].Draw(depthProgram, &s)
    // }
    // gl.BindFramebuffer(gl.FRAMEBUFFER, 0)





    // Render scene as normal
    gl.Viewport(0, 0, int32(settings.WindowWidth), int32(settings.WindowHeight))
    gl.ClearColor(0.1, 0.1, 0.1, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
    
    gl.UseProgram(cubesProgram)
   
    currentFrame := glfw.GetTime()
    deltaTime = float32(currentFrame - lastFrame)
    lastFrame = currentFrame
    // fmt.Println("fps:", 1/deltaTime)
    // fmt.Println("front:", scene.Cam.Front)
    // fmt.Println("light0:", s.Lights[0].Pos)

    // oscillate light position up and down
    // s.Lights[0].Pos[2] = 20 + 20 * float32(math.Sin(float64(glfw.GetTime())))

    view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
    viewLoc := gl.GetUniformLocation(cubesProgram, gl.Str("view\x00"))
    gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

    projection := mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
    projectionLoc := gl.GetUniformLocation(cubesProgram, gl.Str("projection\x00"))
    gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

    model := mgl32.Scale3D(1.0, 1.0, 1.0)
    modelLoc := gl.GetUniformLocation(cubesProgram, gl.Str("model\x00"))
    gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

    // lightSpaceMatrixLoc = gl.GetUniformLocation(depthProgram, gl.Str("lightSpaceMatrix\x00"))
    // gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

    gl.ActiveTexture(gl.TEXTURE1)
    gl.BindTexture(gl.TEXTURE_2D, s.Lights[0].DepthMap)

    for i := 0; i < len(s.Meshes); i++ {
      s.Meshes[i].Draw(cubesProgram, &s)
    }

    window.SwapBuffers()
    glfw.PollEvents()
  }
}
