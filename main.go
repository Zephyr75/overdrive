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

  // Declare main shader programs
  // vertexShaderFile, err := os.ReadFile("shaders/cubes.vert.glsl")
  // if err != nil { panic(err) }
  // vertexShaderSource := string(vertexShaderFile) + "\x00"

  // fragmentShaderFile, err := os.ReadFile("shaders/cubes.frag.glsl")
  // if err != nil { panic(err) }
  // fragmentShaderSource := string(fragmentShaderFile) + "\x00"

  // cubesProgram, err := opengl.CreateProgram(vertexShaderSource, fragmentShaderSource)
  // if err != nil { panic(err) }

  // Declare depth shader programs
  depthVertexShaderFile, err := os.ReadFile("shaders/depth.vert.glsl")
  if err != nil { panic(err) }
  depthVertexShaderSource := string(depthVertexShaderFile) + "\x00"

  depthFragmentShaderFile, err := os.ReadFile("shaders/depth.frag.glsl")
  if err != nil { panic(err) }
  depthFragmentShaderSource := string(depthFragmentShaderFile) + "\x00"

  depthProgram, err := opengl.CreateProgram(depthVertexShaderSource, depthFragmentShaderSource)
  if err != nil { panic(err) }

  // Declare debug shader programs
  depthDebugVertexShaderFile, err := os.ReadFile("shaders/depth_debug.vert.glsl")
  if err != nil { panic(err) }
  depthDebugVertexShaderSource := string(depthDebugVertexShaderFile) + "\x00"

  depthDebugFragmentShaderFile, err := os.ReadFile("shaders/depth_debug.frag.glsl")
  if err != nil { panic(err) }
  depthDebugFragmentShaderSource := string(depthDebugFragmentShaderFile) + "\x00"

  depthDebugProgram, err := opengl.CreateProgram(depthDebugVertexShaderSource, depthDebugFragmentShaderSource)
  if err != nil { panic(err) }

  gl.UseProgram(depthDebugProgram)
  depthMapLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("depthMap\x00"))
  gl.Uniform1i(depthMapLoc, 0)

  // Create debug plane
  planeVertices := []float32{
    // positions         // normals      // texcoords
     25.0, -0.5,  25.0,  0.0, 1.0, 0.0,  25.0,  0.0,
    -25.0, -0.5,  25.0,  0.0, 1.0, 0.0,   0.0,  0.0,
    -25.0, -0.5, -25.0,  0.0, 1.0, 0.0,   0.0, 25.0,

     25.0, -0.5,  25.0,  0.0, 1.0, 0.0,  25.0,  0.0,
    -25.0, -0.5, -25.0,  0.0, 1.0, 0.0,   0.0, 25.0,
     25.0, -0.5, -25.0,  0.0, 1.0, 0.0,  25.0, 25.0,
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
  // lastFrame := 0.0
  var deltaTime float32 = 0.0
  for !window.ShouldClose() {
    input.ProcessInput(window, deltaTime)
    gl.ClearColor(0.1, 0.1, 0.1, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

    // Render scene from light's perspective
    nearPlane := float32(1.0)
    farPlane := float32(50.0)
    // increase 10 to 20 for a wider angle
    lightProjection := mgl32.Ortho(-10.0, 10.0, -10.0, 10.0, nearPlane, farPlane)
    // lightView := mgl32.LookAtV(mgl32.Vec3{-2.0, 4.0, -1.0}, mgl32.Vec3{0.0, 0.0, 0.0}, mgl32.Vec3{0.0, 1.0, 0.0})
    // println(s.Lights[0].Pos.X(), s.Lights[0].Pos.Y(), s.Lights[0].Pos.Z())
    // lightView := mgl32.LookAtV(s.Lights[0].Pos, mgl32.Vec3{0.0, 0.0, 0.0}, mgl32.Vec3{0.0, 1.0, 0.0})
    lightView := mgl32.LookAtV(s.Lights[0].Pos, s.Lights[0].Pos.Sub(s.Lights[0].Dir), mgl32.Vec3{0.0, 1.0, 0.0}) 
    lightSpaceMatrix := lightProjection.Mul4(lightView)

    gl.UseProgram(depthProgram)

    model := mgl32.Scale3D(1.0, 1.0, 1.0)
    modelLoc := gl.GetUniformLocation(depthProgram, gl.Str("model\x00"))
    gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

    lightSpaceMatrixLoc := gl.GetUniformLocation(depthProgram, gl.Str("lightSpaceMatrix\x00"))
    gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

    gl.Viewport(0, 0, int32(settings.ShadowWidth), int32(settings.ShadowHeight))
    gl.BindFramebuffer(gl.FRAMEBUFFER, s.Lights[0].DepthMapFBO)
    gl.Clear(gl.DEPTH_BUFFER_BIT)
    for i := 0; i < len(s.Meshes); i++ {
      s.Meshes[i].Draw(depthProgram, &s)
    }
    gl.BindFramebuffer(gl.FRAMEBUFFER, 0)


    // Clear buffers
    gl.Viewport(0, 0, int32(settings.WindowWidth), int32(settings.WindowHeight))
    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

    // Render debug plane
    gl.UseProgram(depthDebugProgram)
    nearPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("near_plane\x00"))
    gl.Uniform1f(nearPlaneLoc, nearPlane)
    farPlaneLoc := gl.GetUniformLocation(depthDebugProgram, gl.Str("far_plane\x00"))
    gl.Uniform1f(farPlaneLoc, farPlane)
    gl.ActiveTexture(gl.TEXTURE0)
    gl.BindTexture(gl.TEXTURE_2D, s.Lights[0].DepthMap)
    gl.BindVertexArray(planeVAO)
    renderQuad()

    
    // Render scene as normal
   //  gl.UseProgram(cubesProgram)
   // 
   //  currentFrame := glfw.GetTime()
   //  deltaTime = float32(currentFrame - lastFrame)
   //  lastFrame = currentFrame
   //  // fmt.Println("fps:", 1/deltaTime)

   //  view := mgl32.LookAtV(scene.Cam.Pos, scene.Cam.Pos.Add(scene.Cam.Front), scene.Cam.Up)
   //  viewLoc := gl.GetUniformLocation(cubesProgram, gl.Str("view\x00"))
   //  gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

   //  projection := mgl32.Perspective(mgl32.DegToRad(scene.Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
   //  projectionLoc := gl.GetUniformLocation(cubesProgram, gl.Str("projection\x00"))
   //  gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

   //  model := mgl32.Scale3D(1.0, 1.0, 1.0)
   //  modelLoc := gl.GetUniformLocation(cubesProgram, gl.Str("model\x00"))
   //  gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

   //  lightSpaceMatrixLoc = gl.GetUniformLocation(cubesProgram, gl.Str("lightSpaceMatrix\x00"))
   //  gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

   //  gl.ActiveTexture(gl.TEXTURE1)
   //  gl.BindTexture(gl.TEXTURE_2D, s.Lights[0].DepthMap)

   //  for i := 0; i < len(s.Meshes); i++ {
   //    s.Meshes[i].Draw(cubesProgram, &s)
   //  }

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
      -1.0,  1.0, 0.0, 0.0, 1.0,
      -1.0, -1.0, 0.0, 0.0, 0.0,
       1.0,  1.0, 0.0, 1.0, 1.0,
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
