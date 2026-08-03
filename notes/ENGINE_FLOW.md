# ENGINE_FLOW.md — how a frame gets drawn, and what each backend does with it

This document is the reading guide to `src/`. It follows one frame from
`main()` down to the GPU, then walks the `renderer.Backend` contract method by
method, showing what the OpenGL backend and the Vulkan backend each do with it —
where they are the same idea in different words, and where they genuinely differ.

`ARCHITECTURE.md` is the map (where every package and symbol lives) and
`FEATURES.md` is the feature list with the reasoning behind each one. This is the
operational document: what actually happens, in order.

Learning links: **[LOGL]** points at learnopengl.com, **[HTV]** at
howtovulkan.com.

---

## Contents

0. [The `Backend` contract by how often it is called](#0-the-backend-contract-by-how-often-it-is-called)
1. [The layers](#1-the-layers)
2. [Startup, in order](#2-startup-in-order)
3. [The frame loop](#3-the-frame-loop-coreapprun)
4. [The `renderer.Backend` contract, backend by backend](#4-the-rendererbackend-contract-backend-by-backend)
5. [Conventions that make the two outputs match](#5-conventions-that-make-the-two-outputs-match)
6. [Where to look when something is wrong](#6-where-to-look-when-something-is-wrong)
7. [Who owns what, and what dies when (Vulkan)](#7-who-owns-what-and-what-dies-when-vulkan)

---

## 0. The `Backend` contract by how often it is called

`renderer.Backend`'s 25 methods are declared by **resource type** — textures,
buffers, meshes, shaders, targets, draws. That is the wrong axis for remembering
*where a Vulkan call sits in a frame*. This table is the other axis: how often
each method runs, and what it becomes on each backend. §4 walks the same 27
methods in interface order, with the reasoning; this is the index.

The obscure Vulkan names get easier once they are filed by frequency —
`vkAcquireNextImageKHR` is "the per-frame one", `vkCmdPipelineBarrier2` is "the
per-pass one", `vkCmdPushConstants` is "the per-draw one".

### Once, at startup — 3 methods

| Method | OpenGL | Vulkan |
|---|---|---|
| `ConfigureWindow` | `WindowHint`: 4.1 core, forward-compatible, 4× samples | `WindowHint(ClientAPI, NoAPI)` |
| `Init` | `MakeContextCurrent`, `gl.Init`, enable depth/cull/blend, white pixel + black cube, shared UBO on binding 0 | `CreateInstance` → surface → `EnumeratePhysicalDevices` → queue family → `CreateDevice` → `VmaCreateAllocator` → swapchain → `CreateCommandPool` → per-frame data → samplers → descriptors → pipeline layout → default textures |
| `Shutdown` | Nothing — objects die with the context | `DeviceWaitIdle`, then destroy everything in reverse creation order (see §7) |

### Once per resource, at load time — 9 methods

| Method | OpenGL | Vulkan |
|---|---|---|
| `CreateShader` | `CreateShader` ×2-3, `CompileShader`, `LinkProgram`, then pin sampler units + block binding | `CreateShaderModule` ×2-3. **No pipeline yet** — built lazily per (pass, layout) |
| `LoadTexture` | `GenTextures`, `TexImage2D` | `VmaCreateImage` + staging buffer + `immediateSubmit(CmdCopyBufferToImage)` + `CreateImageView` + bindless descriptor write |
| `LoadCubemap` | Six `TexImage2D` onto the cube target | One 6-layer `CubeCompatible` image, six faces staged contiguously, one copy |
| `WhiteTexture` | Returns the built-in texture name | Returns `0` — handle 0 *is* bindless slot 0 |
| `CreateBuffer` | `GenBuffers` + `BufferData` | `VmaCreateBuffer` host-visible + persistently mapped + `MemCopy` |
| `CreateMesh` | `GenVertexArrays`, record the layout's attribute pointers, upload the EBO if indexed | Pair the vertex handle with an index buffer and record the layout. No VAO — the layout keys the pipeline |
| `CreateRenderTarget` | FBO plus a depth texture (white border) or a colour texture with a depth renderbuffer | Image usable as attachment *and* sampled, plus **two** views for cubes: 2D-array to attach, cube to sample |

### On demand, rarely — 6 methods

| Method | OpenGL | Vulkan |
|---|---|---|
| `UpdateBuffer` | `BufferData` again; the driver ghosts old storage | `waitAllFrames()` **then** memcpy. No ghosting — this is a full GPU drain |
| `DestroyTexture` | `DeleteTextures` | `waitAllFrames()`, destroy view + image + staging |
| `DestroyBuffer` | `DeleteBuffers` | `waitAllFrames()`, `VmaDestroyBuffer` |
| `DestroyMesh` | `DeleteVertexArrays` | `waitAllFrames()`, destroy the index buffer |
| `DestroyRenderTarget` | `DeleteFramebuffers` | `waitAllFrames()`, destroy view + image |
| `Supports` | `false` | `false` — the seam for ray tracing / compute |

### Once per frame — 4 methods

| Method | OpenGL | Vulkan |
|---|---|---|
| `BeginFrame` | Nothing | `WaitForFences` (the CPU throttle) → `AcquireNextImageKHR` → `ResetFences` → rewind ring → `drainRetired` → `ResetCommandBuffer` + `BeginCommandBuffer` → `CmdBindDescriptorSets` → flush staged uploads |
| `UpdateTexture2D` | `TexImage2D` immediately — legal mid-pass | Memcpy into a mapped staging buffer, **defer** the copy to the next `BeginFrame`. Costs the overlay one frame of latency |
| `EndFrame` | `SwapBuffers` | Barrier to `PresentSrcKHR` → `EndCommandBuffer` → `QueueSubmit2` (wait acquire sem, signal image's render sem, signal fence) → `QueuePresentKHR` → advance frame slot |

### Once per pass, ×3 a frame — 5 methods

Two shadow passes (depth-only, no colour clear) then the main backbuffer pass.

| Method | OpenGL | Vulkan |
|---|---|---|
| `BindFrameUniforms` | `BufferSubData` 1280 B into the frame UBO, bind the shadow and skybox units | Memcpy the same 1280 B into the ring, cache its device address for the pass's draws |
| `BeginPass` | `BindFramebuffer`, `Viewport`, `Clear` | `imageBarrier` into attachment layout → `CmdBeginRendering` (load ops carry the clear) → `CmdSetViewport` → `CmdSetScissor` → re-issue dynamic state |
| `SetCullFace` | `gl.CullFace` | `CmdSetCullMode` — dynamic state, no extra pipeline |
| `SetDepthFunc` | `gl.DepthFunc` | `CmdSetDepthCompareOp` — dynamic state |
| `EndPass` | Rebind the backbuffer | `CmdEndRendering`, and for a shadow target `imageBarrier` depth-attachment → shader-read-only |

### Once per draw, ~15 a frame — 1 method

| Method | OpenGL | Vulkan |
|---|---|---|
| `Draw` | `UseProgram` → `BufferSubData` 128 B into the draw UBO → bind 2 texture units → `BindVertexArray` → `DrawElements` or `DrawArrays` | `getPipeline(shader, pass, mesh layout)` (skipped if unchanged) → memcpy 128 B into the ring → `CmdPushConstants` (two 8-byte device addresses) → bind vertex (+ index) → `CmdDrawIndexed` or `CmdDraw` |

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
opengl/  vulkan/   the only packages that may import gl.* / vk.*
```

The rule above the line: **nothing in `scene/`, `core/`, `ecs/`, `input/` or
`physics/` imports a graphics API.** They own handles (`renderer.MeshHandle`,
`renderer.TextureHandle`, …), which are opaque integers each backend interprets
in its own table.

The rule inside a frame: **clears and viewports exist only inside
`Backend.BeginPass`.** No free-floating clear calls anywhere else.

---

## 2. Startup, in order

| Step | Code | What happens |
|---|---|---|
| 0 | `settings.Load` | `main.go` decodes the file named by `-config` (`config.toml` by default) over the defaults, then lets `OVERDRIVE_BACKEND` / `OVERDRIVE_MSAA` override it. Everything below reads the result, so it has to run before `core.NewApp`. |
| 1 | `core.createBackend` | Constructs `opengl.New()` / `vulkan.New()` from `settings.Backend`. Lives in `core/` because the backend packages import `renderer`, so `renderer` cannot import them back. |
| 2 | `glfw.Init` | Window system up. |
| 3 | `Backend.ConfigureWindow` | GL: hints a 4.1 core forward-compatible context, plus `settings.MSAASamples` samples — the default framebuffer's sample count can only be chosen here. VK: hints `ClientAPI = NoAPI` — there is no context to create. |
| 4 | `glfw.CreateWindow` | The window exists. |
| 5 | input callbacks | Resize, scroll, mouse. A resize only records the new size — the viewport is a per-pass decision. |
| 6 | `Backend.Init(window)` | GL: makes the context current, enables depth/cull/blend, creates the white pixel, the black dummy cube, and the shared std140 uniform buffer. VK: instance → surface → physical device → queue family → logical device → VMA allocator → swapchain → command pool → per-frame data → samplers → descriptors → pipeline layout → default textures. |
| 7 | `App.Run` → `CreateShader` ×5 | `forward`, `depth`, `depth_cube` (geometry stage), `ui`, `skybox`. |
| 8 | `scene.NewScene` | Parses XML → OBJ/MTL → uploads vertex buffers, per-face-group meshes, material textures; picks the shadow casters; allocates their shadow targets; loads the skybox cubemap. |

Shaders are authored once in Slang (`shaders/slang/`) and compiled by
`build_shaders.sh` into `shaders/gl/*.glsl` (GLSL 4.10) and `shaders/vk/*.spv`
(SPIR-V). Neither backend reads `.slang` at runtime. This is what makes "the
same frame, two APIs" honest: the two backends run the *same shader source*.

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

Uniforms travel as **two** values split by update frequency.
`renderer.FrameUniforms` (1280 bytes) is filled by the frame loop and
`Scene.FillFrameUniforms`, then published once per pass by `BindFrameUniforms`.
`renderer.DrawUniforms` (128 bytes) carries the model matrix and the material,
which `Mesh.draw` rewrites before each draw. Each backend snapshots both at call
time, so the caller may keep mutating them.

Note the shadow bakes overwrite the light matrices in the frame block and rebind
it, which is why it is *pass*-scoped rather than strictly per frame; the skybox
does the same with a stripped-translation view of its own.

Shadow budget is fixed and resolved once at load: the first directional light
gets a 2D depth map, the first point light gets a depth cube. Every other light
is still lit in the forward pass — it just casts nothing.

---

## 4. The `renderer.Backend` contract, backend by backend

### 4.1 Lifecycle

| Method | OpenGL | Vulkan |
|---|---|---|
| `ConfigureWindow` | Context version 4.1 core, forward compatible, 4x samples | `ClientAPI = NoAPI` |
| `Init` | `MakeContextCurrent`, `gl.Init`, enable depth/cull/blend, create built-in textures, create the shared UBO bound to binding 0 | The whole device stack (see §2 step 6). Enables `ScalarBlockLayout`, `BufferDeviceAddress`, descriptor indexing, `DynamicRendering`, `Synchronization2`, `GeometryShader` |
| `Shutdown` | Nothing — GL objects die with the context | `DeviceWaitIdle`, then explicitly destroys every pipeline, module, image, view, buffer, sampler, fence, semaphore, pool, device, instance |

**Same idea:** bring the API up on the window.
**Different:** GL's driver owns object lifetime and thread state; Vulkan makes
every object, and its destruction order, the application's problem.

### 4.2 Frame and passes

| Method | OpenGL | Vulkan |
|---|---|---|
| `BeginFrame` | Nothing | Waits on this frame slot's fence (the CPU throttle for 2 frames in flight), acquires a swapchain image, resets the ring offset, drains retired resources, resets and begins the command buffer, binds the one descriptor set, flushes staged texture uploads |
| `BeginPass` | `BindFramebuffer`, `Viewport`, `Clear(depth [+ color])` | Barriers the target into attachment layout, `CmdBeginRendering` with load ops (`Clear` / `DontCare`), `CmdSetViewport`, `CmdSetScissor`, re-issues cull mode + depth compare |
| `EndPass` | Rebinds the backbuffer | `CmdEndRendering`, and for a shadow target barriers depth-attachment → shader-read-only |
| `EndFrame` | `SwapBuffers` | Barriers the swapchain image to present layout, ends and submits the command buffer (wait on acquire semaphore, signal the image's render semaphore, signal the fence), presents, advances the frame slot |

**Same idea:** a pass = "bind a target, set a viewport, clear, draw, finish".
**Different, and this is the biggest structural gap:**

* **Clears** are a standalone command in GL and a *load op* on an attachment in
  Vulkan — the clear is declared when rendering begins, not issued.
* **Layout transitions.** A GL texture is always "ready"; a Vulkan image is in a
  layout and must be barriered between "rendered into" and "sampled from". That
  is why `EndPass` has a Vulkan-only shadow-map transition. [HTV: barriers]
* **Synchronisation.** `SwapBuffers` hides acquire/submit/present, a fence, and
  two semaphores. Vulkan spells all five out. Note the two index spaces: the
  acquire semaphore and fence are *per in-flight frame*, the render semaphore is
  *per swapchain image* — present waits on the image's own semaphore.
* **Render pass objects** do not exist here: the Vulkan backend uses dynamic
  rendering, so attachments are named at `CmdBeginRendering` and their formats
  are baked into the pipeline.
* **Resize.** GL just gets a new viewport next frame. Vulkan gets
  `ERROR_OUT_OF_DATE_KHR` from acquire or present and rebuilds the swapchain,
  its views, its semaphores and the depth image.

### 4.3 Immediate state

| Method | OpenGL | Vulkan |
|---|---|---|
| `SetCullFace(front)` | `gl.CullFace(FRONT/BACK)` | Records the value, `CmdSetCullMode` when a frame is active |
| `SetDepthFunc(lequal)` | `gl.DepthFunc(LEQUAL/LESS)` | Records the value, `CmdSetDepthCompareOp` |

**Same, deliberately.** Both are Vulkan 1.3 *dynamic state* (promoted from
`VK_EXT_extended_dynamic_state`), which is exactly why the interface can keep
GL's immediate-call shape instead of exploding into one pipeline per
cull/depth combination. The Vulkan side also re-issues both at pass start
(`applyDynamicState`), because the engine sets them between passes as often as
inside them.

Two callers: the skybox flips depth to `LEQUAL` so the cube can sit on the far
plane [LOGL: Cubemaps], and the sun's shadow pass culls front faces to avoid
peter-panning [LOGL: Shadow Mapping].

### 4.4 Shaders and pipelines

| Method | OpenGL | Vulkan |
|---|---|---|
| `CreateShader(name, hasGeometry)` | Compiles `shaders/gl/<name>.{vert,frag,geo}.glsl`, links one program, points every uniform block at binding 0 and pins each sampler to its fixed texture unit — all link-time work. Handle = the GL program name | Loads `shaders/vk/<name>.{vert,frag,geo}.spv` into shader modules. **No pipeline is built** |

**This is the deepest conceptual difference.** GL has one linked *program* that
combines with whatever framebuffer and vertex array happens to be bound. Vulkan
bakes state into a *pipeline*, so one shader needs one pipeline per combination
it is actually drawn with:

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

### 4.5 Uniforms — the two translations

Both backends receive the same two uniform structs and must get them to the
shader. Nothing about this is shared code.

**OpenGL — two std140 uniform blocks.** `glBufferSubData` the struct straight
into the UBOs bound to binding points 0 and 1, then bind the referenced textures
to their fixed units. It is a plain memcpy, the same as Vulkan's, and that is not
free — it is bought by the **16-byte cell rule** in `common.slang`. std140 pads a
`vec3` to 16 bytes and gives a scalar array a 16-byte element stride, so every
`float3` in the two blocks is declared with a scalar behind it, loose scalars
come in fours, and `pointShadowLights` is an `int4` rather than an `int[4]`.
Every declaration group then fills a whole 16-byte cell, at which point std140
*is* scalar layout and no marshalling code exists on either side.
`opengl/uniforms_test.go` re-derives std140 from the generated GLSL and fails if
any member stops matching `unsafe.Offsetof` of its Go field.
[LOGL: Advanced GLSL — uniform buffer objects]

**Vulkan — scalar layout + buffer device address.** Go packs
`float32`/`int32` structs with no padding, which *is* Vulkan's scalar block
layout, so both blocks memcpy straight into this frame's ring buffer (1 MiB,
64-byte aligned entries) and their **GPU addresses** go out as a 16-byte push
constant. The shader dereferences those pointers — the uniform data needs no
descriptor at all. 1280 and 128 bytes, no padding. [HTV: buffer device address]

**Both split the same way.** `BindFrameUniforms` publishes the pass block once;
each `Draw` sends only the transform and material. Before the split a single
1312-byte block went out on every draw, roughly 1.2 KB of which was identical
across the pass.

| | OpenGL | Vulkan |
|---|---|---|
| Transport | two shared UBOs: frame rewritten per pass, draw per draw | per-frame ring buffer: one frame entry per pass, one draw entry per draw |
| Layout | std140, 1280 + 128 bytes | scalar, the same 1280 + 128 bytes |
| Addressing | binding points 0 and 1 | two 64-bit device addresses in one push constant |
| Textures | handles ignored — the shader samples named samplers on fixed units | handles rewritten into **bindless slot indices** in the copy |
| Cost per draw | one 128-byte `BufferSubData` + 2 texture binds | one 128-byte memcpy + one 16-byte push constant |

The two layout rows being identical is deliberate, not luck — see the cell rule
under "OpenGL" above. It is what lets both backends memcpy, and it is the
difference between one shared struct definition and two that drift.

### 4.6 Textures

| Method | OpenGL | Vulkan |
|---|---|---|
| `LoadTexture` | `GenTextures` + `TexImage2D`, linear, repeat | Creates the image, fills it via a staging buffer inside an `immediateSubmit`, creates the view, writes a descriptor into bindless binding 0 |
| `LoadCubemap` | Six `TexImage2D` calls onto the cube target | Stages all six faces as one contiguous block into a 6-layer `CubeCompatible` image, one copy command, bindless binding 1 |
| `WhiteTexture` | The real texture name of the built-in white pixel | `0` — handle 0 *is* the white pixel, and bindless slot 0 |
| `UpdateTexture2D` | `TexImage2D` immediately | **Stages** the pixels into a persistently mapped buffer and defers the copy to the next `BeginFrame` |
| `DestroyTexture` | `DeleteTextures`, the driver defers the free | Drains the frames in flight, then destroys view + image + staging |

**Same idea:** "here are RGBA8 pixels, give me something samplable".

**Different:**

* **How the shader reaches a texture.** GL pins each sampler to a fixed texture
  unit at link time (`shadowMap`=0, `ourTexture`=1, `normalMap`=2,
  `shadowCubeMap[0..3]`=3..6, `skybox`=7) and the backend binds textures to
  those units per draw. Vulkan uses one descriptor set with two **bindless**
  arrays (256 2D, 64 cube, `PartiallyBound | UpdateAfterBind`) and the shader
  indexes them with the slot number that arrived in the uniform block.
  [HTV: descriptor indexing]
* **The shadow maps are the exception on both sides.** Vulkan gives them
  dedicated descriptors (bindings 2 and 3) rather than bindless slots, because
  the PCF kernels tap them 9× / 20× per fragment and some drivers re-fetch a
  dynamically-indexed descriptor per tap.
* **The UI overlay.** GL reuploads the CPU-rasterised widget image immediately —
  legal mid-pass. Vulkan cannot record a copy inside a render pass, so the
  pixels are staged and copied at the top of the next frame: one frame of
  latency, no queue stall. Resizing the canvas *retires* the old image instead
  of destroying it, because the command buffer being recorded still references
  it (`retire` / `drainRetired`, aged `framesInFlight + 1` frames).
* **A "no texture" fallback exists on both sides**, differently spelled: GL
  substitutes the white pixel / black cube at bind time, Vulkan falls back to
  slot 0 when translating a handle.

### 4.7 Buffers and meshes

| Method | OpenGL | Vulkan |
|---|---|---|
| `CreateBuffer` | `GenBuffers` + `BufferData`, `STATIC_DRAW` or `DYNAMIC_DRAW` | Host-visible, persistently mapped VMA allocation + memcpy. `dynamic` is ignored — an update is a memcpy either way |
| `UpdateBuffer` | `BufferData` again. The driver ghosts the old storage if a draw still reads it | Drains the frames in flight, then memcpys. There is no ghosting |
| `CreateMesh` | Builds a **VAO**: binds the vertex buffer, records the layout's attribute pointers, uploads this group's index buffer when there is one | Pairs the vertex buffer handle with an index buffer and stores the layout. There is no VAO — the layout keys the pipeline instead |
| `DestroyMesh` / `DestroyBuffer` | `DeleteVertexArrays` / `DeleteBuffers` | Drains frames in flight first, then `VmaDestroyBuffer` |

**Same idea:** a mesh is one shared vertex buffer plus one index list per
material face group (so a 3-material OBJ is 1 vertex buffer + 3 mesh handles).

**Different:** the VAO/pipeline split above, and *who waits*. GL's driver
tracks whether the GPU still needs a buffer. In Vulkan the backend does it
explicitly, which is what `waitAllFrames` is for — note it skips the frame
currently being recorded, whose fence was reset in `BeginFrame` and can only be
signalled by `EndFrame`.

### 4.8 Offscreen render targets

One `CreateRenderTarget(RenderTargetSpec)` covers both. The spec says what the
target *is* — size, `TargetDepth` or `TargetColor`, cube or not — rather than
what it is for, which is what lets an HDR buffer or a G-buffer be expressed
without widening the interface.

| Method | OpenGL | Vulkan |
|---|---|---|
| `CreateRenderTarget`, depth | FBO + `DEPTH_COMPONENT` texture, `NEAREST`, clamp-to-border with a **white** border so outside the light frustum reads "fully lit", `DrawBuffer(NONE)` | Depth image usable as both attachment and sampled, plus a `ClampToBorder` / `OpaqueWhite` sampler |
| `CreateRenderTarget`, cube | FBO + cubemap texture attached with `FramebufferTexture` (**layered**), the geometry shader routing triangles to faces | 6-layer `CubeCompatible` image, plus **two views of it**: a 2D-array view to attach and a cube view to sample |
| `CreateRenderTarget`, colour | FBO + colour texture + a depth **renderbuffer**, which nothing samples | Colour image + view, rendered with `passOffscreenColor` (flipped viewport, CCW, no depth attachment) |
| `DestroyRenderTarget` | `DeleteFramebuffers`, plus the depth renderbuffer if it owned one | Drains frames, destroys the attachment view and the image |

**Same idea, same technique** — one layered draw for all six cube faces, driven
by a geometry stage. [LOGL: Point Shadows]

**Different:** Vulkan needs the two-views trick (you cannot attach a cube view
or sample an array view), and needs the image's layout tracked across passes.

### 4.9 Draws

| Method | OpenGL | Vulkan |
|---|---|---|
| `Draw` | `UseProgram`, upload the draw block + bind its two textures, `BindVertexArray`, then `DrawElements` or `DrawArrays` on the mesh's recorded count | Resolve handles, bind the pipeline for (shader, current pass, **mesh's** layout) if it changed, memcpy the draw block into the ring, push both addresses, bind vertex (+ index) buffers, `CmdDrawIndexed` or `CmdDraw` |

**One entry point for every drawable.** A face group, the skybox cube and the UI
overlay differ only in what was recorded when the mesh was created — vertex
layout, count, indexed or not — so adding a drawable kind means adding a way to
*build* a mesh, not a way to draw one. The overlay's quad is built once by
`core.createOverlayQuad` and drawn like anything else; its pipeline still tests
depth without writing it, which is keyed off the fullscreen vertex layout.

The interesting asymmetry is that Vulkan tracks `boundPipeline` to skip
redundant binds, whereas GL's `UseProgram` per draw is cheap enough to leave
alone.

### 4.10 Capabilities

`Supports` returns `false` on both today. It is the seam where ray tracing and
compute get added on the Vulkan side without widening the common interface —
the GL backend will simply keep reporting them unsupported.

---

## 5. Conventions that make the two outputs match

These are the subtle ones — the things that would silently render a mirrored,
inside-out, or inverted-depth image if they drifted.

**Clip space handedness.** OpenGL's NDC is y-up; Vulkan's is y-down. The Vulkan
backend fixes this in the main pass with a **negative-height viewport**
(`Y = height`, `Height = -height`), so no geometry, matrix, or shader has to
know. [HTV: viewport]

**Winding follows from that.** Flipping the viewport also flips triangle
winding, which cancels out — so the main pass keeps GL's counter-clockwise front
faces. The shadow passes deliberately use a *positive* viewport, so the shadow
map's memory layout matches GL's and the sampling math in the shaders is
unchanged; the price is inverted winding, which those pipelines declare as
`FrontFace = Clockwise`.

**MSAA is a backbuffer-only property.** `settings.MSAASamples` (1 = off) is read
once at `Init`. GL passes it to `glfw.WindowHint(glfw.Samples, …)`, since the
default framebuffer's sample count is fixed when the window is created. Vulkan
allocates a multisampled colour image plus a matching multisampled depth image,
draws the main pass into them, and resolves into the swapchain image with
`ResolveModeAverage` on the colour attachment. Offscreen targets stay
single-sampled on both backends — a later pass has to *sample* them, and a
multisampled texture is not something these shaders can read — so
`vulkan/shader.go` `passSamples` gives the multisampled count to `passMain`
only. A pipeline whose sample count disagrees with its pass's attachments is
invalid, so this is the one place that decision lives.

**Shadow border colour.** GL sets `TEXTURE_BORDER_COLOR` to opaque white;
Vulkan sets `BorderColor = OpaqueWhiteFloat` on the 2D shadow sampler. Same
result: outside the sun's frustum is unshadowed.

**The uniform struct is the contract.** `renderer/uniforms.go` has an `init`
that panics if `LightData` stops being 80 bytes, `FrameUniforms` 1280 or
`DrawUniforms` 128 — a size that is not a multiple of 16 means a cell was left
unfilled and the two layouts have already diverged. `opengl/uniforms_test.go` is
the guard on the GL side: it re-derives std140 from the generated GLSL and checks
every member against `unsafe.Offsetof` of the matching Go field, in both
directions, so a field added to `common.slang` and forgotten in Go fails too.
Between them, a change to `common.slang` cannot silently break one backend.

---

## 6. Where to look when something is wrong

| Symptom | Look at |
|---|---|
| One backend renders mirrored / culled inside-out | `vulkan/backend.go` `BeginPass` viewport, `vulkan/shader.go` `frontFace` |
| GL renders garbage after a shader edit | `opengl/uniforms.go` offsets — run `go test ./opengl/` |
| Vulkan validation complains about layouts | `imageBarrier` call sites in `BeginPass` / `EndPass` / `recordImageUpload` |
| A resource is destroyed while in use | `waitAllFrames`, `retire`, `drainRetired` in the Vulkan backend |
| Shadows missing on one light | `Scene.pickShadowCasters` — only the first sun and first point light get maps |
| UI overlay lags by a frame on Vulkan | Expected: `UpdateTexture2D` stages, `BeginFrame` copies |
| Neither backend starts | `./build_shaders.sh` — the generated shaders are git-ignored |

Run with `OVERDRIVE_BACKEND=gl` (default) or `=vulkan`, and set
`OVERDRIVE_VK_VALIDATION=1` while developing the Vulkan path.

---

## 7. Who owns what, and what dies when (Vulkan)

OpenGL needs no such section: objects belong to the context and die with it,
which is why `GLBackend.Shutdown` is empty. Vulkan makes every object and its
destruction order the application's problem, so this is the map.

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
