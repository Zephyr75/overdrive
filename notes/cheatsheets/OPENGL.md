# OpenGL — the API, call by call

> **Scope** GLFW setup, buffer objects, the coordinate pipeline, textures, per-fragment tests, framebuffers, GLSL. Go signatures throughout (`go-gl`). API reference only: Overdrive's OpenGL backend was deleted on 2026-08-05.
>
> **Not here** the Vulkan equivalent → `VULKAN.md`. The physics behind lighting → `PBR.md`. Techniques built on the API (shadow maps, deferred, AO) → `GRAPHICS.md` §1.
>
> **Source** [learnopengl.com](https://learnopengl.com).

---

## 1. GLFW: window and input

`glfw.Init()` / `glfw.Terminate()` start and stop GLFW; `glfw.WindowHint(name, value)` sets a parameter ([all hints](https://www.glfw.org/docs/latest/window.html#window_hints)).

```go
// FULL SETUP
glfw.Init()
glfw.WindowHint(glfw.ContextVersionMajor, 4)
glfw.WindowHint(glfw.ContextVersionMinor, 1)
glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
```

```go
// FULL WINDOW DEFINITION
window, err := glfw.CreateWindow(windowWidth, windowHeight, "WindowName", nil, nil)
if err != nil {
    glfw.Terminate()
}
window.MakeContextCurrent()
```

- `window.SwapBuffers()` swap the current and next colour buffers
- `window.ShouldClose()` / `SetShouldClose(true)` detect / request close
- Callbacks: `SetFramebufferSizeCallback`, `SetCursorPosCallback`, `SetScrollCallback`; `SetInputMode(glfw.CursorMode, glfw.CursorDisabled)`
- `glfw.PollEvents()` processes input and calls the callbacks; `window.GetKey(glfw.KeyEscape)` reads a key

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

Maths: GLM in C++, `mgl32` in Go.

## 2. Basics

- `gl.Init()` start OpenGL
- `gl.Viewport(0, 0, 800, 600)` viewport resolution
- `gl.ClearColor(r, g, b, a)` then `gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)`
- `gl.Enable(cap)` depth test, blending, culling…; `gl.GetError()` poll the error flag

## 3. Buffers: VBO, EBO, VAO

- **VBO** stores vertices: `gl.GenBuffers(1, &VBO)`, `gl.BindBuffer(gl.ARRAY_BUFFER, VBO)`, `gl.BufferData(gl.ARRAY_BUFFER, len(v)*4, gl.Ptr(v), gl.STATIC_DRAW)`. `DYNAMIC_DRAW` for data that changes often, `STREAM_DRAW` for set-once-use-few.
- **EBO** stores indices: the same calls with `gl.ELEMENT_ARRAY_BUFFER`.
- **Attributes**: `gl.VertexAttribPointer(location, size, type, normalized, stride, offset)` then `gl.EnableVertexAttribArray(location)`.
- **VAO** records the attribute calls and the VBO/EBO bindings: `gl.GenVertexArrays(1, &VAO)`, `gl.BindVertexArray(VAO)`.

## 4. Shaders and programs

- **Shader**: `gl.CreateShader(type)`, `gl.ShaderSource`, `gl.CompileShader`, check `gl.GetShaderiv(s, gl.COMPILE_STATUS, &ok)` and `gl.GetShaderInfoLog`.
- **Program**: `gl.CreateProgram`, `gl.AttachShader`, `gl.LinkProgram`, `gl.UseProgram`, then `gl.DeleteShader`; check `gl.LINK_STATUS`.
- **Uniforms** are constant for one draw: `gl.GetUniformLocation(program, gl.Str("name\x00"))`, then `gl.Uniform4f` / `gl.UniformMatrix4fv` with the program in use.

```c
// FULL UNIFORM UPDATE (C style)
float timeValue = glfwGetTime();
float greenValue = (sin(timeValue) / 2.0f) + 0.5f;
int vertexColorLocation = glGetUniformLocation(shaderProgram, "ourColor");
glUseProgram(shaderProgram);
glUniform4f(vertexColorLocation, 0.0f, greenValue, 0.0f, 1.0f);
```

## 5. Drawing

`glDrawArrays(GL_TRIANGLES, 0, 3)` from the VBO; `glDrawElements(GL_TRIANGLES, 6, GL_UNSIGNED_INT, 0)` through the EBO.

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

## 6. Transformations and coordinate systems

Combine into one model matrix, **read right to left**: scale, then rotate, then translate (`M = T · R · S`), or the translation gets scaled and rotated too. `mgl32.Translate3D`, `HomogRotate3D(angle, axis)`, `Scale3D`.

```go
// FULL TRANSFORM (translate THEN rotate would be Rotate.Mul4(Translate))
trans := mgl32.Translate3D(0.5, -0.5, 0).Mul4(
         mgl32.HomogRotate3D(float32(glfw.GetTime()), mgl32.Vec3{0, 0, 1}))
gl.UniformMatrix4fv(transformLoc, 1, false, &trans[0])
```

Vertex journey: local → (model) → world → (view) → view → (projection) → clip → (divide + viewport) → screen. Outside NDC `[-1, 1]` is clipped.

$V_{clip} = M_{projection} \cdot M_{view} \cdot M_{model} \cdot V_{local}$

`mgl32.Perspective(fov, aspect, near, far)` and `mgl32.Ortho(left, right, bottom, top, near, far)` (no perspective, for 2D/UI).

```go
// FULL MVP SETUP
model := mgl32.HomogRotate3D(angle, mgl32.Vec3{0.5, 1, 0})
view := mgl32.Translate3D(0, 0, -3) // move scene back = move camera forward
projection := mgl32.Perspective(mgl32.DegToRad(45), 800.0/600.0, 0.1, 100.0)
// in vertex shader: gl_Position = projection * view * model * vec4(aPos, 1.0)
```

**There is no camera**: the view matrix moves the world the other way. `mgl32.LookAtV(position, target, up)` builds it.

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

The scroll wheel usually drives the FOV.

## 7. Textures

`gl.GenTextures`, `gl.ActiveTexture(gl.TEXTURE0)` (16 units guaranteed), `gl.BindTexture(gl.TEXTURE_2D, t)`, `gl.TexParameteri`, `gl.TexImage2D(target, 0, gl.RGBA, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))`, `gl.GenerateMipmap`.

- **Wrapping** per axis: `REPEAT`, `MIRRORED_REPEAT`, `CLAMP_TO_EDGE`, `CLAMP_TO_BORDER`
- **Filtering**: `NEAREST` (blocky) or `LINEAR` (interpolated)
- **Mipmaps**: the texture at /2, /4, /8…, picked by on-screen size. Mip filtering applies to `MIN_FILTER` only (`LINEAR_MIPMAP_LINEAR` is trilinear); on `MAG_FILTER` it is an error

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

## 8. Per-fragment tests

**Depth**: `gl.Enable(gl.DEPTH_TEST)`, clear `DEPTH_BUFFER_BIT` each frame, `gl.DepthFunc(gl.LESS)`, `gl.DepthMask(false)` to test without writing. Precision is non-linear (high near the near plane); z-fighting is fixed by offsetting surfaces or tightening near/far.

**Stencil**: an 8-bit per-pixel mask, tested before depth (outlines, mirrors, portals). `gl.StencilFunc(gl.EQUAL, 1, 0xFF)`, `gl.StencilOp(sfail, dpfail, pass)`, `gl.StencilMask(0xFF)`.

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

**Blending**: `gl.Enable(gl.BLEND)`, `gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)`: $C = \alpha_{src} C_{src} + (1 - \alpha_{src}) C_{dst}$. Draw opaque first, then transparent **sorted far to near**. For fully transparent texels, `discard` instead:

```glsl
if (texture(tex, TexCoords).a < 0.1) discard;
```

**Face culling**: `gl.Enable(gl.CULL_FACE)`, `gl.CullFace(gl.BACK)`, `gl.FrontFace(gl.CCW)`. Needs consistent winding and closed shapes; ~50% fewer fragments.

## 9. Framebuffers and cubemaps

A framebuffer holds colour, depth and stencil attachments: render to texture for post-processing, mirrors, shadow maps. `gl.GenFramebuffers`, `gl.BindFramebuffer(gl.FRAMEBUFFER, FBO)` (0 = the window), `gl.FramebufferTexture2D(..., gl.COLOR_ATTACHMENT0, ...)`, a renderbuffer (`gl.RenderbufferStorage`) for attachments never sampled, and `gl.CheckFramebufferStatus` before use.

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

A **cubemap** is 6 faces sampled by a direction: skyboxes, environment reflections. Load each face with `gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X + i, ...)`, i = 0..5 for +X −X +Y −Y +Z −Z.

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

Draw the skybox last with `gl.DepthFunc(gl.LEQUAL)` so hidden sky fragments are rejected early.

```glsl
// ENVIRONMENT REFLECTION
vec3 I = normalize(FragPos - cameraPos);
vec3 R = reflect(I, normalize(Normal));   // or refract(I, N, 1.0/1.52) for glass
FragColor = texture(skybox, R);
```

## 10. Instancing and MSAA

**Instancing** draws one mesh many times in one call: `gl.DrawArraysInstanced` / `DrawElementsInstanced`, `gl_InstanceID` in the shader, `gl.VertexAttribDivisor(location, 1)` for per-instance attributes (a mat4 takes 4 locations).

**MSAA**: coverage and depth at N samples per pixel, the fragment shader once. `glfw.WindowHint(glfw.Samples, 4)`, `gl.Enable(gl.MULTISAMPLE)`. Offscreen: `gl.TexImage2DMultisample`, then `gl.BlitFramebuffer` to resolve before sampling.

## 11. Lighting (Phong)

`ambient` constant base light · `diffuse` from the normal–light angle · `specular` a highlight depending on the view and the reflection.

> **Normal matrix** = `mat3(transpose(inverse(model)))`: keeps normals perpendicular under non-uniform scale

```glsl
struct Material {
    sampler2D diffuse;   // color per fragment (lighting map) — also used for ambient
    sampler2D specular;  // per-fragment specular intensity (e.g. metal borders shine, wood doesn't)
    float shininess;     // specular exponent: higher = smaller, sharper highlight
};
uniform Material material;
```

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

Several lights: one function per type, summed.

```glsl
vec3 result = CalcDirLight(dirLight, norm, viewDir);
for (int i = 0; i < NR_POINT_LIGHTS; i++)
    result += CalcPointLight(pointLights[i], norm, FragPos, viewDir);
result += CalcSpotLight(spotLight, norm, FragPos, viewDir);
```

**Model loading**: Assimp reads 40+ formats into a scene graph. A `Mesh` is vertices + indices + textures (one VAO, one draw); a `Model` is the meshes of one file. Cache textures by path.

## 12. GLSL

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

Built-ins: `gl_FragCoord` (window position, depth in z), `gl_FrontFacing`, `gl_PointSize`, `gl_VertexID`.

**Uniform buffer objects** share uniforms across programs:

```glsl
layout (std140) uniform Matrices {  // std140 = fixed, predictable memory layout
    mat4 projection;
    mat4 view;
};
```

`gl.BindBufferBase(gl.UNIFORM_BUFFER, 0, UBO)` plus `gl.UniformBlockBinding`. std140: scalars align to 4, vec3 and vec4 to 16, mat4 = 4 × vec4; pad CPU structs to match.

**Geometry shader**: one primitive in, zero or more out (normals as lines, exploding meshes, one-pass cubemaps).

```glsl
layout (triangles) in;
layout (triangle_strip, max_vertices = 3) out;
// EmitVertex() after setting gl_Position; EndPrimitive() to close the strip
```

## 13. Pipeline summary

```
Vertex data → Vertex shader (per vertex: position transform)
            → [Geometry shader]
            → Primitive assembly + clipping
            → Rasterization (primitives → fragments)
            → Fragment shader (per fragment: color)
            → Per-sample ops: stencil test → depth test → blending
            → Framebuffer
```
