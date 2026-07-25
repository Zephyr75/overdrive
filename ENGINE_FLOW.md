# ENGINE_FLOW.md — how a frame gets drawn, and what each backend does with it

This document is the reading guide to `src/`. It follows one frame from
`main()` down to the GPU, then walks the `renderer.Backend` contract method by
method, showing what the OpenGL backend and the Vulkan backend each do with it —
where they are the same idea in different words, and where they genuinely differ.

`GO_BACKEND.md` is the design document (why the abstraction looks like this).
This one is the operational one (what actually happens, in order).

Learning links: **[LOGL]** points at learnopengl.com, **[HTV]** at
howtovulkan.com.

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
renderer/          the abstraction: Backend interface, opaque handles, Uniforms struct
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
| 1 | `core.createBackend` | Reads `OVERDRIVE_BACKEND` (`gl` default, or `vulkan`) and constructs `opengl.New()` / `vulkan.New()`. Lives in `core/` because the backend packages import `renderer`, so `renderer` cannot import them back. |
| 2 | `glfw.Init` | Window system up. |
| 3 | `Backend.ConfigureWindow` | GL: hints a 4.1 core forward-compatible context, 4x MSAA. VK: hints `ClientAPI = NoAPI` — there is no context to create. |
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
      Light.RenderLight:
          BeginPass(shadowTarget, 1024, 1024, nil)   ← no color clear, depth only
          draw every mesh with depth / depth_cube
          EndPass()

  BeginPass(0, w, h, &{0.1,0.1,0.1,1})               ← backbuffer, clears color
      Scene.RenderSkybox     SetDepthFunc(LEQUAL) → draw cube → back to LESS
      Scene.RenderScene      every mesh, every face group, forward shader
      renderUI               rasterise widgets to RGBA → UpdateTexture2D → DrawFullscreenQuad
  EndPass()

Backend.EndFrame()          present
glfw.PollEvents()
```

Uniforms travel as **one** `renderer.Uniforms` value (1312 bytes) that the frame
loop fills, `Scene.FillFrameUniforms` tops up with camera/lights/shadow handles,
and `Mesh.draw` overwrites the material fields before each draw. Each backend
snapshots it at draw time, so the caller may keep mutating it.

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

Both backends receive the same `renderer.Uniforms` and must get it to the
shader. Nothing about this is shared code.

**OpenGL — std140 uniform block.** `marshalStd140` hand-writes the struct into
a byte buffer at explicit offsets, `glBufferSubData`s it into the one UBO bound
to binding point 0, then binds the referenced textures to their fixed units.
std140 pads `vec3` to 16 bytes and rounds array strides up to 16, which is why
the block is 1600 bytes on this side. Those offsets are the only hand-written
layout in the engine, and `opengl/uniforms_test.go` re-derives them from the
generated GLSL and fails on drift. [LOGL: Advanced GLSL — uniform buffer objects]

**Vulkan — scalar layout + buffer device address.** Go packs
`float32`/`int32` structs with no padding, which *is* Vulkan's scalar block
layout, so `pushUniforms` memcpys the struct straight into this frame's ring
buffer (1 MiB, 64-byte aligned entries) and pushes the entry's **GPU address**
as an 8-byte push constant. The shader dereferences that pointer — the uniform
data needs no descriptor at all. 1312 bytes, no padding. [HTV: buffer device
address]

| | OpenGL | Vulkan |
|---|---|---|
| Transport | one shared UBO, rewritten per draw | per-frame ring buffer, one 1312-byte entry per draw |
| Layout | std140, 1600 bytes, hand-written offsets | scalar, 1312 bytes, free from Go's packing |
| Addressing | binding point 0 | 64-bit device address in a push constant |
| Textures | handles ignored — the shader samples named samplers on fixed units | handles rewritten into **bindless slot indices** in the copy |
| Cost per draw | one `BufferSubData` + up to 9 texture binds | one memcpy + one 8-byte push constant |

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
| `CreateMesh` | Builds a **VAO**: binds the shared vertex buffer, records the `pos|normal|uv` attribute pointers, uploads this group's index buffer | Just pairs the vertex buffer handle with a new index buffer. There is no VAO — the layout lives in the pipeline |
| `CreateSkyboxMesh` | A VAO owning a position-only vertex buffer, no indices | A vertex buffer, no index buffer, which is what marks it as the skybox layout at draw time |
| `DestroyMesh` / `DestroyBuffer` | `DeleteVertexArrays` / `DeleteBuffers` | Drains frames in flight first, then `VmaDestroyBuffer` |

**Same idea:** a mesh is one shared vertex buffer plus one index list per
material face group (so a 3-material OBJ is 1 vertex buffer + 3 mesh handles).

**Different:** the VAO/pipeline split above, and *who waits*. GL's driver
tracks whether the GPU still needs a buffer. In Vulkan the backend does it
explicitly, which is what `waitAllFrames` is for — note it skips the frame
currently being recorded, whose fence was reset in `BeginFrame` and can only be
signalled by `EndFrame`.

### 4.8 Shadow render targets

| Method | OpenGL | Vulkan |
|---|---|---|
| `CreateShadowMap2D` | FBO + `DEPTH_COMPONENT` texture, `NEAREST`, clamp-to-border with a **white** border so outside the light frustum reads "fully lit", `DrawBuffer(NONE)` | Depth image usable as both attachment and sampled, plus a `ClampToBorder` / `OpaqueWhite` sampler |
| `CreateShadowCubemap` | FBO + cubemap depth texture attached with `FramebufferTexture` (**layered**), the geometry shader routing triangles to faces | 6-layer `CubeCompatible` depth image, plus **two views of it**: a 2D-array view to attach and a cube view to sample |
| `DestroyFramebuffer` | `DeleteFramebuffers` | Drains frames, destroys the attachment view and the image |

**Same idea, same technique** — one layered draw for all six cube faces, driven
by a geometry stage. [LOGL: Point Shadows]

**Different:** Vulkan needs the two-views trick (you cannot attach a cube view
or sample an array view), and needs the image's layout tracked across passes.

### 4.9 Draws

| Method | OpenGL | Vulkan |
|---|---|---|
| `DrawMesh` | `UseProgram`, upload uniforms + bind textures, `BindVertexArray`, `DrawElements` | Resolve handles, bind the pipeline for (shader, current pass, mesh layout) if it changed, memcpy uniforms into the ring, push the address, bind vertex + index buffers, `CmdDrawIndexed` |
| `DrawSkybox` | Same, `DrawArrays(TRIANGLES, 0, 36)` | Same, `CmdDraw(36, …)`, skybox layout |
| `DrawFullscreenQuad` | Builds the quad VAO on first use, binds the UI texture to `ourTexture`'s unit, `TRIANGLE_STRIP` ×4 | Builds the quad buffer on first use, sends the texture through the ordinary uniform block, 6 vertices (two triangles). The fullscreen pipeline tests depth but does not write it |

**Same idea.** The interesting asymmetry is that Vulkan tracks
`boundPipeline` to skip redundant binds, whereas GL's `UseProgram` per draw is
cheap enough to leave alone.

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

**Shadow border colour.** GL sets `TEXTURE_BORDER_COLOR` to opaque white;
Vulkan sets `BorderColor = OpaqueWhiteFloat` on the 2D shadow sampler. Same
result: outside the sun's frustum is unshadowed.

**The uniform struct is the contract.** `renderer/uniforms.go` has an `init`
that panics if `LightData` stops being 68 bytes or `Uniforms` 1312 — that is the
guard on Vulkan's memcpy path. `opengl/uniforms_test.go` is the guard on the GL
side, re-deriving std140 offsets from the generated GLSL. Between them, a change
to `common.slang` cannot silently break one backend.

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
