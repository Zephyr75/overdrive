# GO_BACKEND.md — A backend-agnostic Go engine (OpenGL + Vulkan)

Plan for promoting `go_deprecated/` back to the main Overdrive implementation
and making it backend-agnostic, using the architecture proven by the C++
rewrite (`cpp/`) and the hand-written Vulkan bindings in the `go-vulkan` repo
(its `vk` package).

How to read this document:

- **Part 1** is the global overview: what the abstraction is, how a frame
  flows through it, and what this design changes compared to the C++ version.
- **Parts 2–4** are the details: the interface, the uniform system, how each
  backend implements the contract, and the shader toolchain.
- **Part 5** is the step-by-step migration plan.
- **Part 6** lists the functions missing from the go-vulkan bindings.
- **[LOGL: …]** marks a link to a learnopengl.com chapter,
  **[HTV: …]** a link to a howtovulkan.com section (the same material is
  condensed in `notes/VULKAN.md`). Abbreviations are spelled out on first use.

---

# Part 1 — Overview

## 1.1 Goal

One Go engine where the scene code (meshes, lights, camera, skybox, physics,
UI) never mentions a graphics API, and where the same demo renders identically
through OpenGL 4.1 or Vulkan 1.3, selected at startup:

```sh
go run . -backend=gl        # or OVERDRIVE_BACKEND=gl
go run . -backend=vulkan
```

Vulkan-only features (ray tracing, compute) must be addable later as
*optional* capabilities without widening the common abstraction — the OpenGL
backend simply reports them as unsupported.

## 1.2 What we start from

| Source | What it gives us |
|---|---|
| `go_deprecated/` | The Go engine: scene/ECS (entity component system)/physics/UI code, OBJ+XML loading, GLFW input — but OpenGL calls leak into every layer |
| `cpp/` | A working two-backend renderer with the same feature set. Its Vulkan backend (`cpp/vulkan/Backend.cpp`) is the debugged reference for every hard problem: coordinate conventions, synchronization, shadow-map parity, uniform layout |
| `cpp/shaders/slang/` | The shader source of truth, written once in Slang and compiled to both GLSL (OpenGL) and SPIR-V (Standard Portable Intermediate Representation — Vulkan's shader binary format) |
| `go-vulkan/vk` | Hand-written cgo bindings covering the howtovulkan.com tutorial path (~78 functions), plus a pure-Go substitute for VMA (Vulkan Memory Allocator). The demo `how_to_vulkan/main.go` is working reference code for almost every call the backend needs |

## 1.3 The design in one page

Three layers, one rule: **nothing above the line imports a graphics API.**

```
┌───────────────────────────────────────────────────────────────┐
│  Scene & app code            scene/  core/  ecs/  input/ ...  │
│  (meshes, lights, camera, physics, UI — plain Go)             │
├───────────────────────── the line ────────────────────────────┤
│  renderer/   the abstraction: one Backend interface,          │
│              opaque handles, one typed Uniforms struct        │
├───────────────┬───────────────────────────────────────────────┤
│  opengl/      │  vulkan/                                      │
│  GLBackend    │  VKBackend (built on go-vulkan's vk package)  │
└───────────────┴───────────────────────────────────────────────┘
```

The abstraction rests on four ideas, all inherited from the C++ version:

1. **One small interface, not a full RHI** (render hardware interface — the
   Unreal/wgpu-style layer with command buffers and bind groups exposed to
   the app). The earlier RHI design in `notes/ABSTRACTION.md` was considered
   and rejected during the C++ rewrite: for one engine with one scene layer
   it adds indirection without payoff. The thin interface keeps scene code
   readable as learnopengl-style code and buries all Vulkan complexity in
   one package.

2. **Pass-based frame structure.** A frame is:

   ```
   BeginFrame
     BeginPass(shadow map A)   draw scene depth      EndPass     ─┐ one pass per
     BeginPass(shadow map B)   draw scene depth      EndPass     ─┘ shadow caster
     BeginPass(backbuffer)     skybox, meshes, UI    EndPass
   EndFrame
   ```

   Clears and viewport changes happen **only** inside `BeginPass` — never
   mid-pass. OpenGL doesn't care, but Vulkan's dynamic rendering
   [HTV: Render loop] requires it, and it is the one structural change the
   current Go frame loop must absorb (today `app.go` and `light.go` both
   clear mid-frame).

3. **Typed uniforms.** All shader parameters live in a single Go struct
   (`renderer.Uniforms`) whose field order matches the `Uniforms` struct in
   `cpp/shaders/slang/common.slang`. Scene code fills fields and passes the
   struct to each draw call. No `GetUniformLocation`, no name strings, no
   per-backend name→offset tables — the compiler checks every access.
   (§1.4 explains why this deliberately differs from the C++ design.)

4. **Handles are opaque `uint32`s.** Textures, buffers, meshes and render
   targets are IDs; each backend keeps its own table mapping IDs to real
   objects (OpenGL object names, or Vulkan image+view+memory bundles).
   Handle 0 is always special: texture 0 = built-in white pixel,
   framebuffer 0 = the window backbuffer.

A draw call, end to end:

```go
u.Model = mesh.ModelMatrix()
u.MatDiffuse = mat.Diffuse
u.TexDiffuse = mat.Texture          // a TextureHandle
backend.DrawMesh(forwardShader, mesh.gpu[i], len(group), &u)
```

- **OpenGL backend**: binds the shader program and the mesh's VAO (vertex
  array object), uploads the struct into a UBO (uniform buffer object,
  [LOGL: Advanced GLSL]) with a fixed std140 marshal, binds the texture
  handles to fixed texture units, calls `glDrawElements`.
- **Vulkan backend**: picks (or lazily builds) the pipeline for
  (shader, pass type, vertex layout), copies the struct into a per-frame
  ring buffer, patches the texture-handle fields into bindless array slots,
  pushes the ring entry's GPU address as a push constant, records
  `vkCmdDrawIndexed`. The shader reads the block through a buffer-reference
  pointer — this is "BDA" (buffer device address): a buffer used as a raw
  64-bit pointer in the shader, so buffers need no descriptors at all
  [HTV: Buffer device address].

## 1.4 What this design changes vs. the C++ version

Three deliberate deviations, each because Go lets us do better:

1. **No `Shader` interface with string setters.** The C++ version emulates
   OpenGL's `setMat4("lightSpaceMatrix", …)` API so its scene code could stay
   unchanged during migration, at the cost of two name→offset maps per
   backend and runtime-only typo detection. We are rewriting call sites
   anyway, so the Go version goes straight to the typed struct. A shader
   becomes just a handle (`CreateShader("forward") → ShaderHandle`); draws
   take the uniforms explicitly. Less code, compile-time checked, and it
   matches what the Slang shaders actually consume (one block), not what
   OpenGL's API historically looked like.

2. **Both backends in one binary.** C++ compiles one backend per build tree
   (link-time choice). In Go, `go-gl` resolves OpenGL function pointers at
   runtime and the `vk` package links only against the Vulkan *loader*
   library, so both packages coexist and the backend is chosen by flag or
   environment variable. A `novulkan` build tag remains as an escape hatch
   for machines without a Vulkan loader.

3. **An explicit mechanism for optional, backend-specific features**
   (§2.5). Ray tracing and compute have no OpenGL 4.1 counterpart, so they
   will never live in the common interface — instead the backend advertises
   capabilities, and Vulkan-only functionality is reached through optional
   Go interfaces. The common abstraction stays small forever; features
   that only Vulkan can do don't distort it.

Everything else — pass lifecycle, bindless textures, lazy pipelines, the
uniform ring, the OpenGL-convention bridging, single-source Slang shaders —
is kept exactly as the C++ backend proved it, because those decisions are
documented, debugged, and known to produce identical images on both APIs
(`notes/BACKEND.md`, `notes/PIPELINE.md`).

---

# Part 2 — The abstraction in detail

## 2.1 Package layout

```
overdrive/ (the Go module — promoted from go_deprecated/)
├── main.go
├── core/                 app lifecycle; owns the Backend + window
│   ├── app.go
│   └── ui.go             UI pass via the Backend (texture update + quad draw)
├── renderer/             THE ABSTRACTION — no graphics imports
│   ├── backend.go        Backend interface, handles, Feature constants
│   ├── uniforms.go       the Uniforms struct (mirror of common.slang)
│   ├── raytracing.go     optional RayTracer interface (sketch, §2.5)
│   └── create.go         CreateBackend(name) → GLBackend | VKBackend
├── opengl/               GLBackend (today's opengl/ + every gl.* call
│   │                     migrated out of core/ and scene/)
├── vulkan/               VKBackend, built on go-vulkan's vk package
│   ├── backend.go        instance/device/swapchain/frames
│   ├── pipeline.go       lazy pipeline cache
│   ├── textures.go       bindless set, samplers, uploads
│   └── uniforms.go       ring buffer + texture-slot patching
├── scene/                mesh/light/skybox/camera/scene — zero graphics
│                         imports; each stores a renderer.Backend at setup
├── shaders/
│   ├── slang/            single source (shared with cpp/shaders/slang)
│   ├── gl/               generated GLSL 4.10   (git-ignored)
│   └── vk/               generated SPIR-V      (git-ignored)
├── ecs/ input/ physics/ settings/ utils/ …   unchanged
```

Scene-layer rules (copied from the C++ migration,
`notes/BACKEND.md` § "Key changes"):

- `Mesh`, `Light`, `Skybox` store a `renderer.Backend` at `setup()` time and
  call through it for everything.
- `Material` keeps texture *paths* from parsing; GPU handles are created in
  `Mesh.setup()`.
- `input.FramebufferSizeCallback` no longer calls `gl.Viewport`; viewports
  are set per pass (the Vulkan backend additionally recreates the swapchain
  on resize [HTV: Surface and swapchain]).
- No free-floating clear / viewport / framebuffer-bind calls anywhere.

## 2.2 The Backend interface

Translated from `cpp/renderer/Backend.hpp` with C++ out-parameters turned
into multiple return values, error returns where creation can fail, draws
taking the uniforms struct, and two additions the C++ version never needed:
`UpdateTexture2D` + `DrawFullscreenQuad` (for the Go engine's UI overlay) and
`Supports` (for optional features).

```go
package renderer

import "github.com/go-gl/glfw/v3.3/glfw"

// Opaque handles; each backend keeps its own table.
// TextureHandle 0 = built-in white pixel. FramebufferHandle 0 = backbuffer.
type (
    TextureHandle     uint32
    BufferHandle      uint32
    MeshHandle        uint32 // GL: vertex array object. VK: mesh-table index
    FramebufferHandle uint32 // GL: framebuffer object.  VK: shadow-target index
    ShaderHandle      uint32
)

type Feature int

const (
    FeatureRayTracing Feature = iota // Vulkan only, and only on capable GPUs
    FeatureCompute                   // Vulkan only (OpenGL ceiling is 4.1)
)

type Backend interface {
    // --- lifecycle -------------------------------------------------------
    // Before glfw.CreateWindow: OpenGL sets context-version hints,
    // Vulkan sets glfw.ClientAPI = glfw.NoAPI.
    ConfigureWindow()
    // After window creation. GL: MakeContextCurrent + gl.Init.
    // VK: instance → surface → device → swapchain [HTV: object hierarchy].
    Init(window *glfw.Window) error
    Shutdown()

    // --- frame -----------------------------------------------------------
    // VK: BeginFrame waits the frame fence, acquires a swapchain image,
    // resets the command buffer and uniform ring; EndFrame submits and
    // presents [HTV: Frames in flight]. GL: EndFrame is SwapBuffers.
    BeginFrame()
    EndFrame()

    // Binds the render target (0 = backbuffer), sets the viewport to w×h,
    // clears depth always and color only when clear != nil. The only place
    // clears happen. GL: glBindFramebuffer + glViewport + glClear.
    // VK: image layout transitions + vkCmdBeginRendering.
    BeginPass(target FramebufferHandle, w, h int, clear *[4]float32)
    EndPass()

    // Immediate state; VK implements them as Vulkan 1.3 dynamic state so no
    // pipeline rebuild is involved.
    SetCullFace(front bool)   // front=true during the sun shadow pass
    SetDepthFunc(lequal bool) // lequal=true while the skybox draws

    // --- resources -------------------------------------------------------
    // Loads the shader set named e.g. "forward"; each backend resolves its
    // own per-stage files (shaders/gl/*.glsl vs shaders/vk/*.spv).
    CreateShader(name string, hasGeometry bool) (ShaderHandle, error)

    LoadTexture(path string) (TextureHandle, error)
    LoadCubemap(faces [6]string) (TextureHandle, error)
    WhiteTexture() TextureHandle
    // UI overlay: (re)upload RGBA8 pixels; call on 0 to allocate.
    UpdateTexture2D(h TextureHandle, w, h int, pixels []byte) TextureHandle
    DestroyTexture(h TextureHandle)

    CreateBuffer(data []float32, dynamic bool) BufferHandle
    UpdateBuffer(h BufferHandle, data []float32)
    DestroyBuffer(h BufferHandle)

    // A mesh = a vertex buffer + an index slice. Vertex layout is fixed:
    // position(3) | normal(3) | uv(2), 32 bytes [LOGL: Hello Triangle].
    // One mesh handle per material face group, all sharing one buffer.
    CreateMesh(vbo BufferHandle, indices []uint32) MeshHandle
    DestroyMesh(m MeshHandle)
    // Skybox: 36 non-indexed vertices, position(3) only.
    CreateSkyboxMesh(verts []float32) MeshHandle

    // Shadow render targets [LOGL: Shadow Mapping / Point Shadows].
    // The TextureHandle goes into Uniforms.TexShadowMap / TexShadowCubeMap.
    CreateShadowMap2D(w, h int) (FramebufferHandle, TextureHandle)
    CreateShadowCubemap(w, h int) (FramebufferHandle, TextureHandle)
    DestroyFramebuffer(f FramebufferHandle)

    // --- draws -----------------------------------------------------------
    // Each draw snapshots *u at call time; the caller may reuse u freely.
    DrawMesh(s ShaderHandle, m MeshHandle, indexCount int, u *Uniforms)
    DrawSkybox(s ShaderHandle, m MeshHandle, u *Uniforms)
    DrawFullscreenQuad(s ShaderHandle, tex TextureHandle)

    // --- capabilities ----------------------------------------------------
    Supports(f Feature) bool
}
```

## 2.3 The Uniforms struct (typed, no strings)

`renderer/uniforms.go` mirrors the `Uniforms` struct in
`cpp/shaders/slang/common.slang` **field for field, in order**. That struct is
the single GPU-facing truth; the C++ mirror is `cpp/vulkan/Uniforms.hpp`.

```go
package renderer

import "github.com/go-gl/mathgl/mgl32"

const MaxLights = 8       // must match MAX_LIGHTS in common.slang
const MaxShadowCubes = 4  // must match MAX_SHADOW_CUBES in common.slang

type LightData struct {              // 68 bytes
    Type                         int32 // 0 = sun, 1 = point
    Constant, Linear, Quadratic  float32
    Cutoff                       float32
    Color                        [3]float32
    Intensity, Diffuse, Specular float32
    Position, Direction          [3]float32
}

type Uniforms struct {               // 1312 bytes
    View, Projection, Model     mgl32.Mat4
    LightSpaceMatrix            mgl32.Mat4
    ShadowMatrices              [6]mgl32.Mat4
    ViewPos                     [3]float32
    FarPlane                    float32
    LightPos                    [3]float32
    MatAmbient, MatDiffuse      [3]float32
    MatSpecular                 [3]float32
    MatShininess                float32
    Lights                      [MaxLights]LightData
    // Texture references. Scene code stores plain TextureHandles here; each
    // backend translates them at draw time (GL: bind to a fixed unit;
    // VK: overwrite with the bindless array slot in its staging copy).
    TexShadowMap                TextureHandle
    TexDiffuse                  TextureHandle
    TexShadowCubeMap            TextureHandle
    TexSkybox                   TextureHandle
    TexNormalMap                TextureHandle
    UseNormalMap                int32
    LightCount, ShadowDirIndex  int32
    MatMetallic, MatRoughness   float32
    MatAo                       float32
    PointShadowLights           [MaxShadowCubes]int32
}
```

Why this works with zero layout tricks: the Vulkan shaders are compiled with
*scalar block layout* (`scalarBlockLayout` — every member aligned to its
scalar size, a vec3 occupies exactly 12 bytes
[HTV: Buffer device address → gotcha]), and Go structs containing only
`float32`/`int32`/`uint32` and arrays of them have **no compiler padding** —
which *is* scalar layout. The Go struct therefore memcpys straight into the
Vulkan ring buffer. Guard it like the C++ `static_assert`s do:

```go
func init() {
    if unsafe.Sizeof(LightData{}) != 68 || unsafe.Sizeof(Uniforms{}) != 1312 {
        panic("renderer.Uniforms no longer matches common.slang scalar layout")
    }
}
```

Per-backend consumption:

- **OpenGL** cannot use scalar layout — OpenGL 4.1 uniform blocks use
  *std140* layout (vec3s padded to 16 bytes, array strides rounded up
  [LOGL: Advanced GLSL → Uniform buffer objects]). The GL backend keeps one
  mechanical function, `marshalStd140(u *renderer.Uniforms, dst []byte)`,
  written once against the block layout, uploading via `glBufferSubData`
  into a single UBO shared by all shaders (this replaces the C++
  `GLShader`'s name→std140-offset map). Texture-handle fields are bound to
  fixed texture units (0–4); the sampler uniforms in the generated GLSL are
  set to those units once at link time.
- **Vulkan** copies the struct into the per-frame ring buffer, patches the
  five `Tex*` fields from handle → bindless slot index, and pushes the ring
  entry's buffer device address as the push constant. Details in §3.2.

Cost check: one 1312-byte copy + a handful of field patches per draw call is
noise at this engine's draw count (a few meshes × ≤4 face groups × 3 passes).

## 2.4 The frame loop after migration (`core/app.go`)

Mirrors `cpp/core/App.cpp`:

```go
backend.BeginFrame()

var u renderer.Uniforms
scene.FillFrameUniforms(&u)          // view, projection, viewPos, lights[]

// Shadow passes — one pass per shadow-casting light.
for i := range s.Lights {
    s.Lights[i].RenderLight(nearPlane, farPlane, depthShader, depthCubeShader, s, &u)
}

// Main pass — the only pass that clears color.
backend.BeginPass(0, settings.WindowWidth, settings.WindowHeight,
    &[4]float32{0.1, 0.1, 0.1, 1})
s.RenderSkybox(skyboxShader, &u)
s.RenderScene(forwardShader, &u)     // sets u.Model / material / textures per draw
core.RenderUI(app, widget)           // fullscreen quad, inside the main pass
backend.EndPass()

backend.EndFrame()
glfw.PollEvents()
```

Gone relative to today's `Run` loop (all three are Vulkan requirements):
the top-of-loop clear, the mid-frame clear between shadow and main passes,
and every `gl.Viewport`/`gl.BindFramebuffer` inside `Light.RenderLight`
(replaced by `BeginPass(light.shadowFBO, …)` … `EndPass()` — see
`cpp/scene/Light.cpp` for the exact shape). `runtime.LockOSThread()` stays:
GLFW and the Vulkan surface both require the main thread.

## 2.5 Optional features: ray tracing and other Vulkan-only paths

The common interface is frozen at "what OpenGL 4.1 and Vulkan can both do".
Anything beyond that uses two standard Go mechanisms, decided now so later
features have a place to land:

1. **Capability query** — `backend.Supports(renderer.FeatureRayTracing)`.
   The OpenGL backend returns `false` for everything; the Vulkan backend
   checks device extensions at `Init` time (ray tracing also needs the GPU
   to expose `VK_KHR_acceleration_structure` + `VK_KHR_ray_tracing_pipeline`
   or `VK_KHR_ray_query`, so even on Vulkan this can be false).

2. **Optional interfaces, discovered by type assertion** — the same pattern
   the standard library uses (`http.Flusher`, `io.ReaderFrom`). The optional
   API lives in `renderer/` (so scene code never imports the vulkan
   package), and only `VKBackend` implements it:

   ```go
   // renderer/raytracing.go — API sketch, finalized when the feature lands.
   type AccelHandle uint32

   type RayTracer interface {
       // Build a BLAS (bottom-level acceleration structure: the per-mesh
       // triangle BVH) from an existing vertex/index buffer pair.
       BuildBLAS(vbo BufferHandle, indices []uint32) AccelHandle
       // Build/refit the TLAS (top-level acceleration structure: the scene
       // of BLAS instances with transforms), once per frame if dynamic.
       BuildTLAS(instances []AccelInstance) AccelHandle
       // e.g. trace shadow rays for the current pass's writes.
       TraceShadows(tlas AccelHandle, u *Uniforms)
   }
   ```

   Call sites guard and degrade:

   ```go
   if rt, ok := backend.(renderer.RayTracer); ok &&
       backend.Supports(renderer.FeatureRayTracing) {
       rt.TraceShadows(tlas, &u)      // ray-traced shadows
   } else {
       light.RenderLight(...)         // shadow-map path (both backends)
   }
   ```

Recommendation for the *first* ray-tracing feature when the time comes:
**ray queries** (`VK_KHR_ray_query`) rather than a full ray-tracing pipeline.
Ray queries let the existing forward *fragment shader* cast rays ("is this
point in shadow?") against a TLAS with no new pipeline type and no SBT
(shader binding table — the table that dispatches per-geometry hit shaders in
a full ray-tracing pipeline). That means: hardware-accurate shadows/ambient
occlusion, Slang supports it, and the binding work in go-vulkan stays modest
(acceleration-structure build + one feature bit — see §6.3). The full
ray-tracing pipeline (ray-gen/miss/hit shaders, SBTs) only becomes worth it
for path tracing, and can be layered on the same `RayTracer` interface later.

The same two mechanisms cover future compute features (GPU clouds, water
simulation, particles): `FeatureCompute` + a `ComputeRunner` optional
interface. Nothing about them needs deciding today beyond this.

---

# Part 3 — How each backend implements the contract

## 3.1 The interface, method by method

For the OpenGL backend this is mostly a relocation exercise — every `gl.*`
call currently in `core/` and `scene/` moves behind the matching method. For
the Vulkan backend each method maps onto the machinery described in §3.2.
This section spells out what every interface method implies on each side.

### Lifecycle

**`ConfigureWindow()`**
- *OpenGL:* sets the GLFW window hints for a 4.1 core context (version,
  core profile, forward-compatible for macOS, 4× samples) — exactly today's
  `core/app.go` hint block. GLFW then creates the window *with* a GL context
  attached.
- *Vulkan:* one hint: `glfw.ClientAPI = glfw.NoAPI`. No GL context is
  created at all — Vulkan connects to the window later, through a
  `VkSurfaceKHR` created in `Init` [HTV: Surface and swapchain].

**`Init(window)`**
- *OpenGL:* `window.MakeContextCurrent()` + `gl.Init()` (loads the GL
  function pointers — the GLAD equivalent), set the global defaults that are
  per-pipeline state on Vulkan (depth test, cull, blend), create the white
  texture. Almost nothing can fail.
- *Vulkan:* the full boot sequence, which is why this method returns an
  error: instance → surface → physical-device pick → logical device with the
  1.2/1.3 feature chain → queue → memory allocator → swapchain + image views
  + depth buffer → per-frame data (command buffer, fence, semaphores,
  uniform ring) → samplers → bindless descriptor set → global pipeline
  layout → white texture. Every arrow is a call that can fail
  [HTV: Object hierarchy].

**`Shutdown()`**
- *OpenGL:* delete tracked GL objects; the context itself dies with the
  window.
- *Vulkan:* `vk.DeviceWaitIdle` first (never destroy what the GPU may still
  read [HTV: Cleanup]), then destroy everything in reverse creation order.
  Must complete **before** the window is destroyed (on Wayland the surface
  depends on the window's connection).

### Frame

**`BeginFrame()`**
- *OpenGL:* a no-op. GL has no explicit frame concept; the driver paces at
  swap time.
- *Vulkan:* wait + reset this frame's fence (the CPU throttle — without it,
  frame N+2 would overwrite buffers the GPU is still reading), acquire the
  next swapchain image (on `vk.ErrOutOfDateKHR`: recreate the swapchain and
  skip the frame), reset + begin the command buffer, reset the uniform-ring
  offset [HTV: Render loop].

**`EndFrame()`**
- *OpenGL:* `window.SwapBuffers()` — the driver's implicit "submit
  everything and present".
- *Vulkan:* the explicit version of what SwapBuffers hides: barrier the
  swapchain image to present layout, end the command buffer,
  `vk.QueueSubmit2` (wait the acquire semaphore **[frameIndex]**, signal the
  render semaphore **[imageIndex]**, signal the fence), `vk.QueuePresentKHR`
  (again handling out-of-date), advance the frame index.

**`BeginPass(target, w, h, clear)`**
- *OpenGL:* `glBindFramebuffer(target)` + `glViewport(0,0,w,h)` + `glClear`
  (depth always, color when `clear != nil`) [LOGL: Framebuffers]. Direct
  mapping — this method exists *because* these three calls must be fenced
  into one place for Vulkan's sake.
- *Vulkan:* transition the target image(s) to attachment layout with
  `vk.CmdPipelineBarrier2` [HTV: Images and layouts], `vk.CmdBeginRendering`
  with the clear values as attachment load-ops, set viewport (negative
  height for the backbuffer pass, positive for shadow passes — §3.2) and
  scissor, and record the pass *type* (main / shadow-2D / shadow-cube),
  which becomes part of the pipeline-cache key.

**`EndPass()`**
- *OpenGL:* rebind framebuffer 0.
- *Vulkan:* `vk.CmdEndRendering`; if the pass rendered a shadow target,
  barrier it to shader-read layout so the main pass can sample it. The
  swapchain image keeps its attachment layout until `EndFrame`'s present
  barrier.

**`SetCullFace(front)` / `SetDepthFunc(lequal)`**
- *OpenGL:* `glCullFace(FRONT|BACK)` / `glDepthFunc(LEQUAL|LESS)` —
  immediate state, as today.
- *Vulkan:* `vkCmdSetCullMode` / `vkCmdSetDepthCompareOp`, recorded into the
  command buffer as Vulkan 1.3 *dynamic state* — the reason these can stay
  immediate calls instead of forcing a different pipeline per combination
  (§6.1 adds the bindings).

### Resources

**`CreateShader(name, hasGeometry)`**
- *OpenGL:* read the generated GLSL from `shaders/gl/<name>.*.glsl`, compile
  each stage, link the program (today's `opengl/shader.go`, nearly
  unchanged), bind its uniform block to the shared UBO once.
- *Vulkan:* read SPIR-V from `shaders/vk/`, wrap each stage with
  `vk.CreateShaderModule`, store modules + the `hasGeometry` flag. **No
  pipeline is built here** — pipelines depend on (shader, pass type, vertex
  layout) and are built lazily at first draw (§3.2).

**`LoadTexture(path)`**
- *OpenGL:* decode with Go's image packages, `glGenTextures` +
  `glTexImage2D` + `glGenerateMipmap`, repeat wrap, linear filter
  [LOGL: Textures].
- *Vulkan:* decode, write pixels into a mapped staging buffer, then in an
  immediate-submit: barrier (undefined → transfer-dst) →
  `vk.CmdCopyBufferToImage` → barrier (→ shader-read). Create the image
  view, write it into the next free slot of the bindless 2D array with
  `vk.UpdateDescriptorSets`, return a handle that maps to that slot. (No
  mipmaps — accepted parity gap with the C++ backend.)

**`LoadCubemap(faces)`**
- *OpenGL:* six `glTexImage2D` calls on the cubemap face targets,
  clamp-to-edge [LOGL: Cubemaps].
- *Vulkan:* one 6-layer image created with the cube-compatible flag (§6.1),
  six copy regions (`BaseArrayLayer` = face index) from one staging buffer,
  a cube-type image view, registered in the bindless *cube* array.

**`WhiteTexture()`**
- *OpenGL:* a 1×1 white texture created during `Init`; returned handle is
  bound whenever a material has no texture.
- *Vulkan:* slot 0 of the bindless 2D array *is* a white pixel, written at
  `Init`; handle 0 = "no texture" samples white with zero special-casing in
  shaders.

**`UpdateTexture2D(h, w, h, pixels)`** (UI overlay)
- *OpenGL:* `glTexSubImage2D` (full `glTexImage2D` on first call / resize);
  the driver synchronizes internally.
- *Vulkan:* keep a persistent mapped staging buffer; `vk.MemCopy` the
  pixels, then record barrier → `vk.CmdCopyBufferToImage` → barrier into the
  current frame's command buffer *before* the main pass begins, so the
  sampled image is never mid-copy while in use.

**`DestroyTexture` / `DestroyBuffer` / `DestroyMesh` / `DestroyFramebuffer`**
- *OpenGL:* the matching `glDelete*` — safe immediately, the driver
  refcounts behind your back.
- *Vulkan:* nothing may be destroyed while any frame in flight might still
  read it: drain (`waitAllFrames`) or queue onto a per-frame trash list,
  then destroy view/image/buffer + free the allocation. Freed bindless
  slots are recycled (the partially-bound descriptor set tolerates holes).

**`CreateBuffer(data, dynamic)`**
- *OpenGL:* `glGenBuffers` + `glBufferData` with `STATIC_DRAW` /
  `DYNAMIC_DRAW`.
- *Vulkan:* `vk.VmaCreateBuffer` with vertex usage, host-visible and
  persistently mapped (sequential-write), `vk.MemCopy` the data in. Good
  enough at this scale; device-local + staging copy is the §6.2 upgrade.

**`UpdateBuffer(h, data)`**
- *OpenGL:* `glBufferData` re-specification; the driver ghosts the old
  storage if the GPU still uses it.
- *Vulkan:* no driver magic — the GPU may still be reading, so drain the
  frames in flight, then `vk.MemCopy`. Deliberately crude (C++ parity):
  mesh vertex rewrites are rare; if they become per-frame, move motion to
  the `Model` matrix instead (Part 7).

**`CreateMesh(vbo, indices)`**
- *OpenGL:* create a VAO (vertex array object) + EBO (element buffer
  object): bind the shared VBO, set the three vertex attributes
  (position/normal/uv, 32-byte stride), upload indices. The VAO snapshots
  all of it [LOGL: Hello Triangle].
- *Vulkan:* **VAOs don't exist** — the vertex layout is baked into the
  pipeline instead. Create a (host-visible) index buffer, store a mesh-table
  entry `{vbo, indexBuffer}`; the actual binds happen per draw.

**`CreateSkyboxMesh(verts)`**
- *OpenGL:* VAO + VBO with a single position attribute.
- *Vulkan:* vertex buffer + a mesh-table entry tagged with the *skybox
  vertex layout* — a different pipeline-cache key than the mesh layout.

**`CreateShadowMap2D(w, h)`**
- *OpenGL:* FBO (framebuffer object) + depth-only texture — nearest filter,
  clamp-to-border with a white border ("outside the map = fully lit"),
  `glDrawBuffer(GL_NONE)` — today's `light.go` code verbatim
  [LOGL: Shadow Mapping].
- *Vulkan:* one D32 image with depth-attachment *and* sampled usage; a 2D
  view serves as the render attachment and, paired with the white-border
  shadow sampler (§6.1), is registered in the bindless 2D array. A
  shadow-target table entry tracks the image's current layout so
  `BeginPass`/`EndPass` know which transition to record.

**`CreateShadowCubemap(w, h)`**
- *OpenGL:* FBO + depth cubemap attached as a *layered* target
  (`glFramebufferTexture`); the geometry shader routes each triangle to a
  face via `gl_Layer` [LOGL: Point Shadows].
- *Vulkan:* one D32 image, 6 array layers, cube-compatible flag; **two
  views** — a 2D-array(6) view as the attachment
  (`RenderingInfo.LayerCount = 6`, the geometry shader writes the layer
  index: same trick, Vulkan spelling) and a cube view registered in the
  bindless cube array for sampling.

### Draws

**`DrawMesh(shader, mesh, indexCount, u)`**
- *OpenGL:* `glUseProgram`, `marshalStd140(u)` → `glBufferSubData` into the
  shared UBO, bind the `u.Tex*` handles to fixed texture units 0–4,
  `glBindVertexArray`, `glDrawElements`.
- *Vulkan:* look up (or lazily build) the pipeline for (shader, current
  pass, mesh vertex layout); bind pipeline / descriptor set / vertex +
  index buffers where changed; copy `*u` into the uniform ring at the
  current offset and patch the five `Tex*` fields from handle → bindless
  slot; `vk.CmdPushConstants` with the ring entry's buffer device address;
  `vk.CmdDrawIndexed`; advance the ring offset. The shader dereferences the
  pushed address to read the block [HTV: Buffer device address].

**`DrawSkybox(shader, mesh, u)`**
- *OpenGL:* like `DrawMesh` but `glDrawArrays(0, 36)`; the caller brackets
  it with `SetDepthFunc(lequal)` [LOGL: Cubemaps].
- *Vulkan:* skybox-layout pipeline, same uniform snapshot, `vk.CmdDraw(36)`
  (non-indexed — §6.1 adds `CmdDraw`).

**`DrawFullscreenQuad(shader, tex)`** (UI overlay)
- *OpenGL:* UI program + the quad VAO absorbed from `utils.RenderQuad`,
  texture on unit 0, alpha blend already enabled.
- *Vulkan:* UI pipeline (alpha blend, no depth write, **no vertex input**);
  a 3-vertex `vk.CmdDraw` where the vertex shader generates a fullscreen
  triangle from the vertex index — cheaper than a quad and avoids a second
  topology. The CPU-side `imaging.FlipV` in `core/ui.go` stays on both
  backends: the negative viewport keeps OpenGL orientation conventions.

### Capabilities

**`Supports(feature)`**
- *OpenGL:* returns `false` for every feature — OpenGL 4.1 has no compute,
  no ray tracing.
- *Vulkan:* computed once during `Init` from the device's extension list and
  feature queries (e.g. `FeatureRayTracing` requires
  `VK_KHR_acceleration_structure` + `VK_KHR_ray_query` support), cached in a
  map.

## 3.2 Vulkan backend: setup and cross-cutting conventions (`vulkan/`)

Structure and all conventions mirror `cpp/vulkan/Backend.cpp`. The table maps
each piece to the go-vulkan calls that implement it; ✅ = working example in
the demo (`how_to_vulkan/main.go`).

| Backend piece | go-vulkan calls | Demo |
|---|---|---|
| Instance | `vk.CreateInstance` | ✅ |
| Surface | `window.CreateWindowSurface(instance, nil)` from go-gl/glfw — accepts the `vk.Instance` uintptr directly | ✅ |
| Physical device pick | `vk.EnumeratePhysicalDevices`, `vk.GetPhysicalDeviceProperties2`, `…QueueFamilyProperties`, `…SurfaceSupportKHR` | ✅ |
| Device + feature chain | `vk.CreateDevice` with `vk.Features{DynamicRendering, Synchronization2, BufferDeviceAddress, DescriptorIndexing, …}` — needs `ScalarBlockLayout` + `GeometryShader` bits added (§6.1) | ✅ |
| Memory | `vk.VmaCreateAllocator` (pure-Go VMA substitute, same call shape) | ✅ |
| Swapchain + resize | `vk.CreateSwapchainKHR` etc.; recreate on `vk.ErrOutOfDateKHR` / `vk.SuboptimalKHR` [HTV: Surface and swapchain] | ✅ |
| Depth buffer (D32) | `vk.VmaCreateImage` + `vk.CreateImageView` | ✅ |
| 2 frames in flight | per-frame command buffer, fence (created signaled), semaphores, mapped ring buffer with `vk.GetBufferDeviceAddress` [HTV: Frames in flight] | ✅ |
| Samplers | `vk.CreateSampler` ×4 (repeat / cube-linear / shadow-2D / shadow-cube) — shadow-2D needs clamp-to-border + white border added (§6.1) | partial |
| Bindless textures | one descriptor set, `sampler2D[256]` + `samplerCube[64]`, partially bound, update-after-bind: `vk.CreateDescriptorSetLayout/Pool`, `vk.AllocateDescriptorSets`, `vk.UpdateDescriptorSets` [HTV: Descriptor indexing] | ✅ |
| Pipeline layout | `vk.CreatePipelineLayout` + 8-byte `vk.PushConstantRange` (the block's address) | ✅ |
| Lazy pipeline cache | `vk.CreateGraphicsPipeline` keyed by (shader, pass type, vertex layout), with `Rendering: &vk.PipelineRenderingCreateInfo{…}` — dynamic rendering, no render-pass objects | ✅ |
| BeginFrame | `vk.WaitForFences`, `vk.ResetFences`, `vk.AcquireNextImageKHR`, `vk.ResetCommandBuffer`, `vk.BeginCommandBuffer` | ✅ |
| BeginPass / EndPass | `vk.CmdPipelineBarrier2` layout transitions [HTV: Images and layouts], `vk.CmdBeginRendering` / `vk.CmdEndRendering`, `vk.CmdSetViewport` / `vk.CmdSetScissor` | ✅ |
| DrawMesh | bind pipeline/descriptors/buffers, `vk.MemCopy` the Uniforms into the ring, patch `Tex*` slots, `vk.CmdPushConstants(&ringAddr)`, `vk.CmdDrawIndexed` | ✅ |
| EndFrame | `vk.EndCommandBuffer`, `vk.QueueSubmit2` — wait acquire-semaphore **[frameIndex]**, signal render-semaphore **[imageIndex]** (the two-semaphore indexing trap [HTV: Semaphores]) — then `vk.QueuePresentKHR` | ✅ |
| Texture upload | staging buffer → barrier → `vk.CmdCopyBufferToImage` → barrier to shader-read, inside an immediate-submit helper | ✅ |
| Cleanup | matching `Destroy*` for every create, after `vk.DeviceWaitIdle` | ✅ |

**OpenGL-convention bridging** (verbatim from the C++ backend — this is the
subtle stuff that makes both backends produce the same image):

- Main pass renders with a **negative-height viewport**, which flips Vulkan's
  y-down clip space back to OpenGL's y-up *and* cancels the winding flip, so
  front faces stay counter-clockwise.
- Shadow passes use a **positive** viewport (so the shadow-map's memory
  layout matches OpenGL and the sampling math in the shaders is unchanged)
  and therefore declare **clockwise** front faces.
- Vertex shaders remap clip-space depth from OpenGL's [-w, w] to Vulkan's
  [0, w] (already in the Slang sources).
- `SetCullFace` / `SetDepthFunc` map to `vkCmdSetCullMode` /
  `vkCmdSetDepthCompareOp` — Vulkan 1.3 dynamic state, no pipeline rebuild
  (bindings addition, §6.1).

**Go-specific care points** (Go issues, not Vulkan issues):

- cgo pointer rules are handled inside the bindings (config structs are plain
  Go literals; nested C memory is arena-allocated per call). Don't hold
  `unsafe.Pointer`s across calls.
- Mapped GPU memory is C memory: write through `vk.MemCopy` /
  `unsafe.Slice`, never by storing Go pointers into it.
- `vk.CmdPushConstants` takes an `unsafe.Pointer` — pass the address of the
  ring entry's `vk.DeviceAddress` exactly as demo line ~618 does.
- Keep vertex slices alive until upload calls return (trivially true with
  synchronous calls).

---

# Part 4 — Shaders: one source, two outputs

Do **not** port the GLSL 3.3 files in `go_deprecated/shaders/`. The
maintained shader set already exists in Slang with all bridging baked in:

| Slang file | Replaces (Go) | Notes |
|---|---|---|
| `forward.slang` | `light.*.glsl` (and `clouds` as main program) | Phong + shadows + PBR (physically based rendering) inputs |
| `depth.slang` | `depth.*.glsl` | sun shadow pass |
| `depth_cube.slang` | `depth_cube.{vert,geo,frag}.glsl` | point-light shadow pass, geometry shader |
| `skybox.slang` | `skybox.*.glsl` | |
| `common.slang` | — | the Uniforms struct; selects per-target resource model (`-DTARGET_VK`) |
| *(to write)* | `ui.slang` | needed for the UI pass; `clouds`/`water`/`depth_debug` later |

Toolchain (same as `cpp/CMakeLists.txt`, re-hosted in `overdrive_build.sh` or
a `go generate` step, since the Go build has no CMake):

- **OpenGL**: `slangc -target glsl -profile glsl_410 -preserve-params`, then
  the mechanical rewrites from `cpp/shaders/slang/downgrade.cmake` (clamp
  `#version` to 410, strip `layout(binding=…)`, array-initializer syntax)
  → `shaders/gl/`. The window already requests a 4.1 core context, so the
  output runs unchanged.
- **Vulkan**: `slangc -target spirv -emit-spirv-directly
  -fvk-use-scalar-layout` → `shaders/vk/`, loaded with
  `vk.CreateShaderModule` [HTV: Shaders — SPIR-V and Slang].

Switching the main program from `clouds` to `forward` is a feature *win*:
shadow mapping and the PBR material path return to the Go engine.

---

# Part 5 — Migration plan

Ordered so the engine runs at the end of every phase. Phases 1–3 are pure-GL
refactors; Vulkan starts in Phase 4.

**Phase 0 — Promote and dust off.**
`git mv go_deprecated go` (or to the repo root — decide once), `go mod tidy`,
bump the Go version and go-gl/glfw pins, delete `tutorial/` leftovers,
confirm the demo runs.

**Phase 1 — Interfaces + pass structure.**
Add `renderer/` (interface, handles, Uniforms struct) and `CreateBackend`.
Implement `GLBackend` by moving every `gl.*` call out of `core/` and
`scene/`. Restructure `App.Run` and `Light.RenderLight` into the pass-based
loop (§2.4). Replace all `gl.GetUniformLocation` code with Uniforms-struct
fills (temporarily marshaled to the old GLSL 3.3 uniforms by the GL backend —
loose-uniform upload keyed off a hardcoded location table is fine as a bridge
until Phase 3 brings the UBO).
*Exit criteria:* `grep -r "go-gl/gl" --include="*.go" .` matches only
`opengl/`; demo renders identically.

**Phase 2 — Scene-layer cleanup.**
`Material` → paths + handles, loading in `Mesh.setup()`. `core/ui.go` →
`UpdateTexture2D` + `DrawFullscreenQuad`. Delete `utils.RenderQuad`.
*Exit criteria:* `scene/` and `core/` have zero graphics imports.

**Phase 3 — Slang shaders.**
Share `cpp/shaders/slang/`, write `ui.slang`, write the build step, switch
the GL backend to the generated GLSL 4.10 + the std140 UBO upload path, and
make `forward` the main program.
*Exit criteria:* GL build renders (with shadows) from Slang-generated code
only.

**Phase 4 — Vulkan backend.**
First extend the bindings (§6.1 — each item is a small, testable PR against
go-vulkan). Then build `vulkan/` in the order the C++ backend was built and
howtovulkan.com teaches: device + swapchain → clear-only frames → forward
pass without shadows (bindless set + ring + lazy pipelines) → skybox →
2D shadows → cube shadows → UI. Wire `-backend` / `OVERDRIVE_BACKEND`. Run
with validation layers from day one [HTV: Validation layers].
*Exit criteria:* demo renders identically on both backends (screenshot
diff); swapchain survives resize.

**Phase 5 — Polish and ports.**
`depth_debug` / `clouds` / `water` Slang ports as wanted; optional parity
items the C++ backend also skipped: MSAA (multisample anti-aliasing),
mipmaps, tighter barriers, GPU timestamps.

**Phase 6 — First optional feature (when wanted): ray-query shadows.**
Bindings work from §6.3, `FeatureRayTracing` + `RayTracer` implementation on
`VKBackend`, guarded call site in the frame loop (§2.5). GL keeps the
shadow-map path forever.

---

# Part 6 — Required additions to the go-vulkan bindings

The bindings cover the tutorial path and that is ~90% of what the backend
needs. The gaps are exactly where the engine goes beyond the tutorial:
shadow maps, non-indexed draws, 1.3 dynamic state — and later, ray tracing.
Proposed signatures follow the package's conventions (config structs without
`sType`/`pNext`, `error` returns, handles as `uintptr`).

## 6.1 Blocking — the backend cannot be written without these

**1. Non-indexed draw** (`vk/cmd.go`) — skybox (36 vertices) and the
fullscreen triangle:

```go
func CmdDraw(cb CommandBuffer, vertexCount, instanceCount, firstVertex, firstInstance uint32)
```

**2. Vulkan 1.3 dynamic-state setters** (`vk/cmd.go`) — what lets
`SetCullFace`/`SetDepthFunc` stay immediate calls. Core in 1.3, no feature
bit required:

```go
func CmdSetCullMode(cb CommandBuffer, mode CullModeFlags)
func CmdSetFrontFace(cb CommandBuffer, ff FrontFace)
func CmdSetDepthCompareOp(cb CommandBuffer, op CompareOp)
```

plus the matching constants in `vk/types.go` (`DynamicStateCullMode`,
`DynamicStateFrontFace`, `DynamicStateDepthCompareOp` — today only
`Viewport`/`Scissor` exist). Front-face must be dynamic (or baked per
pipeline) because main and shadow passes wind differently (§3.2).

**3. Cubemap images** (`vk/resources.go`, `vk/types.go`) —
`ImageCreateInfo` has no `Flags` field and `ImageViewType` only defines 2D:

```go
type ImageCreateFlags uint32
const ImageCreateCubeCompatible = ImageCreateFlags(C.VK_IMAGE_CREATE_CUBE_COMPATIBLE_BIT)

type ImageCreateInfo struct {
    Flags ImageCreateFlags // NEW
    // … existing fields …
}

const (
    ImageViewTypeCube    = ImageViewType(C.VK_IMAGE_VIEW_TYPE_CUBE)     // sampling view
    ImageViewType2DArray = ImageViewType(C.VK_IMAGE_VIEW_TYPE_2D_ARRAY) // layered attachment view
)
```

The surrounding plumbing already exists: `ImageSubresourceRange` and
`BufferImageCopy` both expose `BaseArrayLayer`/`LayerCount`, so layered views
and one-copy-per-face cubemap uploads work as soon as the constants do.

**4. Geometry-shader support** (`vk/device.go`, `vk/types.go`) — the point
shadow pass renders all 6 cube faces in one draw via a geometry shader
writing the render-target layer (the Vulkan spelling of
[LOGL: Point Shadows]' `gl_Layer`):

```go
type Features struct {
    GeometryShader bool // NEW — plain VkPhysicalDeviceFeatures (1.0) bit
    // …
}
const ShaderStageGeometry = ShaderStageFlags(C.VK_SHADER_STAGE_GEOMETRY_BIT)
```

(`PipelineShaderStageCreateInfo` already takes any stage flag, so pipeline
creation needs no change.) *Alternative without a geometry shader:* render 6
single-layer passes, one per face view — fewer binding changes, but 6× the
draws and it diverges from `depth_cube.slang` and the C++ backend.
Recommendation: extend the bindings.

**5. Scalar-block-layout feature bit** (`vk/device.go`) — the SPIR-V is
compiled with `-fvk-use-scalar-layout`; without
`VkPhysicalDeviceVulkan12Features.scalarBlockLayout` enabled, validation
rejects it:

```go
type Features struct {
    ScalarBlockLayout bool // NEW — 1.2 feature
    // …
}
```

**6. Shadow-sampler border** (`vk/resources.go`, `vk/types.go`) —
`CreateSampler` hardcodes an opaque-black border and `SamplerAddressMode`
lacks clamp-to-border; the sun shadow map needs clamp-to-border with an
**opaque white** border (background = "fully lit"):

```go
const SamplerAddressModeClampToBorder = SamplerAddressMode(C.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_BORDER)

type BorderColor int32
const (
    BorderColorOpaqueBlackFloat = BorderColor(C.VK_BORDER_COLOR_FLOAT_OPAQUE_BLACK)
    BorderColorOpaqueWhiteFloat = BorderColor(C.VK_BORDER_COLOR_FLOAT_OPAQUE_WHITE)
)

type SamplerCreateInfo struct {
    BorderColor BorderColor // NEW; zero value keeps today's black default
    // … existing fields …
}
```

## 6.2 Wanted — quality/performance; workarounds exist

**7. Buffer→buffer copy** (`vk/cmd.go`) — staging uploads into device-local
vertex/index buffers [HTV: Buffers → staging upload]:

```go
type BufferCopy struct{ SrcOffset, DstOffset, Size uint64 }
func CmdCopyBuffer(cb CommandBuffer, src, dst Buffer, regions []BufferCopy)
```

*Workaround:* the pure-Go VMA substitute allocates host-visible memory for
sequential-write allocations, so meshes can live in mappable memory like the
demo's do. Add the copy path when profiling asks for it.

**8. Mipmap generation** (`vk/cmd.go`) — parity with `gl.GenerateMipmap`
[LOGL: Textures]; the C++ Vulkan backend also skipped this:

```go
func CmdBlitImage(cb CommandBuffer, src Image, srcLayout ImageLayout,
    dst Image, dstLayout ImageLayout, region ImageBlit, filter Filter)
```

**9. MSAA resolve attachments** — `RenderingAttachmentInfo` lacks
`ResolveImageView`/`ResolveMode`. Only needed to match the GL build's 4×
MSAA; C++ skipped it too.

## 6.3 Future — ray tracing (Phase 6, sized here so it's not a surprise)

For the recommended first step (**ray queries** in the fragment shader,
§2.5):

- Extension plumbing: `VK_KHR_acceleration_structure`,
  `VK_KHR_ray_query`, `VK_KHR_deferred_host_operations` (dependency), plus
  `Features` bits `AccelerationStructure` and `RayQuery`.
- Acceleration-structure API: `CreateAccelerationStructureKHR` /
  `DestroyAccelerationStructureKHR`,
  `GetAccelerationStructureBuildSizesKHR`,
  `CmdBuildAccelerationStructuresKHR`,
  `GetAccelerationStructureDeviceAddressKHR`, and the geometry/build-info
  structs (triangles-from-buffer-device-address, instance buffers).
- One new descriptor type (`DescriptorTypeAccelerationStructure`) so the
  TLAS can be bound next to the bindless textures.
- New buffer-usage flags (`AccelerationStructureStorage`,
  `AccelerationStructureBuildInputReadOnly`, `ShaderBindingTable` later).

The full ray-tracing *pipeline* (ray-gen/miss/hit stages,
`CreateRayTracingPipelinesKHR`, `CmdTraceRaysKHR`, shader-binding-table
helpers) is only needed if Overdrive later wants path tracing; it layers on
top of the same acceleration-structure API.

## 6.4 Explicitly not needed

- **Surface creation** — go-gl/glfw's `window.CreateWindowSurface` accepts
  the `vk.Instance` uintptr directly.
- **Negative-height viewport** — `vk.Viewport` fields are plain `float32`;
  negative height already works (core since Vulkan 1.1).
- **Layered rendering** — `vk.RenderingInfo.LayerCount` already exists.
- **Push constants, buffer device address, bindless descriptors,
  synchronization2 submits** — all present and demo-proven.
- **Legacy render passes / framebuffer objects** — dynamic rendering
  everywhere, matching the C++ backend and the bindings' design.

---

# Part 7 — Risks and open questions

- **cgo call overhead** (~50–100 ns per call) is noise at this draw-call
  count; if scenes ever grow a thousandfold, batched recording becomes a
  design question — not now.
- **The pure-Go VMA substitute** makes one dedicated allocation per resource
  (driver allocation limits are typically 4096). Fine at this scale; revisit
  with sub-allocation only if asset counts explode.
- **`UpdateBuffer` drains the GPU** before writing (C++ parity — mesh moves
  are rare). If moves become per-frame, switch movement to the `Model`
  matrix already in `Uniforms` instead of rewriting vertices.
- **std140 marshal correctness** is the one new hand-written layout in this
  design (Vulkan gets its layout for free). Cover `marshalStd140` with a
  unit test against offsets dumped from the generated GLSL once at Phase 3.
- **Wayland teardown order**: destroy the backend before the window (see the
  comment in `cpp/core/App.cpp`'s destructor); replicate in `App.Shutdown`.
- **Where the module lands** — `go/` subdirectory vs. repository root:
  root makes `go install github.com/Zephyr75/overdrive` work but mixes Go
  and C++ trees. Decide at Phase 0.
