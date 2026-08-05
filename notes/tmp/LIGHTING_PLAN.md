# Lighting plan — clustered forward + shadow atlas

**Status: proposal, under iteration. Nothing here is implemented yet.**

> **Stale as of 2026-08-05 — the constraint this plan was designed around is
> gone.** The OpenGL 4.1 backend was deleted (`BACKEND_DECISION.md`), so the
> "GL 4.1 floor" that shapes §6, §7 and much of §9 no longer applies. Compute
> shaders, storage buffers and storage images are all reachable once
> `go-vulkan/BINDINGS_GAP.md` §7 batches 2-4 land. Specifically, revisit:
>
> - **§6, the CPU-built cluster grid.** Built on the CPU only because GL 4.1 had
>   no compute and no SSBOs. A compute build is now open, and would itself be a
>   worthwhile experiment.
> - **Texture buffers (`samplerBuffer`) as the transport** for the light and tile
>   record arrays. A storage buffer is the natural expression now.
> - **§9's uniform-block size ceilings.** A GL 4.1 uniform block guarantees only
>   16 KB; that number should not constrain anything here.
> - **The parity language throughout** — "symmetric across the two backends",
>   "byte-identical here" — which no longer has a second side to be symmetric
>   with.
> - **The `int4`-not-`int[4]` and 16-byte-cell requirements** in §4. Removed from
>   the engine on 2026-08-05 (`BACKEND_DECISION.md` §5.3); `LightData` is now 68
>   bytes, not 80, so this section's growth arithmetic starts from a lower base.
> - **The ordering.** `BACKEND_DECISION.md` §9 items 1-6 (pipelines, pass list,
>   compute) now come before this plan's Part B onward, since they are what a
>   shadow atlas would be built on.
>
> The lighting *design* — clustered forward, the atlas, the static/dynamic split,
> the capacity arithmetic — is unaffected and still the plan. Only the
> implementation constraints changed, and they all loosened.

Unlike the other engine documents, this one describes what the code *should*
become, not what it does today. §12 is the implementation plan, split into eight
parts meant to be picked up one at a time. When a part lands, its content moves
into `../FEATURES.md` (why it is built that way) and `../ENGINE_FLOW.md` (how a frame
runs), and the part is struck from here. When this file is empty, delete it.

Scope: many shadowed lights, static and dynamic, scalable from a discrete GPU
down to an integrated laptop one.

Not here: the current fixed 1-directional + 1-point shadow budget
(`../FEATURES.md` Part 1), the `Backend` contract as it stands today
(`../ENGINE_FLOW.md` §0), general technique background (`../cheatsheets/GRAPHICS.md`).

---

## 0. Table of contents

1. [Verdict on the proposal](#1-verdict-on-the-proposal)
2. [Four refinements](#2-four-refinements)
3. [The three budgets](#3-the-three-budgets)
4. [Data model](#4-data-model)
5. [The atlases](#5-the-atlases)
6. [Clustering](#6-clustering)
7. [Frame flow](#7-frame-flow)
8. [Backend interface changes](#8-backend-interface-changes)
9. [Quality tiers](#9-quality-tiers)
10. [Forward compatibility: AO and beyond](#10-forward-compatibility-ao-and-beyond)
11. [Backend parity ledger](#11-backend-parity-ledger)
12. [Implementation plan](#12-implementation-plan) — Parts A–H, one at a time
13. [Open questions](#13-open-questions)

---

## 1. Verdict on the proposal

Clustered forward shading plus a shadow atlas, split static/dynamic, with
unshadowed lights as a cheap tier — this is the correct architecture and it is
what shipping engines converged on. It is chosen here for four specific reasons,
not by imitation:

- **It keeps MSAA.** The engine gained 4× MSAA in `b29c645`. Deferred shading
  would force per-sample G-buffer resolve or the abandonment of MSAA for a
  post-process AA. Clustered forward is multisample-native.
- **It degrades by scalar, not by code path.** Low-end is a coarser froxel grid
  and a smaller atlas — the same shaders, the same passes. No second renderer to
  keep alive.
- **It leaves the forward path intact** for transparency and the IBL ambient
  term, both of which a G-buffer makes awkward.
- ~~**It respects the GL 4.1 floor.** Every piece below is expressible without
  compute shaders or SSBOs (see §6).~~ Void — see the banner. The design does not
  *need* compute, which is still a virtue; it is no longer a constraint.

The one expectation to correct: clustered shading reduces *shading* cost only.
It does not make a single shadow map cheaper to bake. The shadowed-light budget
is won in §5, not §6. The two are independent, which is why §3 keeps three
separate budgets rather than one "light count".

---

## 2. Four refinements

### 2.1 Static/dynamic is per-tile, not per-light

The natural reading of "static lights computed once, dynamic lights computed
always" splits *lights* into two sets. That misses the common case: a **static
light with a dynamic caster walking under it**. Freezing that light's map leaves
the moving object shadowless.

So the split is per-tile, over two atlas textures:

- `staticAtlas` — baked once at load, holds only immovable casters. Persistent,
  never re-rendered unless the scene is reloaded or the light's tile is
  reallocated.
- `dynamicAtlas` — rebuilt per frame for lights that have a movable caster in
  range. Each such light **blits its static tile across from `staticAtlas`**,
  then draws only the movable casters on top with a normal depth test. The
  result in the tile is the union of both shadow sets, in one sample.

A light with no movable caster in range keeps pointing at `staticAtlas` and
costs nothing that frame. `ShadowRecord.Flags` carries which atlas the shader
samples, so the fragment path is one lookup either way.

The blit is a texels copy, not a scene re-draw — cheap and constant per tile.
Cross-texture, so it stays well-defined on both APIs (§8); blitting a region of
a texture onto another region of *the same* texture is what would be undefined.

> This is also what makes "disable dynamic shadows on low-end" a one-line
> config: skip the dynamic pass entirely, leave every record pointing at
> `staticAtlas`. Moving objects lose their shadows, everything else is
> unchanged, and the frame cost of shadows drops to zero.

### 2.2 Cluster assignment doubles as the shadow relevance test

Clustered shading has to compute, per frame, which lights touch which froxel.
That same result answers "which lights are worth an atlas tile": a light
intersecting zero clusters is invisible this frame and needs no tile, no bake,
no blit. Feeding the cluster build's output into the tile allocator makes
off-screen lights free rather than merely cheap, and it is the same loop.

This is the one place where §6 does help the shadow budget, and it is worth
building the two in the order that exploits it.

### 2.3 Add spot lights before scaling point lights

A point light costs **six** tiles and six bakes. A spot light costs one. The
engine has `LightSun` and `LightPoint` only, though `LightData.Cutoff` already
exists unused. Adding `LightSpot = 2` is a small change that makes the most
common "shadowed light in a room" case six times cheaper in both atlas texels
and bake time. Do it before tuning any point-light budget.

### 2.4 A depth prepass is the cheapest thing you can do for the future

§10 covers this in full, but the ordering matters: adding a depth (and packed
normal) prepass early costs one geometry pass and immediately pays for itself by
eliminating overdraw in the forward pass. It then becomes the input every later
technique wants — AO, SSR, volumetrics, TAA. Retrofitting it after those
techniques exist is far more disruptive than having it from the start.

---

## 3. The three budgets

Keeping these separate is what stops the design collapsing into one wrong
number. Targets are 1080p, discrete GPU, and are goals for the finished system:

| budget | limited by | sun | spot | point |
|---|---|---|---|---|
| **unshadowed** | shading ALU × clusters | n/a | 1000s | 1000s |
| **static shadowed** | atlas texels (VRAM) | 1–4 | ~256 | ~42 |
| **dynamic shadowed** | rebake texels per frame | 1 | ~64 | ~10 |

Figures are for a 4096² atlas at 256²/tile and a 4.19 M texel rebake budget;
§5.5 has the full table and the other atlas sizes. A point light costs six
tiles to a spot light's one, which is where the 6× gap between the columns
comes from, and why §2.3 adds spot lights early.

The third row is the scarce one. Everything in §5 exists to move lights out of
it and into the second — a light that stops moving stops costing.

An unshadowed light is not a degenerate case to be tolerated — it is the correct
tier for small details, distant objects, and fill lighting, and the allocator in
§5.3 demotes lights into it deliberately when the budget runs out.

---

## 4. Data model

### 4.1 `LightData` — 68 → 96 bytes

The struct carries three `Reserved` slots and the plan needs four new fields, so
it grows by exactly one 16-byte cell rather than being squeezed. The last two
cells become:

```go
Type        int32
OuterCutoff float32 // outer cone cosine; Cutoff above is the inner one
Radius      float32 // attenuation cutoff, for the early-out and cluster bounds
ShadowFirst int32   // index of this light's first ShadowRecord, -1 = unshadowed

ShadowCount int32   // 1 for sun and spot, 6 for point
Reserved0, Reserved1, Reserved2 float32
```

No existing member moves, so the mirror in `common.slang` stays valid — but the
`init()` size guard must be updated in the same commit. The 16-byte cell padding
shown here is no longer required (`BACKEND_DECISION.md` §5.3); keeping or
dropping it is free.

`Radius` is derived once at load from the attenuation terms — the distance at
which `intensity / (c + l·d + q·d²)` falls below 1/255. It is what both the
shading early-out and the cluster intersection test need. `Cutoff` already
exists but is hardcoded to cos 45° in `scene/scene.go`; spot lights make it and
`OuterCutoff` real per-light values.

`MaxLights` goes 8 → 16 for the uniform block; the cluster light list (§6)
carries the rest, so 16 is the per-*cluster* cap, not the scene cap. The light
array is then 16 × 96 = 1536 bytes, and `FrameUniforms` settles at 1792 once §4.3
removes the six-matrix array.

### 4.2 `ShadowRecord` — new, 96 bytes

One record per shadow *tile*: one for a sun or spot, six for a point light.

```go
// 96 bytes, 16-byte cells — see the LAYOUT RULE in common.slang
type ShadowRecord struct {
	LightSpace mgl32.Mat4 // world -> this tile's clip space
	AtlasRect  [4]float32 // uv offset.xy, uv scale.xy
	TexelSize  float32    // 1 / tile pixels, the PCF step
	FarPlane   float32    // for the cube depth comparison
	Face       int32      // 0..5 for a cube face, -1 for a 2D tile
	Flags      int32      // bit 0: sample dynamicAtlas rather than staticAtlas
}
```

**Records live in a texel buffer, not in `FrameUniforms`.** The §5.1 partition
is 337 tiles, so 337 records — 32 KB at 96 bytes each, well past what a uniform
block is a sensible home for, and the count is data-driven rather than fixed.
(The original argument was GL 4.1's guaranteed 16 KB uniform block; that number
no longer applies, but a growable array still does not belong in `FrameUniforms`.) So `ShadowRecord[]` goes in the same texture buffer as the cluster data
(§6), and `CreateTexelBuffer` lands in Part B, well before clustering needs it.

`FrameUniforms` keeps only `TexShadowRecords`, the handle to that buffer.

### 4.3 `FrameUniforms` — net simplification

| removed | added |
|---|---|
| `LightSpaceMatrix` | `BakeMatrix` (one matrix, the tile being baked) |
| `ShadowMatrices[6]` | `TexShadowRecords` (handle to the record texel buffer) |
| `ShadowDirIndex` | `TexShadowStatic`, `TexShadowDynamic` |
| `PointShadowLights[4]` | `ClusterGrid [4]int32` (x, y, z, maxPerCluster) |
| `TexShadowCubeMap` | |

`FrameUniforms` gets *smaller*: the six-matrix array leaves and nothing
fixed-size replaces it.

Because each cube face becomes an ordinary tile draw with its own matrix, the
six-matrix array and the geometry stage that consumed it both disappear —
`depth_cube.slang` is deleted, along with the `FrontFace = Clockwise` divergence
it forced on the Vulkan backend (§11).

The `init()` size guard in `renderer/uniforms.go` must be updated in the same
commit as any of this, and the shaders rebuilt. The guard catches a wrong
*size*; a wrong field *order* renders silent garbage and nothing catches it
automatically, so eyeball the showcase scene.

---

## 5. The atlases

### 5.1 Layout

**Two textures for the whole engine**, `staticAtlas` and `dynamicAtlas`,
allocated once at startup — not two per light, and not one per light. Every
shadow in the scene is a *sub-rect* of one of them. 1 shadowed light or 200,
the texture count does not change, and `Light.shadowTarget`, `Light.depthMap`
and `Light.depthCubeMap` all disappear.

That is the difference from the current scheme, which calls
`CreateRenderTarget` once per casting light and so needs one texture, one
framebuffer and one sampler binding each — one target per casting light, which
is the wall this design exists to get past. (It was a harder wall under GL 4.1's
16-units-per-stage guarantee; with bindless descriptors it is a soft one, so the
atlas is now a bandwidth and cache argument rather than a capacity one.)

Each atlas is a single 2D depth target carved into power-of-two tiles by a
quadtree allocator (4096 → 2048 → … → 128). Everything is a 2D tile: a sun gets
one, a spot gets one, a point light gets six perspective tiles at 90°.

Tiles per light, which is what every count below is derived from:

| light type | tiles | why |
|---|---|---|
| sun (`LightSun`) | 1 | one ortho projection |
| spot (`LightSpot`) | 1 | one perspective cone |
| point (`LightPoint`) | 6 | one 90° perspective per cube face |

A representative partition of a 4096² atlas. The four quadrants hold an equal
number of *texels* (4.19 M each) and a wildly unequal number of *tiles*, which
is the entire point of a quadtree:

```
staticAtlas — ONE 4096x4096 depth texture, ONE handle, ONE framebuffer

      0                              2048                          4096
    0 +------------------------------+------------------------------+
      |                              |  16 tiles of 512             |
      |                              |                              |
      |     1 tile of 2048           |   =  2 point lights  (12)    |
      |                              |    +  4 spot lights   (4)    |
      |     =  1 sun                 |                              |
      |                              |      near lights             |
      |        the one light every   |                              |
      |        pixel sees            |                              |
 2048 +------------------------------+------------------------------+
      |  64 tiles of 256             | 256 tiles of 128             |
      |                              |                              |
      |   = 10 point lights  (60)    |   = 40 point lights  (240)   |
      |    +  4 spot lights   (4)    |    + 16 spot lights   (16)   |
      |                              |                              |
      |      mid distance            |      distant / small         |
 4096 +------------------------------+------------------------------+

   1 sun  +  52 point  +  24 spot   =   77 shadowed lights, 337 tiles
```

Seventy-seven lights, four resolutions, one texture. The partition is not fixed
in code — the allocator rebalances it per frame from the score in §5.3 — it is
shown here to make the capacity concrete.

**Why a rect and not a texture-array layer.** A `sampler2DArray` requires every
layer to be the same size, so variable resolution would need one array per tier
— back to several textures and several sampler units. A rect inside one texture
has no such constraint, which is the whole reason the sun at 2048 and a distant
light at 128 can share an image. The same argument retires the cubemap: six
tiles may differ in size per face, and the face cull in §5.4 can decline to
allocate the faces pointing away from the camera, which a cubemap cannot
express.

The cost of the freedom is that neither tile boundaries nor cube-face
boundaries get hardware clamping or filtering any more. §5.2 is what pays it.

Sizing, D24:

| atlas | VRAM each, D24 | fits |
|---|---|---|
| 2048² | 16 MiB | sun @1024 + ~24 tiles @256 |
| 4096² | 64 MiB | sun @2048 + 2 lights @512 + ~40 tiles @256 |
| 8192² | 256 MiB | sun @4096 + 8 lights @512 + ~150 tiles @256 |

`dynamicAtlas` can be allocated smaller than `staticAtlas`, since only lights
with movable casters need a tile in it.

### 5.2 Tile sampling rules

Two rules, both mandatory, both silent-corruption bugs if missed:

- **Clamp every PCF tap to the tile rect, inset by one texel.** Without it the
  kernel reads a neighbouring light's tile and produces shadows from nowhere.
- **Widen each cube face's FOV to `90° + 2 texels`.** An atlas has no hardware
  cross-face filtering, so the kernel must stay inside its own tile at a face
  boundary or the seams show.

### 5.3 Allocation policy

Per frame, per light, in `scene`:

```
score = radius / distance_to_camera        // ≈ screen-space footprint
tier  = high if score > 0.50               // e.g. 1024
        mid  if score > 0.20               //      512
        low  if score > 0.08               //      256
        none otherwise                     // unshadowed, ShadowFirst = -1
```

Lights are sorted by score and allocated greedily; those that do not fit fall to
the unshadowed tier. That is the graceful-degradation property — the budget
running out costs shadow quality, never frame time.

Two details that matter in practice:

- **Hysteresis.** Only change a light's tier when its score crosses a threshold
  by more than 20%. Without it, a light hovering on a boundary reallocates every
  frame, and every reallocation forces a full re-bake.
- **Cluster gate.** A light touching zero clusters (§2.2) skips allocation
  entirely, whatever its score.

### 5.4 Bake scheduling

A tile is re-baked when its light moved, its tile was reallocated, or a movable
caster in range moved. `Scene.UpdateMeshes` already knows which meshes a physics
step touched — that set feeds the per-light dirty flag.

Two culls before any draw:

- **Caster cull.** Skip meshes whose bounds miss the light's radius.
- **Face cull.** Skip cube faces whose 90° frustum misses the camera frustum —
  typically 2–4 of the 6.

Then a **re-bake budget**, counted in texels rather than tiles — a 2048 tile is
256× the work of a 128 tile and they cannot share a counter. Tiles are queued by
score until the frame's allowance runs out. This is what prevents a hitch when
the camera turns into a room of twenty dynamic lights; the overflow resolves
over the next few frames (§5.5 for the numbers).

Static scene, static lights ⇒ zero bakes per frame after load.

All of it is **one pass over one target**, the tile chosen by viewport rather
than by binding a different image:

```go
b.BeginPass(atlasTarget, atlasSize, atlasSize, nil) // once, for every tile
for _, t := range dirtyTiles {
	// The only thing that makes this an atlas rather than N render targets
	b.SetViewportScissor(t.X, t.Y, t.W, t.H)
	f.BakeMatrix = t.LightSpace
	b.BindFrameUniforms(&f)
	for _, m := range t.VisibleCasters {
		m.draw(depthShader, &u)
	}
}
b.EndPass()
```

Today the same work is one `BeginPass` per casting light, each on its own
target. A viewport change is nearly free; a target bind is not, and on Vulkan it
is a render-pass boundary.

### 5.5 Capacity, by light type

Cost in texels, which is the only currency the atlas trades in:

| tier | sun / spot (1 tile) | point (6 tiles) |
|---|---|---|
| 2048 | 4.19 M | — (24 M, never worth it) |
| 1024 | 1.05 M | 6.29 M |
| 512 | 262 K | 1.57 M |
| 256 | 65.5 K | 393 K |
| 128 | 16.4 K | 98.3 K |

A point light is **six times** a spot light of the same tier. This is why §2.3
insists on adding spot lights before scaling anything.

Spending a whole atlas on a single type — the honest upper bounds:

| whole atlas spent on | 2048² | 4096² | 8192² |
|---|---|---|---|
| point lights @512 | 2 | **10** | 42 |
| point lights @256 | 10 | **42** | 170 |
| point lights @128 | 42 | **170** | 682 |
| spot lights @512 | 16 | **64** | 256 |
| spot lights @256 | 64 | **256** | 1024 |
| spot lights @128 | 256 | **1024** | 4096 |
| one sun, full atlas | 1 | 1 | 1 |

VRAM at D24: 16 MiB, 64 MiB, 256 MiB. Capacity scales 4× per step, so the atlas
size is a *resolution* knob at fixed light count, or a *count* knob at fixed
resolution — not both at once.

Mixed, as in the §5.1 partition: **1 sun + 52 point + 24 spot = 77 shadowed
lights** in one 4096² atlas. That is the realistic number to design against.

**Static versus dynamic.** Everything above is the *static* budget, bounded by
VRAM alone: a light that never moves over geometry that never moves is baked at
load and costs nothing per frame, so all 77 can be static. The *dynamic* budget
is separate and bounded by time — `rebakeBudget`, a texel allowance per frame:

| `rebakeBudget` | fully dynamic every frame |
|---|---|
| 1.05 M texels | 2 point @256, or 16 spot @256 |
| 4.19 M texels | **10 point @256**, or 2 point @512, or 64 spot @256 |
| 16.8 M texels | 42 point @256, or 10 point @512 |

Lights beyond the allowance are not dropped, they re-bake on later frames in
score order — 42 dynamic point lights at a 4.19 M budget refresh over 4 frames.
Blur on fast-moving shadows is the failure mode, not a stall.

> Ten fully dynamic shadowed point lights, refreshed every frame, cost about one
> quadrant of a 4096² atlas per frame. That is the original question, answered
> in the design's own units.

**Total lights, shadowed or not**, is a different ceiling again, and it moves
with the parts of §12:

| after part | scene lights | shadowed | limited by |
|---|---|---|---|
| today | 8 | 1 sun + 1 point | `MaxLights`, one target per light |
| **A** | 16 | 1 sun + 1 point | `MaxLights` |
| **C–E** | 16 | up to 16 | `MaxLights`, not the atlas |
| **G** | **1000s** | 77 (§5.1) | atlas texels; 16 *per cluster* |

Note the ordering trap: until Part G the atlas can hold far more shadowed lights
than `FrameUniforms` can name. Parts C–E are worth shipping anyway — they make
16 lights *good* rather than *many* — but the light-count headline arrives with
clustering.

---

## 6. Clustering

A froxel grid over the view frustum, default 16 × 9 × 24, exponential in Z. Per
frame the CPU tests every light's bounding sphere against every froxel and
writes two arrays:

- `clusterOffsets` — one `uint2` per cluster: offset into the index list, count.
- `clusterIndices` — a flat `uint` list of light indices.

The fragment shader computes its cluster from `gl_FragCoord.xy` and view depth,
reads its offset and count, and loops only over that cluster's lights.

**CPU-built first, but no longer forced.** This was originally CPU-only because
GL 4.1 had neither compute shaders nor SSBOs. With that gone, start on the CPU
because it is simpler, then move the build to compute once `Dispatch` exists
(`BACKEND_DECISION.md` §9 item 6) — a GPU cluster build is a good first user of
it. The transport was going to be a **texture buffer** (`Buffer<uint>` in Slang);
a plain storage buffer, or a device address like the uniform blocks already use,
is the simpler expression now.

**Before any of this**, the attenuation early-out is worth doing on its own:

```
// Skip a light once the fragment is outside its radius
// Most fragments touch a few lights, not all of them — this is what makes a
// plain forward loop affordable while clustering is still being built
if (light.type != LIGHT_SUN) {
    float3 d = light.position - fragPos;
    if (dot(d, d) > light.radius * light.radius) continue;
}
```

Six lines, no layout change, and it lifts the current brute-force loop from 8
usable lights to roughly 64. Clustering then takes it to thousands. Shipping the
early-out first also means the clustered path has a correct, cheap fallback to
compare against.

---

## 7. Frame flow

Changes to `core/App.Run`, in order. New steps marked ▸:

```
world.Update, Scene.UpdateMeshes
input
BeginFrame
▸ cluster build          CPU: light spheres vs froxels, upload the two buffers
▸ tile allocation        score, hysteresis, quadtree; cluster-gated
▸ static atlas bakes     only on load, or on reallocation
▸ dynamic atlas tiles    per dirty light: blit static tile, draw movable casters
▸ depth prepass          backbuffer, depth only (see §10)
  main pass              skybox (LEQUAL), forward scene, UI quad
EndFrame
```

The shadow work stays where the current shadow bakes already are — before the
main pass, each on its own target — so invariant 2 (clears and viewports live
only inside `BeginPass`) holds, with the one amendment in §8.

---

## 8. Backend interface changes

Three additions. Re-read these against `BACKEND_DECISION.md` §6 before building
them — `PipelineSpec` and the `Pass` interface may absorb the first, and
`CreateStorageBuffer` supersedes the third.

```go
// Restricts drawing to a sub-rect of the current pass's target
//
// Viewport and scissor together: the scissor is not optional, it is what stops
// a shadow tile's clear and its PCF kernel reaching into its neighbours
SetViewportScissor(x, y, w, h int)

// Copies a depth rect between two render targets, without a draw
//
// The static-to-dynamic tile promotion in the shadow atlas, which must not
// re-render the static casters
CopyDepthRegion(src, dst RenderTargetHandle, srcX, srcY, dstX, dstY, w, h int)

// Creates a buffer readable by shaders as a flat array of texels
//
// The cluster light-index list, too large for a uniform block
//
// Superseded: CreateStorageBuffer (BACKEND_DECISION.md §9 item 6) is the same
// thing without the texel-buffer indirection
CreateTexelBuffer(sizeBytes int) (BufferHandle, TextureHandle)
UpdateTexelBuffer(h BufferHandle, data []uint32)
```

Mapping:

| method | Vulkan 1.3 |
|---|---|
| `SetViewportScissor` | `vkCmdSetViewport` + `vkCmdSetScissor`, already dynamic |
| `CopyDepthRegion` | `vkCmdCopyImage` — needs no new binding |
| `CreateTexelBuffer` | `VK_BUFFER_USAGE_UNIFORM_TEXEL_BUFFER_BIT` + buffer view, or just a storage buffer |

`SetViewportScissor` amends invariant 2, which currently says viewports exist
only inside `BeginPass`. The amended rule: **a viewport is set by `BeginPass`,
or narrowed by `SetViewportScissor` within a pass on an atlas target.** Nothing
outside the shadow-atlas code may call it. `../ENGINE_FLOW.md` §5 must record this
when the phase lands, or the invariant quietly stops meaning anything.

---

## 9. Quality tiers

Every knob is a scalar the allocator and the shaders already read. Low-end is
smaller numbers, never a different code path.

```toml
[shadows]
staticAtlas   = 4096      # 2048 low / 4096 mid / 8192 high
dynamicAtlas  = 2048      # 0 disables dynamic shadows entirely
maxCasters    = 64        # lights allowed a tile at all
rebakeBudget  = 4194304   # texels re-baked per frame, not tiles: a 2048 tile
                          # and a 128 tile are not the same unit of work
tierHigh      = 1024      # tile sizes for the three score tiers
tierMid       = 512
tierLow       = 256
pcf           = "full"    # full 9-tap | cheap 4-tap | hard 1-tap

[lighting]
clusters      = [16, 9, 24]   # froxel grid, [8, 5, 12] on low
maxPerCluster = 16            # lights evaluated per fragment
```

`dynamicAtlas = 0` is the low-end switch from §2.1: no dynamic pass, every
record points at `staticAtlas`, moving objects cast nothing, frame cost of
shadows goes to zero. Nothing else changes.

---

## 10. Forward compatibility: AO and beyond

The requirement that this stay compatible with ambient occlusion and later
lighting work resolves almost entirely into one decision: **add a depth prepass,
and write packed normals alongside it.**

- Depth only, at first: draw `depth.slang` to the backbuffer, then run the
  forward pass with `EQUAL`. Costs one geometry pass, removes all shading of
  hidden fragments — with 16 lights per fragment that pays for itself at any
  overdraw above ~1.5.
- Then a single `RG16` colour attachment for octahedral-packed view-space
  normals. Depth + normals is exactly the input GTAO and SSAO want. This is
  **not** a G-buffer and not a step toward deferred: no albedo, no material, no
  lighting read from it.

What it unlocks, in rough order of likely interest:

| technique | needs |
|---|---|
| SSAO / GTAO | prepass depth + normals ✔ |
| screen-space shadows (contact) | prepass depth ✔ |
| SSR | prepass depth + normals ✔, plus a colour copy |
| volumetric fog / light shafts | prepass depth ✔ + the cluster grid ✔ |
| TAA | prepass, plus a velocity attachment |
| shadow-map ray-traced fallback | Vulkan `FeatureRayTracing`, BLAS/TLAS |

Two constraints to design around now rather than discover later:

- **MSAA.** The prepass must be multisampled to match the main pass, or the
  `EQUAL` test fails at edges. AO then runs on a resolved depth, and its result
  is applied per-pixel. Budget for the resolve.
- **Where AO applies.** It multiplies the ambient/IBL term, not the direct
  lighting — `forward.slang` already isolates `ambient`, so the insertion point
  is a single multiply. Keeping that separation is the whole compatibility
  requirement, and clustered forward preserves it; deferred would not, without a
  separate ambient pass.

The froxel grid is reusable as-is for volumetrics — same clusters, same light
list, a different integration. That is a second reason to build clustering
rather than a flat light list.

---

## 11. Backend parity ledger

Parity was a stated goal, so it is tracked explicitly. This plan is net
**positive** for it.

Divergences removed:

- `depth_cube.slang` and its geometry stage disappear — every tile is an
  ordinary 2D depth draw. Vulkan's `FrontFace = Clockwise` shadow-pass
  declaration, which exists only to make the geometry-stage cube map's memory
  layout come out right, goes with it.
- The cube sampler array (`shadowCubeMap[MAX_SHADOW_CUBES]`) collapses to two
  ordinary 2D samplers.

Divergences added: **none**, as long as the cluster build stays on the CPU (§6).

Divergences that remain untouched: the negative-height viewport in Vulkan's main
pass, and the bindless-vs-unit texture binding for material textures. Both are
documented in `../ENGINE_FLOW.md` §5 and are unaffected by this work.

---

## 12. Implementation plan

**Split out to `LIGHTING_IMPL.md`** — eight parts, each independently shippable,
each ending on a green build and a correct showcase scene. That file
carries the per-part steps, test gates and risks; this is the shape of it.

| part | theme | backend work | unlocks |
|---|---|---|---|
| **A** | light model, spot lights | none | ~64 lights |
| **B** | atlas plumbing | both | tiles drawable |
| **C** | records, atlas sampling | both | one sampler, N tiles |
| **D** | allocator | none | variable resolution |
| **E** | static/dynamic, caching | both | the shadow budget |
| **F** | depth prepass | both | overdraw, AO input |
| **G** | clustered forward | none | 1000s of lights |
| **H** | quality tiers | none | the low-end story |

Three positions in that order are fixed, and argued in full there:

- **Spot lights first (A).** Six times cheaper per shadowed light (§5.5), and
  every capacity figure in this document assumes they exist. Zero coupling to
  any other part.
- **Depth prepass at F**, after the shadow work and immediately before
  clustering. Earlier it returns little — the forward pass is not yet
  overdraw-bound; later and Part G pays to shade hidden fragments with a full
  per-cluster light loop. It also lands §10's depth buffer before any AO work.
- **Config knobs last (H).** A knob for a system that does not exist yet is a
  guess at its range.

Parts E and G answer the two halves of the original question — E for how many
*shadowed* lights, G for how many lights at all.

Deferred shading appears nowhere in that list, on purpose. It costs MSAA, an MRT
rewrite, and the clean ambient/direct split §10 depends on — in
exchange for a bandwidth-versus-ALU trade that only pays at light counts this
design reaches by other means.

---

## 13. Open questions

Points to settle while iterating on this document:

1. **`dynamicAtlas` sizing.** A separate texture is simple and keeps the blit
   cross-texture and well-defined. A single atlas with a reserved dynamic region
   halves the VRAM but makes the blit same-texture, which is undefined when the
   regions overlap and awkward to prove otherwise. Recommendation: two textures,
   revisit if VRAM ever binds.
2. ~~**Where records live.**~~ *Settled:* not in `FrameUniforms`. The §5.1
   partition alone is 337 records = 32 KB, and the count is data-driven. A
   storage buffer or a device address is the natural home now that GL 4.1's
   uniform-block ceiling is not the reason.
3. **Sun cascades.** This plan gives the directional light one tile. A single
   ortho map over a large scene is the current quality ceiling, and cascades
   (3–4 tiles, split by view depth) fit the atlas naturally — but they are a
   separate feature with their own selection and blending logic. Deliberately
   out of scope here; flag if the showcase scene grows.
4. **Point-light alternatives.** Dual-paraboloid halves a point light to 2 tiles
   instead of 6, at the cost of tessellation-dependent artifacts. Probably not
   worth it once §2.3's spot lights carry the common cases, but it is the lever
   if point lights stay dominant.
5. **Cluster Z distribution.** Exponential is standard; the near/far it is fit
   to interacts with the shadow `farPlane`, currently a hardcoded 50 in
   `core/app.go`. Both should come from the same scene bounds.
