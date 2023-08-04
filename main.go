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


  var s scene.Scene = scene.LoadScene("assets/untitled.xml")

  // fmt.Println(s.Meshes[0].Vertices)

  for i := 0; i < len(s.Meshes); i++ {
    s.Meshes[i].Setup()
  }

 
  // Window lifecycle
  lastFrame := 0.0
  var deltaTime float32 = 0.0
  for !window.ShouldClose() {
    input.ProcessInput(window, deltaTime)

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

    for i := 0; i < len(s.Meshes); i++ {
      s.Meshes[i].Draw(cubesProgram, &s)
    }

    window.SwapBuffers()
    glfw.PollEvents()
  }
}
