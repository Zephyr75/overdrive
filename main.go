package main

import (
	"fmt"
	// "go/build"
	"image"
	"image/draw"
	_ "image/png"
	// "log"
	"os"
	"runtime"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	// "github.com/go-gl/mathgl/mgl32"
  // "math"
)

const windowWidth = 800
const windowHeight = 600

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
  window, err := glfw.CreateWindow(windowWidth, windowHeight, "Cube", nil, nil)
  if err != nil {
      glfw.Terminate()
  }
  window.MakeContextCurrent()

  // OPENGL SETUP
  gl.Init()



  vertices := []float32{
    // positions       // colors       // texture coords
     0.5,  0.5, 0.0,   1.0, 0.0, 0.0,//   1.0, 1.0,   // top right
     0.5, -0.5, 0.0,   0.0, 1.0, 0.0,//   1.0, 0.0,   // bottom right
    -0.5, -0.5, 0.0,   0.0, 0.0, 1.0,//   0.0, 0.0,   // bottom left
    -0.5,  0.5, 0.0,   1.0, 1.0, 0.0,//   0.0, 1.0,    // top left 

  }

  indices := []uint32{
    0, 1, 3,
    1, 2, 3,
  }


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
  program, err := createProgram(vertexShaderSource, fragmentShaderSource)
  if err != nil {
    panic(err)
  }

  gl.UseProgram(program)


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

  
  gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 6*4, gl.PtrOffset(0))
  gl.EnableVertexAttribArray(0)

  gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 6*4, gl.PtrOffset(3*4))
  gl.EnableVertexAttribArray(1)

  // gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))
  // gl.EnableVertexAttribArray(2)
  


  
  gl.BindBuffer(gl.ARRAY_BUFFER, 0)
  gl.BindVertexArray(0)



  texture, err := newTexture("textures/cube.png")


 
  // FULL WINDOW LIFECYCLE
  for !window.ShouldClose() {
    processInput(window)

    gl.ClearColor(0.2, 0.3, 0.3, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT)
    
    gl.BindTexture(gl.TEXTURE_2D, texture)

    
    gl.UseProgram(program)
    
    // timeValue := glfw.GetTime()
    // var greenValue float32 = float32((math.Sin(timeValue) / 2.0) + 0.5)
    // vertexColorLocation := gl.GetUniformLocation(program, gl.Str("ourColor\x00"))
    // gl.Uniform4f(vertexColorLocation, 0.0, greenValue, 0.0, 1.0)

    gl.BindVertexArray(VAO)

    // gl.ActiveTexture(gl.TEXTURE0)

    // gl.DrawArrays(gl.TRIANGLES, 0, 3)
    gl.DrawElements(gl.TRIANGLES, 6, gl.UNSIGNED_INT, gl.PtrOffset(0))


    window.SwapBuffers()
    glfw.PollEvents()
  }

}

func processInput(window *glfw.Window) {
  if window.GetKey(glfw.KeyEscape) == glfw.Press {
    window.SetShouldClose(true)
  }
}

func createShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csources, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to compile %v: %v", source, log)
	}

	return shader, nil
}

func createProgram(vertexShaderSource, fragmentShaderSource string) (uint32, error) {
	vertexShader, err := createShader(vertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}

	fragmentShader, err := createShader(fragmentShaderSource, gl.FRAGMENT_SHADER)
	if err != nil {
		return 0, err
	}

	program := gl.CreateProgram()

	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	return program, nil
}

func newTexture(file string) (uint32, error) {
	imgFile, err := os.Open(file)
	if err != nil {
		return 0, fmt.Errorf("texture %q not found on disk: %v", file, err)
	}
	img, _, err := image.Decode(imgFile)
	if err != nil {
		return 0, err
	}

	rgba := image.NewRGBA(img.Bounds())
	if rgba.Stride != rgba.Rect.Size().X*4 {
		return 0, fmt.Errorf("unsupported stride")
	}
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{0, 0}, draw.Src)

	var texture uint32
	gl.GenTextures(1, &texture)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, texture)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexImage2D(
		gl.TEXTURE_2D,
		0,
		gl.RGBA,
		int32(rgba.Rect.Size().X),
		int32(rgba.Rect.Size().Y),
		0,
		gl.RGBA,
		gl.UNSIGNED_BYTE,
		gl.Ptr(rgba.Pix),
  )

  // gl.GenerateMipmap(gl.TEXTURE_2D)

	return texture, nil
}
