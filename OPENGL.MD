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

`window.SwapBuffers()` swap current and next color buffers

`glfw.PollEvents()` get inputs

```go
// FULL WINDOW LIFECYCLE
for !window.ShouldClose() {
    ...
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

## Generic

`gl.Init()` setup OpenGL

`gl.Viewport(0, 0, 800, 600)` set resolution

`gl.ClearColor(0.2, 0.3, 0.3, 1.0)` set background color

`gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)` clear buffer

```go
// MAP CALLBACK METHODS TO EVENTS
window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)
window.SetCursorPosCallback(input.MouseCallback)
window.SetScrollCallback(input.ScrollCallback)
window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
```

## Storing vertices : VBO

> Vertex Buffer Object stores vertices

`var VBO uint32` declare ID

`gl.GenBuffers(1, &VBO)` create buffer and store its ID

`gl.BindBuffer(gl.ARRAY_BUFFER, VBO)` bind buffer to OpenGL buffer

`gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)` set buffer structure and data

## Storing triangles : EBO

`var EBO uint32` declare ID

`gl.GenBuffers(1, &EBO)` create buffer and store its ID

`gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, EBO)` bind buffer to OpenGL buffer

`gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices)*4, gl.Ptr(indices), gl.STATIC_DRAW)` set buffer structure and data

## Interpret buffer data

`gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 3*4, gl.PtrOffset(0))` specify how to interpret the data

Parameters :
- 1st : location of the vertex attribute in the shader
- 2nd : size of the vertex attribute
- 3rd : type of the data
- 4th : whether the data should be normalized
- 5th : stride (space between consecutive vertex attributes)
- 6th : offset of the position where the data starts

`gl.EnableVertexAttribArray(0)` enable the vertex attribute with given location

## Store buffer config : VAO

Vertex Array Object stores calls to:
- `glEnableVertexAttribArray`
- `glDisableVertexAttribArray`
- `glVertexAttribPointer`

Vertex Array Object stores bindings to:
- VBO
- EBO

`var VAO uint32` declare ID

`gl.GenVertexArrays(1, &VAO)` create buffer and store its ID

`gl.BindVertexArray(VAO)` bind buffer to OpenGL buffer

## Drawing

`glDrawArrays(GL_TRIANGLES, 0, 3)` draw triangles

`glDrawElements(GL_TRIANGLES, 6, GL_UNSIGNED_INT, 0)` draw elements from EBO

```go
// INITIALIZATION CODE
// 1. bind Vertex Array Object
glBindVertexArray(VAO)
// 2. copy our vertices array in a vertex buffer for OpenGL to use
glBindBuffer(GL_ARRAY_BUFFER, VBO)
glBufferData(GL_ARRAY_BUFFER, sizeof(vertices), vertices, GL_STATIC_DRAW)
// 3. copy our index array in a element buffer for OpenGL to use
glBindBuffer(GL_ELEMENT_ARRAY_BUFFER, EBO)
glBufferData(GL_ELEMENT_ARRAY_BUFFER, sizeof(indices), indices, GL_STATIC_DRAW)
// 4. then set the vertex attributes pointers
glVertexAttribPointer(0, 3, GL_FLOAT, GL_FALSE, 3 * sizeof(float), (void*)0)
glEnableVertexAttribArray(0)

...

// DRAWING CODE (IN RENDER LOOP)
glUseProgram(shaderProgram)
glBindVertexArray(VAO)
glDrawElements(GL_TRIANGLES, 6, GL_UNSIGNED_INT, 0)
glBindVertexArray(0)
```

## Load texture

`var texture uint32` declare ID

`gl.GenTextures(1, &texture)` create texture and store its ID

`gl.BindTexture(gl.TEXTURE_2D, texture)` bind texture to OpenGL texture

`gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)` set option

`gl.TexImage2D()` load image data to texture

`gl.GenerateMipmap(gl.TEXTURE_2D)` generate MipMap (multi-resolution images for faster loading)

> The `stencil buffer` define which pixels to render for example to draw outlines

## Shader

`gl.CreateShader(shaderType)` create shader

`gl.ShaderSource(shader, 1, csources, nil)` set shader code

`gl.CompileShader(shader)` compile shader

`gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)` get shader information and errors

## Program

`gl.CreateProgram()` create program

`gl.AttachShader(program, vertexShader)` assign shader to program

`gl.LinkProgram(program)` link program shaders together

`gl.UseProgram()` use program

`gl.DeleteShader(vertexShader)` delete shader once linked

`gl.GetProgramiv(program, gl.LINK_STATUS, &status)` get program information and errors

## Framebuffer

`var FBO uint32` declare ID

`gl.GenFramebuffers(1, &FBO)` create buffer and store its ID

`gl.BindFramebuffer(gl.ARRAY_BUFFER, FBO)` bind buffer to OpenGL buffer


# GLSL

Vertex shader takes inputs from program and feeds outputs to fragment shader

## Vertex shader

```glsl
#version 330 core
// VERTEX DEPENDENT INPUT VARIABLES
layout (location = 0) in vec3 aPos;
layout (location = 1) in vec3 aNormal;

// OUTPUT VARIABLES TO FRAGMENT SHADER
out vec3 FragPos;
out vec3 Normal;

// UNIFORM INPUT VARIABLES
uniform mat4 model;
uniform mat4 view;
uniform mat4 projection;

void main()
{
    FragPos = vec3(model * vec4(aPos, 1.0));
    Normal = mat3(transpose(inverse(model))) * aNormal; 
    // MAIN 2D POSITION OUTPUT 
    gl_Position = projection * view * vec4(FragPos, 1.0);
}
```

## Fragment shader

```glsl
#version 330 core
// MAIN COLOR OUTPUT
out vec4 FragColor;

// INPUT VARIABLES FROM VERTEX SHADER
in vec3 Normal;  
in vec3 FragPos;  

// UNIFORM INPUT VARIABLES
uniform vec3 lightPos; 
uniform vec3 viewPos; 
uniform vec3 lightColor;
uniform vec3 objectColor;

void main()
{
    // ambient
    float ambientStrength = 0.1;
    vec3 ambient = ambientStrength * lightColor;
  	
    // diffuse 
    vec3 norm = normalize(Normal);
    vec3 lightDir = normalize(lightPos - FragPos);
    float diff = max(dot(norm, lightDir), 0.0);
    vec3 diffuse = diff * lightColor;
    
    // specular
    float specularStrength = 0.5;
    vec3 viewDir = normalize(viewPos - FragPos);
    vec3 reflectDir = reflect(-lightDir, norm);  
    float spec = pow(max(dot(viewDir, reflectDir), 0.0), 32);
    vec3 specular = specularStrength * spec * lightColor;  
        
    vec3 result = (ambient + diffuse + specular) * objectColor;
    FragColor = vec4(result, 1.0);
} 
```
