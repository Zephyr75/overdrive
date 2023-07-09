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




