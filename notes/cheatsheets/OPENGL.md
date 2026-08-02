# Libraries

## GLFW : window manager and input handler

`glfw.Init()` initialize GLFW  

`glfw.WindowHint(glfw.Resizable, glfw.False)` set GLFW parameter
> All parameters available in [GLFW's documentation](https://www.glfw.org/docs/latest/window.html#window_hints)

```go
// FULL SETUP
glfw.Init()
glfw.WindowHint(glfw.ContextVersionMajor, 4)
glfw.WindowHint(glfw.ContextVersionMinor, 1)
glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
```

### Window

`glfw.CreateWindow(800, 600, "LearnOpenGL", nil [monitor], nil [window])` create window

`glfw.Terminate()` terminate GLFW

`window.MakeContextCurrent()` activate window context

```go
// FULL WINDOW DEFINITION
window, err := glfw.CreateWindow(windowWidth, windowHeight, "WindowName", nil, nil)
if err != nil {
    glfw.Terminate()
}
window.MakeContextCurrent()
```

`window.SwapBuffers()` swap current and next color buffers

`window.ShouldClose()` detects close request

`window.SetShouldClose(true)` send close request

### Inputs

**Callbacks**

`window.SetFramebufferSizeCallback(input.FramebufferSizeCallback)` attach function to window size change

`window.SetCursorPosCallback(input.MouseCallback)` attach function to mouse move

`window.SetScrollCallback(input.ScrollCallback)` attach function to mouse scroll

`window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)` set input modes

`glfw.PollEvents()` checks input events and calls the attached functions

**Keys**

`window.GetKey(glfw.KeyEscape)` get state of given key

```go
// FULL WINDOW LIFECYCLE
for !window.ShouldClose() {
    if window.GetKey(glfw.KeyEscape) == glfw.Press {
        window.SetShouldClose(true)
    }
    ...rendering logic...
    window.SwapBuffers()
    glfw.PollEvents()
}
```

## GLAD

OpenGL only defines a specification, the implementation is different for each driver. GLAD makes the driver functions accessible to our code.

> Not used in Go bindings.

```c++
if (!gladLoadGLLoader((GLADloadproc)glfwGetProcAddress))
{
    std::cout << "Failed to initialize GLAD" << std::endl;
    return -1;
}    
```

## GLM

OpenGL Mathematics library. Go equivalent: `mgl32` (github.com/go-gl/mathgl/mgl32).

# OpenGL

## Generic

`gl.Init()` start OpenGL

`gl.Viewport(0, 0, 800, 600)` set viewport resolution

`gl.ClearColor(0.2, 0.3, 0.3, 1.0)` set color to clear buffer with

`gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)` clear buffer using defined color

`gl.Enable(gl.DEPTH_TEST)` enable a capability (depth test, blending, culling...)

`gl.GetError()` poll error flag

> Check extension support before using non-core features:
```c
if (GLAD_GL_ARB_extension_name) { /* modern path */ } else { /* fallback */ }
```

## Storing vertices : VBO

**Vertex Buffer Object** stores vertices

`var VBO uint32` declare ID

`gl.GenBuffers(1, &VBO)` create buffer associated to ID

`gl.BindBuffer(gl.ARRAY_BUFFER, VBO)` bind buffer to OpenGL target buffer

`gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)` use target binding to set buffer structure and data 

> Use gl.DYNAMIC_DRAW for vertex data that changes frequently, gl.STREAM_DRAW for data set once and used a few times

## Storing triangles : EBO

**Element Buffer Object** stores indices

`var EBO uint32` declare ID

`gl.GenBuffers(1, &EBO)` create buffer and store its ID

`gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, EBO)` bind buffer to OpenGL target buffer

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

## Shader

`var shader uint32` declare ID

`shader = gl.CreateShader(shaderType)` create shader associated to ID

`gl.ShaderSource(shader, 1, csources, nil)` set shader source code

`gl.CompileShader(shader)` compile shader

`gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)` get shader information and errors

`gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))` get compilation error log

### Uniforms

Global variables set from the program, constant for a whole draw call

`gl.GetUniformLocation(program, gl.Str("ourColor\x00"))` get uniform location in program

`gl.Uniform4f(location, 0.0, greenValue, 0.0, 1.0)` set vec4 uniform (program must be in use)

`gl.UniformMatrix4fv(location, 1, false, &matrix[0])` set mat4 uniform

```c
// FULL UNIFORM UPDATE (C style)
float timeValue = glfwGetTime();
float greenValue = (sin(timeValue) / 2.0f) + 0.5f;
int vertexColorLocation = glGetUniformLocation(shaderProgram, "ourColor");
glUseProgram(shaderProgram);
glUniform4f(vertexColorLocation, 0.0f, greenValue, 0.0f, 1.0f);
```

## Program

`var program uint32` declare ID

`program = gl.CreateProgram()` create program associated to ID

`gl.AttachShader(program, shader)` assign shader to program

`gl.LinkProgram(program)` link program shaders together

`gl.UseProgram()` use program

`gl.DeleteShader(shader)` remove linked shader

`gl.GetProgramiv(program, gl.LINK_STATUS, &status)` get program information and errors

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

## Transformations

Combine scale, rotate, translate into one `model` matrix. **Read right to left**: the matrix written last is applied first.

`mgl32.Translate3D(0.5, -0.5, 0)` translation matrix

`mgl32.HomogRotate3D(angle, mgl32.Vec3{0, 0, 1})` rotation matrix around axis

`mgl32.Scale3D(0.5, 0.5, 0.5)` scale matrix

```go
// FULL TRANSFORM (translate THEN rotate would be Rotate.Mul4(Translate))
trans := mgl32.Translate3D(0.5, -0.5, 0).Mul4(
         mgl32.HomogRotate3D(float32(glfw.GetTime()), mgl32.Vec3{0, 0, 1}))
gl.UniformMatrix4fv(transformLoc, 1, false, &trans[0])
```

> Recommended order: scale first, then rotate, then translate ($M = T \cdot R \cdot S$), otherwise translation gets scaled/rotated too

## Coordinate systems

Vertex journey: **local space** → (model matrix) → **world space** → (view matrix) → **view space** → (projection matrix) → **clip space** → (perspective divide + viewport) → **screen space**

$V_{clip} = M_{projection} \cdot M_{view} \cdot M_{model} \cdot V_{local}$

Everything outside Normalized Device Coordinates ($-1.0$ to $1.0$ after perspective divide) is clipped

`mgl32.Perspective(mgl32.DegToRad(45), width/height, 0.1, 100.0)` perspective projection (fov, aspect ratio, near plane, far plane)

`mgl32.Ortho(0, 800, 0, 600, 0.1, 100)` orthographic projection (no perspective, for 2D/UI)

```go
// FULL MVP SETUP
model := mgl32.HomogRotate3D(angle, mgl32.Vec3{0.5, 1, 0})
view := mgl32.Translate3D(0, 0, -3) // move scene back = move camera forward
projection := mgl32.Perspective(mgl32.DegToRad(45), 800.0/600.0, 0.1, 100.0)
// in vertex shader: gl_Position = projection * view * model * vec4(aPos, 1.0)
```

## Camera

OpenGL has no camera: moving the camera = moving the whole world the opposite way (the view matrix)

`mgl32.LookAtV(position, target, up)` build view matrix from camera position, look target, world up vector

```go
// FLY CAMERA STATE
cameraPos   := mgl32.Vec3{0, 0, 3}
cameraFront := mgl32.Vec3{0, 0, -1}
cameraUp    := mgl32.Vec3{0, 1, 0}
view := mgl32.LookAtV(cameraPos, cameraPos.Add(cameraFront), cameraUp)

// KEYBOARD (scale by deltaTime for framerate independence)
speed := float32(2.5 * deltaTime)
if window.GetKey(glfw.KeyW) == glfw.Press { cameraPos = cameraPos.Add(cameraFront.Mul(speed)) }
if window.GetKey(glfw.KeyA) == glfw.Press { cameraPos = cameraPos.Sub(cameraFront.Cross(cameraUp).Normalize().Mul(speed)) }

// MOUSE -> EULER ANGLES (yaw around Y, pitch around X, clamp pitch to ±89°)
front := mgl32.Vec3{
    cos(radians(yaw)) * cos(radians(pitch)),
    sin(radians(pitch)),
    sin(radians(yaw)) * cos(radians(pitch)),
}.Normalize()
```

> Scroll wheel typically drives the fov passed to `Perspective` (zoom)

## Load texture

`var texture uint32` declare ID

`gl.GenTextures(1, &texture)` create texture and store its ID

`gl.ActiveTexture(gl.TEXTURE0)` select texture unit (16 minimum guaranteed)

`gl.BindTexture(gl.TEXTURE_2D, texture)` bind texture to active texture unit

`gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)` set option

`gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, width, height, 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))` load image data to texture

`gl.GenerateMipmap(gl.TEXTURE_2D)` generate mipmaps

**Wrapping** (per axis S/T): `gl.REPEAT`, `gl.MIRRORED_REPEAT`, `gl.CLAMP_TO_EDGE`, `gl.CLAMP_TO_BORDER`

**Filtering**: `gl.NEAREST` (blocky, no interpolation) or `gl.LINEAR` (interpolate neighboring texels)

**Mipmaps**: the texture is also stored at /2, /4, /8... resolution; OpenGL picks the level matching the on-screen size (avoids artifacts + cache misses on far objects)
> Mipmap filtering only applies to MIN_FILTER: `gl.LINEAR_MIPMAP_LINEAR` interpolates both within and between mip levels (trilinear). Setting a mipmap option on MAG_FILTER is an error.

```go
// MULTIPLE TEXTURES IN ONE SHADER
gl.ActiveTexture(gl.TEXTURE0)
gl.BindTexture(gl.TEXTURE_2D, texture1)
gl.ActiveTexture(gl.TEXTURE1)
gl.BindTexture(gl.TEXTURE_2D, texture2)
gl.Uniform1i(gl.GetUniformLocation(program, gl.Str("texture1\x00")), 0) // sampler = unit index
gl.Uniform1i(gl.GetUniformLocation(program, gl.Str("texture2\x00")), 1)
```

```c
// Vertex shader
#version 330 core
layout (location = 0) in vec3 aPos;
layout (location = 1) in vec3 aColor;
layout (location = 2) in vec2 aTexCoord;

out vec3 ourColor;
out vec2 TexCoord;

void main()
{
    gl_Position = vec4(aPos, 1.0);
    ourColor = aColor;
    TexCoord = aTexCoord;
}
```

```c
// Fragment shader
#version 330 core
out vec4 FragColor;
  
in vec3 ourColor;
in vec2 TexCoord;

uniform sampler2D ourTexture;

void main()
{
    FragColor = texture(ourTexture, TexCoord);
}
```

## Depth testing

Depth buffer stores per-pixel depth; fragments behind already-drawn fragments are discarded

`gl.Enable(gl.DEPTH_TEST)` enable depth testing

`gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)` also clear depth each frame

`gl.DepthFunc(gl.LESS)` comparison function (LESS default; ALWAYS disables testing effect)

`gl.DepthMask(false)` read-only depth buffer (test but don't write)

> Depth precision is non-linear: very high near the near plane, low far away. `Z-fighting` = two surfaces too close for depth precision to order them; fix by offsetting surfaces or tightening near/far planes

## Stencil testing

Stencil buffer = 8-bit per-pixel mask defining which pixels to render; runs before depth test. Classic use: object outlines, mirrors, portals

`gl.Enable(gl.STENCIL_TEST)` enable

`gl.StencilFunc(gl.EQUAL, 1, 0xFF)` pass test if stencil value == 1

`gl.StencilOp(gl.KEEP, gl.KEEP, gl.REPLACE)` what to do on stencil fail / depth fail / both pass

`gl.StencilMask(0xFF)` enable writing to stencil buffer (0x00 = read-only)

```go
// OBJECT OUTLINE PATTERN
// 1. draw object normally, writing 1s to stencil
gl.StencilFunc(gl.ALWAYS, 1, 0xFF)
gl.StencilMask(0xFF)
drawObject()
// 2. draw scaled-up object only where stencil != 1, with flat color shader
gl.StencilFunc(gl.NOTEQUAL, 1, 0xFF)
gl.StencilMask(0x00)
gl.Disable(gl.DEPTH_TEST)
drawScaledObject()
```

## Blending

Renders transparency by combining the fragment color with the color already in the buffer

`gl.Enable(gl.BLEND)` enable

`gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)` standard alpha blending: $C_{result} = \alpha_{src} \cdot C_{src} + (1 - \alpha_{src}) \cdot C_{dst}$

> For fully transparent texels (grass sprites), skip blending and `discard` in the fragment shader instead:
```glsl
if (texture(tex, TexCoords).a < 0.1) discard;
```

> Blending order matters: draw opaque objects first, then transparent objects **sorted far to near** (depth buffer doesn't know about transparency)

## Face culling

Skip rendering triangles facing away from the camera (back of closed objects), ~50% fewer fragment shader runs

`gl.Enable(gl.CULL_FACE)` enable

`gl.CullFace(gl.BACK)` which faces to cull (BACK default)

`gl.FrontFace(gl.CCW)` winding order that defines a front face (counter-clockwise default)

> Requires consistent winding order in vertex data. Only works for closed shapes (culling a grass quad's back makes it invisible from behind)

## Framebuffer

Framebuffer Object = render target holding color + depth + stencil attachments. Render to texture → post-processing, mirrors, shadow maps

`var FBO uint32` declare ID

`gl.GenFramebuffers(1, &FBO)` create framebuffer and store its ID

`gl.BindFramebuffer(gl.FRAMEBUFFER, FBO)` bind (0 = default window framebuffer)

`gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texColorBuffer, 0)` attach texture as color buffer

`gl.RenderbufferStorage(gl.RENDERBUFFER, gl.DEPTH24_STENCIL8, width, height)` renderbuffer = write-only attachment, faster than texture when you never sample it

`gl.FramebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_STENCIL_ATTACHMENT, gl.RENDERBUFFER, RBO)` attach renderbuffer

`gl.CheckFramebufferStatus(gl.FRAMEBUFFER) == gl.FRAMEBUFFER_COMPLETE` verify completeness before use

```go
// POST-PROCESSING PATTERN
// pass 1: render scene into FBO's color texture
gl.BindFramebuffer(gl.FRAMEBUFFER, FBO)
gl.Enable(gl.DEPTH_TEST)
drawScene()
// pass 2: render fullscreen quad sampling that texture with an effect shader
gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
gl.Disable(gl.DEPTH_TEST)
gl.BindTexture(gl.TEXTURE_2D, texColorBuffer)
drawFullscreenQuad() // kernel effects: blur, sharpen, edge detection, grayscale...
```

## Cubemaps

Texture made of 6 faces, sampled with a 3D direction vector. Main uses: skybox, environment reflection/refraction

`gl.BindTexture(gl.TEXTURE_CUBE_MAP, texture)` bind

`gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X + i, ...)` load each of the 6 faces (i = 0..5: +X -X +Y -Y +Z -Z)

```glsl
// SKYBOX SHADERS
// vertex: strip translation from view so skybox follows camera,
// force depth to 1.0 so it's always behind everything
mat4 view = mat4(mat3(viewWithoutTranslation));
gl_Position = (projection * view * vec4(aPos, 1.0)).xyww;
// fragment
uniform samplerCube skybox;
FragColor = texture(skybox, TexCoords); // TexCoords = local cube position
```

> Draw skybox last with `gl.DepthFunc(gl.LEQUAL)`: depth buffer fills first, skybox fragments behind geometry are discarded (early depth test saves fragment runs)

```glsl
// ENVIRONMENT REFLECTION
vec3 I = normalize(FragPos - cameraPos);
vec3 R = reflect(I, normalize(Normal));   // or refract(I, N, 1.0/1.52) for glass
FragColor = texture(skybox, R);
```

## Instancing

Draw the same mesh many times in one call: removes per-draw CPU→GPU overhead (the actual bottleneck with thousands of objects)

`gl.DrawArraysInstanced(gl.TRIANGLES, 0, count, instanceCount)` instanced draw

`gl.DrawElementsInstanced(...)` instanced indexed draw

`gl_InstanceID` built-in shader variable: current instance index

`gl.VertexAttribDivisor(location, 1)` make attribute advance per **instance** instead of per vertex (`instanced array`, for per-instance model matrices)

> A mat4 attribute occupies 4 consecutive attribute locations: set pointer + divisor on each

## Anti-aliasing (MSAA)

Multisampling: depth/stencil tested at N sample points per pixel, fragment shader runs once, color contribution = fraction of covered samples → smooth edges

`glfw.WindowHint(glfw.Samples, 4)` request multisampled default framebuffer

`gl.Enable(gl.MULTISAMPLE)` enable (often default)

> Offscreen MSAA: create textures with `gl.TexImage2DMultisample`, then `gl.BlitFramebuffer` to resolve into a normal framebuffer before sampling

# Lighting

## Phong model

`ambient` constant base light (fake global illumination)
`diffuse` proportional to angle between normal and light direction
`specular` shiny highlight, depends on view direction and reflection

> `Normal matrix` = `mat3(transpose(inverse(model)))`: transforms normals correctly under non-uniform scaling (plain model matrix would skew them)

## Materials

```glsl
struct Material {
    sampler2D diffuse;   // color per fragment (lighting map) — also used for ambient
    sampler2D specular;  // per-fragment specular intensity (e.g. metal borders shine, wood doesn't)
    float shininess;     // specular exponent: higher = smaller, sharper highlight
};
uniform Material material;
```

## Light casters

```glsl
// DIRECTIONAL (sun): no position, only direction; no attenuation
struct DirLight { vec3 direction; vec3 ambient, diffuse, specular; };

// POINT (bulb): position + attenuation so light fades with distance
struct PointLight {
    vec3 position;
    float constant, linear, quadratic;  // attenuation = 1/(Kc + Kl*d + Kq*d²)
    vec3 ambient, diffuse, specular;
};

// SPOTLIGHT (flashlight): point light limited to a cone
struct SpotLight {
    vec3 position, direction;
    float cutOff, outerCutOff;  // cos of inner/outer cone angle; interpolate between for soft edges
    ...
};
// intensity = clamp((theta - outerCutOff) / (cutOff - outerCutOff), 0.0, 1.0)
```

> Multiple lights = one function per light type, sum the results:
```glsl
vec3 result = CalcDirLight(dirLight, norm, viewDir);
for (int i = 0; i < NR_POINT_LIGHTS; i++)
    result += CalcPointLight(pointLights[i], norm, FragPos, viewDir);
result += CalcSpotLight(spotLight, norm, FragPos, viewDir);
```

# Model loading

`Assimp` library loading 40+ model formats into a uniform scene graph; learnopengl wraps it in Mesh/Model classes

`Mesh` = vertices (position, normal, texcoords) + indices + textures → one VAO/VBO/EBO + one draw call

`Model` = collection of meshes from one file; recursively walks Assimp's node tree

> Cache loaded textures by path: models reuse the same texture across meshes

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

## Advanced GLSL

`gl_FragCoord` fragment's window-space position (x, y, depth in z)

`gl_FrontFacing` bool, true when fragment belongs to a front face (two-sided materials)

`gl_PointSize` point size output from vertex shader (enable `gl.PROGRAM_POINT_SIZE`)

`gl_VertexID` current vertex index

**Uniform Buffer Objects** share uniforms (e.g. projection + view) across all shader programs:

```glsl
layout (std140) uniform Matrices {  // std140 = fixed, predictable memory layout
    mat4 projection;
    mat4 view;
};
```

`gl.BindBufferBase(gl.UNIFORM_BUFFER, 0, UBO)` bind buffer to binding point 0; link shader's block to the same point with `gl.UniformBlockBinding`

> std140 layout rules: scalars align to 4 bytes, vec3 and vec4 both align to 16, mat4 = 4 × vec4. Pad CPU-side structs accordingly

## Geometry shader

Optional stage between vertex and fragment: takes one primitive, emits zero or more primitives

```glsl
layout (triangles) in;
layout (triangle_strip, max_vertices = 3) out;
// EmitVertex() after setting gl_Position; EndPrimitive() to close the strip
```

Uses: visualize normals as lines, explode meshes, render to cubemap faces in one pass

# Rendering pipeline summary

```
Vertex data → Vertex shader (per vertex: position transform)
            → [Geometry shader]
            → Primitive assembly + clipping
            → Rasterization (primitives → fragments)
            → Fragment shader (per fragment: color)
            → Per-sample ops: stencil test → depth test → blending
            → Framebuffer
```
