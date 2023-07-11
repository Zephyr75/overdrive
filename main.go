package main

import (
	"os"
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"

  "overdrive/input"

  "overdrive/settings"
  "overdrive/opengl"
  "overdrive/scene"
)


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
  window, err := glfw.CreateWindow(settings.WindowWidth, settings.WindowHeight, "Cube", nil, nil)
  if err != nil {
      glfw.Terminate()
  }
  window.MakeContextCurrent()

  // CALLBACKS
  window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
  window.SetCursorPosCallback(input.MouseCallback)
  window.SetScrollCallback(input.ScrollCallback)

  // CAPTURE MOUSE
  window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)

  // OPENGL SETUP
  gl.Init()
  gl.Enable(gl.DEPTH_TEST)


  vertices := []float32{
    // positions        // texture coords
     0.5,  0.5,  0.5,   1.0, 1.0, 
     0.5, -0.5,  0.5,   1.0, 0.0,
    -0.5, -0.5,  0.5,   0.0, 0.0,
    -0.5,  0.5,  0.5,   0.0, 1.0, 
     0.5,  0.5, -0.5,   1.0, 1.0, 
     0.5, -0.5, -0.5,   1.0, 0.0,
    -0.5, -0.5, -0.5,   0.0, 0.0,
    -0.5,  0.5, -0.5,   0.0, 1.0, 

  }

  cubePositions := []mgl32.Vec3{
    mgl32.Vec3{ 0.0,  0.0,  0.0}, 
    mgl32.Vec3{ 2.0,  5.0, -15.0},
    mgl32.Vec3{-1.5,  5.0, -2.5},
    mgl32.Vec3{-3.8,  2.0, -12.3},
    mgl32.Vec3{ 2.4,  0.4, -3.5},
    mgl32.Vec3{-1.7,  3.0, -7.5},
    mgl32.Vec3{ 1.3, -2.0, -2.5},
    mgl32.Vec3{ 1.5,  2.0, -2.5},
    mgl32.Vec3{ 1.5,  0.2, -1.5},
    mgl32.Vec3{-1.3,  1.0, -1.5},
  }

  indices := []uint32{
    0, 1, 3,
    1, 2, 3,
    0, 1, 4,
    1, 4, 5,
    0, 3, 4,
    3, 4, 7,
    1, 2, 5,
    2, 5, 6,
    2, 3, 6,
    3, 6, 7,
    4, 5, 6,
    4, 6, 7,

  }

  // DECLARE SHADERS
  vertexShaderFile, err := os.ReadFile("shaders/vert.glsl")
  if err != nil {
    panic(err)
  }
  vertexShaderSource := string(vertexShaderFile) + "\x00"

  fragmentShaderFile, err := os.ReadFile("shaders/frag.glsl")
  if err != nil {
    panic(err)
  }
  fragmentShaderSource := string(fragmentShaderFile) + "\x00"
  program, err := opengl.CreateProgram(vertexShaderSource, fragmentShaderSource)
  if err != nil {
    panic(err)
  }

  gl.UseProgram(program)


  // DECLARE BUFFERS
  var VAO uint32
  var VBO uint32
  var EBO uint32
  gl.GenVertexArrays(1, &VAO)
  gl.GenBuffers(1, &VBO)
  gl.GenBuffers(1, &EBO)

  gl.BindVertexArray(VAO)

  gl.BindBuffer(gl.ARRAY_BUFFER, VBO)
  gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)

  gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, EBO)
  gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices)*4, gl.Ptr(indices), gl.STATIC_DRAW)

  
  gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 5*4, gl.PtrOffset(0))
  gl.EnableVertexAttribArray(0)

  gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 5*4, gl.PtrOffset(3*4))
  gl.EnableVertexAttribArray(1)

  
  // RESET BUFFERS
  gl.BindBuffer(gl.ARRAY_BUFFER, 0)
  gl.BindVertexArray(0)


  // DECLARE TEXTURES
  texture, err := opengl.CreateTexture("textures/square.png")

 
  // FULL WINDOW LIFECYCLE
  lastFrame := 0.0
  var deltaTime float32 = 0.0
  for !window.ShouldClose() {
    input.ProcessInput(window, deltaTime)

    gl.ClearColor(0.2, 0.3, 0.3, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
    
    gl.BindTexture(gl.TEXTURE_2D, texture)

    
    gl.UseProgram(program)
   
    gl.BindVertexArray(VAO)

    currentFrame := glfw.GetTime()
    deltaTime = float32(currentFrame - lastFrame)
    lastFrame = currentFrame



    view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
    viewLoc := gl.GetUniformLocation(program, gl.Str("view\x00"))
    gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])


    projection := mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
    projectionLoc := gl.GetUniformLocation(program, gl.Str("projection\x00"))
    gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])


    for i := 0; i < len(cubePositions); i++ {
      model := mgl32.Translate3D(cubePositions[i][0], cubePositions[i][1], cubePositions[i][2])
      model = model.Mul4(mgl32.HomogRotate3D(float32(glfw.GetTime()) * mgl32.DegToRad(float32(i) * 20.0), mgl32.Vec3{1.0, 0.3, 0.5}))
      modelLoc := gl.GetUniformLocation(program, gl.Str("model\x00"))
      gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])
      // gl.DrawArrays(gl.TRIANGLES, 0, 36)

      gl.DrawElements(gl.TRIANGLES, 36, gl.UNSIGNED_INT, gl.PtrOffset(0))
    }


    window.SwapBuffers()
    glfw.PollEvents()
  }

}
