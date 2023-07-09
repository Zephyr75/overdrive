package main

import (
	"fmt"
	// "go/build"
	// "image"
	// "image/draw"
	_ "image/png"
	// "log"
	// "os"
	"runtime"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	// "github.com/go-gl/mathgl/mgl32"
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
    -0.5, -0.5, 0.0, // left
    0.5, -0.5, 0.0, // right
    0.0,  0.5, 0.0,  // top
  }


  var vertexShaderSource = `
    #version 330 core
    layout (location = 0) in vec3 aPos;
    void main()
    {
      gl_Position = vec4(aPos.x, aPos.y, aPos.z, 1.0);
    }
  ` + "\x00"

  var fragmentShaderSource = `
    #version 330 core
    out vec4 FragColor;

    void main()
    {
        FragColor = vec4(1.0f, 0.5f, 0.2f, 1.0f);
    } 

  ` + "\x00"

  program, err := createProgram(vertexShaderSource, fragmentShaderSource)
  if err != nil {
    panic(err)
  }

  gl.UseProgram(program)


  var VAO uint32
  var VBO uint32
  gl.GenVertexArrays(1, &VAO)
  gl.GenBuffers(1, &VBO)
  gl.BindVertexArray(VAO)
  gl.BindBuffer(gl.ARRAY_BUFFER, VBO)
  gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)

  gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 3*4, gl.PtrOffset(0))
  gl.EnableVertexAttribArray(0)

  
  gl.BindBuffer(gl.ARRAY_BUFFER, 0)
  gl.BindVertexArray(0)





 
  // FULL WINDOW LIFECYCLE
  for !window.ShouldClose() {
    processInput(window)

    gl.ClearColor(0.2, 0.3, 0.3, 1.0)
    gl.Clear(gl.COLOR_BUFFER_BIT)
    



    gl.UseProgram(program)
    gl.BindVertexArray(VAO)
    gl.DrawArrays(gl.TRIANGLES, 0, 3)


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
