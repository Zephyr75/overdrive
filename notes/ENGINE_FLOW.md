# ENGINE_FLOW.md — how a frame gets drawn

This document is the reading guide to `src/`. It follows one frame from
`main()` down to the GPU, then walks the `renderer.Backend` contract method by
method, showing what the Vulkan backend does with each one and why.

`ARCHITECTURE.md` is the map (where every package and symbol lives) and
`FEATURES.md` is the feature list with the reasoning behind each one.
`tmp/BACKEND_DECISION.md` is where the interface is *going*. This is the operational
document: what actually happens, in order.

An OpenGL 4.1 backend existed until 2026-08-05. Where a decision here only makes
sense as a legacy of it — the y-up clip space, the `[-w, w]` projections, the
16-byte uniform cells — that is called out rather than left as an unexplained
convention.

Learning links: **[LOGL]** points at learnopengl.com, **[HTV]** at
howtovulkan.com, whose stack (dynamic rendering, buffer device address,
descriptor indexing, synchronization2, VMA) is the one this backend uses.

---

## Contents

0. [The `Backend` contract by how often it is called](#0-the-backend-contract-by-how-often-it-is-called)
1. [The layers](#1-the-layers)
2. [Startup, in order](#2-startup-in-order)
3. [The frame loop](#3-the-frame-loop-coreapprun)
4. [The `renderer.Backend` contract, method by method](#4-the-rendererbackend-contract-method-by-method)
5. [Conventions that keep the image right side out](#5-conventions-that-keep-the-image-right-side-out)
6. [Where to look when something is wrong](#6-where-to-look-when-something-is-wrong)
7. [Who owns what, and what dies when](#7-who-owns-what-and-what-dies-when)

---

## 0. The `Backend` contract by how often it is called

`renderer.Backend`'s 25 methods are declared by **resource type** — textures,
buffers, meshes, shaders, targets, draws. That is the wrong axis for remembering
*where a Vulkan call sits in a frame*. This table is the other axis: how often
each method runs. §4 walks the same methods in interface order, with the
reasoning; this is the index.

The obscure Vulkan names get easier once they are filed by frequency —
`vkAcquireNextImageKHR` is "the per-frame one", `vkCmdPipelineBarrier2` is "the
per-pass one", `vkCmdPushConstants` is "the per-draw one".

### Once, at startup — 3 methods

| Method | What it does |
|---|---|
| `ConfigureWindow` | `WindowHint(ClientAPI, NoAPI)` — there is no context to create |
| `Init` | `CreateInstance` → surface → `EnumeratePhysicalDevices` → queue family → `CreateDevice` → `VmaCreateAllocator` → swapchain → `CreateCommandPool` → per-frame data → samplers → descriptors → pipeline layout → default textures |
| `Shutdown` | `DeviceWaitIdle`, then destroy everything in reverse creation order (see §7) |

### Once per resource, at load time — 9 methods

| Method | What it does |
|---|---|
| `CreateShader` | `CreateShaderModule` ×2-3. **No pipeline yet** — built lazily per (pass, layout) |
| `LoadTexture` | `VmaCreateImage` + staging buffer + `immediateSubmit(CmdCopyBufferToImage)` + `CreateImageView` + bindless descriptor write |
| `LoadCubemap` | One 6-layer `CubeCompatible` image, six faces staged contiguously, one copy |
| `WhiteTexture` | Returns `0` — handle 0 *is* bindless slot 0 |
| `CreateBuffer` | `VmaCreateBuffer` host-visible + persistently mapped + `MemCopy` |
| `CreateMesh` | Pair the vertex handle with an index buffer and record the layout. No VAO equivalent — the layout keys the pipeline |
| `CreateRenderTarget` | Image usable as attachment *and* sampled, plus **two** views for cubes: 2D-array to attach, cube to sample |

### On demand, rarely — 6 methods

| Method | What it does |
|---|---|
| `UpdateBuffer` | `waitAllFrames()` **then** memcpy. This is a full GPU drain |
| `DestroyTexture` | `waitAllFrames()`, destroy view + image + staging |
| `DestroyBuffer` | `waitAllFrames()`, `VmaDestroyBuffer` |
| `DestroyMesh` | `waitAllFrames()`, destroy the index buffer |
| `DestroyRenderTarget` | `waitAllFrames()`, destroy view + image |
| `Supports` | `false` — the seam for ray tracing and compute (§4.10) |

### Once per frame — 4 methods

| Method | What it does |
|---|---|
| `BeginFrame` | `WaitForFences` (the CPU throttle) → `AcquireNextImageKHR` → `ResetFences` → rewind ring → `drainRetired` → `ResetCommandBuffer` + `BeginCommandBuffer` → `CmdBindDescriptorSets` → flush staged uploads |
| `UpdateTexture2D` | Memcpy into a mapped staging buffer, **defer** the copy to the next `BeginFrame`. Costs the overlay one frame of latency |
| `EndFrame` | Barrier to `PresentSrcKHR` → `EndCommandBuffer` → `QueueSubmit2` (wait acquire sem, signal image's render sem, signal fence) → `QueuePresentKHR` → advance frame slot |

### Once per pass, ×3 a frame — 5 methods

Two shadow passes (depth-only, no colour clear) then the main backbuffer pass.

| Method | What it does |
|---|---|
| `BindFrameUniforms` | Memcpy 1184 B into the ring, cache its device address for the pass's draws |
| `BeginPass` | `imageBarrier` into attachment layout → `CmdBeginRendering` (load ops carry the clear) → `CmdSetViewport` → `CmdSetScissor` → re-issue dynamic state |
| `SetCullFace` | `CmdSetCullMode` — dynamic state, no extra pipeline |
| `SetDepthFunc` | `CmdSetDepthCompareOp` — dynamic state |
| `EndPass` | `CmdEndRendering`, and for a shadow target `imageBarrier` depth-attachment → shader-read-only |

### Once per draw, ~15 a frame — 1 method

| Method | What it does |
|---|---|
| `Draw` | `getPipeline(shader, pass, mesh layout)` (skipped if unchanged) → memcpy 128 B into the ring → `CmdPushConstants` (two 8-byte device addresses) → bind vertex (+ index) → `CmdDrawIndexed` or `CmdDraw` |

The mesh carries its own vertex layout, count and indexed-ness, so one entry
point serves face groups, the skybox cube and the UI overlay alike.

### What this table makes obvious

* **Vulkan front-loads.** Almost everything expensive is startup or load time.
  The per-frame and per-draw rows are short — that is the whole point of the API.
* **The per-draw row is deliberately thin.** The uniform block is split by
  update frequency, so a draw sends 128 bytes of transform and material rather
  than the whole 1.3 KB of camera and light state. That block goes out once per
  pass instead, in `BindFrameUniforms`.
* **`waitAllFrames` appears in five methods.** Every one is a full pipeline
  drain. They are all rare by design — if one starts running per frame,
  throughput collapses.
* **`UpdateTexture2D` is per-frame, not per-resource.** It is the UI overlay, and
  it is the only reason the deferred-upload machinery (`pendingUploads`,
  `retire`, `drainRetired`) exists.

---

## 1. The layers

```
main.go            builds an App, loads a Scene, builds an ECS World
  │
core/              App.NewApp (window + backend), App.Run (the frame loop), renderUI
  │
scene/  ecs/       meshes, lights, camera, skybox, materials, physics entities
input/  physics/   — plain Go, zero graphics calls
  │
renderer/          the abstraction: Backend interface, opaque handles, the two uniform structs
  │
vulkan/            the only package that may import vk.*
```

The rule above the line: **nothing in `scene/`, `core/`, `ecs/`, `input/` or
`physics/` imports a graphics API.** They own handles (`renderer.MeshHandle`,
`renderer.TextureHandle`, …), which are opaque integers the backend interprets
in its own table. This is why `go test ./...` needs no GPU, and it is why the
abstraction is kept with a single backend (`tmp/BACKEND_DECISION.md` §4).

The rule inside a frame: **clears and viewports exist only inside
`Backend.BeginPass`.** No free-floating clear calls anywhere else.

---

## 2. Startup, in order

| Step | Code | What happens |
|---|---|---|
| 0 | `settings.Load` | `main.go` decodes the file named by `-config` (`configs/vulkan.toml` by default) over the defaults — the engine's only configuration input. Everything below reads the result, so it has to run before `core.NewApp`. |
| 1 | `vulkan.New()` | Called directly by `core.NewApp` and held as a `renderer.Backend`, which is what keeps invariant 1. |
| 2 | `glfw.Init` | Window system up. |
| 3 | `Backend.ConfigureWindow` | Hints `ClientAPI = NoAPI` — GLFW must not create a GL context. |
| 4 | `glfw.CreateWindow` | The window exists. |
| 5 | input callbacks | Resize, scroll, mouse. A resize only records the new size — the viewport is a per-pass decision. |
| 6 | `Backend.Init(window)` | Instance → surface → physical device → queue family → logical device → VMA allocator → swapchain → command pool → per-frame data → samplers → descriptors → pipeline layout → default textures. |
| 7 | `App.Run` → `CreateShader` ×5 | `forward`, `depth`, `depth_cube` (geometry stage), `ui`, `skybox`. |
| 8 | `scene.NewScene` | Parses XML → OBJ/MTL → uploads vertex buffers, per-face-group meshes, material textures; picks the shadow casters; allocates their shadow targets; loads the skybox cubemap. |

Shaders are authored once in Slang (`shaders/slang/`) and compiled by
`build_shaders.sh` into `shaders/vk/*.spv`. The backend does not read `.slang` at
runtime, so the script must run before the first build and after every shader
edit.

---

## 3. The frame loop (`core/App.Run`)

```
world.Update(1/60)          physics: entity updates, collisions, Verlet integration
scene.UpdateMeshes()        reupload vertex buffers of meshes physics moved
input                       camera moves

Backend.BeginFrame()

  for each shadow caster (≤1 sun, ≤1 point light):
      Light.RenderShadowMap:
          BeginPass(shadowTarget, 1024, 1024, nil)   ← no color clear, depth only
          draw every mesh with depth / depth_cube
          EndPass()

  BeginPass(0, w, h, &{0.1,0.1,0.1,1})               ← backbuffer, clears color
      Scene.RenderSkybox     SetDepthFunc(LEQUAL) → draw cube → back to LESS
      Scene.RenderScene      every mesh, every face group, forward shader
      renderUI               rasterise widgets to RGBA → UpdateTexture2D → Draw(quad)
  EndPass()

Backend.EndFrame()          present
glfw.PollEvents()
```

The frame shape is **hardcoded here**, which is the constraint
`tmp/BACKEND_DECISION.md` §6 identifies: a new pass — a probe capture, a tonemap, a
volumetric composite — is an edit to `App.Run` rather than a new file. The `Pass`
interface in §9 item 5 is what changes that.

Uniforms travel as **two** values split by update frequency.
`renderer.FrameUniforms` (1184 bytes) is filled by the frame loop and
`Scene.FillFrameUniforms`, then published once per pass by `BindFrameUniforms`.
`renderer.DrawUniforms` (128 bytes) carries the model matrix and the material,
which `Mesh.draw` rewrites before each draw. The backend snapshots both at call
time, so the caller may keep mutating them.

Note the shadow bakes overwrite the light matrices in the frame block and rebind
it, which is why it is *pass*-scoped rather than strictly per frame; the skybox
does the same with a stripped-translation view of its own.

Shadow budget is fixed and resolved once at load: the first directional light
gets a 2D depth map, the first point light gets a depth cube. Every other light
is still lit in the forward pass — it just casts nothing.

---

## 4. The `renderer.Backend` contract, method by method

### 4.1 Lifecycle

| Method | What it does |
|---|---|
| `ConfigureWindow` | `ClientAPI = NoAPI` |
| `Init` | The whole device stack (see §2 step 6). Enables `ScalarBlockLayout`, `BufferDeviceAddress`, descriptor indexing, `DynamicRendering`, `Synchronization2`, `GeometryShader` |
| `Shutdown` | `DeviceWaitIdle`, then explicitly destroys every pipeline, module, image, view, buffer, sampler, fence, semaphore, pool, device, instance |

Every object, and its destruction order, is the application's problem. §7 is the
map of that.

`GeometryShader` is enabled because `depth_cube.slang` routes triangles to the
six faces of a point light's shadow cube in one layered pass. It is a deliberate
choice, not an accident — see `tmp/BACKEND_DECISION.md` §10.

### 4.2 Frame and passes

| Method | What it does |
|---|---|
| `BeginFrame` | Waits on this frame slot's fence (the CPU throttle for 2 frames in flight), acquires a swapchain image, resets the ring offset, drains retired resources, resets and begins the command buffer, binds the one descriptor set, flushes staged texture uploads |
| `BeginPass` | Barriers the target into attachment layout, `CmdBeginRendering` with load ops (`Clear` / `DontCare`), `CmdSetViewport`, `CmdSetScissor`, re-issues cull mode + depth compare |
| `EndPass` | `CmdEndRendering`, and for a shadow target barriers depth-attachment → shader-read-only |
| `EndFrame` | Barriers the swapchain image to present layout, ends and submits the command buffer (wait on acquire semaphore, signal the image's render semaphore, signal the fence), presents, advances the frame slot |

A pass is "bind a target, set a viewport, clear, draw, finish". Four things about
how Vulkan spells that are worth knowing before touching it:

* **Clears are a *load op* on an attachment**, not a command. The clear is
  declared when rendering begins. That is why `BeginPass` takes the clear colour
  as a parameter rather than exposing a `Clear` method.
* **Layout transitions.** An image is in a layout and must be barriered between
  "rendered into" and "sampled from". That is why `EndPass` has a shadow-map
  transition. [HTV: barriers]
* **Synchronisation is explicit**: a fence, two semaphores, acquire, submit,
  present. Note the two index spaces — the acquire semaphore and fence are *per
  in-flight frame*, the render semaphore is *per swapchain image*, and present
  waits on the image's own semaphore. §7 has the full rule.
* **No render pass objects.** The backend uses dynamic rendering, so attachments
  are named at `CmdBeginRendering` and their formats are baked into the pipeline.
* **Resize** arrives as `ERROR_OUT_OF_DATE_KHR` from acquire or present. The
  backend rebuilds the swapchain, its views, its semaphores and the depth image.
  That error *is* how a resize reaches a Vulkan app.

### 4.3 Immediate state

| Method | What it does |
|---|---|
| `SetCullFace(front)` | Records the value, `CmdSetCullMode` when a frame is active |
| `SetDepthFunc(lequal)` | Records the value, `CmdSetDepthCompareOp` |

Both are Vulkan 1.3 *dynamic state* (promoted from
`VK_EXT_extended_dynamic_state`), which is why the interface can keep an
immediate-call shape here instead of exploding into one pipeline per
cull/depth combination. The backend re-issues both at pass start
(`applyDynamicState`), because the engine sets them between passes as often as
inside them.

Two callers: the skybox flips depth to `LEQUAL` so the cube can sit on the far
plane [LOGL: Cubemaps], and the sun's shadow pass culls front faces to avoid
peter-panning [LOGL: Shadow Mapping].

> These two are the interface's only pipeline state, which is exactly the gap
> `tmp/BACKEND_DECISION.md` §6 names: there is no blend control and no depth-write
> control, so a transparent material cannot be expressed. The `PipelineSpec`
> work item replaces both methods.

### 4.4 Shaders and pipelines

| Method | What it does |
|---|---|
| `CreateShader(name, hasGeometry)` | Loads `shaders/vk/<name>.{vert,frag,geo}.spv` into shader modules. **No pipeline is built** |

A shader is not one object. Vulkan bakes state into a *pipeline*, so one shader
needs one pipeline per combination it is actually drawn with:

```
pipelines[passKind][vertexLayout]
   passKind:      passMain | passShadow2D | passShadowCube
   vertexLayout:  layoutMesh | layoutSkybox | layoutFullscreen
```

Built lazily on first use in `getPipeline`. What each axis decides:

* **pass** → attachment formats (main has color + depth, shadow passes depth
  only), blending (main only), front-face winding.
* **layout** → vertex input state: mesh is 32-byte `pos|normal|uv`, skybox is
  12-byte `pos`, fullscreen is 20-byte `pos|uv`. Depth-only passes drop normals
  and UVs, because declaring attributes the shader never reads is rejected.

Everything else is dynamic: viewport, scissor, cull mode, depth compare.

Note that the shader is selected **by name at startup** in `App.Run` and the
pipeline axes are a closed enum. A material cannot bring its own shader, which
is the other half of the `PipelineSpec` gap.

### 4.5 Uniforms — scalar layout and buffer device address

Go packs `float32`/`int32` structs with no padding, which *is* Vulkan's scalar
block layout (Slang compiles with `-fvk-use-scalar-layout`). So both blocks
memcpy straight into this frame's ring buffer (1 MiB, 64-byte aligned entries)
and their **GPU addresses** go out as a 16-byte push constant. The shader
dereferences those pointers — the uniform data needs no descriptor at all. 1184
and 128 bytes, no padding, no marshalling code.
[HTV: buffer device address]

`ScalarBlockLayout` is enabled at device creation (`vulkan/backend.go:414`) and
is load-bearing: `LightData` is 68 bytes, so `lights[]` has a stride that is not
16-aligned and the *standard* layout rules reject it. `spirv-val` must be given
`--scalar-block-layout` or it fails on every module.

**The split.** `BindFrameUniforms` publishes the pass block once; each `Draw`
sends only the transform and material. Before the split a single 1312-byte block
went out on every draw, roughly 1.2 KB of which was identical across the pass.

| | How |
|---|---|
| Transport | per-frame ring buffer: one frame entry per pass, one draw entry per draw |
| Layout | scalar, 1184 + 128 bytes |
| Addressing | two 64-bit device addresses in one push constant |
| Textures | handles rewritten into **bindless slot indices** in the copy |
| Cost per draw | one 128-byte memcpy + one 16-byte push constant |

**The one rule:** keep the field *order* in `renderer/uniforms.go` and
`shaders/slang/common.slang` identical, and use only `float32`/`int32`, arrays
of those, and matrices. Scalar layout and Go packing then agree by construction.

Nothing else is required. The 16-byte cells, the `float3`-plus-scalar pairing and
the `int4`-not-`int[4]` trick were std140's rule, mandatory while OpenGL was a
backend, and were removed on 2026-08-05 along with `LightData`'s three reserved
floats (80 → 68 bytes, and `FrameUniforms` 1280 → 1184).

The guard is the `init()` size panic in `renderer/uniforms.go`. It catches a
member added, removed or resized. It does **not** catch two members swapped —
that leaves every size and offset identical and renders silent garbage.

So after editing `common.slang`, rebuild the shaders and look at the scene. To
check a layout by hand, the compiler records what it actually chose:

```sh
spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate
```

Those offsets should equal `unsafe.Offsetof` of the matching Go field, in order.

### 4.6 Textures

| Method | What it does |
|---|---|
| `LoadTexture` | Creates the image, fills it via a staging buffer inside an `immediateSubmit`, creates the view, writes a descriptor into bindless binding 0 |
| `LoadCubemap` | Stages all six faces as one contiguous block into a 6-layer `CubeCompatible` image, one copy command, bindless binding 1 |
| `WhiteTexture` | `0` — handle 0 *is* the white pixel, and bindless slot 0 |
| `UpdateTexture2D` | **Stages** the pixels into a persistently mapped buffer and defers the copy to the next `BeginFrame` |
| `DestroyTexture` | Drains the frames in flight, then destroys view + image + staging |

Four things worth knowing:

* **How the shader reaches a texture.** One descriptor set with two **bindless**
  arrays (256 2D, 64 cube, `PartiallyBound | UpdateAfterBind`). The shader
  indexes them with the slot number that arrived in the uniform block.
  [HTV: descriptor indexing]
* **The shadow maps are the exception.** They get dedicated descriptors
  (bindings 2 and 3) rather than bindless slots, because the PCF kernels tap them
  9× / 20× per fragment and some drivers re-fetch a dynamically-indexed
  descriptor per tap. Going bindless there cost ~1.7× the frame time.
* **The UI overlay.** A copy cannot be recorded inside a render pass, so the
  pixels are staged and copied at the top of the next frame: one frame of
  latency, no queue stall. Resizing the canvas *retires* the old image instead
  of destroying it, because the command buffer being recorded still references
  it (`retire` / `drainRetired`, aged `framesInFlight + 1` frames).
* **A "no texture" fallback**: handle translation falls back to slot 0, the
  built-in white pixel.

### 4.7 Buffers and meshes

| Method | What it does |
|---|---|
| `CreateBuffer` | Host-visible, persistently mapped VMA allocation + memcpy. `dynamic` is ignored — an update is a memcpy either way |
| `UpdateBuffer` | Drains the frames in flight, then memcpys. There is no driver-side ghosting to hide behind |
| `CreateMesh` | Pairs the vertex buffer handle with an index buffer and stores the layout. The layout keys the pipeline |
| `DestroyMesh` / `DestroyBuffer` | Drains frames in flight first, then `VmaDestroyBuffer` |

A mesh is one shared vertex buffer plus one index list per material face group,
so a 3-material OBJ is 1 vertex buffer + 3 mesh handles.

*Who waits* is the thing to remember: the backend tracks whether the GPU still
needs a buffer, explicitly, which is what `waitAllFrames` is for — note it skips
the frame currently being recorded, whose fence was reset in `BeginFrame` and can
only be signalled by `EndFrame`.

### 4.8 Offscreen render targets

One `CreateRenderTarget(RenderTargetSpec)` covers every kind. The spec says what
the target *is* — size, `TargetDepth` or `TargetColor`, cube or not — rather than
what it is for, which is what lets an HDR buffer or a G-buffer be expressed
without widening the interface.

| Spec | What it builds |
|---|---|
| depth | Depth image usable as both attachment and sampled, plus a `ClampToBorder` / `OpaqueWhite` sampler so outside the light frustum reads "fully lit" |
| cube | 6-layer `CubeCompatible` image, plus **two views of it**: a 2D-array view to attach and a cube view to sample |
| colour | Colour image + view, rendered with `passOffscreenColor` (flipped viewport, CCW, no depth attachment) |
| `DestroyRenderTarget` | Drains frames, destroys the attachment view and the image |

All six cube faces are drawn in one layered pass, driven by a geometry stage.
[LOGL: Point Shadows]

The two-views trick is required: a cube view cannot be attached and an array view
cannot be sampled as a cube. The image's layout is tracked across passes.

> `TargetColor` exists but nothing uses it yet — it is the seam an HDR target
> lands on, once the half-float format is bound (`tmp/BACKEND_DECISION.md` §7).

### 4.9 Draws

| Method | What it does |
|---|---|
| `Draw` | Resolve handles, bind the pipeline for (shader, current pass, **mesh's** layout) if it changed, memcpy the draw block into the ring, push both addresses, bind vertex (+ index) buffers, `CmdDrawIndexed` or `CmdDraw` |

**One entry point for every drawable.** A face group, the skybox cube and the UI
overlay differ only in what was recorded when the mesh was created — vertex
layout, count, indexed or not — so adding a drawable kind means adding a way to
*build* a mesh, not a way to draw one. The overlay's quad is built once by
`core.createOverlayQuad` and drawn like anything else; its pipeline still tests
depth without writing it, which is keyed off the fullscreen vertex layout.

`boundPipeline` is tracked so redundant binds are skipped.

### 4.10 Capabilities

`Supports` returns `false` today and has never been wired. With one backend it
means what it says — *does this physical device have the extension* — rather
than the cross-backend performance hint an earlier design intended. The first
real answer will be `FeatureRayTracing` against `VK_KHR_ray_query` availability,
which is a genuine runtime fork: a GTX 1080 runs the same engine with a compute
BVH instead. See `tmp/BACKEND_DECISION.md` §5.2 and §8.

---

## 5. Conventions that keep the image right side out

These are the subtle ones — the things that would silently render a mirrored,
inside-out, or inverted-depth image if they drifted. Several are inherited from
the OpenGL era; they are kept because the maths in `scene/` and the shaders is
written against them, not because anything forces them.

**Clip space handedness.** Vulkan's NDC is y-down. The main pass fixes this with
a **negative-height viewport** (`Y = height`, `Height = -height`), giving the
y-up clip space the projection matrices in `scene/` assume, so no geometry,
matrix or shader has to know. [HTV: viewport]

**Winding follows from that.** Flipping the viewport also flips triangle
winding, which cancels out — so the main pass keeps counter-clockwise front
faces. The shadow passes deliberately use a *positive* viewport, since a shadow
map is sampled rather than presented and the depth comparison in the shaders
expects that memory layout; the price is inverted winding, which those pipelines
declare as `FrontFace = Clockwise`.

**Depth range.** The projections built in `scene/` are the OpenGL convention,
giving clip z in `[-w, w]`, while Vulkan clips to `[0, w]`. Every vertex stage
therefore calls `TO_VK_DEPTH` from `common.slang`. Changing the projections
instead would remove the macro — a cleanup, not a bug.

**MSAA is a backbuffer-only property.** `settings.MSAASamples` (1 = off) is read
once at `Init`. The backend allocates a multisampled colour image plus a matching
multisampled depth image, draws the main pass into them, and resolves into the
swapchain image with `ResolveModeAverage` on the colour attachment. Offscreen
targets stay single-sampled — a later pass has to *sample* them, and a
multisampled texture is not something these shaders can read — so
`vulkan/shader.go` `passSamples` gives the multisampled count to `passMain`
only. A pipeline whose sample count disagrees with its pass's attachments is
invalid, so this is the one place that decision lives.

**Shadow border colour.** `BorderColor = OpaqueWhiteFloat` on the 2D shadow
sampler, so outside the sun's frustum reads unshadowed.

**The uniform struct is the contract.** `renderer/uniforms.go` has an `init`
that panics if `LightData` stops being 68 bytes, `FrameUniforms` 1184 or
`DrawUniforms` 128. Field *order* is what has to match, and the size panic does
not check order — see §4.5 for how to verify it.

---

## 6. Where to look when something is wrong

| Symptom | Look at |
|---|---|
| The image is mirrored, or culled inside-out | `vulkan/backend.go` `BeginPass` viewport, `vulkan/shader.go` `frontFace` (§5) |
| Garbage uniforms after editing `common.slang` | `go test ./renderer/` after rebuilding shaders — it diffs offsets and names against the SPIR-V (§4.5) |
| `spirv-val` rejects every module | Missing `--scalar-block-layout`; `LightData`'s 68-byte stride is legal only under it (§4.5) |
| Validation complains about image layouts | `imageBarrier` call sites in `BeginPass` / `EndPass` / `recordImageUpload` |
| A resource is destroyed while in use | `waitAllFrames`, `retire`, `drainRetired` (§7) |
| Shadows missing on one light | `Scene.pickShadowCasters` — only the first sun and first point light get maps |
| UI overlay lags by a frame | Expected: `UpdateTexture2D` stages, `BeginFrame` copies |
| Nothing starts | `./build_shaders.sh` — the generated shaders are git-ignored |
| Pipeline creation fails after a pass change | Sample count or attachment formats disagreeing with the pass (§5, MSAA) |

Set `OVERDRIVE_VK_VALIDATION=1` while developing. It is the main reason a wrong
image gets diagnosed rather than guessed at.

---

## 7. Who owns what, and what dies when

Vulkan makes every object and its destruction order the application's problem,
so this is the map.

### The ownership tree

Indentation is containment. The right column is what tears each level down.

```
Instance                                          DestroyInstance
├── SurfaceKHR                                     DestroySurfaceKHR
└── Device                                         DestroyDevice
    ├── VmaAllocator                                VmaDestroyAllocator
    │
    ├── SwapchainKHR ─────────── sized to the window
    │   ├── swapImages[]          owned by the swapchain, never destroyed
    │   ├── swapViews[]           DestroyImageView          ┐
    │   ├── renderSems[]          DestroySemaphore          │ destroySwapchain
    │   ├── depthImage/View       VmaDestroyImage           │
    │   └── msaaImage/View        VmaDestroyImage           ┘ only when MSAA is on
    │
    ├── CommandPool                                 DestroyCommandPool
    │   └── frames[2].cb          freed with the pool
    │
    ├── frames[2]  ───────────── one set per frame in flight
    │   ├── fence                 DestroyFence
    │   ├── acquireSem            DestroySemaphore
    │   └── ring (1 MiB, mapped)  VmaDestroyBuffer
    │
    ├── DescriptorPool                              DestroyDescriptorPool
    │   └── descriptorSet         freed with the pool
    ├── DescriptorSetLayout                         DestroyDescriptorSetLayout
    ├── PipelineLayout                              DestroyPipelineLayout
    ├── samplers ×4                                 DestroySampler
    │
    └── resource tables ──────── grow at load time, indexed by handle
        ├── shaders[]   modules ×2-3 + pipelines[pass][layout]
        ├── textures[]  image + view (+ staging, for the UI overlay)
        ├── buffers[]   VmaCreateBuffer, host-visible, mapped
        ├── meshes[]    index buffer (the vertex buffer is shared, not owned)
        └── targets[]   image + attachment view (+ a cube view to sample)
```

### Five lifetime classes

| Class | Objects | Created | Destroyed |
|---|---|---|---|
| **Permanent** | instance, surface, device, allocator, command pool, descriptor pool/set/layout, pipeline layout, samplers | `Init`, once | `Shutdown`, reverse order |
| **Swapchain-sized** | swapchain, image views, render semaphores, depth image + view, MSAA colour image + view | `createSwapchain` | `destroySwapchain` — **also on every resize** |
| **Per frame in flight** (×2) | command buffer, fence, acquire semaphore, uniform ring | `createFrameData` | `Shutdown` |
| **Per resource** | shader modules + pipelines, textures, buffers, meshes, shadow targets | load time, on demand | `Destroy*` (after `waitAllFrames`) or `Shutdown` |
| **Retired** | images/views/staging replaced mid-frame | `retire`, when the UI canvas resizes | `drainRetired`, once `framesInFlight + 1` frames have passed |

### The three rules that make it safe

1. **Nothing is destroyed while the GPU might still read it.** `Shutdown` opens
   with `DeviceWaitIdle`; the `Destroy*` methods call `waitAllFrames` instead,
   which is cheaper but still a full drain. `waitAllFrames` deliberately skips
   the frame being recorded — its fence was reset in `BeginFrame` and can only be
   signalled by `EndFrame`, so waiting on it would deadlock.

2. **Mid-frame replacement retires rather than destroys.** `UpdateTexture2D` runs
   inside the main pass, where the command buffer already references the old
   image, and where `waitAllFrames` would stall every frame. So the old objects
   go on the `retired` list tagged with `frameCounter`, and `drainRetired` frees
   them once no in-flight frame can reference them. This is the only
   deferred-destruction path in the engine.

3. **Resize is a partial teardown.** `recreateSwapchain` blocks while minimised
   (a zero-sized surface has no valid swapchain), waits idle, then destroys and
   rebuilds exactly the swapchain-sized class. Everything else survives. It is
   triggered by `ErrOutOfDateKHR` from either `AcquireNextImageKHR` or
   `QueuePresentKHR` — that error *is* how a resize reaches a Vulkan app.

### Two index spaces that are easy to confuse

`frameIndex` cycles `0..framesInFlight-1` and selects the command buffer, fence,
acquire semaphore and ring. `imageIndex` comes back from `AcquireNextImageKHR`
and selects the swapchain image, its view and its render semaphore. They are not
interchangeable and the swapchain may hold a different number of images than
there are frames in flight — which is exactly why `renderSems` is sized per
image while `acquireSem` lives per frame.
