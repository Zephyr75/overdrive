# ENGINE_FLOW.md — how a frame gets drawn

This document is the reading guide to `src/`. It follows one frame from
`main()` down to the GPU, then walks the `renderer.Backend` contract method by
method, showing what the Vulkan backend does with each one and why.

`ARCHITECTURE.md` is the map (where every package and symbol lives) and
`FEATURES.md` is the feature list with the reasoning behind each one.
`tmp/BACKEND_DECISION.md` is where the interface is _going_. This is the operational
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

`renderer.Backend`'s 27 methods are declared by **resource type** — textures,
buffers, meshes, shaders, targets, draws. That is the wrong axis for remembering
_where a Vulkan call sits in a frame_. This table is the other axis: how often
each method runs. §4 walks the same methods in interface order, with the
reasoning; this is the index.

The obscure Vulkan names get easier once they are filed by frequency —
`vkAcquireNextImageKHR` is "the per-frame one", `vkCmdPipelineBarrier2` is "the
per-pass one", `vkCmdPushConstants` is "the per-draw one".

### Once, at startup — 2 methods

| Method     | What it does                                                                                                                                                                                                                    |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Init`     | `CreateInstance` → surface → `EnumeratePhysicalDevices` → queue family → `CreateDevice` → `VmaCreateAllocator` → swapchain → `CreateCommandPool` → per-frame data → samplers → descriptors → pipeline layout → default textures |
| `Shutdown` | `DeviceWaitIdle`, then destroy everything in reverse creation order (see §7)                                                                                                                                                    |

### Once per resource, at load time — 6 methods

| Method               | What it does                                                                                                                |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `CreateShader`       | `CreateShaderModule` ×2-3. **No pipeline yet** — built lazily per (pass, layout)                                            |
| `CreateTexture`      | `VmaCreateImage` + staging buffer + `immediateSubmit(CmdCopyBufferToImage)` + `CreateImageView` + bindless descriptor write |
| `CreateCubemap`      | One 6-layer `CubeCompatible` image, six faces staged contiguously, one copy                                                 |
| `CreateBuffer`       | `VmaCreateBuffer` host-visible + persistently mapped + `MemCopy`                                                            |
| `CreateMesh`         | Pair the vertex handle with an index buffer and record the layout. No VAO equivalent — the layout keys the pipeline         |
| `CreateRenderTarget` | Image usable as attachment _and_ sampled, plus **two** views for cubes: 2D-array to attach, cube to sample                  |

### On demand, rarely — 6 methods

| Method                | What it does                                                |
| --------------------- | ----------------------------------------------------------- |
| `UpdateBuffer`        | `waitAllFrames()` **then** memcpy. This is a full GPU drain |
| `DestroyTexture`      | `waitAllFrames()`, destroy view + image + staging           |
| `DestroyBuffer`       | `waitAllFrames()`, `VmaDestroyBuffer`                       |
| `DestroyMesh`         | `waitAllFrames()`, destroy the index buffer                 |
| `DestroyRenderTarget` | `waitAllFrames()`, destroy view + image                     |
| `Supports`            | `false` — the seam for ray tracing and compute (§4.10)      |

### Once per frame — 4 methods

| Method              | What it does                                                                                                                                                                                                                            |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BeginFrame`        | `WaitForFences` (the CPU throttle) → `AcquireNextImageKHR` → `ResetFences` → rewind ring → `drainRetired` → `ResetCommandBuffer` + `BeginCommandBuffer` → `CmdBindDescriptorSets` → flush staged uploads → seed one empty shadow record |
| `BindShadowRecords` | Memcpy the whole record array into the ring, cache its device address for every draw of the frame                                                                                                                                       |
| `UpdateTexture2D`   | Memcpy into a mapped staging buffer, **defer** the copy to the next `BeginFrame`. Costs the overlay one frame of latency                                                                                                                |
| `EndFrame`          | Barrier to `PresentSrcKHR` → `EndCommandBuffer` → `QueueSubmit2` (wait acquire sem, signal image's render sem, signal fence) → `QueuePresentKHR` → advance frame slot                                                                   |

### Once per pass, ×2 a frame — 5 methods

One shadow-atlas pass (depth-only, no colour clear) then the main backbuffer
pass. It was one pass per casting light until the atlas landed; now every shadow
in the scene is a tile of one target, so the pass count no longer grows with the
light count — only the viewport changes inside it do.

| Method              | What it does                                                                                                                                                       |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `BindFrameUniforms` | Memcpy 4844 B into the ring, cache its device address for the pass's draws. Also called **per tile** inside the atlas pass, each tile needing its own `BakeMatrix` |
| `BeginPass`         | `imageBarrier` into attachment layout → `CmdBeginRendering` (load ops carry the clear) → `CmdSetViewport` → `CmdSetScissor` → re-issue dynamic state               |
| `SetCullMode`       | `CmdSetCullMode` — dynamic state, no extra pipeline                                                                                                                |
| `SetDepthCompare`   | `CmdSetDepthCompareOp` — dynamic state                                                                                                                             |
| `EndPass`           | `CmdEndRendering`, and for a shadow target `imageBarrier` depth-attachment → shader-read-only                                                                      |

### Once per shadow tile — 2 methods

The shadow-atlas plumbing. `Scene.BakeShadows` (`scene/shadowatlas.go`) is the
caller of the first; the second waits for Part E's static/dynamic split.

| Method               | What it does                                                                                                                                                                                            |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SetViewportScissor` | `CmdSetViewport` + `CmdSetScissor` narrowed to one tile of the pass's target. The only sanctioned way to change a viewport mid-pass (§5)                                                                |
| `CopyDepthRegion`    | Two `imageBarrier`s into `TRANSFER_SRC`/`TRANSFER_DST` → `CmdCopyImage` on the depth aspect → two more back to shader-read. Illegal inside a pass, so it sits between them. **Still called by nothing** |

### Once per draw, ~15 a frame — 2 methods

| Method       | What it does                                                                                                                                                                                              |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BindShader` | Records the handle. The pipeline also depends on the pass and the mesh's layout, neither known until `Draw`                                                                                               |
| `Draw`       | `getPipeline(shader, pass, mesh layout)` (skipped if unchanged) → memcpy 128 B into the ring → `CmdPushConstants` (three 8-byte device addresses) → bind vertex (+ index) → `CmdDrawIndexed` or `CmdDraw` |

The mesh carries its own vertex layout, count and indexed-ness, so one entry
point serves face groups, the skybox cube and the UI overlay alike.

### What this table makes obvious

- **Vulkan front-loads.** Almost everything expensive is startup or load time.
  The per-frame and per-draw rows are short — that is the whole point of the API.
- **The per-draw row is deliberately thin.** The uniform block is split by
  update frequency, so a draw sends 128 bytes of transform and material rather
  than the whole 5 KB of camera and light state. That block goes out once per
  pass instead, in `BindFrameUniforms`.
- **`waitAllFrames` appears in five methods.** Every one is a full pipeline
  drain. They are all rare by design — if one starts running per frame,
  throughput collapses.
- **`UpdateTexture2D` is per-frame, not per-resource.** It is the UI overlay, and
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
renderer/          the abstraction: Backend interface, opaque handles, the three uniform structs
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

| Step | Code                          | What happens                                                                                                                                                                                                          |
| ---- | ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | `settings.Load`               | `main.go` decodes the file named by `-config` (`configs/vulkan.toml` by default) over the defaults — the engine's only configuration input. Everything below reads the result, so it has to run before `core.NewApp`. |
| 1    | `vulkan.New()`                | Called directly by `core.NewApp` and held as a `renderer.Backend`, which is what keeps invariant 1.                                                                                                                   |
| 2    | `glfw.Init`                   | Window system up.                                                                                                                                                                                                     |
| 3    | `glfw.WindowHint`             | `ClientAPI = NoAPI` in `core.NewApp` — GLFW must not create a GL context.                                                                                                                                             |
| 4    | `glfw.CreateWindow`           | The window exists.                                                                                                                                                                                                    |
| 5    | input callbacks               | Resize, scroll, mouse. A resize only records the new size — the viewport is a per-pass decision.                                                                                                                      |
| 6    | `Backend.Init(window)`        | Instance → surface → physical device → queue family → logical device → VMA allocator → swapchain → command pool → per-frame data → samplers → descriptors → pipeline layout → default textures.                       |
| 7    | `App.Run` → `CreateShader` ×5 | `forward`, `depth`, `depth_point`, `ui`, `skybox`.                                                                                                                                                                    |
| 8    | `scene.NewScene`              | Parses XML → OBJ/MTL → uploads vertex buffers, per-face-group meshes, material textures; picks the shadow casters; allocates the **one** shadow atlas; loads the skybox cubemap.                                      |

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

  Scene.UpdateShadows      allocate a tile per caster, build one record per tile
  BindShadowRecords        the whole array into the ring, once for the frame
  Scene.FillFrameUniforms  camera, lights, each light's record index

  Scene.BakeShadows:                                 ← one pass, every shadow
      BeginPass(atlasTarget, nil)                    ← no color clear, depth only
      for each tile:
          SetViewportScissor(tile)                   ← what makes it an atlas
          BindFrameUniforms(BakeMatrix = tile's)
          draw every mesh with depth / depth_point
      EndPass()

  BeginPass(0, w, h, &{0.1,0.1,0.1,1})               ← backbuffer, clears color
      Scene.RenderSkybox     SetDepthCompare(LessEqual) → draw cube → back to Less
      Scene.RenderScene      every mesh, every face group, forward shader
      renderUI               rasterise widgets to RGBA → UpdateTexture2D → Draw(quad)
  EndPass()

Backend.EndFrame()          present
glfw.PollEvents()
```

The frame shape is **hardcoded here**, which is the constraint
`tmp/BACKEND_DECISION.md` §6 identifies: a new pass — a probe capture, a tonemap, a
volumetric composite — is an edit to `App.Run` rather than a new file. The `Pass`
interface in §9 item 7 is what changes that.

Uniforms travel as **three** values split by update frequency.
`renderer.FrameUniforms` (4844 bytes) is filled by the frame loop and
`Scene.FillFrameUniforms`, then published once per pass by `BindFrameUniforms`.
`renderer.DrawUniforms` (128 bytes) carries the model matrix and the material,
which `Mesh.draw` rewrites before each draw. `renderer.ShadowRecord` (96 bytes
each) is a whole _array_, published once per frame by `BindShadowRecords`,
because its length is a property of the scene rather than a constant. The backend
snapshots all three at call time, so the caller may keep mutating them.

Note the tile bakes overwrite `BakeMatrix` in the frame block and rebind it once
per tile, which is why it is _pass_-scoped rather than strictly per frame — in
the atlas pass it is closer to per draw call; the skybox does the same with a
stripped-translation view of its own.

Shadow budget is still fixed and resolved once at load: `pickShadowCasters` picks
the first directional and the first point light. What changed is where the result
goes — one tile of the atlas for the sun, six for the point light, rather than
two textures of their own. Every other light is still lit in the forward pass, it
just gets no tile. `tmp/LIGHTING_IMPL.md` Part D replaces the fixed pick with a
per-frame score over the atlas's 16 tiles.

---

## 4. The `renderer.Backend` contract, method by method

### 4.1 Lifecycle

| Method     | What it does                                                                                                                                                              |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Init`     | The whole device stack (see §2 step 6). Enables `ScalarBlockLayout`, `BufferDeviceAddress`, descriptor indexing, `DynamicRendering`, `Synchronization2`, `GeometryShader` |
| `Shutdown` | `DeviceWaitIdle`, then explicitly destroys every pipeline, module, image, view, buffer, sampler, fence, semaphore, pool, device, instance                                 |

Every object, and its destruction order, is the application's problem. §7 is the
map of that.

`GeometryShader` is still enabled, but **nothing uses it any more**:
`depth_cube.slang` routed triangles to the six faces of a shadow cube in one
layered pass, and the atlas retired it for six ordinary tile bakes
(`tmp/LIGHTING_IMPL.md` Part C). The feature bit and the `passShadowCube`
pipeline kind are both still there, unexercised — see `tmp/BACKEND_DECISION.md`
§10 for whether that stays.

### 4.2 Frame and passes

| Method       | What it does                                                                                                                                                                                                                                                 |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `BeginFrame` | Waits on this frame slot's fence (the CPU throttle for 2 frames in flight), acquires a swapchain image, resets the ring offset, drains retired resources, resets and begins the command buffer, binds the one descriptor set, flushes staged texture uploads |
| `BeginPass`  | Barriers the target into attachment layout, `CmdBeginRendering` with load ops (`Clear` / `DontCare`), `CmdSetViewport`, `CmdSetScissor`, re-issues cull mode + depth compare                                                                                 |
| `EndPass`    | `CmdEndRendering`, and for a shadow target barriers depth-attachment → shader-read-only                                                                                                                                                                      |
| `EndFrame`   | Barriers the swapchain image to present layout, ends and submits the command buffer (wait on acquire semaphore, signal the image's render semaphore, signal the fence), presents, advances the frame slot                                                    |

A pass is "bind a target, set a viewport, clear, draw, finish". Four things about
how Vulkan spells that are worth knowing before touching it:

- **Clears are a _load op_ on an attachment**, not a command. The clear is
  declared when rendering begins. That is why `BeginPass` takes the clear colour
  as a parameter rather than exposing a `Clear` method.
- **Layout transitions.** An image is in a layout and must be barriered between
  "rendered into" and "sampled from". That is why `EndPass` has a shadow-map
  transition. [HTV: barriers]
- **Synchronisation is explicit**: a fence, two semaphores, acquire, submit,
  present. Note the two index spaces — the acquire semaphore and fence are _per
  in-flight frame_, the render semaphore is _per swapchain image_, and present
  waits on the image's own semaphore. §7 has the full rule.
- **No render pass objects.** The backend uses dynamic rendering, so attachments
  are named at `CmdBeginRendering` and their formats are baked into the pipeline.
- **Resize** arrives as `ERROR_OUT_OF_DATE_KHR` from acquire or present. The
  backend rebuilds the swapchain, its views, its semaphores and the depth image.
  That error _is_ how a resize reaches a Vulkan app.

### 4.3 Immediate state

| Method                | What it does                                               |
| --------------------- | ---------------------------------------------------------- |
| `SetCullMode(m)`      | Records the value, `CmdSetCullMode` when a frame is active |
| `SetDepthCompare(op)` | Records the value, `CmdSetDepthCompareOp`                  |

Both are Vulkan 1.3 _dynamic state_ (promoted from
`VK_EXT_extended_dynamic_state`), which is why the interface can keep an
immediate-call shape here instead of exploding into one pipeline per
cull/depth combination. Front face is deliberately _not_ dynamic: it is a
property of a pass's winding convention, so it is baked into the pipeline. The backend re-issues both at pass start
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

| Method               | What it does                                                                                                                  |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `CreateShader(name)` | Loads `shaders/vk/<name>.{vert,frag}.spv` into shader modules, plus `.geo.spv` when the set has one. **No pipeline is built** |

A shader is not one object. Vulkan bakes state into a _pipeline_, so one shader
needs one pipeline per combination it is actually drawn with:

```
pipelines[passKind][vertexLayout]
   passKind:      passMain | passShadow2D | passShadowCube
   vertexLayout:  layoutMesh | layoutSkybox | layoutFullscreen
```

Built lazily on first use in `getPipeline`. What each axis decides:

- **pass** → attachment formats (main has color + depth, shadow passes depth
  only), blending (main only), front-face winding.
- **layout** → vertex input state: mesh is 32-byte `pos|normal|uv`, skybox is
  12-byte `pos`, fullscreen is 20-byte `pos|uv`. Depth-only passes drop normals
  and UVs, because declaring attributes the shader never reads is rejected.

Everything else is dynamic: viewport, scissor, cull mode, depth compare.

Note that the shader is selected **by name at startup** in `App.Run` and the
pipeline axes are a closed enum. A material cannot bring its own shader, which
is the other half of the `PipelineSpec` gap.

### 4.5 Uniforms — scalar layout and buffer device address

Go packs `float32`/`int32` structs with no padding, which _is_ Vulkan's scalar
block layout (Slang compiles with `-fvk-use-scalar-layout`). So every block
memcpys straight into this frame's ring buffer (1 MiB, 64-byte aligned entries)
and their **GPU addresses** go out as a 24-byte push constant. The shader
dereferences those pointers — the uniform data needs no descriptor at all. 4844,
128 and 96×N bytes, no padding, no marshalling code.
[HTV: buffer device address]

The third pointer is the shadow record array. It rides the same ring rather than
a storage buffer of its own: the ring is already device-addressable and already
rewound per frame, so a variable-length array only needed `writeRingSlice`. It is
a _pointer_ rather than a `FrameUniforms` member precisely because its length is
data — 7 records in the showcase, 337 in the partition `tmp/LIGHTING_PLAN.md`
§4.1 sizes for.

`ScalarBlockLayout` is enabled at device creation (`vulkan/backend.go:414`) and
is load-bearing: `LightData` is 72 bytes, so `lights[]` has a stride that is not
16-aligned and the _standard_ layout rules reject it. `spirv-val` must be given
`--scalar-block-layout` or it fails on every module.

**The split.** `BindFrameUniforms` publishes the pass block once; each `Draw`
sends only the transform and material. Before the split a single 1312-byte block
went out on every draw, roughly 1.2 KB of which was identical across the pass.

|               | How                                                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Transport     | per-frame ring buffer: one frame entry per pass _and per atlas tile_, one draw entry per draw, one record array per frame |
| Layout        | scalar, 4844 + 128 + 96×N bytes                                                                                           |
| Addressing    | three 64-bit device addresses in one push constant                                                                        |
| Textures      | handles rewritten into **bindless slot indices** in the copy                                                              |
| Cost per draw | one 128-byte memcpy + one 24-byte push constant                                                                           |

**The one rule:** keep the field _order_ in `renderer/uniforms.go` and
`shaders/slang/common.slang` identical, and use only `float32`/`int32`, arrays
of those, and matrices. Scalar layout and Go packing then agree by construction.

Nothing else is required. The 16-byte cells, the `float3`-plus-scalar pairing and
the `int4`-not-`int[4]` trick were std140's rule, mandatory while OpenGL was a
backend, and were removed on 2026-08-05 along with `LightData`'s three reserved
floats (80 → 68 bytes, and `FrameUniforms` 1280 → 1184). The lighting work then
took `LightData` to **72** bytes and `MaxLights` from 8 to 64 (1184 → 5248), and
the atlas took the six-matrix cube array and the cube bookkeeping back out, so
`FrameUniforms` is **4844** today. All but 236 bytes of that is `lights[]`, which
is what Part G moves to the record buffer.

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

| Method            | What it does                                                                                                                                |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `CreateTexture`   | Creates the image, fills it via a staging buffer inside an `immediateSubmit`, creates the view, writes a descriptor into bindless binding 0 |
| `CreateCubemap`   | Stages all six faces as one contiguous block into a 6-layer `CubeCompatible` image, one copy command, bindless binding 1                    |
| `UpdateTexture2D` | **Stages** the pixels into a persistently mapped buffer and defers the copy to the next `BeginFrame`                                        |
| `DestroyTexture`  | Drains the frames in flight, then destroys view + image + staging                                                                           |

Four things worth knowing:

- **How the shader reaches a texture.** One descriptor set with two **bindless**
  arrays (256 2D, 64 cube, `PartiallyBound | UpdateAfterBind`). The shader
  indexes them with the slot number that arrived in the uniform block.
  [HTV: descriptor indexing]
- **The shadow atlases are the exception.** They get dedicated descriptors
  (binding 2 static, binding 3 dynamic) rather than bindless slots, because the
  PCF kernel taps them up to 13× per fragment and some drivers re-fetch a
  dynamically-indexed descriptor per tap. Going bindless there cost ~1.7× the
  frame time. Both are `Sampler2D` now — a cube face is an ordinary tile, so the
  `SamplerCube[MAX_SHADOW_CUBES]` array at binding 3 is gone.
- **The UI overlay.** A copy cannot be recorded inside a render pass, so the
  pixels are staged and copied at the top of the next frame: one frame of
  latency, no queue stall. Resizing the canvas _retires_ the old image instead
  of destroying it, because the command buffer being recorded still references
  it (`retire` / `drainRetired`, aged `framesInFlight + 1` frames).
- **A "no texture" fallback**: handle translation falls back to slot 0, the
  built-in white pixel.

### 4.7 Buffers and meshes

| Method                          | What it does                                                                                                       |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `CreateBuffer`                  | Host-visible, persistently mapped VMA allocation + memcpy. `dynamic` is ignored — an update is a memcpy either way |
| `UpdateBuffer`                  | Drains the frames in flight, then memcpys. There is no driver-side ghosting to hide behind                         |
| `CreateMesh`                    | Pairs the vertex buffer handle with an index buffer and stores the layout. The layout keys the pipeline            |
| `DestroyMesh` / `DestroyBuffer` | Drains frames in flight first, then `VmaDestroyBuffer`                                                             |

A mesh is one shared vertex buffer plus one index list per material face group,
so a 3-material OBJ is 1 vertex buffer + 3 mesh handles.

_Who waits_ is the thing to remember: the backend tracks whether the GPU still
needs a buffer, explicitly, which is what `waitAllFrames` is for — note it skips
the frame currently being recorded, whose fence was reset in `BeginFrame` and can
only be signalled by `EndFrame`.

### 4.8 Offscreen render targets

One `CreateRenderTarget(RenderTargetSpec)` covers every kind. The spec says what
the target _is_ — size, `TargetDepth` or `TargetColor`, cube or not — rather than
what it is for, which is what lets an HDR buffer or a G-buffer be expressed
without widening the interface.

| Spec                  | What it builds                                                                                                                                                                                   |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| depth                 | Depth image usable as attachment, sampled _and_ transfer source/destination, plus a `ClampToBorder` / `OpaqueWhite` sampler. The one live user is the 4096² shadow atlas                         |
| cube                  | 6-layer `CubeCompatible` image, plus **two views of it**: a 2D-array view to attach and a cube view to sample. **Nothing asks for one any more** — the skybox is a `CreateCubemap`, not a target |
| colour                | Colour image + view, rendered with `passOffscreenColor` (flipped viewport, CCW, no depth attachment)                                                                                             |
| `DestroyRenderTarget` | Drains frames, destroys the attachment view and the image                                                                                                                                        |

The cube path survives because it costs nothing to keep and `ShadowRecord.Flags`
reserves a bit to switch point lights back to a cube texture if atlas corner
filtering disappoints (`tmp/LIGHTING_PLAN.md` §11.3). Until then the layered
geometry-stage draw it was built for is gone. [LOGL: Point Shadows]

The two-views trick is required: a cube view cannot be attached and an array view
cannot be sampled as a cube. The image's layout is tracked across passes.

> `TargetColor` exists but nothing uses it yet — it is the seam an HDR target
> lands on, once the half-float format is bound (`tmp/BACKEND_DECISION.md` §7).

**The atlas methods.** A shadow atlas is not a new kind of target: it is an
ordinary large `TargetDepth` with `Cube: false`, and what makes it an atlas is
how it is drawn into. Two methods do that, added by `tmp/LIGHTING_IMPL.md` Part B
and taken up by Part C.

| Method               | What it does                                                                                                                                                                                                                                                                                            |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SetViewportScissor` | Narrows viewport _and_ scissor to one tile, inside a pass. `viewportFor` gives it the pass's own y handedness, so a tile of the atlas is oriented like the whole target would be (§5)                                                                                                                   |
| `CopyDepthRegion`    | Barriers both images into `TRANSFER_SRC`/`TRANSFER_DST`, `CmdCopyImage` on the depth aspect, barriers both back to shader-read. Refuses to run inside a pass, where a copy is invalid, and refuses a region that does not fit either target — an out-of-bounds copy is a device loss, not a clipped one |

Depth targets are therefore created with `TransferSrc | TransferDst` usage on top
of attachment and sampled. That is the whole cost of the static/dynamic split:
one cached atlas is copied tile-by-tile into a second, which then has only the
movable casters drawn on top.

**What a tile is, from the shader's side.** `ShadowRecord` (96 bytes) carries the
tile's `LightSpace` matrix, its `AtlasRect` (uv offset and scale), `TexelSize`,
`FarPlane`, `Face` and `Flags`. The same matrix bakes the tile and samples it,
which is what keeps the two from disagreeing. `Face` doubles as the depth-encoding
selector: `-1` is a sun or spot tile holding ordinary projected depth, `0..5` is a
cube face holding **linear radial distance to the light**, which stays continuous
across a face boundary so one bias covers all six.

### 4.9 Draws

| Method | What it does                                                                                                                                                                                                           |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Draw` | Resolve handles, bind the pipeline for (shader, current pass, **mesh's** layout) if it changed, memcpy the draw block into the ring, push both addresses, bind vertex (+ index) buffers, `CmdDrawIndexed` or `CmdDraw` |

**One entry point for every drawable.** A face group, the skybox cube and the UI
overlay differ only in what was recorded when the mesh was created — vertex
layout, count, indexed or not — so adding a drawable kind means adding a way to
_build_ a mesh, not a way to draw one. The overlay's quad is built once by
`core.createOverlayQuad` and drawn like anything else; its pipeline still tests
depth without writing it, which is keyed off the fullscreen vertex layout.

`boundPipeline` is tracked so redundant binds are skipped.

### 4.10 Capabilities

`Supports` returns `false` today and has never been wired. With one backend it
means what it says — _does this physical device have the extension_ — rather
than the cross-backend performance hint an earlier design intended. The first
real answer will be `FeatureRayTracing` against `VK_KHR_ray_query` availability,
which is a genuine runtime fork: a GTX 1080 runs the same engine with a compute
BVH instead. See `tmp/BACKEND_DECISION.md` §5.2 and §8.

### 4.11 What reaches the GPU, and when

Every `vkCmd*` call _records_ into a command buffer; nothing executes until a
submit. Two facts are worth holding separately.

**The queue is touched in exactly three places.**

| site                             | operation                        | why                                                                                                                       |
| -------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `EndFrame` (`vulkan/backend.go`) | `QueueSubmit2`                   | **one submit carries the whole frame** — the atlas pass and all its tiles, skybox, every draw, the UI quad, every barrier |
| `EndFrame`                       | `QueuePresentKHR`                | hands the image to the compositor, waiting on that image's render semaphore                                               |
| `immediateSubmit`                | `QueueSubmit2` + `QueueWaitIdle` | load-time texture uploads only. Blocks the CPU, which is acceptable only because nothing else is queued yet               |

The frame submit is where the three sync objects meet, each with a different
job: it _waits_ on `acquireSem[frameIndex]`, _signals_ `renderSems[imageIndex]`
for present, and _signals_ `fence[frameIndex]` for the CPU throttle. §7's last
subsection is why those two indices are not the same.

**Recording, grouped by how often it happens.**

| frequency                        | commands                                                                                                                                                                                                                              |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| per frame                        | `flushPendingUploads` (barrier, `CmdCopyBufferToImage`, barrier — the UI overlay), `CmdBindDescriptorSets` once for the whole frame, and `EndFrame`'s barrier to `PRESENT_SRC_KHR`                                                    |
| per pass, ×2                     | 1-3 barriers into attachment layout, `CmdBeginRendering`, `CmdSetViewport`, `CmdSetScissor`, `applyDynamicState` (cull + depth compare); then `CmdEndRendering` and, for an offscreen target, a barrier to `SHADER_READ_ONLY_OPTIMAL` |
| per shadow tile, ×7              | `CmdSetViewport` + `CmdSetScissor` narrowed to the tile, then a whole mesh loop. No barrier and no `CmdBeginRendering` — that is the saving the atlas exists for                                                                      |
| occasionally                     | `CmdSetCullMode` (a sun tile flips to front-face culling), `CmdSetDepthCompareOp` (the skybox flips to `LESS_OR_EQUAL` and back)                                                                                                      |
| per draw, ~15 + 7 tiles × meshes | `CmdBindPipeline` _(skipped when unchanged)_, `CmdPushConstants` (24 bytes), `CmdBindVertexBuffer`, `CmdBindIndexBuffer` when indexed, `CmdDrawIndexed` / `CmdDraw`                                                                   |
| load time                        | `recordImageUpload`'s barrier + copy + barrier, on a throwaway command buffer                                                                                                                                                         |

Two things this makes obvious:

- **Barriers dominate.** Nine of the ~21 recording sites are layout transitions,
  all of them through `imageBarrier` — the work OpenGL did invisibly. What one
  does, and why the access masks matter as much as the stage masks, is in
  `cheatsheets/VULKAN.md` §8.
- **The per-draw path is four or five commands**, one of which is usually
  skipped. That is the payoff of the BDA design in §4.5: no descriptor bind and
  no uniform buffer bind per draw, just a 24-byte push constant.

> The UI overlay is the case that explains the machinery. `UpdateTexture2D` runs
> _inside_ the main pass, where a copy cannot be recorded, and `immediateSubmit`
> would stall the queue every frame. So the pixels are staged and the copy is
> recorded at the top of the _next_ frame, riding the ordinary frame submit —
> one frame of latency, no extra queue operation.

`CmdPipelineBarrier2` and `QueueSubmit2` are both `VK_KHR_synchronization2`,
enabled at device creation. The engine uses the pair consistently rather than
mixing sync2 barriers with a 1.0 submit.

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
faces. The shadow passes deliberately use a _positive_ viewport, since a shadow
map is sampled rather than presented and the depth comparison in the shaders
expects that memory layout; the price is inverted winding, which those pipelines
declare as `FrontFace = Clockwise`.

**One place decides the flip.** `vulkan/backend.go` `viewportFor` builds every
viewport the backend sets, from the pass kind: negative height for `passMain` and
`passOffscreenColor`, positive for the two shadow kinds. `BeginPass` calls it for
the whole target, `SetViewportScissor` for a rect of it, so a tile inherits its
pass's handedness rather than restating it.

**A viewport is set by `BeginPass`, or narrowed by `SetViewportScissor` within
that pass, and by nothing else.** This is the amendment to the "clears and
viewports live inside `BeginPass`" invariant, and it exists for one case: an
atlas target holds many independent shadow tiles, and baking each through a pass
of its own would mean one `CmdBeginRendering` and one layout transition per tile.
The scissor is set alongside the viewport every time — the viewport transforms
clip space, but only the scissor keeps a clear or an out-of-range primitive off
the neighbouring tiles.

**Depth range.** The projections built in `scene/` are the OpenGL convention,
giving clip z in `[-w, w]`, while Vulkan clips to `[0, w]`. Every vertex stage
therefore calls `TO_VK_DEPTH` from `common.slang`. Changing the projections
instead would remove the macro — a cleanup, not a bug.

**MSAA is a backbuffer-only property.** `settings.MSAASamples` (1 = off) is read
once at `Init`. The backend allocates a multisampled colour image plus a matching
multisampled depth image, draws the main pass into them, and resolves into the
swapchain image with `ResolveModeAverage` on the colour attachment. Offscreen
targets stay single-sampled — a later pass has to _sample_ them, and a
multisampled texture is not something these shaders can read — so
`vulkan/shader.go` `passSamples` gives the multisampled count to `passMain`
only. A pipeline whose sample count disagrees with its pass's attachments is
invalid, so this is the one place that decision lives.

**Outside a light's frustum reads unshadowed — and the sampler no longer says
so.** `BorderColor = OpaqueWhiteFloat` gave that free while a shadow map was a
whole texture. Inside an atlas a tile's neighbours _are_ what lies past its edge,
so `shadowLookup` tests the tile-local uv (and `z > 1`) explicitly and returns 0
before it samples, while every PCF tap is clamped to the tile inset by one texel.
Drop either and a fragment outside one light's frustum picks up another light's
shadow. The border colour is now only a backstop.

**The uniform struct is the contract.** `renderer/uniforms.go` has an `init`
that panics if `LightData` stops being 72 bytes, `FrameUniforms` 4844,
`ShadowRecord` 96 or `DrawUniforms` 128. Field _order_ is what has to match, and the size panic does
not check order — see §4.5 for how to verify it.

---

## 6. Where to look when something is wrong

| Symptom                                                               | Look at                                                                                                                                                                    |
| --------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The image is mirrored, or culled inside-out                           | `vulkan/backend.go` `BeginPass` viewport, `vulkan/shader.go` `frontFace` (§5)                                                                                              |
| Garbage uniforms after editing `common.slang`                         | `spirv-dis shaders/vk/forward.frag.spv \| grep OpMemberDecorate` against `unsafe.Offsetof`, in order — the `init()` size panic cannot see a swap (§4.5)                    |
| `spirv-val` rejects every module                                      | Missing `--scalar-block-layout`; `LightData`'s 72-byte stride is legal only under it (§4.5)                                                                                |
| Validation complains about image layouts                              | `imageBarrier` call sites in `BeginPass` / `EndPass` / `recordImageUpload`                                                                                                 |
| A resource is destroyed while in use                                  | `waitAllFrames`, `retire`, `drainRetired` (§7)                                                                                                                             |
| Shadows missing on one light                                          | `Scene.pickShadowCasters` — only the first sun and first point light get tiles                                                                                             |
| Shadows from the wrong light, or a hairline crack at a cube-face edge | `forward.slang` `shadowLookup` — the tap clamp and the `+2 texel` FOV widening in `scene/shadowatlas.go` `cubeFaceFov` are what prevent each (`tmp/LIGHTING_PLAN.md` §4.2) |
| A point shadow lands on the wrong face                                | `cubeFaceDirs` (`scene/shadowatlas.go`) and `cubeFace()` (`forward.slang`) are two lists that must agree; `scene/shadowatlas_test.go` is what checks it                    |
| UI overlay lags by a frame                                            | Expected: `UpdateTexture2D` stages, `BeginFrame` copies                                                                                                                    |
| Nothing starts                                                        | `./build_shaders.sh` — the generated shaders are git-ignored                                                                                                               |
| Pipeline creation fails after a pass change                           | Sample count or attachment formats disagreeing with the pass (§5, MSAA)                                                                                                    |

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

| Class                        | Objects                                                                                                   | Created                              | Destroyed                                                    |
| ---------------------------- | --------------------------------------------------------------------------------------------------------- | ------------------------------------ | ------------------------------------------------------------ |
| **Permanent**                | instance, surface, device, allocator, command pool, descriptor pool/set/layout, pipeline layout, samplers | `Init`, once                         | `Shutdown`, reverse order                                    |
| **Swapchain-sized**          | swapchain, image views, render semaphores, depth image + view, MSAA colour image + view                   | `createSwapchain`                    | `destroySwapchain` — **also on every resize**                |
| **Per frame in flight** (×2) | command buffer, fence, acquire semaphore, uniform ring                                                    | `createFrameData`                    | `Shutdown`                                                   |
| **Per resource**             | shader modules + pipelines, textures, buffers, meshes, the shadow atlas                                   | load time, on demand                 | `Destroy*` (after `waitAllFrames`) or `Shutdown`             |
| **Retired**                  | images/views/staging replaced mid-frame                                                                   | `retire`, when the UI canvas resizes | `drainRetired`, once `framesInFlight + 1` frames have passed |

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
   `QueuePresentKHR` — that error _is_ how a resize reaches a Vulkan app.

### Two index spaces that are easy to confuse

`frameIndex` cycles `0..framesInFlight-1` and selects the command buffer, fence,
acquire semaphore and ring. `imageIndex` comes back from `AcquireNextImageKHR`
and selects the swapchain image, its view and its render semaphore. They are not
interchangeable and the swapchain may hold a different number of images than
there are frames in flight — which is exactly why `renderSems` is sized per
image while `acquireSem` lives per frame.
