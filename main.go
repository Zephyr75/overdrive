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

  // vertices := []float32{
  //   // positions         // normal          // texture coords
  //    0.5,  0.5,  0.5,    1.0,  1.0,  1.0,   1.0, 1.0,
  //    0.5, -0.5,  0.5,    1.0, -1.0,  1.0,   1.0, 0.0,
  //   -0.5, -0.5,  0.5,   -1.0, -1.0,  1.0,   0.0, 0.0,
  //   -0.5,  0.5,  0.5,   -1.0,  1.0,  1.0,   0.0, 1.0,
  //    0.5,  0.5, -0.5,    1.0,  1.0, -1.0,   0.0, 0.0,
  //    0.5, -0.5, -0.5,    1.0, -1.0, -1.0,   0.0, 1.0,
  //   -0.5, -0.5, -0.5,   -1.0, -1.0, -1.0,   1.0, 1.0,
  //   -0.5,  0.5, -0.5,   -1.0,  1.0, -1.0,   1.0, 0.0,
  // }


  // lightPositions := []mgl32.Vec3{
  //   {-1.7,  3.0, -7.5},
  //   { 1.3, -2.0, -2.5},
  //   { 1.5,  2.0, -2.5},
  //   { 1.5,  0.2, -1.5},
  //   {-1.3,  1.0, -1.5},
  // }

  // indices := []uint32{
  //   0, 1, 3,
  //   1, 2, 3,
  //   0, 1, 4,
  //   1, 4, 5,
  //   0, 3, 4,
  //   3, 4, 7,
  //   1, 2, 5,
  //   2, 5, 6,
  //   2, 3, 6,
  //   3, 6, 7,
  //   4, 5, 6,
  //   4, 6, 7,

  // }

  // DECLARE SHADERS
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

  // vertexShaderFile, err = os.ReadFile("shaders/light.vert.glsl")
  // if err != nil {
  //   panic(err)
  // }
  // vertexShaderSource = string(vertexShaderFile) + "\x00"

  // fragmentShaderFile, err = os.ReadFile("shaders/light.frag.glsl")
  // if err != nil {
  //   panic(err)
  // }
  // fragmentShaderSource = string(fragmentShaderFile) + "\x00"
  // lightsProgram, err := opengl.CreateProgram(vertexShaderSource, fragmentShaderSource)



  // // Declare VBO and EBO
  // var EBO uint32
  // gl.GenBuffers(1, &EBO)
  // var VBO uint32
  // gl.GenBuffers(1, &VBO)

  // // Declare main VAO
  // var cubesVAO uint32
  // gl.GenVertexArrays(1, &cubesVAO)

  // // Bind VAO to VBO and gl.VertexAttribPointer, gl.EnableVertexAttribArray calls
  // gl.BindVertexArray(cubesVAO)
  // // Copy VBO to an OpenGL buffer
  // gl.BindBuffer(gl.ARRAY_BUFFER, VBO)
  // // Define OpenGL buffer structure
  // gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)
  // // Copy EBO to an OpenGL buffer
  // gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, EBO)
  // // Define OpenGL buffer structure
  // gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices)*4, gl.Ptr(indices), gl.STATIC_DRAW)

  // // Define Vertex Attrib to be used by the shader
  // gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
  // gl.EnableVertexAttribArray(0)
  // gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(3*4))
  // gl.EnableVertexAttribArray(1)
  // gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))
  // gl.EnableVertexAttribArray(2)
  // 
  // // Declare lights VAO
  // var lightsVAO uint32
  // gl.GenVertexArrays(1, &lightsVAO)

  // // Bind VAO to VBO and gl.VertexAttribPointer, gl.EnableVertexAttribArray calls
  // gl.BindVertexArray(lightsVAO)
  // // Copy VBO to an OpenGL buffer
  // gl.BindBuffer(gl.ARRAY_BUFFER, VBO)
  // // Copy EBO to an OpenGL buffer
  // gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, EBO)
  // // Define Vertex Attrib to be used by the shader
  // gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
  // gl.EnableVertexAttribArray(0)

  
  // Reset OpenGL buffers
  // gl.BindBuffer(gl.ARRAY_BUFFER, 0)
  // gl.BindVertexArray(0)


  // DECLARE TEXTURES
  // texture, err := opengl.CreateTexture("textures/square.png")

 
  // FULL WINDOW LIFECYCLE
  lastFrame := 0.0
  var deltaTime float32 = 0.0
  for !window.ShouldClose() {
    input.ProcessInput(window, deltaTime)

    gl.ClearColor(0.2, 0.3, 0.3, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
    
    // gl.BindTexture(gl.TEXTURE_2D, texture)

    
    gl.UseProgram(cubesProgram)
   
    // gl.BindVertexArray(cubesVAO)

    currentFrame := glfw.GetTime()
    deltaTime = float32(currentFrame - lastFrame)
    lastFrame = currentFrame



    view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
    viewLoc := gl.GetUniformLocation(cubesProgram, gl.Str("view\x00"))
    gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])


    projection := mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
    projectionLoc := gl.GetUniformLocation(cubesProgram, gl.Str("projection\x00"))
    gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

    model := mgl32.Scale3D(5.0, 1.0, 1.0)
    modelLoc := gl.GetUniformLocation(cubesProgram, gl.Str("model\x00"))
    gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

    // lightColorLoc := gl.GetUniformLocation(cubesProgram, gl.Str("lightColor\x00"))
    // gl.Uniform3f(lightColorLoc, 1.0, 0.0, 1.0)

    // lightPosLoc := gl.GetUniformLocation(cubesProgram, gl.Str("lightPos\x00"))
    // gl.Uniform3f(lightPosLoc, 1.2, float32(glfw.GetTime()) - 5.0, 1.0)

    // viewPosLoc := gl.GetUniformLocation(cubesProgram, gl.Str("viewPos\x00"))
    // gl.Uniform3f(viewPosLoc, scene.Cam.Pos.X(), scene.Cam.Pos.Y(), scene.Cam.Pos.Z())

    // gl.DrawElements(gl.TRIANGLES, 36, gl.UNSIGNED_INT, gl.PtrOffset(0))







    // Draw lights

    // gl.UseProgram(lightsProgram)

    // gl.BindVertexArray(lightsVAO)


    // view = mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
    // viewLoc = gl.GetUniformLocation(lightsProgram, gl.Str("view\x00"))
    // gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])


    // projection = mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
    // projectionLoc = gl.GetUniformLocation(lightsProgram, gl.Str("projection\x00"))
    // gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

    // model = mgl32.Translate3D(1.2, float32(glfw.GetTime()) - 5.0, 1.0)
    // modelLoc = gl.GetUniformLocation(lightsProgram, gl.Str("model\x00"))
    // gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

    // gl.DrawElements(gl.TRIANGLES, 36, gl.UNSIGNED_INT, gl.PtrOffset(0))



    // gl.DrawArrays(gl.TRIANGLES, 0, 36)
    window.SwapBuffers()
    glfw.PollEvents()
  }

}
