# Multi-Backend Abstraction Plan

Goal: decouple Overdrive from OpenGL so the same scene/renderer code can run on OpenGL 4.1, Vulkan 1.2+, and DirectX 12 without rewrites. The approach is a **Render Hardware Interface (RHI)** — a thin, explicit abstraction layer inspired by Unreal's RHI, bgfx, wgpu, and Sokol.

---

## 1. Current State

OpenGL leaks directly into gameplay/scene code:

| Concern | Current location | GL coupling |
|---|---|---|
| Window + context | `core/app.go` | GLFW hints, `gl.Init`, `gl.Enable` |
| Main loop | `core/app.go:Run` | `gl.ClearColor`, `gl.Clear`, `gl.Viewport`, `SwapBuffers` |
| Shaders | `opengl/shader.go` | `gl.CreateShader`, `gl.CompileShader`, `gl.LinkProgram` |
| Textures | `opengl/texture.go` | `gl.GenTextures`, `gl.TexImage2D` |
| Meshes | `scene/mesh.go` | VAO/VBO/EBO, `gl.VertexAttribPointer`, `gl.DrawElements` |
| Lights/shadows | `scene/light.go` | FBOs, depth textures, `gl.CullFace` |
| Uniforms | `scene/mesh.go:draw` | `gl.GetUniformLocation` + per-frame string lookups |
| UI | `core/ui.go` | Off-screen FBO + quad |
| Scene dispatch | `scene/scene.go` | Calls all of the above |

GLSL 3.3 shaders live in `shaders/*.glsl` and embed OpenGL-specific conventions (attribute location layouts, sampler binding via `glUniform1i`).

---

## 2. Target Architecture

Three layers:

```
+----------------------------------------------------+
|  Game / Scene / ECS  (backend-agnostic)            |  <- main.go, scene/, ecs/
+----------------------------------------------------+
|  Renderer              (high-level, stateless)     |  <- renderer/
|    - Scene graph traversal                         |
|    - Frame graph / render passes                   |
|    - Material system                               |
+----------------------------------------------------+
|  RHI                   (low-level, per-backend)    |  <- rhi/, rhi/gl, rhi/vk, rhi/dx12
|    - Device, Swapchain, CommandBuffer              |
|    - Buffer, Texture, Pipeline, Shader             |
|    - BindGroup / DescriptorSet                     |
+----------------------------------------------------+
|  Platform              (window + input)            |  <- platform/
|    - Surface creation (GLFW/Win32/SDL)             |
+----------------------------------------------------+
```

**Key principle:** nothing above the RHI line imports `go-gl/gl`, Vulkan bindings, or D3D12 bindings.

---

## 3. RHI Interface (Go)

Package `rhi/` defines opaque handle types and a `Device` interface. All resources are created through the device; the device is backed by a concrete backend.

### 3.1 Handles (opaque)

```go
type BufferHandle   uint64
type TextureHandle  uint64
type SamplerHandle  uint64
type ShaderHandle   uint64
type PipelineHandle uint64
type BindGroupHandle uint64
type RenderPassHandle uint64
```

Handles are backend-agnostic IDs. Each backend keeps its own table (GLuint / VkBuffer / ID3D12Resource*).

### 3.2 Core interfaces

```go
package rhi

type Backend int
const (
    BackendOpenGL Backend = iota
    BackendVulkan
    BackendD3D12
)

type Device interface {
    Backend() Backend

    // Resource creation
    CreateBuffer(desc BufferDesc) BufferHandle
    CreateTexture(desc TextureDesc) TextureHandle
    CreateSampler(desc SamplerDesc) SamplerHandle
    CreateShader(desc ShaderDesc) (ShaderHandle, error)
    CreatePipeline(desc PipelineDesc) PipelineHandle
    CreateBindGroupLayout(desc BindGroupLayoutDesc) BindGroupLayoutHandle
    CreateBindGroup(desc BindGroupDesc) BindGroupHandle

    // Frame
    BeginFrame() CommandBuffer
    Submit(cb CommandBuffer)
    Present()

    // Teardown
    Destroy(h any)
    Shutdown()
}

type CommandBuffer interface {
    BeginRenderPass(desc RenderPassDesc)
    EndRenderPass()

    SetPipeline(p PipelineHandle)
    SetBindGroup(slot uint32, bg BindGroupHandle, dynamicOffsets []uint32)
    SetVertexBuffer(slot uint32, b BufferHandle, offset uint64)
    SetIndexBuffer(b BufferHandle, offset uint64, format IndexFormat)
    SetViewport(x, y, w, h float32)
    SetScissor(x, y, w, h int32)

    Draw(vertexCount, instanceCount, firstVertex, firstInstance uint32)
    DrawIndexed(indexCount, instanceCount, firstIndex, baseVertex, firstInstance uint32)

    CopyBufferToBuffer(src, dst BufferHandle, srcOffset, dstOffset, size uint64)
    CopyBufferToTexture(src BufferHandle, dst TextureHandle, region TextureRegion)
}
```

### 3.3 Descriptor structs

Descriptor structs mirror modern explicit APIs (WebGPU-style is the cleanest template):

```go
type BufferDesc struct {
    Size  uint64
    Usage BufferUsage // Vertex | Index | Uniform | Storage | CopySrc | CopyDst
    Data  []byte      // optional initial upload
}

type TextureDesc struct {
    Width, Height, Depth uint32
    MipLevels, ArrayLayers uint32
    Format  Format         // RGBA8Unorm, D32Float, BC7, ...
    Usage   TextureUsage   // Sampled | Storage | RenderTarget | DepthStencil
    Dim     TextureDim     // 2D | 3D | Cube
    Samples uint32
}

type ShaderDesc struct {
    Stage      ShaderStage     // Vertex | Fragment | Geometry | Compute
    EntryPoint string
    Code       []byte          // SPIR-V (Vulkan), DXIL (DX12), GLSL source (GL)
    CodeType   ShaderCodeType  // SPIRV | DXIL | GLSL | HLSL | WGSL
}

type PipelineDesc struct {
    Shaders      []ShaderHandle       // vertex+fragment (+geometry)
    VertexLayout VertexLayout         // attributes, strides
    BindGroups   []BindGroupLayoutHandle
    DepthStencil DepthStencilState
    Raster       RasterState          // CullMode, FillMode, FrontFace
    Blend        BlendState
    ColorFormats []Format
    DepthFormat  Format
    Topology     PrimitiveTopology
}

type BindGroupLayoutDesc struct {
    Entries []BindGroupLayoutEntry // {binding, type: Uniform|Storage|Sampler|Texture, stages}
}

type RenderPassDesc struct {
    ColorAttachments []ColorAttachment // {texture, loadOp, storeOp, clearColor}
    DepthAttachment  *DepthAttachment
}
```

### 3.4 Why this shape

- **Pipeline-state-object (PSO) model** matches Vulkan/DX12 natively and is emulated on GL (track currently bound program + state).
- **Bind groups / descriptor sets** replace per-draw `glUniform1i` string lookups — huge CPU win and maps to `VkDescriptorSet` / DX12 root signatures.
- **Explicit render passes** let Vulkan drive subpasses and DX12 drive resource barriers; on GL, `BeginRenderPass` just binds an FBO and clears.
- **Command buffers** let each backend record work deferred (Vk/DX12) or immediately (GL) behind the same API.

---

## 4. Backend Mapping

| RHI concept | OpenGL 4.1 | Vulkan | DX12 |
|---|---|---|---|
| `Device` | GL context | `VkDevice` + queues | `ID3D12Device` + queues |
| `Swapchain` | GLFW default framebuffer | `VkSwapchainKHR` | `IDXGISwapChain3` |
| `Buffer` | VBO/EBO/UBO | `VkBuffer` + `VkDeviceMemory` | `ID3D12Resource` |
| `Texture` | Texture object | `VkImage` + `VkImageView` | `ID3D12Resource` + SRV |
| `Sampler` | Sampler object | `VkSampler` | static sampler |
| `Shader` | GLSL compile | SPIR-V module | DXIL blob |
| `Pipeline` | program + cached state | `VkPipeline` | `ID3D12PipelineState` |
| `BindGroup` | uniform bindings cache | `VkDescriptorSet` | descriptor heap range |
| `RenderPass` | FBO bind + clear | `VkRenderPass` + `VkFramebuffer` | OMSetRenderTargets + barriers |
| `CommandBuffer` | direct GL calls | `VkCommandBuffer` | `ID3D12GraphicsCommandList` |

### 4.1 OpenGL backend (`rhi/gl/`)

- Reuses existing `go-gl/gl` code; easiest migration path.
- `CommandBuffer` is a thin wrapper that calls GL immediately (no real recording).
- PSO is a struct holding shader program + cached raster/depth/blend state; `SetPipeline` diffs against last-bound state to minimize GL calls.
- BindGroup = slice of `{binding, kind, handle}`; `SetBindGroup` emits `glUniform1i`/`glBindBufferBase`/`glBindTextureUnit`.
- Render pass = `glBindFramebuffer` + `glClear` using `loadOp`.

### 4.2 Vulkan backend (`rhi/vk/`)

- Use `github.com/vulkan-go/vulkan` or `github.com/goki/vulkan`.
- Proper command pools per-frame, ring of 2–3 frames in flight.
- Requires explicit memory allocator — wrap VMA via cgo, or start with per-buffer `vkAllocateMemory` and optimize later.
- Resource barriers inferred from RHI attachment load/store ops and bind group usage hints.

### 4.3 DirectX 12 backend (`rhi/dx12/`)

- Use `github.com/rewrking/go-dx12` or generate bindings via `go-d3d12`.
- Descriptor heaps: one CBV/SRV/UAV heap + one sampler heap, ring-allocated per frame.
- Root signature generated from `BindGroupLayoutDesc`.
- Fences for CPU/GPU sync.

---

## 5. Shader Strategy

Three viable options, in order of increasing engineering cost:

### Option A — per-backend source trees (pragmatic start)
Keep `shaders/*.glsl`; add `shaders/*.hlsl` and precompiled `shaders/*.spv` later. Author each shader once per backend. Simple, no tooling, diverges over time.

### Option B — GLSL source → SPIR-V → cross-compile (recommended)
Author in **GLSL 4.50 / Vulkan dialect**. Pipeline:

```
GLSL (source)
  │ glslang / shaderc  (build-time or first-run)
  ▼
SPIR-V (.spv)
  │ spirv-cross
  ├─► GLSL 4.1           (OpenGL backend)
  ├─► SPIR-V (native)    (Vulkan backend)
  └─► HLSL → dxc → DXIL  (DX12 backend)
```

Drive from a Go build step that shells out to `glslangValidator` and `spirv-cross`. Cache by hash. Ship compiled artifacts in `shaders/compiled/`.

### Option C — Slang or WGSL as authoring language
Overkill for now; flag as future upgrade path.

**Decision:** start with A during migration (GL still works), introduce B once a second backend lands.

---

## 6. Uniform & Bind Group Redesign

Current mesh draw calls do 20+ `gl.GetUniformLocation(...)` per frame (see `scene/mesh.go:310-354`). Replace with:

- **Frame-uniform UBO**: view matrix, projection, camera pos, time.
- **Pass-uniform UBO**: light array, shadow matrices, far plane.
- **Material UBO**: ambient/diffuse/specular/shininess.
- **Per-object UBO or push-constant**: model matrix.

These map to bind group slots 0–3 and cost zero per-frame string lookups. Uniform locations get resolved once at pipeline-creation time in the GL backend.

---

## 7. Package Layout (target)

```
overdrive/
├── main.go
├── platform/
│   ├── window.go            // Surface interface
│   ├── glfw_gl.go           // GLFW + GL context
│   ├── glfw_vulkan.go       // GLFW + VkSurfaceKHR
│   └── win32_dx12.go        // raw Win32 + HWND
├── rhi/
│   ├── types.go             // enums, descs, handles
│   ├── device.go            // Device interface
│   ├── command.go           // CommandBuffer interface
│   ├── gl/                  // existing opengl/ migrates here
│   │   ├── device.go
│   │   ├── buffer.go
│   │   ├── texture.go
│   │   ├── pipeline.go
│   │   └── shader.go
│   ├── vk/                  // future
│   └── dx12/                // future
├── renderer/
│   ├── renderer.go          // owns rhi.Device, frame graph
│   ├── pass_shadow.go       // depth pass (was scene/light.go render code)
│   ├── pass_forward.go      // main lit pass
│   ├── pass_skybox.go
│   ├── pass_ui.go
│   └── material.go          // material → pipeline mapping
├── scene/                   // unchanged conceptually, no GL
│   ├── mesh.go              // Mesh holds rhi handles, not GLuints
│   ├── light.go
│   ├── camera.go
│   └── scene.go
├── shaders/
│   ├── src/                 // GLSL 4.50 Vulkan-dialect source
│   └── compiled/            // .spv, .glsl, .dxil (generated)
├── core/
│   └── app.go               // owns platform.Window + renderer.Renderer
├── ecs/  input/  physics/  algorithms/  settings/  utils/    // unchanged
└── opengl/                  // DELETED — moved into rhi/gl/
```

---

## 8. Migration Phases

Phased so `main` stays runnable at every step.

### Phase 1 — Introduce RHI surface (no behavior change)
- Add `rhi/` package with `Device`, `CommandBuffer`, descriptor structs.
- Implement `rhi/gl/` wrapping current GL code 1:1.
- `core/app.go` gets a `rhi.Device` but still uses GL directly alongside it.
- **Exit criteria:** RHI compiles; no callers yet.

### Phase 2 — Port resources to RHI
- `scene/mesh.go` stores `BufferHandle` instead of `vbo/vao/ebo`; `setup()` uses `Device.CreateBuffer`.
- `opengl/texture.go` → `rhi/gl/texture.go`; callers use `Device.CreateTexture`.
- Shader compilation moves behind `Device.CreateShader`.
- **Exit criteria:** zero `go-gl/gl` imports outside `rhi/gl/`.

### Phase 3 — Port draw calls
- Introduce `renderer/` package; move `Scene.RenderScene`, `Light.RenderLight`, `Scene.RenderSkybox`, `renderUI` into render passes.
- Passes record commands into a `CommandBuffer`.
- `core/app.go:Run` loop becomes: `BeginFrame → passes → Submit → Present`.
- **Exit criteria:** `scene/` has zero rendering code; only data.

### Phase 4 — Pipeline + bind group migration
- Replace per-draw `glGetUniformLocation` spam with `BindGroup` layouts resolved once.
- Materials become `PipelineHandle` + `BindGroupHandle`.
- **Exit criteria:** one GL program = one pipeline object; uniform strings gone from hot path.

### Phase 5 — Second backend (Vulkan recommended first)
- Add `rhi/vk/` + `platform/glfw_vulkan.go`.
- Wire shader cross-compilation (Option B above).
- Select backend via CLI flag or env var (`OVERDRIVE_BACKEND=vulkan`).
- **Exit criteria:** sphere demo runs on both GL and Vulkan, identical visuals.

### Phase 6 — DirectX 12 backend (Windows only)
- Add `rhi/dx12/` behind `//go:build windows`.
- Add `platform/win32_dx12.go` (or reuse GLFW native HWND).
- **Exit criteria:** sphere demo runs on GL, Vulkan, and DX12.

### Phase 7 — Polish
- Resource lifetime / destruction API.
- Pipeline cache serialization.
- Debug markers (`KHR_debug`, `VK_EXT_debug_utils`, PIX events).
- Validation wrapper (`rhi/debug/`) that checks handle lifetime + layout transitions.

---

## 9. Risks & Open Questions

- **Go + Vulkan ergonomics.** Go's Vulkan bindings are verbose and GC interacts poorly with mapped memory. Consider cgo wrapper around a C helper library (VMA, volk).
- **Shader semantics drift.** GLSL 3.3 features (e.g., `gl_FragCoord` origin, row-vs-column matrix layout) differ from Vulkan. The spirv-cross roundtrip catches most but not all.
- **Depth range.** GL uses [-1,1], Vulkan/DX use [0,1]. Fix by using `GL_ARB_clip_control` on GL and keeping a single convention, or by patching projection matrices per backend.
- **Coordinate system.** Vulkan's Y flip in NDC vs GL. Handle by flipping viewport or negating clip-space Y in the vertex shader.
- **Threading.** Vulkan/DX12 benefit from multi-threaded command recording. The RHI should allow `Device.BeginCommandBuffer()` from worker threads in the future; GL backend keeps single-threaded invariant.
- **Scope.** Rewriting rendering while also adding Vulkan is a large undertaking. Phases 1–4 (pure GL refactor) deliver value even if Vulkan/DX12 are never finished.

---

## 10. First Concrete PRs

1. `rhi: skeleton package with types and Device interface` — no behavior change.
2. `rhi/gl: port shader compilation behind Device.CreateShader` — `opengl/shader.go` callers updated.
3. `rhi/gl: port textures behind Device.CreateTexture` — `opengl/texture.go` callers updated.
4. `scene/mesh: store rhi.BufferHandle instead of raw GLuints`.
5. `renderer: extract forward pass from scene.RenderScene`.

Each is reviewable on its own and keeps `main` green.
