# INTERFACE_PLAN.md — a technique-complete `Backend`

Extends `BACKEND_DECISION.md` §6 and §9. Where that file says *what the interface
cannot express*, this one says *what replaces it*, method by method.

Scope: the `renderer.Backend` interface and the `go-vulkan` batches it needs.
Not here: why Vulkan only (`BACKEND_DECISION.md` §1), the lighting work
(`LIGHTING_PLAN.md`), or the binding inventory itself
(`go-vulkan/BINDINGS_GAP.md`).

---

## 0. Table of contents

1. [Why](#1-why)
2. [Design decisions](#2-design-decisions)
3. [The interface](#3-the-interface)
4. [Coverage](#4-coverage)
5. [`go-vulkan` batches](#5-go-vulkan-batches)
6. [Rollout](#6-rollout)
7. [Verification](#7-verification)
8. [Costs and open risks](#8-costs-and-open-risks)

---

## 1. Why

Understanding a frame currently means reading `src/vulkan/` (2251 lines).
`renderer.Backend`'s 28 methods describe *when* to call things — `BeginFrame`,
`BeginPass`, `BeginDepthPrepass` — rather than what they do. Ordering rules live
in comments and are enforced at runtime by eleven silent
`if !backend.frameActive { return }` guards.

The goal is **not** portability. There is no second backend planned and the
OpenGL one was deleted on 2026-08-05. The goal is that every technique on the
roadmap is buildable in ordinary Go with no edit to `src/vulkan/`.

Two rules follow:

1. **The backend must not know the word "shadow"** — or "bloom", or "volumetric".
   It knows images, buffers, pipelines, passes and dispatches.
2. **Prefer a method over hardcoding.** A technique that cannot be expressed is
   worse than an interface with a few more entries.

**The method count is not the metric.** The result is 29 methods across four
interfaces against 28 on one today — flat. What changes is that the frame stops
being hardcoded in `core/app.go` and the 34 techniques in §4 become expressible.
Any pressure to hit a smaller number should be resisted; rule 2 wins.

### 1.1 The premise, corrected

Shadow *policy* is already outside `vulkan/`. `scene/shadowatlas.go` is 819 lines
holding the slot layout, tier scores, rank-vs-ceiling, both hysteresis margins,
the static/dynamic split, the bake queue and frustum culling. What leaks is
**~90 lines in six places**, and closing them is a design change, not a move:

| # | leak | where |
|---|---|---|
| 1 | `ShadowTile` type + `BindShadowRecords` — a shadow-named channel in the interface | `renderer/backend.go:137`, `renderer/uniforms.go:43` |
| 2 | six shadow fields inside the shared frame block | `renderer/uniforms.go:56-67` |
| 3 | `bindShadowMaps` + two cached handles + dedicated bindings 2/3 | `vulkan/draw.go:138-157` |
| 4 | sampler inferred as "depth target ⇒ shadow sampler" | `vulkan/texture.go:303-306` |
| 5 | `passShadow2D`/`passShadowCube` deciding winding, blend, sample count | `vulkan/backend.go:118-127`, `shader.go:159-196` |
| 6 | geometry stage in `pushStages` for the point-shadow pass | `vulkan/backend.go:39,404` |

Leaks 1-3 close with opaque uniform upload; 4-6 with explicit `PipelineSpec` and
`SamplerSpec`.

### 1.2 What does not change

Listed because a refactor this wide invites scope creep, and because each of
these is load-bearing:

- **Invariant 1.** Nothing above `renderer/` imports a graphics API, and
  `go test ./...` keeps running without a GPU.
- **The three uniform structs.** `FrameUniforms` (4848), `DrawUniforms` (128),
  `ShadowTile` (96) and the `init()` size guards in `renderer/uniforms.go` stay
  exactly as they are. They stop being *backend* types and become *scene* types —
  the declaration does not move, the knowledge of it does.
- **Scalar layout.** Slang still compiles with `-fvk-use-scalar-layout`, Go
  packing still is Vulkan scalar layout, `spirv-val` still needs
  `--scalar-block-layout`. `Upload` memcpys; it does not reinterpret.
- **Shaders authored once in Slang**, compiled by `build_shaders.sh`.
- **The shadow atlas design** — fixed slot layout, rank picks the slot and the
  tier caps it, both hysteresis margins, the static/dynamic split. This plan
  changes how those tiles reach the GPU, not how they are chosen.

---

## 2. Design decisions

### 2.1 Uniforms become opaque bytes

The backend already treats uniforms as bytes — `writeArena` is a memcpy and
`pushAddresses` is three device addresses. Make that the interface:

```go
Upload(data any) Address   // memcpy into the frame arena, return a device address
Slot(Handle) uint32        // the shader-visible index for an image
```

`BindFrameUniforms` and `BindShadowRecords` disappear. `scene/` uploads its own
`FrameUniforms` and its own `[]ShadowTile` and hands the addresses to draws;
bloom uploads `bloomParams{threshold, knee, radius}` the same way. This is also
§6's per-material parameter blob, for free.

The texture-handle→slot translation at `vulkan/draw.go:83,94-95` moves up: the
caller calls `Slot()` while filling its own struct.

**Free win.** `TODO.md` carries an open item — the shadow bake republishes all
4848 bytes of `FrameUniforms` per tile when only `CurWorldToTile`, `CurLightPos`
and `CurFarPlane` change, so a tile costs 4864 arena bytes instead of ~100. That
overflowed the 1 MiB arena at 259 tiles and is why `arenaSize` is 4 MiB today.
With `Upload`, the per-tile block is just a different struct: a full 337-slot
atlas drops from ~1.6 MiB a frame to ~34 KiB. The item closes as a side effect of
stage 1.

### 2.2 Barriers become resource intent

The backend tracks `lastUse` per image and buffer; every operation declares what
it is about to do. One table, ~60 lines, replacing ~120 lines of hand-written
barriers across twelve call sites and three hand-maintained layout fields
(`targetEntry.layout`, `VKBackend.depthLayout`, the implicit Undefined in
`recordImageUpload`).

```go
type Use int
const (UseNone Use = iota; UseSampled; UseColorAttach; UseDepthAttach
       UseCopySrc; UseCopyDst; UseStorage; UseIndirect; UseAccelBuild; UsePresent)
```

| Use | layout | stage | access |
|---|---|---|---|
| `UseNone` | UNDEFINED | NONE | 0 |
| `UseSampled` | SHADER_READ_ONLY_OPTIMAL | FRAGMENT\|COMPUTE_SHADER | SHADER_SAMPLED_READ |
| `UseColorAttach` | COLOR_ATTACHMENT_OPTIMAL | COLOR_ATTACHMENT_OUTPUT | COLOR_ATTACHMENT_WRITE |
| `UseDepthAttach` | DEPTH_ATTACHMENT_OPTIMAL | EARLY\|LATE_FRAGMENT_TESTS | DEPTH_STENCIL_ATTACHMENT_WRITE |
| `UseCopySrc` / `UseCopyDst` | TRANSFER_SRC/DST_OPTIMAL | COPY | TRANSFER_READ/WRITE |
| `UseStorage` | GENERAL | COMPUTE_SHADER | SHADER_STORAGE_READ\|WRITE |
| `UseIndirect` | — | DRAW_INDIRECT | INDIRECT_COMMAND_READ |
| `UseAccelBuild` | — | ACCELERATION_STRUCTURE_BUILD | AS_READ\|AS_WRITE |
| `UsePresent` | PRESENT_SRC_KHR | NONE | 0 |

**The layout column applies to images only.** A buffer has no layout, so a buffer
transition emits stage and access masks alone — which is exactly the barrier kind
`CmdPipelineBarrier2` cannot express today, and why batch 2 exists. One `Use`
enum serves both; the table's first column is simply unread for buffers.

Conservative: one barrier per transition, no batching, no split barriers. At ~10
transitions a frame that is not measurable.

### 2.3 Descriptors become a slot table

One descriptor set built at `Init`: binding 0 = sampled 2D array, 1 = cube array,
2 = storage images, 3 = TLAS. `Slot()` allocates on first call and writes the
descriptor. The dedicated hot shadow bindings (a measured 1.7x win,
`vulkan/backend.go:519`) stay a private optimisation with no shadow name on them.
Descriptor layouts, pools, sets and writes never appear in the interface.

**Slots must become reclaimable.** Today `registerTexture` only increments
`next2DSlot` / `nextCubeSlot` (`texture.go:95-103`) and `DestroyTexture` sets
`valid = false` without returning anything. Harmless while every texture is
load-time, but `Destroy` plus any streaming or resizing exhausts the 256-entry
array. `Slot` and `Destroy` share a free list per kind, and a slot returns to it
only once the retire queue has actually freed the image — a slot reused while a
frame in flight still samples it is silent wrong pixels, not a validation error.

### 2.4 Pipelines become objects

Per `BACKEND_DECISION.md` §6 and §9 item 6. `PipelineSpec` carries the shader
set, vertex attributes, cull, depth compare, depth write, blend, attachment
formats and the material-blob size. Replaces `CreateShader`, `SetCullMode`,
`SetDepthCompare` and the inferred `passKind` table — and is the only way
tessellation and mesh stages ever become expressible.

### 2.5 A compute pass declares its resources up front

The one asymmetry in the design, and it needs writing down rather than
discovering. A `Pass` learns what to transition from its attachments: they are in
`PassSpec`, so the barriers are recordable before `CmdBeginRendering`. A
`Compute` has no equivalent — a dispatch's resources are reached through
descriptors and device addresses, which the backend cannot inspect.

So `ComputeSpec` names them:

```go
type ComputeSpec struct {
	Name   string
	Reads  []Handle   // transitioned to UseSampled or read-only UseStorage
	Writes []Handle   // transitioned to UseStorage
}
```

Transitions are recorded before the closure opens, and `lastUse` updated after it
closes. Getting this wrong is a missing barrier — a compute pass reading a
storage image the previous pass wrote, with no hazard recorded — which validation
layers catch, so `validation = true` is the gate for any stage that adds compute.

The alternative, deriving resources from the pipeline's descriptor usage, was
rejected: it cannot see anything reached by device address, which is how this
engine passes most data.

---

## 3. The interface

**29 methods across four interfaces** — `Backend` 18, `Frame` 8, `Pass` 2,
`Compute` 1 — against 28 on one interface today.

### 3.1 The four interfaces

```go
type Backend interface {
	// --- lifetime
	Init(window *glfw.Window, req Request) error   // Request{Extensions, Features}
	Shutdown()
	Caps() Caps

	// --- resources
	CreateImage(ImageSpec) ImageHandle             // sampled|storage|attachment, 2D/3D/cube/array, mips
	CreateView(ImageHandle, ViewSpec) ViewHandle   // one mip, one slice, one aspect
	UpdateImage(ImageHandle, ImageData)            // CPU pixels, whole or a region
	CreateBuffer(BufferSpec) (BufferHandle, Address)  // vertex|index|storage|indirect, host|device
	UpdateBuffer(BufferHandle, offset uint64, data any)
	ReadBuffer(BufferHandle) []byte                // GPU → CPU; the missing readback path
	CreateMesh(MeshSpec) MeshHandle                // Attributes []VertexAttr, Share, Indices
	CreateSampler(SamplerSpec) SamplerHandle       // filter, address, border, Compare
	CreatePipeline(PipelineSpec) (PipelineHandle, error)   // graphics | compute | ray
	CreateAccel(AccelSpec) (AccelHandle, Address)  // BLAS / TLAS
	Slot(Handle) uint32                            // the shader-visible index
	Destroy(Handle)
	ReloadPipelines() error                        // shader hot-reload

	// --- frame
	Frame(record func(Frame))
	Timings() []Timing                             // GPU ms per named pass, previous frame
}

type Frame interface {
	Upload(data any) Address                 // transient arena block, one frame's life
	Pass(PassSpec, func(Pass))
	Compute(ComputeSpec, func(Compute))
	Copy(CopySpec)                           // image↔image, image↔buffer, buffer↔buffer
	Clear(ClearSpec)                         // a storage image before a compute pass
	GenerateMips(ImageHandle)
	BuildAccel(AccelHandle, AccelBuild)
	Timestamp(name string)
}

type Pass interface {
	Viewport(x, y, w, h int)
	Draw(DrawCall)
}

type Compute interface {
	Dispatch(DispatchCall)
}
```

Nesting is a scope, not a rule: a `Pass` value cannot exist outside `Frame.Pass`,
so the eleven `frameActive` / `passActive` guards become unreachable and are
deleted. A dispatch inside a render pass is illegal in Vulkan and becomes a
compile error here, rather than the stderr line `CopyDepthRegion` prints today
(`backend.go:984`).

### 3.2 Supporting types

```go
// Named uint32 types carrying a kind, so one Destroy and one Slot serve all
type Handle interface{ kind() handleKind }

type PassSpec struct {
	Color   []Attachment   // plural: G-buffer, velocity, probe capture
	Depth   *Attachment    // nil for a colour-only post pass
	Name    string         // RenderDoc label (batch 8), and the Timings() key
	Layers  int            // 6 renders a cube probe in one pass
	Samples int
	FlipY   bool           // the negative-height viewport, until clip space is fixed
}

type Attachment struct {
	View  ViewHandle       // a view, so a mip level or cube face is addressable
	Clear *[4]float32      // nil: load what is already there
	Store bool
}

type DrawCall struct {
	Pipeline  PipelineHandle
	Mesh      MeshHandle
	Push      [4]Address     // whatever the pipeline declares
	Instances int            // 0 and 1 both mean one
	Indirect  *IndirectRef   // batch 9
}

type DispatchCall struct {
	Pipeline PipelineHandle
	Push     [4]Address
	Groups   [3]int          // workgroups, not threads; local size lives in the shader
	Indirect *IndirectRef    // batch 9
}

type Caps struct {
	MaxAnisotropy float32    // props.MaxSamplerAnisotropy
	SampleCounts  int        // FramebufferColorSampleCounts & FramebufferDepthSampleCounts
	Formats       func(Format) bool
	Features      Features   // what Request actually got, not what it asked for
}
```

`Backbuffer` stays a reserved `ViewHandle`, so the swapchain is an ordinary
attachment and `BeginDepthPrepass` becomes `PassSpec{Depth: …}` with no colour.

`Caps.Features` reporting what was *granted* matters: `Request` may ask for ray
tracing on a device that has none, and `Supports(FeatureRayTracing)` is the fork
`BACKEND_DECISION.md` §5.2 and §8.3 depend on.

### 3.3 Semantics that are easy to get wrong

- **`Upload` is frame-scoped.** The arena resets in `BeginFrame`, so an address
  stored across frames points at another frame's data. A stale address is a GPU
  fault or silent garbage, never a compile error. Blocks are 64-byte aligned, as
  `writeArenaSlice` already does (`draw.go:117`).
- **Arena overflow must stop being silent.** Today `writeArenaSlice` wraps to
  offset 0 with one stderr line and lets the frame draw wrong (`draw.go:118-121`).
  With `Upload` open to every caller this gets hit more often, so it should either
  panic or grow the arena. Panicking is the honest choice: an overflowed frame is
  already wrong.
- **`ReadBuffer` stalls.** It waits on the frames in flight before mapping.
  Correct for screenshots and image tests, wrong in a frame loop; that is the
  intended trade and it should be documented at the method rather than
  discovered.
- **`Destroy` defers, it does not wait.** Today `DestroyTexture` calls
  `waitAllFrames()` (`texture.go:272`) — a full CPU/GPU sync per destroy. The
  retire queue already built for the UI overlay's resize path (`retire` /
  `drainRetired`, `texture.go:230-256`) frees an item once `framesInFlight`
  further frames have begun. `Destroy` routes everything through it, releasing
  the bindless slot at the same moment.
- **`CreateMesh` shares vertex buffers.** A multi-material OBJ is several meshes
  over one buffer, so destroying a mesh frees its index buffer only.
- **`Groups` is workgroups.** A 1920×1080 image with an 8×8 local size dispatches
  `{240, 135, 1}`.

---

## 4. Coverage

Every item on `BACKEND_DECISION.md` §9, `FEATURES.md` Part 2 and `TODO.md`, plus
the techniques raised in discussion. **M** = needs one of the methods above,
**F** = a field or enum value only. The last column is the `go-vulkan` batch from
§5, blank when nothing is needed.

| # | technique | how it is expressed | | batch |
|---|---|---|---|---|
| 1 | Transparency, glass, water | `PipelineSpec.Blend`/`DepthWrite`; params via `Upload` | F | 6 |
| 2 | Alpha cutout | pipeline variant + the same `discard` in the prepass shader | F | — |
| 3 | Instancing | `DrawCall.Instances` | F | — |
| 4 | HDR + tonemap | `ImageSpec.Format = RGBA16F`, a final fullscreen `Pass` | F | 1 |
| 5 | Bloom | mip chain of `CreateView`s, `Pass` per level, `Blend: Add` | M | 1, 6 |
| 6 | SSAO | depth as `UseSampled`, `Compute` or fullscreen `Pass`, noise texture | M | 3 |
| 7 | Post-process AA (FXAA) | scene into an offscreen colour target, one fullscreen pass | F | — |
| 8 | TAA | two history images ping-ponged, velocity attachment, jitter via `Upload` | M | 1 |
| 9 | Deferred / G-buffer | `PassSpec.Color []Attachment` | M | — |
| 10 | Mipmaps + anisotropy | `Frame.GenerateMips`, `SamplerSpec.MaxLod` | M | 5 |
| 11 | Clustered forward | light-list `CreateBuffer(Storage)`, filled on CPU or in `Compute` | M | 2, 3 |
| 12 | Reflection probes | cube `ImageSpec`, `PassSpec.Layers`, prefilter in `Compute` | M | 3, 5 |
| 13 | IBL (prefiltered env + BRDF LUT) | same, plus a one-off LUT `Compute` | M | 3, 5 |
| 14 | Volumetrics | 3D storage image, froxel fill in `Compute`, raymarch composite | M | 2, 3, 4 |
| 15 | Compute, generally | `CreatePipeline(Compute)` + `Frame.Compute` | M | 2, 3 |
| 16 | Software BVH + `traceRay` | nodes in a storage buffer, traversal in `Compute` | M | 2, 3 |
| 17 | Ray queries | `CreateAccel` + `Frame.BuildAccel`, `rayQueryEXT` in a compute pipeline | M | 10 |
| 18 | Ray-tracing pipelines (SBT) | `PipelineSpec.Kind = Ray` + shader groups | F | 10+ |
| 19 | GPU-driven culling / indirect | `DrawCall.Indirect`, `DispatchCall.Indirect`, `UseIndirect` | F | 9 |
| 20 | Occlusion culling | depth pyramid = per-mip `CreateView` + `Compute` | M | 2, 3, 9 |
| 21 | Particles | `Compute` writes a storage buffer, indirect instanced draw | M | 2, 3, 9 |
| 22 | Skinning / animation | bone matrices in a storage buffer; `MeshSpec.Attributes` carries indices/weights | M | — |
| 23 | Cascaded shadow maps | existing atlas mechanism + `Upload` for the per-cascade matrices | — | — |
| 24 | VSM / ESM shadows | colour target `RG32F`, blur pass, linear `SamplerSpec` | F | 1 |
| 25 | Hardware PCF | `SamplerSpec.Compare` | F | — |
| 26 | Screen-space reflections | depth + colour as `UseSampled`, `Compute` | M | 3 |
| 27 | Decals | pipeline variant, depth-test-no-write, projected UV via `Upload` | F | — |
| 28 | Reverse-Z | `CompareGreater`, `PassSpec.Depth.Clear = 0` | F | 6 |
| 29 | Tessellation / mesh shaders | `PipelineSpec.Stages` | F | new — not yet inventoried |
| 30 | Compressed textures (BC7) | `Format` enum values | F | 1 |
| 31 | Shader hot-reload | `ReloadPipelines` | M | — |
| 32 | GPU profiling | `Frame.Timestamp` + `Backend.Timings`, replacing the FPS subtraction `FEATURES.md` records as invalid | M | 7 |
| 33 | RenderDoc pass labels | `PassSpec.Name` | F | 8 |
| 34 | Screenshots / image tests | `Frame.Copy` image→buffer, then `ReadBuffer` | M | — |

### 4.1 Deliberately not covered

Each would change `Frame`'s shape, and none is on the roadmap:

- **Async compute** — needs a second queue and timeline semaphores. `Frame` is
  single-queue by construction; adding it later means a `Queue` concept.
- **Sparse / virtual textures** — residency binding, a separate allocator path.
- **Multi-GPU.**

---

## 5. `go-vulkan` batches

From `go-vulkan/BINDINGS_GAP.md` §7. The sibling repo is reached by a `replace`
directive in `src/go.mod`; extending it is part of this plan, not a blocker on
it. Update `BINDINGS_GAP.md` as each batch lands.

| # | batch | new funcs | effort | needed by |
|---|---|---|---|---|
| 1 | Formats — HDR, RG32F, BC7 | 0 | 1h | stage 2 |
| 2 | Barrier rework — `DependencyInfo`, buffer and global barriers | 0 (1 changed) | 3h | stage 4 |
| 3 | Compute — `CreateComputePipeline`, `CmdDispatch`, enums | 2 | 4h | stage 6 |
| 4 | Storage images + 3D views | 0 | 1h | stage 6 |
| 5 | `CmdBlitImage` | 1 | 2h | stage 6 |
| 6 | Blend and misc enums, incl. `CompareOpGreater` | 0 | 1h | stage 3 |
| 7 | Timestamp queries | 5 | 4h | stage 6 |
| 8 | Debug labels | 2-3 | 2h | stage 5 |
| 9 | `CmdDispatchIndirect` | 1 | 1h | later |
| 10 | Ray-query acceleration structures | 5 | days | later |

Batches 1-8 are roughly 18 hours. **Only batch 2 is breaking** — the
`CmdPipelineBarrier2` signature, one call site per backend — and stage 4 is the
call site that rewrites it anyway, so the break and the fix land together.

---

## 6. Rollout

Each stage compiles and runs. Bindings interleave rather than trail: the batch a
stage needs is done immediately before it.

**Stage 1 — opaque uniforms.** *No bindings.* Add `Upload` and `Slot` **to
`Backend`**, frame-scoped between the existing `BeginFrame` and `EndFrame` —
`Frame` does not exist until stage 5, and both move onto it there. Delete
`BindFrameUniforms` and `BindShadowRecords`; move the handle→slot translation up
into `scene/`; split the per-tile bake block out of `FrameUniforms` (§2.1).
Closes leaks 1-3. Nothing else changes shape.

**Stage 2 — resources.** *Batch 1 first.* Unify `CreateTexture` /
`CreateCubemap` / `CreateRenderTarget` into `CreateImage` + `CreateView`; add
`CreateBuffer`, `CreateSampler`, `MeshSpec.Attributes`, and the bindless free
list (§2.3). Closes leak 4. Storage usage and 3D images land here even though
nothing uses them yet.

**Stage 3 — pipelines.** *Batch 6 first.* `CreatePipeline(PipelineSpec)`
replacing `CreateShader`, `SetCullMode`, `SetDepthCompare` and the inferred
`passKind` table. Closes leaks 5-6. Touches every draw site — §6 is right that
this is cheapest while the interface is small.

**Stage 4 — automatic barriers.** *Batch 2 first.* The `Use` table replacing
`imageBarrier`, `barrierDiscardToColor`, `barrierBackbufferDepth` and
`barrierToShaderRead`. Validation-clean is the gate.

**Stage 5 — the frame.** *Batch 8 alongside.* `Backend.Frame(func(Frame))` and
`Frame.Pass(spec, func(Pass))` closures replacing `BeginFrame` / `EndFrame` /
`BeginPass` / `EndPass` / `BeginDepthPrepass`; `Upload` and the rest move onto
`Frame`; `PassSpec.Color []Attachment` and `Depth *Attachment`; MSAA resolve
decoupled from the swapchain image; the eleven guards deleted. **Highest-risk
stage.**

**Stage 6 — the rest.** *Batches 3, 4, 5, 7.* `Compute`, `GenerateMips`,
`Clear`, `ReadBuffer`, `Timestamp`, `ReloadPipelines`. `CreateAccel` /
`BuildAccel` declared, implemented when batch 10 lands.

**Proof:** build HDR + tonemap + bloom entirely in `scene/` or a new `effects/`
package, touching no file under `vulkan/`. `BACKEND_DECISION.md` §9 item 11
already nominates this as the thing that proves the stack.

---

## 7. Verification

```sh
cd src
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh
go build ./... && go vet ./...   # vet: the 2 known unsafe.Pointer reports only
```

**There are no tests.** `go test ./...` reports `[no test files]` for every
package, and the `init()` size guards in `renderer/uniforms.go` fire when the
engine *runs*, not when it builds. So every stage below is verified by building,
running, and looking at the scene. `shadowAtlas.allocate` is pure CPU logic and
should get the first unit test — see `TODO.md`.

Per stage, and all of it again at stage 5:

- `go run .` — a settled scene must show `bakes: 0 static 0 dynamic`. Non-zero
  after settling means the static/dynamic split broke.
- `go run . -scene stress.xml` — 64 lights, all casting; exercises every pool and
  the fallback to `ShadowIndex = -1`. After stage 1 it is also the arena test:
  337 tiles must fit comfortably, where they cost ~1.6 MiB before.
- `go run . -config configs/low.toml` — 2048 atlas, no dynamic atlas, cheap PCF,
  no MSAA. Catches anything that assumed the default tier.
- `validation = true` in `[debug]` — **zero validation errors gates stages 4, 5
  and any stage adding compute.**
- `noShadows = true` A/B — separates "the scene is dark" from "every light is
  wrongly occluded".
- `depthPrepass` on/off A/B — `EQUAL` rejects a last-bit difference and shows it
  as speckle, not an error. `prepass.slang` must keep combining `projection`,
  `view` and `model` in exactly `forward.slang`'s order.
- `lockCamera = true` + a RenderDoc capture before and after each stage; compare
  the swapchain image and both atlases. Until stage 6's `ReadBuffer` lands this
  is the only image check — after it, a captured buffer can be diffed instead.

`common.slang` and the `init()` size guards in `renderer/uniforms.go` should not
change, except where stage 1 splits the per-tile block out. If a shader does:
`spirv-val --scalar-block-layout shaders/vk/*.spv`.

---

## 8. Costs and open risks

- Stage 3 touches every draw site; stage 5 touches `core/app.go`,
  `scene/scene.go`, `scene/skybox.go`, `scene/shadowatlas.go` and `core/ui.go`.
- **`vulkan/` does not get smaller.** It loses a little to the `Use` table and
  the deleted guards, then grows as batches 3-8 land. Size was never the metric;
  what changes is that it is organised by concept (`frame.go`, `barrier.go`,
  `slots.go`, `pipeline.go`) so a question about frame pacing is ~120 lines, not
  a package.
- Automatic barriers are conservative. If a hand-tuned barrier is ever needed,
  that is the moment to insert a Vulkan-shaped `rhi.Device` underneath; the
  interface above does not change when that happens, so it is not a dead end.
- **Reusing a bindless slot too early is silent.** The free list must be fed from
  the retire queue, not from `Destroy` (§2.3).
- **A missing compute barrier is silent without validation on** (§2.5).
- `go-vulkan` remains the largest maintenance item in the project
  (`BACKEND_DECISION.md` §11.1). Batches 1-8 add roughly 18 hours to it.
