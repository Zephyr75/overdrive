# Backend decision — Vulkan only, abstraction kept

**Status: decided. §5.1–5.4 and §5.6 executed 2026-08-05; §5.5 and §9 items 3 onward still open.**

Why OpenGL goes, why the `Backend` interface stays anyway, what the interface
must grow to express the rendering ideas this engine exists for, and what that
costs. Supersedes the 2026-08-04 decision to keep both backends; §2 records what
changed and why.

Scope: the strategic choice — which API, what the abstraction is for once there
is only one, and the ordered work to get there.

Not here: the `Backend` contract as it stands today (`../ENGINE_FLOW.md` §0), the
lighting work this unblocks (`LIGHTING_PLAN.md`, `LIGHTING_IMPL.md`), ray
tracing theory (`../cheatsheets/RAYTRACING.md`).

---

## 0. Table of contents

1. [The decision](#1-the-decision)
2. [What changed since 2026-08-04](#2-what-changed-since-2026-08-04)
3. [The backend is already modern](#3-the-backend-is-already-modern)
4. [Why the abstraction survives one backend](#4-why-the-abstraction-survives-one-backend)
5. [Removing OpenGL](#5-removing-opengl) — §5.5 is the legacy still outstanding
6. [What the interface cannot express today](#6-what-the-interface-cannot-express-today)
7. [`go-vulkan` is the blocker](#7-go-vulkan-is-the-blocker)
8. [Ray tracing](#8-ray-tracing)
9. [Work items](#9-work-items)
10. [Debts taken deliberately](#10-debts-taken-deliberately)
11. [Open questions](#11-open-questions)

---

## 1. The decision

**Vulkan is the only backend. The `renderer` abstraction stays. A second backend
— WebGPU or otherwise — is a possibility the structure leaves open, not a
constraint any design decision is made against.**

The engine's purpose is unchanged: a substrate for trying rendering ideas —
shadows, custom material shaders for glass and water, reflection probes,
volumetrics, PBR, custom tracing — where successful experiments graduate into
shipped games. What changed is the reading of what gates that. It is not API
choice. It is **interface coverage**: how often a new idea forces an edit to
`renderer/` and both backends underneath it rather than a new shader and a new
pass.

One backend halves the cost of every interface extension. That is the whole
argument, and it points the opposite way from the previous decision because the
work ahead is almost entirely interface extension.

**The abstraction is not kept for portability.** It is kept for invariant 1 —
nothing above `renderer/` imports a graphics API — which is what keeps `scene/`,
`ecs/`, `core/`, `input/` and `physics/` readable and testable without a GPU.
That the same boundary would also accept a second backend later is a side
benefit, deliberately not paid for in advance (§10).

## 2. What changed since 2026-08-04

The previous decision was *keep both, bump OpenGL 4.1 → 4.6*. Its reasoning was
sound on its own terms and is preserved here where it still holds. What it got
wrong:

- **It priced OpenGL at 1093 lines.** True for `opengl/`, but the real cost is
  the tax on every future interface method: two implementations, two shader
  outputs, a parity contract to police, and the std140 discipline (§5.3)
  constraining the uniform structs. The engine's next phase is a long run of new
  interface methods — compute, pipelines, passes, storage images. That tax
  compounds exactly where the work is.
- **It valued OpenGL as a reference implementation.** Real, but it buys
  debugging speed on a class of bug — mirrored, inside-out, garbage uniforms —
  that mostly arises *from having two backends with different conventions*
  (`../ENGINE_FLOW.md` §5). Validation layers and RenderDoc cover the rest.
- **Its parity contract (old §6) constrained the design.** "Every feature runs
  on both backends" and "any new technique must be expressible in compute
  before it ships" ruled out hardware ray tracing as a first-class path and made
  mesh shaders permanently out of scope. Both are now available.

Void: the parity contract, the one-abstraction-two-lowerings rule, "design to
Vulkan's grain, build in OpenGL's order", the GL 4.6 bump, and wiring
`Supports()` as a cross-backend hint.

Still standing: ray tracing (§8 here), the software-BVH argument, and the
observation that prototyping speed comes from interface coverage rather than
from the API.

## 3. The backend is already modern

`vulkan/` was audited against *How to Vulkan in 2026* (howtovulkan.com,
Sascha Willems) on 2026-08-05. It already uses every practice that tutorial
teaches:

| practice | where |
|---|---|
| VMA for all allocation | `vulkan/backend.go:309` |
| Buffer device address | `vulkan/backend.go:415,462` — uniform ring, pushed as two addresses |
| Descriptor indexing (bindless) | `vulkan/backend.go:416` |
| Dynamic rendering, no render pass objects | `vulkan/backend.go:422,744` |
| Synchronization2 | `vulkan/backend.go:423` — `CmdPipelineBarrier2`, `QueueSubmit2` |
| Slang → SPIR-V | `build_shaders.sh` |

So "implement the backend the way howtovulkan.com does" is not migration work.
The stack matches; the remaining differences are that the tutorial loads KTX
textures where the engine loads raw RGBA, and that it is C++. **Treat the site
as the reference for anything new** — compute pipelines, blend state, 3D images
— and the existing code stays consistent with it by construction.

## 4. Why the abstraction survives one backend

Four reasons, in order of weight:

1. **Invariant 1 is the engine's main structural rule.** `scene/`, `ecs/`,
   `core/`, `input/` and `physics/` hold opaque handles and never see a Vulkan
   type. That is what makes `go test ./...` run without a GPU and what keeps the
   Vulkan object graph confined to one package (`../ENGINE_FLOW.md` §7).
2. **It is 269 lines.** `renderer/backend.go` plus `renderer/uniforms.go`. The
   previous decision already concluded this earns its place with a single
   backend, and that conclusion is untouched.
3. **It is where the engine's vocabulary lives.** `RenderTargetSpec`,
   `VertexLayout`, `PipelineSpec` (to come) are how a rendering idea is
   *expressed*. Deleting the layer would not simplify anything — it would scatter
   the vocabulary into `vulkan/`.
4. **It leaves a second backend possible.** Last, and worth nothing in advance.

> The abstraction is a vocabulary and a boundary, not a portability layer. Judge
> every addition to it by whether it lets an idea be written once in `scene/` or
> a pass file — never by whether a hypothetical second backend could implement
> it.

## 5. Removing OpenGL — **done, 2026-08-05**

Mechanical, as predicted. Every site, and what it became:

| file | edit |
|---|---|
| `opengl/` | deleted — 1093 lines including `uniforms_test.go` |
| `core/app.go` | import and `createBackend` switch gone; `NewApp` calls `vulkan.New()` and holds it as a `renderer.Backend` |
| `settings/config.go` | `normaliseBackend` now *rejects* `gl`/`opengl` with an explanation rather than accepting it (§5.2) |
| `main.go` | default `-config` → `configs/vulkan.toml` |
| `configs/opengl.toml` | deleted; `vulkan.toml` is the default and its header says so |
| `build_shaders.sh` | `downgrade()`, `gl_dir`, `tmp_dir` and the second `slangc` invocation gone — 89 → 56 lines. `-DTARGET_VK` dropped too, since the `#ifdef` it fed is gone |
| `shaders/gl/` | dropped from `.gitignore`, directory removed |
| `common.slang` | the dead `#else // OpenGL` branch removed — std140 UBOs, named samplers, the no-op `TO_VK_DEPTH` |
| comments in `vulkan/`, `renderer/`, `core/` | rewritten where they described OpenGL as a live backend rather than as a convention's origin |

Documentation, same pass: `../../CLAUDE.md`, `../../README.md`, `../ENGINE_FLOW.md` (rewritten —
every method table was a two-column comparison), `../ARCHITECTURE.md`,
`../FEATURES.md`, `../TODO.md`, `../README.md`, and staleness banners on
`LIGHTING_PLAN.md` / `LIGHTING_IMPL.md`, whose design was built around the GL 4.1
floor.

Verified: `go build ./...` and `go test ./...` clean, shaders recompile, and the
engine runs with `OVERDRIVE_VK_VALIDATION=1` without a validation message.
`go vet` still reports two pre-existing `possible misuse of unsafe.Pointer` in
`vulkan/backend.go` — the device-address arithmetic, untouched by this.

### 5.1 `createBackend` can go home

It lives in `core/` only to avoid the import cycle `renderer` → backend →
`renderer`. With one backend the indirection has no purpose: `core.NewApp` calls
`vulkan.New()`. Keep the `Backend` interface as the type `App.Backend` holds.

### 5.2 `Supports()` and `Feature`

Old §6 redefined these as cross-backend performance hints. With one backend they
recover their literal meaning: **does this physical device have the extension**.
`FeatureRayTracing` becomes a real query against `VK_KHR_ray_query` availability
(§8), which is a genuine runtime fork — a GTX 1080 has no RT cores. Wire it when
§9 item 15 lands, not before.

### 5.3 The 16-byte cell rule dies — **done, 2026-08-05**

The rule existed solely because OpenGL 4.1 uniform blocks must use std140.
Vulkan compiles with `-fvk-use-scalar-layout` and reads the blocks through a
device-address pointer, so scalar layout — exactly Go's packing for
`float32`/`int32` structs — is what the shader sees regardless.

Removed:

- `LightData.Reserved0/1/2`, three floats that existed only because std140
  rounds a struct's array stride to 16 and 17 fields do not divide by 4.
  **80 → 68 bytes**, and `FrameUniforms` **1280 → 1184** (8 lights × 12 bytes).
- `int4 pointShadowLights` → `int pointShadowLights[MAX_SHADOW_CUBES]`. Same
  size; the vector was a std140 dodge for the 16-byte scalar-array stride.
- The LAYOUT RULE block in `common.slang` and the std140 commentary above
  `FrameUniforms`, replaced by one paragraph: keep the field order in step, use
  only scalars, vectors and matrices.

Field order was **not** changed. Reordering is free now, but it buys nothing and
every reorder is a chance to introduce a silent mismatch.

**Verified against the compiled SPIR-V**, member by member: `spirv-dis` emits
explicit `Offset` decorations, and all 36 members of the three structs matched
`unsafe.Offsetof` across all 11 shader stages.

**No automated guard covers field order.** The `init()` size panic catches a
member added, removed or resized; two members swapped leaves every size and
offset identical. `opengl/uniforms_test.go` used to close that gap and went with
the backend. A replacement reading the SPIR-V is cheap to write if a layout bug
ever costs an afternoon — until then, rebuild and look at the scene.

**One new coupling.** `ScalarBlockLayout` (`vulkan/backend.go`) was already
enabled, but it is now *load-bearing* rather than convenient: at 68 bytes
`LightData` gives `lights[]` a stride that is not 16-aligned, which the standard
layout rules reject. `spirv-val` fails on every module unless given
`--scalar-block-layout`. Before this change the structs satisfied both rule sets;
now only the scalar one.

### 5.4 GL-shaped interface members — **done, 2026-08-05**

Three `Backend` members existed only because the interface was drawn against
OpenGL's model. All removed, no behaviour change:

| removed | why it was there |
|---|---|
| `CreateBuffer`'s `dynamic bool` | GL's `STATIC_DRAW` / `DYNAMIC_DRAW` usage hint. The Vulkan allocation is host-visible either way, and `vulkan/buffer.go` said so in a comment while still taking the parameter |
| `WhiteTexture() TextureHandle` | GL needed a real texture *name* for "no texture". Under bindless, handle 0 *is* slot 0 — the method returned the constant `0` and had **zero callers** |
| `CreateShader`'s `hasGeometry bool` | A GL link-time "which stages to compile" flag. The backend now probes for `<name>.geo.spv`, so a shader set declares its own stages |

The probe is strictly safer than the flag: a caller passing `false` for
`depth_cube` silently lost point shadows, and that failure mode is now gone.
Verified that `depth_cube` is the only set detected as having a geometry stage.

### 5.6 GL-shaped semantics — **done, 2026-08-05**

The members Tier A could not simply delete, because they needed a
Vulkan-shaped replacement rather than removal:

| before | after | why it was GL-shaped |
|---|---|---|
| `SetCullFace(front bool)` | `SetCullMode(CullMode)` — `CullBack`/`CullFront`/`CullNone` | A bool encodes 2 of 3 options. There was no way to ask for two-sided geometry |
| `SetDepthFunc(lequal bool)` | `SetDepthCompare(CompareOp)` — `CompareLess`/`LessEqual`/`Always` | Same. Reverse-Z will add `CompareGreater` here (§5.5) rather than inventing a second bool |
| `LoadTexture(path string)`, `LoadCubemap(faces [6]string)` | `CreateTexture(pixels, w, h)`, `CreateCubemap(faces [6][]byte, w, h)` | The backend opened files and ran `image.Decode`, because GL fed a decoded buffer straight into `TexImage2D`. Decoding now lives in `scene/image.go`, and `vulkan/texture.go` no longer imports `image`, `image/jpeg`, `image/png` or `os` |
| `Draw(shader, mesh, u)` | `BindShader(shader)` + `Draw(mesh, u)` | Passing the shader per draw is `glUseProgram`'s model, and it makes sorting a run of draws by pipeline impossible at the call site |
| `BeginPass(target, w, h, clear)` | `BeginPass(target, clear)` | `w, h` were `glViewport` arguments. A target knows its own extent, so a pass can no longer be handed one that disagrees with its attachments — the backbuffer now takes the swapchain's extent, which is also more correct during a resize |

The bind/draw split is deliberately the shape `PipelineSpec` wants (§6), so
those call sites do not move a second time when pipelines land.

**Bindings removed as a consequence:** `CmdSetFrontFace` and
`DynamicStateFrontFace` in `go-vulkan`. Front face is a property of a pass's
winding convention, baked into the pipeline by `frontFace(pass)`, and was never
set dynamically by anything — engine or demo.

Nothing else in `go-vulkan` was orphaned. Two constants were *revived*:
`CompareOpAlways` and `CullModeNone` are now reachable through the new enums.
An audit of the remaining 36 unreferenced exports found almost all of them
forward-looking — `ImageType3D` (volumetrics), `DescriptorTypeStorageBuffer`
(compute), `PhysicalDeviceTypeDiscreteGPU` (device scoring),
`VertexInputRateInstance` (instancing), `PresentModeMailboxKHR` (vsync modes) —
so they stay.

### 5.5 The GL clip-space convention survives — **not done**

The largest remaining OpenGL legacy, and the one that still costs something
every day. `scene/` builds its projections with `mgl32.Perspective` and
`mgl32.Ortho` (`scene/scene.go:165`, `skybox.go:87`, `light.go:117,129`) — the
GL convention: **y up, z in `[-w, w]`**. Vulkan wants y down and z in `[0, w]`.
Four coupled workarounds bridge the gap:

1. `TO_VK_DEPTH(pos)` called in every vertex stage (`common.slang:132`, used by
   `forward`, `depth`, `depth_cube`) to remap z.
2. A **negative-height viewport** in `BeginPass` to flip y.
3. That flip inverts winding, so `frontFace()` returns CCW for the main pass.
4. Shadow passes need a *positive* viewport, and therefore `FrontFace =
   Clockwise` as a special case.

`ENGINE_FLOW.md` §5 exists largely to document this, and §6's symptom table
leads with the mirrored / inside-out bugs it causes.

**The fix is one line at projection-build time**, the standard Vulkan idiom:

```go
proj := mgl32.Perspective(fov, aspect, near, far)
proj[5] *= -1   // flip Y in the projection instead of the viewport
```

plus a `[0, 1]`-depth projection rather than `[-1, 1]`. Then `TO_VK_DEPTH` is
deleted from three shaders, every viewport is positive, and `frontFace()`
collapses to a constant. It removes more code than it adds.

**And it unlocks reverse-Z.** With `[0, 1]` depth you can swap near and far,
clear depth to `0` instead of `1` (`vulkan/backend.go:747`) and compare
`GREATER` — which redistributes floating-point precision so distant geometry
stops z-fighting. It is nearly free, and it is *impossible* under GL's
`[-1, 1]` convention. Currently blocked on two enum constants: `go-vulkan` binds
only `CompareOpLess`, `LessOrEqual` and `Always`
(`go-vulkan/BINDINGS_GAP.md` §6, "Misc").

Split accordingly: the clip-space fix needs no bindings work and can land on its
own; reverse-Z follows once the compare ops exist.

## 6. What the interface cannot express today

The 25-method `Backend` cannot state any of the target features. Not "would be
slow" — cannot be written. Seven gaps, and they are the real project:

| gap | what it blocks | why blocked today |
|---|---|---|
| **Pipeline objects** | glass, water, any transparent or custom-shaded material | shader is chosen by name at startup and selected with `BindShader`; state is two immediate setters, `SetCullMode` and `SetDepthCompare`. No blend, no depth-write control, no per-material shader |
| **Pass list** | reflection probes, post-processing, deferred, anything multi-pass | the frame is hardcoded in `core/app.go:130-175` — shadows, skybox, forward, UI. A new pass is an edit to `App.Run` |
| **Compute** | volumetrics, probe prefilter, custom tracing, GPU-driven anything | absent from the interface *and* from the bindings (§7) |
| **Per-material parameters** | water needs time and wave state, glass needs IOR and thickness | `DrawUniforms` is a fixed 128-byte struct of Phong-plus-PBR scalars. No room, no extension point |
| **HDR formats** | tonemapping, bloom, any physically-scaled lighting | `go-vulkan` exposes no half-float format (§7) — already recorded in `../FEATURES.md` Part 2 |
| **Storage images, 2D and 3D** | volumetric froxel grids, compute output | no interface method, no bindings |
| **Multiple color attachments** | G-buffer, probe capture, velocity buffers | `BeginPass` takes one target handle |

Two of these carry a design note worth settling before the code:

**Pipelines vs immediate state.** `SetCullMode` and `SetDepthCompare` are
OpenGL-shaped, surviving on Vulkan only because both happen to be dynamic state.
The replacement is a `PipelineSpec` — shader set, vertex layout, cull, depth
compare, depth write, blend, attachment formats — baked at load and selected per
draw. It touches every draw site, so do it while the interface is small.

**Per-material parameters.** Two options: grow `DrawUniforms` with a fixed
superset of every material's fields, or make the material half an opaque byte
slice whose size and meaning belong to the pipeline. The second is the one that
does not need editing every time a new material shader is written — which is the
whole point. It costs a typed-struct guarantee, so the slice should be produced
by a small per-material Go struct that the pipeline records the size of.

## 7. `go-vulkan` — smaller than it looks

Fully inventoried on 2026-08-05: **`go-vulkan/BINDINGS_GAP.md`** lists every
exported function, whether howtovulkan.com's reference program uses it, and what
is missing per feature. The summary:

**The gap is 3 new functions and 1 signature change**, not the 600–1200 lines
first estimated. The rest is enum constants and struct fields.

| needed | status |
|---|---|
| `CreateComputePipeline`, `CmdDispatch` | new — small, no vertex input or blend state to marshal |
| `CmdPipelineBarrier2` → take a `DependencyInfo` with buffer and global barriers, not just image barriers | **the one structural gap.** Without it a compute pass writing a storage buffer cannot be synchronised with the graphics pass reading it |
| `CmdBlitImage` | new — mip generation, which is IBL and probe prefiltering |
| HDR and compressed formats, storage-image and 3D-view enums, compute stage/access flags | ~25 one-line constants |

Three things assumed missing that are **already present**:

- **Blend state is complete.** `PipelineColorBlendAttachmentState` carries
  `BlendEnable` and all six factor/op fields, and `SrcAlpha`/`OneMinusSrcAlpha`
  are already defined. Glass and water need no bindings work at all.
- **Multiple color attachments already work** — every relevant field is a slice.
  A G-buffer is free.
- **Storage buffers need nothing**, provided they are reached by device address
  rather than a descriptor. That is already how the uniform ring works.

So item 3 in §9 is **~2 days, not 3–5**, and it stops being the blocker it
looked like. `go-vulkan` remains the largest maintenance item in the project
(§11.1), but not the critical-path risk.

## 8. Ray tracing

Unchanged from the previous decision except that nothing forces a compute
fallback to ship first. Both paths remain wanted; they answer different
questions.

### 8.1 What is and is not customisable

**The acceleration structure is a black box.** Hand the driver triangles or
AABBs and it builds an opaque BVH. Its layout cannot be inspected, its build
heuristic cannot be chosen, and it cannot be traversed manually.

**The shader stages are fully yours:**

| stage | what you control |
|---|---|
| ray generation | ray origins and directions, sampling, accumulation, the outer loop |
| **intersection** | **your own intersection maths for AABB (procedural) geometry** |
| any-hit | alpha testing, stochastic transparency, early termination |
| closest-hit / miss | shading, recursion, what a hit means |
| callable | dynamic dispatch for material variety |

The intersection shader is the one that surprises people. Fill a BLAS with AABBs
instead of triangles, write your own intersection test, and the hardware BVH
will cull for *any* primitive — SDFs, spheres, voxels, curves, splats. The
structure does spatial culling; you define what a primitive is.

What genuinely cannot be done: change the BVH build, traverse in a chosen order,
keep your own traversal stack, cone- or beam-trace through the structure, or
observe intermediate traversal state.

**Two API flavours.** Ray tracing *pipelines* (`vkCmdTraceRaysKHR`) bring the
full stage machinery plus a shader binding table. Ray *queries* (`rayQueryEXT`)
are inline traversal callable from any shader stage including plain compute — no
SBT, no new pipeline type. **Ray queries are the right entry point**: they drop
into a compute shader that already exists, which is why §9 orders compute first.

### 8.2 Below the ray or above it

- **Below the ray** — inventing traversal schemes, acceleration structures,
  marching strategies, spatial layouts. Hardware RT is the *wrong* tool; its
  entire value is the fixed traversal being replaced. Compute shader plus a
  structure in a storage buffer.
- **Above the ray** — taking "nearest hit along this ray" as a cheap primitive
  and building lighting on it: GI schemes, probe layouts, participating media.
  **Hardware RT, and worth the bindings work.**

### 8.3 Why hardware traversal is ~10× faster, and where it is not

Five mechanisms, roughly in order of contribution:

1. **Fixed-function box and triangle test units.** Traversal is ray-vs-AABB
   tests and pointer chasing; in compute that is ALU plus incoherent loads.
2. **Divergence handling — the big one.** Rays in a warp take different paths at
   different depths. Compute runs SIMD lockstep, so every lane waits for the
   deepest. RT hardware traverses independently and regroups work; NVIDIA's
   Shader Execution Reordering re-sorts rays by hit material before shading.
   This is a scheduling capability, not replicable in a shader.
3. **Stack in dedicated storage.** A compute traversal keeps its stack in
   registers or shared memory, and that pressure crushes occupancy exactly when
   more rays in flight are needed to hide latency.
4. **Driver-chosen node layout**, compressed for the traversal unit's cache
   lines — the reason the structure is opaque in the first place.
5. **Traversal overlaps shading.**

Ballpark on one discrete GPU: a good compute BVH does a few hundred Mrays/s on
incoherent rays; hardware RT reaches low single-digit Grays/s. Call it **10–30×
for incoherent rays against triangle geometry.**

**That gap applies to one workload only:**

| technique | hardware RT advantage |
|---|---|
| SDF ray marching | ~none, no BVH involved |
| Voxel DDA, uniform grids | ~none, regular traversal |
| Screen-space tracing | none, marches the depth buffer |
| Radiance cascades, probe and surfel methods | little, mostly not ray-scene queries |
| **Ray queries against scene triangles** | **10–30×** |

**A software path is still needed.** A shipped game meets players with no RT
cores — GTX 10-series, older Arc, most laptop iGPUs. So the compute BVH is not a
fallback for a missing backend, it is the baseline, and hardware RT accelerates
it where the hardware exists. `Supports(FeatureRayTracing)` is the fork (§5.2).

## 9. Work items

In order. Each is independently useful, and each earlier item makes the next
cheaper. Day figures are solo-developer estimates for the engine work only, not
for the rendering techniques built on top.

| # | item | days | why |
|---|---|---|---|
| 1 | ~~**Delete OpenGL**~~ (§5.1–5.3) | ✅ | Done 2026-08-05, layout cleanup included |
| 2 | ~~**GL-shaped interface members**~~ (§5.4) | ✅ | Done 2026-08-05. `dynamic`, `WhiteTexture`, `hasGeometry` |
| 3 | **Vulkan-native clip space** (§5.5) | 0.5 | Deletes `TO_VK_DEPTH`, the negative viewport, and the shadow-pass winding special case — four workarounds for one convention. Needs no bindings. Do it before item 5, which would otherwise inherit them |
| 4 | **Shader hot-reload** | 1 | Biggest single velocity win, and independent of everything else. Watch `shaders/slang/`, re-run `slangc`, rebuild pipelines, keep the frame running |
| 5 | **`go-vulkan`: formats, barrier rework, compute, storage images, blit** (§7) | 2 | Batches 1–6 of `go-vulkan/BINDINGS_GAP.md` §7. Nothing from item 8 onward can start without it. Add `CompareOpGreater` here, which is all reverse-Z still needs |
| 6 | **`PipelineSpec`, replacing `CreateShader` + the two state setters** (§6) | 2 | Glass and water are pipelines, not shaders. Touches every draw site — do it while the interface is small |
| 7 | **`Pass` interface + pass list in `App.Run`** | 1.5 | Turns "try an idea" from engine surgery into a new file |
| 8 | **Compute in `Backend`** — `Dispatch`, `CreateStorageBuffer`, `CreateStorageImage` (2D and 3D) | 1 | Makes most remaining ideas expressible without touching `vulkan/` |
| 9 | **Reverse-Z** | 0.5 | Free precision once items 3 and 5 have landed: swap near/far, clear depth to 0, compare `GREATER` |
| 10 | **Per-material parameter blob** (§6) | 1 | Water and glass need fields `DrawUniforms` has no room for |
| 11 | **HDR target + tonemap pass** | 1 | First user of items 5, 7 and 8 together; proves the stack |
| 12 | **Reflection probes** | 2 | Cube targets already exist (`RenderTargetSpec{Cube: true}`); needs the pass list and a prefilter compute pass |
| 13 | **Volumetrics** | 2+ | 3D storage image, froxel fill in compute, raymarch composite |
| 14 | **Software BVH + `traceRay`** | — | The §8.3 baseline |
| 15 | **Ray queries** — `VK_KHR_acceleration_structure` + `VK_KHR_ray_query` bindings, BLAS/TLAS lifetimes | — | ~600–1000 lines. Additive, and item 8 is its host |

**Items 1–8 are the substrate and should come before rendering features,
including `LIGHTING_PLAN.md` beyond its Part A.** That is roughly **two weeks**;
items 7–10 add another week and are where the target feature list starts
appearing on screen.

Sketch of items 4, 5 and 6, to be designed properly when they are built:

```go
// A pipeline is the shader set plus the fixed state it is valid under, baked
// once at load. Glass is a pipeline; so is water; so is the existing forward
// shading. Replaces CreateShader, SetCullMode and SetDepthCompare.
type PipelineSpec struct {
	Shader       string
	Layout       VertexLayout
	CullBack     bool
	DepthCompare CompareOp
	DepthWrite   bool
	Blend        BlendMode
	Targets      []TargetFormat
	// Bytes of per-draw material data this pipeline reads, appended to
	// DrawUniforms. Zero for the built-in material struct.
	MaterialSize int
}
CreatePipeline(spec PipelineSpec) (PipelineHandle, error)

// Compute, the substrate for volumetrics, probe prefilter and custom tracing
Dispatch(p PipelineHandle, groupsX, groupsY, groupsZ int)
CreateStorageBuffer(sizeBytes int) BufferHandle
CreateStorageImage(spec RenderTargetSpec) TextureHandle

// A pass owns its resources and its place in the frame, so an experiment is a
// new file rather than an edit to core/app.go
type Pass interface {
	Name() string
	Setup(b Backend) error
	Execute(b Backend, f *FrameContext)
}
```

The existing frame then becomes a slice — shadow, skybox, forward, UI — and an
experiment is `experiments/<name>/pass.go` plugged into it.

## 10. Debts taken deliberately

Three decisions that would cost a future second backend something, taken anyway
because the cost is now and the benefit is hypothetical. **Recorded so that a
later port knows the bill, not so that anything is avoided today.**

| decision | why it is taken | what a second backend would owe |
|---|---|---|
| **GLFW stays in the `Backend` interface** — `ConfigureWindow()`, `Init(*glfw.Window)` | It is convenient and it works. Abstracting the surface buys nothing today | Little for a native target: `wgpu-native` and friends take a native window handle, which GLFW hands out. Real cost only for a browser target, which is not one |
| **Geometry shaders are allowed** — `depth_cube.slang` uses one for point shadow cubes, and more may follow | One pass instead of six for a cube face render, and they are a legitimate tool | WebGPU and Metal have none. Any such shader would need rewriting as N passes or layered instancing. Confined to whichever shaders use it |
| **Scalar layout, no 16-byte cell rule** (§5.3) | Deletes a whole discipline and the padding fields it forces | WGSL's uniform address space enforces 16-byte alignment much like std140. Mitigation is to put the blocks in the storage address space instead, whose rules are relaxed — which is what a bindless design wants anyway |

None of these is load-bearing for a port; all three are local and reversible.
The rule going forward:

> Design for Vulkan. Do not pay for portability in advance. When a second
> backend is actually wanted, re-read this table and pay then.

## 11. Open questions

1. **Vendor `go-vulkan`?** It is a sibling repository behind a `replace`
   directive resolving to `/home/zeph/GitHub/go-vulkan`, it is 3080 lines, and
   §7 puts it squarely on the critical path — the next month's work is as much
   in it as in this repository. Vendoring makes one commit span both; keeping it
   separate keeps it reusable. Decide before item 3 starts.
2. **Fixed superset or opaque blob for material parameters?** §6 argues for the
   blob. Settle it when item 7 is built, since item 4 records the size.
3. **Where the BVH is built.** CPU is simplest; compute is faster and would
   itself be a worthwhile experiment. Start on the CPU.
4. **KTX textures.** howtovulkan.com loads KTX; the engine loads raw RGBA and
   has no compressed-format bindings. Worth adopting when item 3 adds the BC
   formats, since mipmaps and compression arrive together.
5. **Does anything still want a second implementation to diff against?** Old
   §8.2's strongest argument for OpenGL was exactly this. If a class of bug
   turns out to survive validation layers and RenderDoc, revisit — but a debug
   pass or a CPU reference rasteriser is a cheaper answer than a whole backend.
