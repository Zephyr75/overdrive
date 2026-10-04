# ENGINE_FLOW.md — how a frame gets drawn

This document is the reading guide to `src/`. It follows one frame from
`main()` down to the GPU, then walks the four interfaces in `renderer/` method
by method, showing what the Vulkan backend does with each one and why.

`ARCHITECTURE.md` is the map (where every package and symbol lives) and
`FEATURES.md` is the feature list with the reasoning behind each one.
`tmp/INTERFACE_PLAN.md` is the plan this interface was built from; its §8 records
where the built thing differs from the plan.

An OpenGL 4.1 backend existed until 2026-08-05. Where a decision here only makes
sense as a legacy of it — the y-up clip space, the `[-w, w]` projections — that
is called out rather than left as an unexplained convention.

Learning links: **[LOGL]** points at learnopengl.com, **[HTV]** at
howtovulkan.com, whose stack (dynamic rendering, buffer device address,
descriptor indexing, synchronization2, VMA) is the one this backend uses.

---

## Contents

0. [The interface by how often it is called](#0-the-interface-by-how-often-it-is-called)
1. [The layers](#1-the-layers)
2. [Startup, in order](#2-startup-in-order)
3. [The frame loop](#3-the-frame-loop-coreapprun)
4. [The contract, method by method](#4-the-contract-method-by-method)
5. [Conventions that keep the image right side out](#5-conventions-that-keep-the-image-right-side-out)
6. [Where to look when something is wrong](#6-where-to-look-when-something-is-wrong)
7. [Who owns what, and what dies when](#7-who-owns-what-and-what-dies-when)

---

## 0. The interface by how often it is called

**24 methods across four interfaces**: `Backend` 16, `Frame` 5, `Pass` 2,
`Compute` 1. The nesting is the ordering rule — a `Pass` value cannot exist
outside `Frame.Pass`, so "no copy inside a render pass" and "no dispatch inside
one" are compile errors rather than runtime guards.

```go
Backend.Frame(func(f Frame) {
    f.Pass(spec, func(p Pass) { p.Draw(...) })
    f.Copy(...)                       // legal here, not inside the closure above
    f.Compute(spec, func(c Compute) { c.Dispatch(...) })
})
```

### Once, at startup — 3 methods

| Method | What it does |
| --- | --- |
| `Init(window, Request)` | instance, device, allocator, swapchain, frames, descriptors, defaults |
| `Caps()` | what the device can do, and which of `Request`'s features it granted |
| `Shutdown()` | waits idle, destroys everything in reverse order |

### Once per resource, at load time — 7 methods

| Method | What it does |
| --- | --- |
| `CreateImage(ImageSpec)` | one image: sampled, storage, attachment, or any mix |
| `CreateView(ImageHandle, ViewSpec)` | one slice and one aspect of it |
| `UpdateImage(ImageHandle, ImageData)` | CPU pixels in, whole image or a region |
| `CreateBuffer(BufferSpec)` | a buffer plus its device address |
| `CreateMesh(MeshSpec)` | a shared vertex buffer plus this face group's indices |
| `CreateSampler(SamplerSpec)` | filtering, wrapping, border, comparison |
| `CreatePipeline(PipelineSpec)` | shaders, vertex layout, raster, depth, blend, formats |

### On demand, rarely — 4 methods

`UpdateBuffer`, `ReadBuffer` (stalls), `Destroy`, `ReloadPipelines`.

### Once per image, then never again — 1 method

`Slot(Handle)` allocates the shader-visible index on first call and writes the
descriptor. The caller stores the number in its own uniform block; nothing in
the backend ever reads that block back.

### Once per frame — 1 method

`Frame(record)`.

### Inside a frame — 5 methods

`Upload` (per pass and per draw), `Pass`, `Compute`, `Copy`, `Clear`.

### Inside a pass — 3 methods

`Pass.Viewport` (once per shadow tile), `Pass.Draw` (~15 a frame in the showcase,
one per face group per pass), `Compute.Dispatch`.

### What this table makes obvious

- **The backend does not know the word "shadow"**, or bloom, or volumetric. It
  knows images, buffers, pipelines, passes and dispatches. The shadow atlas is
  two images and two passes that `scene/` opens.
- **Uniforms are opaque.** `Upload` memcpys bytes and returns a device address;
  `DrawCall.Push` carries four of those addresses positionally. What each slot
  means is declared in `shaders/slang/common.slang` and filled in `scene/`.
- **State that used to be immediate is baked.** There is no `SetCullMode` or
  `SetDepthCompare`: cull, winding, depth compare, depth write and blend are
  `PipelineSpec` fields, so a pipeline is a value you can compare rather than an
  order-dependent sequence of calls.
- **Barriers are not in the interface at all.** Every operation declares what it
  is about to do with a resource; the backend remembers the last use and emits
  the transition. `vulkan/barrier.go` is that whole table, ~60 lines.

---

## 1. The layers

```
main.go            builds an App, loads a Scene, builds an ECS World
core/              NewApp (window + backend), App.Run (the frame loop), the UI overlay
scene/ ecs/        meshes, lights, camera, skybox, materials, pipelines, physics entities
input/ physics/    plain Go, zero graphics calls
renderer/          Backend / Frame / Pass / Compute, opaque handles, the uniform blocks
vulkan/            the only package that may import vk.*
```

`vulkan/` is organised by concept, so a question has one file to read:

| File | What is in it |
| --- | --- |
| `backend.go` | the struct, `Init`, `Shutdown`, `Caps`, device and instance setup |
| `frame.go` | `Frame`/`Pass`/`Compute`, the arena, copies, clears, capture labels |
| `barrier.go` | the `use` enum and the one table that turns a use change into a barrier |
| `slots.go` | the descriptor set, `Slot`, `Destroy`, the retire queue and the slot free list |
| `pipeline.go` | `CreatePipeline`, `ReloadPipelines`, the shader-module cache |
| `image.go` | images, views, uploads |
| `buffer.go` | buffers, meshes, readback |
| `convert.go` | every engine enum translated into Vulkan's, plus `CreateSampler` |
| `swapchain.go` | the swapchain and the two images sized to it |

---

## 2. Startup, in order

1. `settings.Load` reads the TOML. Everything after this depends on it, which is
   why it runs before the window exists.
2. `core.NewApp` creates the backend object, then GLFW's window with
   `ClientAPI = NoAPI`, then wires the input callbacks.
3. `Backend.Init(window, Request)`:
   instance (validation layers when `[debug] validation` is set, and
   `VK_EXT_debug_utils` when the loader has it) → the label entry points →
   physical device, queue family, logical device →
   VMA allocator → sample count → swapchain, depth and MSAA images →
   command pool → per-frame data (command buffer, fence, semaphore, 2 MiB mapped
   arena) → the default sampler → the descriptor set →
   the global pipeline layout → the white pixel and black cube → `Caps`.
4. `scene.NewScene` parses the XML, uploads each mesh's vertex buffer, index
   buffers and textures, creates the two shadow-atlas images and the skybox
   cubemap, and calls `Slot` on each so their descriptors are written once.
5. `scene.NewPipelines` builds the five graphics pipelines (`forward`, `skybox`,
   `prepass`, `depth`, `depth_point`); `core.newOverlay` builds the sixth.
6. `App.Run` enters the loop.

**The validation layer has no default output path.** It reports through a
`VkDebugUtilsMessengerEXT` or through its own logger, and the engine registers no
messenger — so `[debug] validation = true` alone loads the layer and says
nothing. Point `VK_LAYER_SETTINGS_PATH` at a file setting
`khronos_validation.debug_action = VK_DBG_LAYER_ACTION_LOG_MSG` and
`khronos_validation.log_filename = stdout` (CLAUDE.md carries the full recipe);
that needs no code in the engine.

---

## 3. The frame loop (`core/App.Run`)

```
world.Update                      physics and ECS, fixed 1/60 step
s.UpdateMeshes                    re-upload the vertices a move dirtied
input                             camera, unless [debug] lockCamera

Backend.Frame(func(f) {
    s.UpdateShadows               allocate tiles, decide the bake queues, build the records
    s.FillFrameUniforms           camera, lights, skybox slot
    f.Upload(&frameUniforms)      once — the prepass and the forward pass share the address
    f.Upload(shadowTiles)         once — a variable-length array, so it is a pointer

    s.BakeShadows                 the static atlas pass, when allocation moved
                                  a Copy per dirty dynamic tile
                                  the dynamic atlas pass
    s.RunDepthPrepass             depth only, no colour attachment

    f.Pass("main", …) {           the only pass that clears colour
        s.RenderSkybox            its own uploaded block, view translation stripped
        s.RenderScene             EQUAL against the depth the prepass left
        overlay.draw              a fullscreen quad over the finished scene
    }
})
```

**A settled scene runs neither bake pass**, which is what the `bakes:` counter
beside the FPS is for. Non-zero after settling means the static/dynamic split
broke.

**One `Upload` of `FrameUniforms` serves both depth passes on the screen.** The
prepass and the forward pass are handed the same address, so they read the same
bytes rather than two independently rebuilt copies of them — which matters
because `EQUAL` rejects a difference in the last bit and shows it as speckle.
`prepass.slang` must still combine `projection`, `view` and `model` in exactly
the order `forward.slang`'s `vsMain` does.

**The order inside the frame is a data dependency, not a convention.**
`UpdateShadows` decides each light's `ShadowIndex`, which `FillFrameUniforms`
copies out; the bake walks the same tiles; the main pass declares both atlases in
`PassSpec.Reads`, which is what transitions them out of the layout the bake left
them in.

---

## 4. The contract, method by method

### 4.1 Lifecycle

`Init(window, Request) error` brings up the whole device stack. `Request` names
optional features; `Caps().Features` reports what was actually granted, which is
not always what was asked for, and that is the fork a caller branches on.

`Caps()` also carries `MaxAnisotropy`, the supported `SampleCounts`,
`BackbufferSamples` (what a pipeline drawn on the screen has to match) and
`Formats(Format) bool`, which probes the device rather than assuming.

`Shutdown()` waits idle, ages the retire queue out, then destroys everything in
reverse creation order.

### 4.2 Frames and passes

`Frame(record func(Frame))` waits on this slot's fence, acquires a swapchain
image, resets the arena, records the image uploads staged during the previous
frame, runs the closure, transitions the swapchain image to present, submits and
presents.

`Frame.Pass(PassSpec, func(Pass))` transitions everything the spec names,
opens `CmdBeginRendering`, sets the full-target viewport, runs the closure and
closes it. `PassSpec` carries:

- `Color []Attachment` — plural, so a G-buffer or a velocity target needs no new
  method. `Depth *Attachment` is nil for a colour-only post pass.
- `Reads []Handle` — the images and buffers the pass samples. This is the one
  thing a pass cannot learn from its own attachments, and leaving it out is a
  missing barrier.
- `Layers` — 6 renders a cube probe in one pass.
- `FlipY` — the negative-height viewport (§5).
- `Name` — the `VK_EXT_debug_utils` label a capture groups by (§4.9).

An `Attachment` clears when `Clear` is non-nil and loads otherwise, which is how
the main pass keeps the depth the prepass stored. `Resolve` names where a
multisampled attachment resolves to; the reserved `Backbuffer` view fills it in
automatically when the backend multisamples, since the caller does not know
whether it does.

`Frame.Compute(ComputeSpec, func(Compute))` is the asymmetry, and it is
deliberate: a dispatch reaches its resources through descriptors and device
addresses, which the backend cannot inspect, so `ComputeSpec.Reads` and `.Writes`
name them. Getting it wrong is a missing barrier, which validation catches — so
`validation = true` is the gate on anything that adds compute.

### 4.3 Uniforms — scalar layout and buffer device address

`Frame.Upload(data any) Address` memcpys a block into this frame's arena and
returns its device address. It takes a pointer to a value or a slice, because
those are the two things with an address Go will hand out.

```
FrameUniforms   4760 bytes   camera + 64 lights + the skybox slot   once per pass
BakeUniforms      80 bytes   the one tile a depth pass is baking    once per tile
ShadowTile        96 bytes   one shadow tile                        an array, once per frame
DrawUniforms     128 bytes   model matrix + material                once per draw
```

They mirror `shaders/slang/common.slang` field for field. That works because
Slang compiles with `-fvk-use-scalar-layout` and scalar layout is exactly Go's
packing for `float32`/`int32` structs — so **keeping the field order in step is
the whole requirement**. Use only `float32`, `int32`, arrays of those and
`mgl32` matrices; anything with a wider alignment breaks the correspondence.

The guard is an `init()` size panic in `renderer/uniforms.go`. It catches a
member added, removed or resized — it cannot catch two members swapped, which
leaves the size identical and renders silent garbage. To check by hand:

```sh
spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate
```

`ScalarBlockLayout` is load-bearing: `LightData` is 72 bytes, so `lights[]` has a
non-16-aligned stride that the standard layout rules reject, and `spirv-val`
fails on these modules unless given `--scalar-block-layout`.

**The arena is frame-scoped.** It resets at the top of every frame, so an address
kept across frames points at another frame's data — a GPU fault or silent
garbage, never a compile error. Blocks are 64-byte aligned, and an overflow
**panics**: an overflowed frame is already wrong, and the old behaviour of
wrapping to offset 0 made it wrong silently.

Splitting the per-tile block out of `FrameUniforms` is what made 2 MiB generous.
The bake used to republish all 4848 bytes per tile when only three fields
changed, so a full 337-slot atlas cost ~1.6 MiB a frame; the same atlas now costs
~40 KiB.

### 4.4 Push constants

One 32-byte range, four device addresses, positional:

```
0  frame     FrameUniforms*     1  draw   DrawUniforms*
2  records   ShadowRecord*      3  bake   BakeUniforms*
```

`scene.PushFrame` … `scene.PushBake` are the only names that give them meaning;
the backend pushes four opaque words. A shader dereferences only the slots it
declares — the main pass leaves `bake` unset, a depth pass leaves `frame` and
`records` unset.

### 4.5 Images, views and slots

`CreateImage(ImageSpec)` describes an image by what it is — size, format, usage,
kind, mips, samples — never by what it is for. `CreateView` narrows it to one
mip, one slice and one aspect, which is what makes a mip chain or a cube face
addressable as an attachment.

`Slot(Handle) uint32` is the whole of the handle-to-shader translation. One
descriptor set, built at `Init`:

| Binding | Contents |
| --- | --- |
| 0 | bindless sampled 2D, 256 entries. Slot 0 is the white pixel, which an unset texture falls back to |
| 1 | bindless cubemaps, 64 entries. Slot 0 is a black dummy |
| 2 | bindless storage images, 64 entries |
| 3 | 4 dedicated sampled 2D descriptors, indexed in the shader by a **literal** |

Binding 3 exists for one measured reason: some drivers re-fetch a dynamically
indexed bindless descriptor on every tap, and a PCF kernel taps the shadow atlas
nine times per fragment — going bindless there cost ~1.7x the frame time. An
image asks for it with `ImageSpec.Hot` and names which of the four with
`HotSlot`, so the two sides agree on a number rather than on a creation order.
The backend still never says "shadow"; `scene/shadowatlas.go` puts the static
atlas in hot slot 0 and the dynamic one in 1, and `ShadowRecord.Flags` bit 0
picks between them.

**Slots are reclaimable, and only through the retire queue.** `Destroy` retires
the image; `drainRetired` frees it and gives the slot back once `framesInFlight`
further frames have begun. Handing the slot back at `Destroy` would let a frame
still in flight sample a descriptor that now points at something else — silent
wrong pixels, not a validation error.

### 4.6 Pipelines

`CreatePipeline(PipelineSpec)` bakes shaders, vertex layout, cull, winding, depth
compare, depth write, blend, attachment formats and sample count into one object.
`FormatBackbuffer` and `FormatBackbufferDepth` let a caller declare the screen's
formats without knowing what the swapchain picked.

Only the viewport and scissor stay dynamic state. The lazy
`pipelines[pass][layout]` table is gone, and with it the inference that decided
winding, blending and sample count from what a target looked like.

`ReloadPipelines()` drops the shader-module cache and rebuilds every live
pipeline from its stored spec, which is shader hot-reload.

### 4.7 Buffers, meshes and readback

`CreateBuffer` returns the handle and the device address together, because that
is how a shader reaches one here. Every buffer is address-capable, so there is no
second kind. `LocationHost` is persistently mapped and updated by memcpy;
`LocationDevice` goes through a staging copy.

`Location` and `Usage` answer different questions — where it lives, so whether the
CPU can reach it, against which commands may name it. `cheatsheets/VULKAN.md`
has the general model. Two things are specific to here:

- **Nothing sets `LocationDevice`.** `LocationHost` is also the zero value, so every
  buffer in the engine is host-visible and mapped, and the staging branches in
  `UpdateBuffer` and `CreateBuffer` are written but never taken.
- **`BufferCopyDst` is overloaded**: `vulkan/buffer.go:58` reads it as "the CPU
  will read this back" and switches VMA from `HostAccessSequentialWrite` to
  `HostAccessRandom`, i.e. cached rather than write-combined. That is a
  performance choice, not the flag's meaning, and it is a proxy that only holds
  for the screenshot buffer — a device-written buffer the CPU never reads would
  get cached memory it does not need. A `Readback` flag would say it properly.

`CreateMesh` shares vertex buffers: a multi-material OBJ is several meshes over
one buffer, so destroying a mesh frees its index buffer only. The attribute
layout is on the pipeline, where the hardware wants it; the mesh carries a
stride so a non-indexed draw can derive its vertex count.

`ReadBuffer` **stalls** — it waits on the frames in flight before mapping.
Correct for a screenshot or an image test, wrong in a frame loop. `main.go`'s
`-screenshot` flag is that path: `Frame.Copy` from `renderer.BackbufferImage` into
a host buffer, then `ReadBuffer`, then a PNG.

### 4.8 Barriers

The backend tracks one `use` per image and per buffer, and every operation
declares what it is about to do. `vulkan/barrier.go` is one table:

| Use | Layout | Stage | Access |
| --- | --- | --- | --- |
| `useNone` | UNDEFINED | NONE | 0 |
| `useSampled` | SHADER_READ_ONLY | FRAGMENT\|COMPUTE | SHADER_SAMPLED_READ |
| `useShaderRead` | — | VERTEX\|FRAGMENT\|COMPUTE | SHADER_READ\|STORAGE_READ |
| `useColorAttach` | COLOR_ATTACHMENT | COLOR_ATTACHMENT_OUTPUT | COLOR_ATTACHMENT_WRITE |
| `useDepthAttach` | DEPTH_ATTACHMENT | EARLY\|LATE_FRAGMENT_TESTS | DEPTH_STENCIL_ATTACHMENT_WRITE |
| `useCopySrc` / `useCopyDst` | TRANSFER_SRC/DST | ALL_TRANSFER | TRANSFER_READ/WRITE |
| `useStorage` | GENERAL | COMPUTE | SHADER_STORAGE_READ\|WRITE |
| `useIndirect` | — | DRAW_INDIRECT | INDIRECT_COMMAND_READ |
| `usePresent` | PRESENT_SRC | NONE | 0 |

The layout column applies to images only; a buffer transition emits stage and
access masks alone, which is the barrier kind the old `CmdPipelineBarrier2`
signature could not express and why the `go-vulkan` batch that rewrote it exists.

**A same-use transition still emits a barrier when the use writes.** Two
consecutive reads need nothing; two consecutive writes are a hazard. That is what
puts a barrier between the depth prepass and the main pass, which both leave the
backbuffer depth in `useDepthAttach`.

Conservative by design: one barrier per transition, no batching, no split
barriers, at roughly ten transitions a frame.

### 4.9 Timing and labels

The engine writes no GPU timestamps: RenderDoc is the profiler, and the query
pool that fed `Backend.Timings` was deleted rather than carried unused.

`PassSpec.Name` and `ComputeSpec.Name` *are* `VK_EXT_debug_utils` labels —
`beginLabel`/`endLabel` in `vulkan/frame.go` bracket `CmdBeginRendering` and the
compute closure — which is what groups a capture by pass. Both are conditioned on
the same `name != ""`, so an unnamed pass opens no region and the stack cannot
unbalance. The extension is optional: `createInstance` enumerates the loader's
extensions and enables it only when present, leaving `hasLabels` false and every
label call a no-op otherwise. Object names are not set, so images, buffers and
pipelines still show as raw handles; a `Name` reaches those only as an error and
`fatal` string.

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

**One place decides the flip.** `PassSpec.FlipY` is the whole of it: the screen
passes set it, the atlas passes do not, and `vkPass.Viewport` reads it for both
the full-target viewport a pass opens with and every rect narrowed inside it, so
a tile inherits its pass's handedness rather than restating it. A pipeline
declares the winding that follows in `PipelineSpec.FrontFace`.

**A viewport is set by `Frame.Pass`, or narrowed by `Pass.Viewport` within that
pass, and by nothing else.** This is the amendment to the "clears and viewports
live inside a pass" invariant, and it exists for one case: an
atlas target holds many independent shadow tiles, and baking each through a pass
of its own would mean one `CmdBeginRendering` and one layout transition per tile.
The scissor is set alongside the viewport every time — the viewport transforms
clip space, but only the scissor keeps a clear or an out-of-range primitive off
the neighbouring tiles.

**Depth range.** The projections built in `scene/` are the OpenGL convention,
giving clip z in `[-w, w]`, while Vulkan clips to `[0, w]`. Every vertex stage
therefore calls `TO_VK_DEPTH` from `common.slang`. Changing the projections
instead would remove the macro — a cleanup, not a bug.

**MSAA is a backbuffer-only property, and the backbuffer is the backend's.**
`settings.MSAASamples` (1 = off) is read once at `Init`. The backend allocates a
multisampled colour image plus a matching multisampled depth image; a pass that
attaches the reserved `Backbuffer` view draws into the multisampled one and
resolves into the swapchain image with `ResolveModeAverage`, which the caller
never has to know. Offscreen images stay single-sampled unless their `ImageSpec`
asks otherwise — a later pass has to *sample* them, and these shaders cannot read
a multisampled texture. A pipeline whose sample count disagrees with its pass's
attachments is invalid, so a pipeline drawn on the screen takes
`Caps().BackbufferSamples` and everything else takes 1.

The reason the two backbuffer images are the backend's rather than the caller's
is that only the backend sees a resize: `renderer.Backbuffer` and
`renderer.BackbufferDepth` are reserved views, and `recreateSwapchain` rebuilds
what they point at without anything above `renderer/` noticing.

**Outside a light's frustum reads unshadowed — and the sampler no longer says
so.** `BorderColor = OpaqueWhiteFloat` gave that free while a shadow map was a
whole texture. Inside an atlas a tile's neighbours _are_ what lies past its edge,
so `shadowLookup` tests the tile-local uv (and `z > 1`) explicitly and returns 0
before it samples, while every PCF tap is clamped to the tile inset by one texel.
Drop either and a fragment outside one light's frustum picks up another light's
shadow. The border colour is now only a backstop.

**The uniform struct is the contract.** `renderer/uniforms.go` has an `init`
that panics if `LightData` stops being 72 bytes, `FrameUniforms` 4760,
`BakeUniforms` 80, `ShadowTile` 96 or `DrawUniforms` 128. Field _order_ is what
has to match, and the size panic does not check order — see §4.3 for how to
verify it.

---

## 6. Where to look when something is wrong

| Symptom                                                               | Look at                                                                                                                                                                    |
| --------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The image is mirrored, or culled inside-out                           | `PassSpec.FlipY` at the pass site, `PipelineSpec.FrontFace` at the pipeline site (§5)                                                                                      |
| Garbage uniforms after editing `common.slang`                         | `spirv-dis shaders/vk/forward.frag.spv \| grep OpMemberDecorate` against `unsafe.Offsetof`, in order — the `init()` size panic cannot see a swap (§4.3)                    |
| `spirv-val` rejects every module                                      | Missing `--scalar-block-layout`; `LightData`'s 72-byte stride is legal only under it (§4.3)                                                                                |
| Validation complains about image layouts                              | `vulkan/barrier.go`, then whichever spec failed to declare the resource: `PassSpec.Reads`, `ComputeSpec.Reads`/`.Writes` (§4.8)                                            |
| A resource is destroyed while in use                                  | `waitAllFrames`, `retire`, `drainRetired` (§7)                                                                                                                             |
| Shadows missing on one light                                          | `shadowAtlas.allocate` — its score fell under the last tier, or every pool was full. Its `shadowIndex` is -1; the atlas in a RenderDoc capture shows which tiles were baked |
| A shadow pops coarse/sharp as the camera moves                        | `nextTierThreshold` in `scene/shadowatlas.go` — the 20% band is what stops a boundary score changing a light's ceiling every frame                                          |
| Two shadows flicker against each other, neither camera nor light moving | `slotStickiness` — they are trading the last slot of a contended pool, each changing tile size or position frame to frame                                                  |
| A whole slot size sits idle in the atlas while lights degrade          | the scene's light mix does not match `slotLayout`. Phase 1b should have re-offered the spare slots; if it did not, that is the bug. Otherwise retune the layout             |
| The whole scene is near black, and bright with `[debug] noShadows` | Every light is self-shadowing. A single-sided plane baked into its own shadow map shades against itself, and acne over the whole plane reads as a scene with no lights. Set `<castsShadow>false</castsShadow>` on it — `Mesh.CastsShadow`, checked in `BakeShadows` |
| A lit disc under a round object, its shadow starting a diameter away | Peter-panning. The bake is culling front faces somewhere, so the *far* surface of a closed caster is what landed in the map. `BakeShadows` must leave the cull mode at `CullBack` |
| A light is bright but casts nothing you can see                       | Usually placement, not code. A light needs to be well above its caster and off to one side, or the shadow lands where no visible ground catches it. Look at its tile in a capture: an empty tile means the bake saw nothing, a full tile means the shadow is off-screen |
| Shadows from the wrong light, or a hairline crack at a cube-face edge | `forward.slang` `shadowLookup` — the tap clamp and the `+2 texel` FOV widening in `scene/shadowatlas.go` `cubeFaceFov` are what prevent each (`tmp/LIGHTING_PLAN.md` §4.2) |
| A point shadow lands on the wrong face                                | `cubeFaceDirs` (`scene/shadowatlas.go`) and `cubeFace()` (`forward.slang`) are two lists that must agree                                                                    |
| UI overlay lags by a frame                                            | Expected: `UpdateImage` called inside a pass stages, the next frame's start copies                                                                                         |
| Nothing starts                                                        | `./build_shaders.sh` — the generated shaders are git-ignored                                                                                                               |
| Pipeline creation fails after a pass change                           | Sample count or attachment formats disagreeing with the pass (§5, MSAA)                                                                                                    |

Set `[debug] validation = true` while developing, with the `VK_LAYER_SETTINGS_PATH`
file of §2 — the engine registers no messenger, so without it the layer is loaded
and silent.

`go run . -screenshot out.png` writes one PNG of the rendered frame and quits,
which is the other way to look at what changed. It captures frame 90, late enough
for the physics and the shadow allocator to have settled, so two runs photograph
the same scene.

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
    │   ├── arena (2 MiB, mapped) VmaDestroyBuffer
    │   └── queryPool             DestroyQueryPool
    │
    ├── DescriptorPool                              DestroyDescriptorPool
    │   └── descriptorSet         freed with the pool
    ├── DescriptorSetLayout                         DestroyDescriptorSetLayout
    ├── PipelineLayout                              DestroyPipelineLayout
    │
    └── resource tables ──────── grow at load time, indexed by handle
        ├── images[]    image + whole-image view (+ staging, when the CPU rewrites it)
        ├── views[]     one mip/slice/aspect of an image
        ├── buffers[]   VmaCreateBuffer, host or device
        ├── meshes[]    index buffer (the vertex buffer is shared, not owned)
        ├── pipelines[] one VkPipeline plus the spec it was built from
        ├── samplers[]  DestroySampler; index 0 is the default repeat sampler
        └── modules{}   shader modules, cached by "<set>.<stage>"
```

`renderer.BackbufferImage` and the two reserved views are not in these tables:
they name whichever swapchain image the frame acquired, plus the depth and MSAA
images, all of which belong to the swapchain-sized class above.

### Five lifetime classes

| Class                        | Objects                                                                                                   | Created                              | Destroyed                                                    |
| ---------------------------- | --------------------------------------------------------------------------------------------------------- | ------------------------------------ | ------------------------------------------------------------ |
| **Permanent**                | instance, surface, device, allocator, command pool, descriptor pool/set/layout, pipeline layout | `Init`, once                  | `Shutdown`, reverse order                                    |
| **Swapchain-sized**          | swapchain, image views, render semaphores, depth image + view, MSAA colour image + view                   | `createSwapchain`                    | `destroySwapchain` — **also on every resize**                |
| **Per frame in flight** (×2) | command buffer, fence, acquire semaphore, uniform arena                                       | `createFrameData`                    | `Shutdown`                                                   |
| **Per resource**             | shader modules, pipelines, images, views, buffers, meshes, samplers                                       | load time, on demand                 | `Destroy` (which retires) or `Shutdown`                      |
| **Retired**                  | anything `Destroy` touched, and staging buffers replaced mid-frame                                        | `retire`                             | `drainRetired`, once `framesInFlight + 1` frames have passed |

### The three rules that make it safe

1. **Nothing is destroyed while the GPU might still read it.** `Shutdown` opens
   with `DeviceWaitIdle`. `Destroy` never waits: it retires. The paths that do
   have to wait — `UpdateBuffer` on a mapped buffer, `ReadBuffer` — call
   `waitAllFrames`, which deliberately skips the frame being recorded, since that
   fence was reset at the frame's start and can only be signalled at its end.

2. **`Destroy` defers, and the descriptor slot goes back with it.** The old
   objects go on the `retired` list tagged with `frameCounter`, and
   `drainRetired` frees them — and only then returns the bindless slot to the
   free list — once no in-flight frame can reference them. Handing the slot back
   any earlier is silent wrong pixels rather than a validation error. The UI
   overlay's canvas takes this path on every window resize.

3. **Resize is a partial teardown.** `recreateSwapchain` blocks while minimised
   (a zero-sized surface has no valid swapchain), waits idle, then destroys and
   rebuilds exactly the swapchain-sized class. Everything else survives. It is
   triggered by `ErrOutOfDateKHR` from either `AcquireNextImageKHR` or
   `QueuePresentKHR` — that error _is_ how a resize reaches a Vulkan app.

### Two index spaces that are easy to confuse

`frameIndex` cycles `0..framesInFlight-1` and selects the command buffer, fence,
acquire semaphore and arena. `imageIndex` comes back from `AcquireNextImageKHR`
and selects the swapchain image, its view and its render semaphore. They are not
interchangeable and the swapchain may hold a different number of images than
there are frames in flight — which is exactly why `renderSems` is sized per
image while `acquireSem` lives per frame.
