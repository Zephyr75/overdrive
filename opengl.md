# GLFW : window manager

`glfw.Init()` initialize GLFW  

`glfw.WindowHint(glfw.Resizable, glfw.False)` set GLFW parameter
> All parameters available at [GLFW documentation](https://www.glfw.org/docs/latest/window.html#window_hints)

```go
// FULL SETUP
glfw.Init()
glfw.WindowHint(glfw.ContextVersionMajor, 4)
glfw.WindowHint(glfw.ContextVersionMinor, 1)
glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
```

`glfw.CreateWindow(800, 600, "LearnOpenGL", nil [monitor], nil [window])` create window

`glfw.Terminate()` terminate GLFW

`window.MakeContextCurrent()` activate window context

```go
// FULL WINDOW DEFINITION
window, err := glfw.CreateWindow(windowWidth, windowHeight, "Cube", nil, nil)
if err != nil {
    glfw.Terminate()
}
window.MakeContextCurrent()
```

`window.ShouldClose()` detects close request

`window.SwapBuffers()` swap current and next buffers

`glfw.PollEvents()` get inputs

```go
// FULL WINDOW LIFECYCLE
for !window.ShouldClose() {
    window.SwapBuffers()
    glfw.PollEvents()
}
```

`window.GetKey(glfw.KeyEscape)` get state of given key

`window.SetShouldClose(true)` send close request

```go
// CLOSE ON ESC KEY PRESSED
if window.GetKey(glfw.KeyEscape) == glfw.Press {
    window.SetShouldClose(true)
}
```


# OpenGL

## Setup

`gl.Init()` setup OpenGL

## Generic

`gl.ClearColor(0.2, 0.3, 0.3, 1.0)` set background color

`gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)` clear buffer

## Storing vertices

> Vertex Buffer Object stores vertices

`var VBO uint32` declare buffer

`gl.GenBuffers(1, &VBO)` set alias for buffer

`gl.BindBuffer(gl.ARRAY_BUFFER, VBO)` set buffer type

`gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)` set buffer structure

## Shader

 var vertexShaderSource = `
    #version 410 core
    layout (location = 0) in vec3 aPos;
    void main()
    {
      gl_Position = vec4(aPos.x, aPos.y, aPos.z, 1.0);
    }
`
`gl.CreateShader(shaderType)` create shader

`gl.ShaderSource(shader, 1, csources, nil)` set shader source code

`gl.CompileShader(shader)` compile shader

`gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)` get shader information

## Program

`gl.CreateProgram()` create program

`gl.AttachShader(program, vertexShader)` assign shader to program

`gl.LinkProgram(program)` link program shaders together

`gl.UseProgram()` use program

`gl.GetProgramiv(program, gl.LINK_STATUS, &status)` get program information

`gl.DeleteShader(vertexShader)` delete shader once linked

## Interpret buffer data

`gl.GetAttribLocation(program, gl.Str("vert\x00"))` get matching part of program

`gl.EnableVertexAttribArray(vertAttrib)` enable vertex attribute

TODO: describe parameters

`gl.VertexAttribPointerWithOffset(vertAttrib, 3, gl.FLOAT, false, 5*4, 0)` 

```go
vertAttrib := uint32(gl.GetAttribLocation(program, gl.Str("vert\x00")))
gl.EnableVertexAttribArray(vertAttrib)
gl.VertexAttribPointerWithOffset(vertAttrib, 3, gl.FLOAT, false, 5*4, 0)
```

## Store buffer config

gl.GenVertexArrays(1, &VAO)

gl.BindVertexArray(VAO)



