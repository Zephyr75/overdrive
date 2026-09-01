# Lighting plan — shadow atlas + clustered forward

**Status: Parts A–D of `LIGHTING_IMPL.md` are built** — spot lights, the atlas
and its plumbing, the records, and the allocator. E–H (static/dynamic caching,
depth prepass, clustering, quality tiers) are not. `LIGHTING_IMPL.md` is the
build order and records where the shipped shape deviates from this document;
§4.1 and §4.3 below carry the largest such notes.

**Part D's per-frame buddy quadtree has since been replaced** by a slot layout
carved once at load (`slotLayout` / `buildLayout` in `scene/shadowatlas.go`), and
that layout **is §4.1's partition** — the drawing there is now what the engine
builds, quadrant for quadrant, rather than an illustration. The tier is a ceiling
rather than an allocation, the rects never move (which is what Part E's caching
needs), and the atlas size became a pure resolution knob since every slot size is
a division of it. §4.1, §4.3 and §4.5 carry the notes; §4.3's tier sizes are the
one place this document is now stale on purpose, and §4.1 says why.

Scope: many shadowed lights, static and dynamic, scaling from a discrete GPU
down to an integrated laptop one. The decisions taken, the atlas that was
designed, and the numbers they imply.

Not here: the build order and per-step instructions (`LIGHTING_IMPL.md`), what
the shadow system does today (`../FEATURES.md` Part 1),
the `Backend` contract as it stands (`../ENGINE_FLOW.md` §0), technique
background (`../cheatsheets/GRAPHICS.md`).

---

## 0. Contents

1. [The shape of it](#1-the-shape-of-it)
2. [Decisions](#2-decisions)
3. [The three budgets](#3-the-three-budgets)
4. [The atlas](#4-the-atlas)
5. [Data model](#5-data-model)
6. [Clustering](#6-clustering)
7. [Frame flow](#7-frame-flow)
8. [Backend additions](#8-backend-additions)
9. [Quality tiers](#9-quality-tiers)
10. [Depth prepass and what it unlocks](#10-depth-prepass-and-what-it-unlocks)
11. [Open questions](#11-open-questions)

---

## 1. The shape of it

**Clustered forward shading, plus one shadow atlas split static/dynamic, plus
unshadowed lights as a cheap tier.** Four reasons this and not deferred:

- **It keeps MSAA.** The engine gained 4× MSAA in `b29c645`. A G-buffer forces
  per-sample resolve or a post-process AA instead. Clustered forward is
  multisample-native.
- **It degrades by scalar, not by code path.** Low-end is a coarser froxel grid
  and a smaller atlas — same shaders, same passes.
- **It leaves the forward path intact** for transparency and the IBL ambient
  term, both of which a G-buffer makes awkward.
- **It preserves the ambient/direct split** that AO needs (§10).

One expectation to correct up front: clustering reduces *shading* cost only. It
does not make a shadow map cheaper to bake. The shadowed-light budget is won in
§4, the light-count headline in §6, and §3 keeps them as separate budgets so the
design does not collapse into one wrong number.

---

## 2. Decisions

### 2.1 Static/dynamic is per-tile, not per-light

The obvious reading of "static lights baked once, dynamic lights baked always"
splits *lights* into two sets, and that misses the common case: a **static light
with a moving object walking under it**. Freezing that light's map leaves the
object shadowless.

So the split is per *tile*, over two atlas textures:

- **`staticAtlas`** — baked at load from immovable casters only. Never re-baked
  unless the scene reloads or the light's tile is reallocated.
- **`dynamicAtlas`** — rebuilt per frame, only for lights that have a movable
  caster in range. Each such light **copies its static tile across**, then draws
  the movable casters on top with an ordinary depth test. The result in the tile
  is the union of both shadow sets, read with one sample.

A light with no movable caster in range keeps pointing at `staticAtlas` and
costs nothing that frame. `ShadowRecord.Flags` bit 0 says which atlas to sample,
so the fragment path is one lookup either way.

The copy is texels, not a re-draw: cheap and constant per tile. Cross-texture,
which keeps it well-defined; copying a region of a texture onto another region
of *the same* texture is what would not be.

### 2.2 Cluster assignment doubles as the shadow relevance test

Clustering already computes, per frame, which lights touch which froxel. That
same result answers "which lights deserve a tile": a light intersecting zero
clusters is invisible this frame and needs no tile, no bake, no copy. Feeding
the cluster build into the tile allocator makes off-screen lights free rather
than merely cheap, out of the same loop.

### 2.3 Spot lights come before scaling point lights

A point light costs **six** tiles and six bakes; a spot light costs one. The
engine has `LightSun` and `LightPoint` only, and `LightData.Cutoff` already
exists unused. Adding `LightSpot = 2` makes the most common case — a shadowed
light in a room — six times cheaper in both atlas texels and bake time. Every
capacity figure below assumes spots exist.

### 2.4 Everything is a 2D tile, cubemaps are retired

A point light becomes six ordinary 2D tiles at 90°, not a cubemap. This follows
from the atlas rather than standing on its own, and the reason is **one code
path, not six benefits**:

A cube *array* forces every layer to the same size, so keeping cubemaps means
one array per resolution tier — and then point lights live outside the atlas
entirely. Separate textures, a separate allocator, a separate dirty-tracking and
re-bake mechanism whose cost cannot be counted in the same texel currency, and a
fork on light type inside `shadowLookup`. The allocator (§4.3) and the
static/dynamic caching (§2.1) would each grow a second implementation for the
light type that occupies six sevenths of the tile budget in §4.1. That is the
argument. Uniformity is the point.

Be honest about what is *not* an argument for it. Face culling works fine with a
cubemap — Vulkan renders to a single cube face through a 2D image view. So does
deleting `depth_cube.slang`, its geometry stage and the Vulkan
`FrontFace = Clockwise` special case they force: six ordinary passes to six face
views does that whichever way the texture is shaped. And bindless descriptors
make the sampler count a soft limit, as §4.1 already concedes for the 2D case.
All three are worth having and none of them decides this.

**The price is real and paid in §4.2.** Hardware seamless cube filtering is free
and exact; tile padding is neither. The `90° + 2 texel` widening fixes face
*edges* but only approximates *corners*, where three faces meet and no single
widened frustum can supply the right neighbourhood, and every PCF tap carries a
clamp it would not otherwise need.

Two consequences worth fixing in place while the decision is fresh:

- **Store linear radial distance to the light, not per-face projected depth** —
  which is what `ShadowRecord.FarPlane` is for. Radial distance is
  face-independent, so depth stays continuous across a face boundary and the
  bias tunes once instead of six times.
- The escape hatch is cheap if corner filtering disappoints: `ShadowRecord`
  already carries `Face`, so a `Flags` bit meaning "sample a cube texture
  instead" reverses this for point lights alone. See §11.3.

### 2.5 Records travel by device address, not a texel buffer

The record array is data-driven and outgrows a uniform block — the §4.1
partition alone is 337 records. It goes in a storage buffer reached by **buffer
device address**, pushed as a third pointer beside the two the engine already
pushes:

```slang
struct PushConstants { FrameUniforms *frame; DrawUniforms *draw; ShadowRecord *records; };
```

This needs no new descriptor, no new binding, and no `go-vulkan` work — the
uniform arena already works exactly this way (`BACKEND_DECISION.md` §7). The
cluster arrays (§6) take a fourth pointer when they land.

### 2.6 A depth prepass early, not late

One extra geometry pass that pays for itself by eliminating overdraw in the
forward pass, and then becomes the input every later technique wants — AO, SSR,
volumetrics, TAA. Retrofitting it after those exist is far more disruptive. §10
has the detail.

### 2.7 Deferred shading is out

It costs MSAA, an MRT rewrite, and the clean ambient/direct split §10 depends
on, in exchange for a bandwidth-versus-ALU trade that only pays at light counts
this design reaches by other means.

---

## 3. The three budgets

Keeping these separate is what stops the design collapsing into one number.
Targets are 1080p on a discrete GPU, for the finished system:

| budget | limited by | sun | spot | point |
|---|---|---|---|---|
| **unshadowed** | shading ALU × clusters | n/a | 1000s | 1000s |
| **static shadowed** | atlas texels (VRAM) | 1–4 | ~256 | ~42 |
| **dynamic shadowed** | re-bake texels per frame | 1 | ~64 | ~10 |

Figures are for a 4096² atlas at 256²/tile and a 4.19 M texel re-bake budget;
§4.5 has the full tables. The 6× gap between the spot and point columns is
exactly §2.3's argument.

The third row is the scarce one. Everything in §4 exists to move lights out of
it into the second — a light that stops moving stops costing.

An unshadowed light is not a degenerate case to tolerate. It is the correct tier
for small details, distant objects and fill lighting, and the allocator demotes
lights into it deliberately when the budget runs out.

---

## 4. The atlas

### 4.1 Layout

**Two textures for the whole engine**, `staticAtlas` and `dynamicAtlas`,
allocated once at startup. Not two per light, not one per light. Every shadow in
the scene is a *sub-rect* of one of them. One shadowed light or two hundred, the
texture count does not change, and `Light.shadowTarget`, `Light.depthMap` and
`Light.depthCubeMap` all disappear.

That is the difference from today, which calls `CreateRenderTarget` once per
casting light — one texture, one framebuffer and one binding each. Bindless
descriptors make that a soft wall rather than a hard one, so the atlas is a
bandwidth, cache and pass-count argument now, not a capacity one: N tile bakes
become one pass with N viewport changes, and a viewport change is nearly free
where a target bind is a render-pass boundary.

Each atlas is one 2D depth target carved into power-of-two tiles (4096 → 2048 →
… → 128). As shipped the carve happens **once at load**, into the fixed slot
counts `slotLayout` declares, and only occupancy is decided per frame; the plan
below assumed a quadtree repartitioning every frame, and §4.3's note records why
that changed. Tiles per light:

| light type | tiles | why |
|---|---|---|
| sun (`LightSun`) | 1 | one ortho projection |
| spot (`LightSpot`) | 1 | one perspective cone |
| point (`LightPoint`) | 6 | one 90° perspective per face |

A representative partition of a 4096² atlas. The four quadrants hold an equal
number of *texels* (4.19 M each) and a wildly unequal number of *tiles*, which
is the whole point of a quadtree:

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

It is drawn here to make the capacity concrete, and **the engine implements it**:
`slotLayout` in `scene/shadowatlas.go` declares exactly these counts, and
`buildLayout` carves them at scene load.

> **What the allocator produces against this drawing.** Measured on
> `go run . -scene stress.xml` (from the atlas dump that existed at the time,
> since removed in favour of RenderDoc): 64 lights, 259
> tiles, nothing degraded, nothing unshadowed. **The partition matches
> quadrant for quadrant** — 1 slot of 2048, 16 of 512, 64 of 256, 256 of 128,
> which is 4 + 4 + 4 + 4 of the atlas's 16 cells of 1024² and so exactly 100% of
> it. Two things still differ, and neither is the partition:
>
> - **Occupancy is 74.2%, not 100%.** This drawing is a 77-light spend and
>   `MaxLights` is 64, so the last quarter is unreachable until Part G moves the
>   light array to a storage buffer — §4.5 flags exactly this. Filling the gap
>   instead by letting lights climb above the ceiling their score earns
>   was tried and reverted: with this much spare every light reaches the largest
>   pool there is, the score stops selecting a resolution, and a lone spot ends up
>   holding the *sun's* 2048 slot at every distance.
>   `TestTileSizeTracksCameraDistance` is the guard.
> - **Point lights sit where this drawing puts them.** They briefly did not: a
>   point light was capped a tier below its score so six faces would not cost 6× a
>   spot's texels, which meant no point light could enter the 512 quadrant and it
>   stood 4/16 full while the 256 and 128 pools saturated. The cap was rationing
>   against the old repartitioning tree and is redundant here — a pool bounds what
>   it can hand out — so it went, and the quadrant fills 2 point lights + 4 spots
>   exactly as drawn.
>
> **§4.3 disagrees with this drawing, and the drawing wins.** §4.3 writes the
> tiers as 1024 / 512 / 256; the partition here steps 2048 → 512 → 256 → 128 and
> has no 1024 at all. Both cannot hold: a 1024 row of four slots costs 4 cells, a
> whole quadrant, and the only quadrant it could come from is the 128 row. So a
> 4096² atlas buys §4.3's high tier *or* the 40 far point lights drawn bottom
> right, never both. `shadowTiers` is set to 512 / 256 / 128 to follow the
> drawing. An 8192 atlas is what buys both, and `atlasSize` is the one knob that
> rescales the whole layout — see `../FEATURES.md` "Retuning the layout".

**Why a rect and not a texture-array layer.** A `sampler2DArray` requires every
layer to be the same size, so variable resolution would need one array per tier
— back to several textures. A rect inside one texture has no such constraint,
which is exactly how the sun at 2048 and a distant light at 128 share an image.

VRAM, D24:

| atlas | VRAM each | fits |
|---|---|---|
| 2048² | 16 MiB | sun @1024 + ~24 tiles @256 |
| 4096² | 64 MiB | sun @2048 + 2 lights @512 + ~40 tiles @256 |
| 8192² | 256 MiB | sun @4096 + 8 lights @512 + ~150 tiles @256 |

`dynamicAtlas` can be smaller than `staticAtlas`, since only lights with movable
casters need a tile in it.

### 4.2 Tile sampling rules

This is the price of §2.4 — what a cubemap's hardware seamless filtering would
have given free. Two rules, both mandatory, both silent corruption if missed:

- **Clamp every PCF tap to the tile rect, inset by one texel.** Without it the
  kernel reads a neighbouring light's tile and produces shadows from nowhere.
- **Widen each cube face's FOV to `90° + 2 texels`.** An atlas has no hardware
  cross-face filtering, so the kernel must stay inside its own tile at a face
  boundary or the seams show as hairline cracks.

### 4.3 Allocation policy

Per frame, per light, in `scene`:

```
score = radius / distance_to_camera        // ≈ screen-space footprint
tier  = high if score > 0.50               // e.g. 1024
        mid  if score > 0.20               //      512
        low  if score > 0.08               //      256
        none otherwise                     // unshadowed, ShadowIndex = -1
```

Lights sort by score and allocate greedily; what does not fit falls to the
unshadowed tier. That is the graceful-degradation property — running out of
budget costs shadow quality, never frame time.

Two details that matter in practice:

- **Hysteresis.** Change a light's tier only when its score crosses a threshold
  by more than 20%. Without it a light hovering on a boundary reallocates every
  frame, and every reallocation forces a full re-bake — the exact opposite of
  what the caching is for.
- **Cluster gate.** A light touching zero clusters (§2.2) skips allocation
  entirely, whatever its score.

> **How this shipped, and the one thing the plan did not anticipate.** The tier
> above is now a **ceiling**, not an allocation: `slotLayout` fixes the partition
> at load and lights take the best free slot no larger than the tier their score
> earns. Rank picks the slot, the tier caps it.
>
> What the plan missed is that a fixed pool has a failure a repartitioning tree
> does not. Once a pool is empty, two lights with near-equal scores trade its last
> slot every frame, and the loser cannot be served a step smaller out of the same
> space — a forced re-bake plus a visible flicker. The 20% margin above does not
> damp it, because that margin is against a *fixed threshold* and this contention
> is *between lights*. So there are two margins now, on two axes:
> `nextTierThreshold` for the tier, `slotStickiness` for the ranking.
>
> A second correction: a ceiling that stops a light *competing* for a big slot
> must not stop it *using* an idle one. Without that re-offer pass, a layout tuned
> for one light mix wasted its unused sizes on a scene with another — measured at
> 11 points of occupancy on `stress.xml`, which has no near point lights.

### 4.4 Bake scheduling

A tile is re-baked when its light moved, its tile was reallocated, or a movable
caster in range moved. `Scene.UpdateMeshes` already knows which meshes a physics
step touched; that set feeds the per-light dirty flag.

Two culls before any draw:

- **Caster cull** — skip meshes whose bounds miss the light's radius.
- **Face cull** — skip cube faces whose 90° frustum misses the camera frustum,
  typically 2–4 of the 6.

Then a **re-bake budget counted in texels**, not tiles: a 2048 tile is 256× the
work of a 128 tile and they cannot share a counter. Tiles queue by score until
the frame's allowance runs out; the overflow resolves over the next few frames.
This is what prevents a hitch when the camera turns into a room of twenty
dynamic lights.

Static scene, static lights ⇒ **zero bakes per frame** after load.

All of it is one pass over one target, the tile chosen by viewport rather than
by binding a different image:

```go
b.BeginPass(atlasTarget, nil) // once, for every tile
for _, t := range dirtyTiles {
	// The only thing that makes this an atlas rather than N render targets
	b.SetViewportScissor(t.X, t.Y, t.W, t.H)
	f.BakeMatrix = t.LightSpace
	b.BindFrameUniforms(&f)
	for _, m := range t.VisibleCasters {
		m.draw(&u)
	}
}
b.EndPass()
```

### 4.5 Capacity

Cost in texels, the only currency the atlas trades in:

| tier | sun / spot (1 tile) | point (6 tiles) |
|---|---|---|
| 2048 | 4.19 M | — (24 M, never worth it) |
| 1024 | 1.05 M | 6.29 M |
| 512 | 262 K | 1.57 M |
| 256 | 65.5 K | 393 K |
| 128 | 16.4 K | 98.3 K |

Spending a whole atlas on one light type — the honest upper bounds:

| whole atlas spent on | 2048² | 4096² | 8192² |
|---|---|---|---|
| point lights @512 | 2 | **10** | 42 |
| point lights @256 | 10 | **42** | 170 |
| point lights @128 | 42 | **170** | 682 |
| spot lights @512 | 16 | **64** | 256 |
| spot lights @256 | 64 | **256** | 1024 |
| spot lights @128 | 256 | **1024** | 4096 |
| one sun, full atlas | 1 | 1 | 1 |

Capacity scales 4× per step, so atlas size is a *resolution* knob at fixed light
count or a *count* knob at fixed resolution — not both at once.

Mixed, as in §4.1: **1 sun + 52 point + 24 spot = 77 shadowed lights** in one
4096² atlas. That is the realistic number to design against.

> **The engine builds this partition, and runs it under-filled.** `slotLayout` is
> these four rows exactly — 337 slots, 100% of a 4096². What a scene actually
> claims is another matter: `stress.xml` fills 74.2% with 64 lights and cannot go
> further, because 64 is `MaxLights` and this is a 77-light spend. The last
> quarter is unreachable until Part G, exactly as the trap below this table says.
> The gap is deliberately not redistributed either — see §4.1 for why handing the
> surplus out breaks the score's control of resolution.
>
> The thing to watch before authoring toward 77 is not texels. Every tile
> re-bakes every frame until Part E lands, and a tile re-draws every casting
> mesh, so 337 tiles is 337 × casters draws per frame — comfortable against
> `stress.xml`'s 4 casting meshes, ruinous against 200. **Part E is the
> prerequisite for actually filling this drawing**, not a bigger atlas.

**Static versus dynamic.** Everything above is the *static* budget, bounded by
VRAM alone — all 77 can be static, baked at load, costing nothing per frame. The
*dynamic* budget is separate and bounded by time:

| `rebakeBudget` | fully dynamic every frame |
|---|---|
| 1.05 M texels | 2 point @256, or 16 spot @256 |
| 4.19 M texels | **10 point @256**, or 2 point @512, or 64 spot @256 |
| 16.8 M texels | 42 point @256, or 10 point @512 |

Lights beyond the allowance are not dropped; they re-bake on later frames in
score order. 42 dynamic point lights at a 4.19 M budget refresh over 4 frames,
so the failure mode is blur on fast-moving shadows, not a stall.

> Ten fully dynamic shadowed point lights, refreshed every frame, cost about one
> quadrant of a 4096² atlas per frame.

**Total lights, shadowed or not**, is a different ceiling again, and it moves
with the parts in `LIGHTING_IMPL.md`:

| after part | scene lights | shadowed | limited by |
|---|---|---|---|
| today | 8 | 1 sun + 1 point | `MaxLights`, one target per light |
| **A** | 64 | 1 sun + 1 point | `MaxLights`, and the early-out's shading cost |
| **C–E** | 64 | up to 64 | `MaxLights`, not the atlas |
| **G** | **1000s** | 77 (§4.1) | atlas texels; `maxPerCluster` per fragment |

Note the trap: until Part G the atlas holds more shadowed lights than
`FrameUniforms` can name — 77 against 64, and against 16 if `MaxLights` is left
where the first draft of this plan put it. Parts C–E are still worth shipping,
they make those lights *good* rather than *many*, but the light-count headline
arrives with clustering.

---

## 5. Data model

Sizes below are the scalar-layout truth: Go packs `float32`/`int32` structs with
no padding and Slang's `-fvk-use-scalar-layout` matches it field for field. No
16-byte cells, no padding members — that rule died with the OpenGL backend
(`BACKEND_DECISION.md` §5.3).

### 5.1 `LightData` — 68 → 72 bytes *(landed)*

Four fields in, three dead ones out. `Specular`, `Linear` and `Quadratic` were
filled by `scene/scene.go` and read by no shader — a Phong leftover and two
terms of an attenuation model `forward.slang` never implemented:

```go
Cutoff      float32 // spot only, inner cone cosine
OuterCutoff float32 // spot only, outer cone cosine, where the falloff ends
Radius      float32 // attenuation cutoff, for the early-out and cluster bounds
ShadowIndex int32   // index of this light's first ShadowRecord, -1 = unshadowed
ShadowCount int32   // 1 for sun and spot, 6 for point
```

The last three are unread until Parts A and C, and are in early on purpose: this
is the riskiest edit in the tree, so it happens once. `FrameUniforms` is 1216 at
today's `MaxLights = 8`, and 5248 once Part A takes it to 64.

`Radius` is derived once at load, in `scene.lightRadius`: the distance at which
the light's brightest channel falls below `lightCutoff`. Both the shading
early-out and the cluster intersection test need it. It inverts the falloff
`forward.slang` really implements, `1 / (kConstant + d²)`, not the
constant/linear/quadratic model the old field names implied.

`lightCutoff` is `1/255` in **linear** space, which is not one 8-bit step:
`fsMain` ends on a Reinhard curve and a 1/2.2 gamma, and a linear 1/255 lands at
roughly 21/255 on screen. So the threshold is conservative rather than
perceptual — the right direction for culling, and a quality knob for §9 later.
Deriving a "correct" value through the tonemap is not meaningful while exposure
is fixed in the shader.

`Cutoff` was in the same state, hardcoded to cos 45° and unread. It and
`OuterCutoff` are now real per-light values, converted in `LightXml.toLight`
from the Blender terms the XML carries — `<cone>` in degrees and `<coneBlend>`
as a 0..1 fraction — so the scene file stays legible and the export round-trips.

`MaxLights` goes 8 → **64**, sized to what the §6 early-out makes affordable
rather than to storage: the shader loops to `lightCount`, never to `MaxLights`,
so the array size costs only its memcpy (~5 KB per pass) and nothing per
fragment. After Part G the light array moves to a storage buffer and
`MaxLights` stops bounding the scene at all — `maxPerCluster` becomes the only
per-fragment cap.

### 5.2 `ShadowRecord` — new, 96 bytes

One record per shadow *tile*: one for a sun or spot, six for a point light.

```go
type ShadowRecord struct {
	LightSpace mgl32.Mat4 // world -> this tile's clip space
	AtlasRect  [4]float32 // uv offset.xy, uv scale.xy
	TexelSize  float32    // 1 / tile pixels, the PCF step
	FarPlane   float32    // for the cube depth comparison
	Face       int32      // 0..5 for a cube face, -1 for a 2D tile
	Flags      int32      // bit 0: sample dynamicAtlas rather than staticAtlas
}
```

Records live in a storage buffer reached by device address (§2.5), not in
`FrameUniforms`.

### 5.3 `FrameUniforms` — a net simplification

| removed | added |
|---|---|
| `LightSpaceMatrix`, `ShadowMatrices[6]` | `BakeMatrix` — the one tile being baked |
| `ShadowDirIndex`, `PointShadowLights[4]` | `TexShadowStatic`, `TexShadowDynamic` |
| `TexShadowMap`, `TexShadowCubeMap` | `ClusterGrid [4]int32` (Part G) |
| `FarPlane`, `LightPos` | |

`FarPlane` and `LightPos` were the cube path's per-light state; the record
carries both now. The six-matrix array leaves because each cube face is an
ordinary tile draw with its own matrix, and nothing fixed-size replaces it — so
the block **shrinks at Part C** even though the light count is eight times what
it was: **1216** today, **5248** once Part A takes `MaxLights` to 64, then down
to **4828**. `LIGHTING_IMPL.md` carries the expected size at each part, and the
`init()` guard in `renderer/uniforms.go` must be updated to match each time.

---

## 6. Clustering

A froxel grid over the view frustum, default 16 × 9 × 24, exponential in Z. Per
frame the CPU tests every light's bounding sphere against every froxel and
writes two arrays:

- `clusterOffsets` — one `uint2` per cluster: offset into the index list, count.
- `clusterIndices` — a flat `uint` list of light indices.

The fragment shader derives its cluster from `gl_FragCoord.xy` and view depth,
reads offset and count, and loops only that cluster's lights.

**Build on the CPU first, move to compute later.** CPU is simpler and is not
obviously the bottleneck; a GPU build becomes a legitimate follow-up — and a
good first user of `Dispatch` — once `BACKEND_DECISION.md` §9 item 8 lands.
Transport is the same device-address storage buffer as the records (§2.5).

**Before any of this**, the attenuation early-out is worth doing alone:

```
// Skip a light once the fragment is outside its radius
// Most fragments touch a few lights, not all of them — this is what makes a
// plain forward loop affordable while clustering is still being built
if (light.type != LIGHT_SUN) {
    float3 d = light.position - fragPos;
    if (dot(d, d) > light.radius * light.radius) continue;
}
```

Six lines, no layout change, and it lifts the brute-force loop from 8 usable
lights to roughly 64. Clustering then takes it to thousands, and the early-out
stays as the inner guard and as a correct cheap path to compare against.

---

## 7. Frame flow

Changes to `core/App.Run`, in order. New steps marked ▸:

```
world.Update, Scene.UpdateMeshes
input
BeginFrame
▸ cluster build          CPU: light spheres vs froxels, upload both arrays
▸ tile allocation        score, hysteresis, slot pools; cluster-gated
▸ static atlas bakes     only on load, or on reallocation
▸ dynamic atlas tiles    per dirty light: copy static tile, draw movable casters
▸ depth prepass          backbuffer, depth only (§10)
  main pass              skybox (LEQUAL), forward scene, UI quad
EndFrame
```

The shadow work stays where the current bakes already are — before the main
pass, on its own target — so invariant 2 holds with the one amendment in §8.

---

## 8. Backend additions

Two methods. Re-read both against `BACKEND_DECISION.md` §6 before building them:
`PipelineSpec` and the `Pass` interface may absorb the first, and
`CreateStorageBuffer` (§9 item 8 there) is what §2.5's record buffer wants.

```go
// Restricts drawing to a sub-rect of the current pass's target
//
// Viewport and scissor together: the scissor is not optional, it is what stops
// a tile's clear and its PCF kernel reaching into its neighbours
SetViewportScissor(x, y, w, h int)

// Copies a depth rect between two render targets, without a draw
//
// The static-to-dynamic tile promotion, which must not re-render static casters
CopyDepthRegion(src, dst RenderTargetHandle, srcX, srcY, dstX, dstY, w, h int)
```

| method | Vulkan 1.3 | bindings |
|---|---|---|
| `SetViewportScissor` | `vkCmdSetViewport` + `vkCmdSetScissor`, already dynamic | present |
| `CopyDepthRegion` | `vkCmdCopyImage` + depth-aspect layout transitions | **missing from `go-vulkan`** |

`SetViewportScissor` amends invariant 2, which currently says viewports exist
only inside `BeginPass`. Amended: **a viewport is set by `BeginPass`, or
narrowed by `SetViewportScissor` within a pass on an atlas target.** Nothing
outside the shadow-atlas code may call it, and `../ENGINE_FLOW.md` §5 must
record this when it lands or the invariant quietly stops meaning anything.

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
record points at `staticAtlas`, moving objects cast nothing, per-frame shadow
cost goes to zero. Nothing else changes.

---

## 10. Depth prepass and what it unlocks

Staying compatible with ambient occlusion and later lighting work resolves
almost entirely into one decision: **add a depth prepass, and write packed
normals alongside it when AO arrives.**

- Depth only, first: draw `depth.slang` to the backbuffer, then run the forward
  pass with `EQUAL`. One geometry pass, and no shading of hidden fragments — at
  `maxPerCluster` (16) lights a fragment, that pays for itself above ~1.5
  overdraw.
- Then a single `RG16` attachment for octahedral-packed view-space normals.
  Depth + normals is exactly what GTAO and SSAO want. This is **not** a G-buffer
  and not a step toward deferred: no albedo, no material, no lighting read back.

| technique | needs |
|---|---|
| SSAO / GTAO | prepass depth + normals |
| screen-space contact shadows | prepass depth |
| SSR | prepass depth + normals, plus a colour copy |
| volumetric fog / light shafts | prepass depth + the cluster grid |
| TAA | prepass, plus a velocity attachment |
| ray-traced shadow fallback | `FeatureRayTracing`, BLAS/TLAS |

Two constraints to design around now rather than discover later:

- **MSAA.** The prepass must be multisampled to match the main pass or the
  `EQUAL` test fails along every geometric edge. AO then runs on resolved depth
  and applies per-pixel; budget for the resolve.
- **Where AO applies.** It multiplies the ambient/IBL term, not direct lighting
  — `forward.slang` already isolates `ambient`, so the insertion point is one
  multiply. Keeping that separation is the whole compatibility requirement, and
  clustered forward preserves it where deferred would not.

The froxel grid is reusable as-is for volumetrics: same clusters, same light
list, different integration. A second reason to build clustering rather than a
flat light list.

---

## 11. Open questions

1. **`dynamicAtlas` sizing.** A separate texture keeps the copy cross-texture
   and well-defined. One atlas with a reserved dynamic region halves the VRAM
   but makes the copy same-texture, undefined when the regions overlap.
   *Recommendation: two textures; revisit only if VRAM binds.*
2. **Sun cascades.** This plan gives the directional light one tile, which is
   the quality ceiling over a large scene. Cascades (3–4 tiles split by view
   depth) fit the atlas naturally but bring their own selection and blending
   logic. Out of scope here; flag it if the showcase scene grows.
3. **Point lights, if six tiles disappoint.** Two independent levers, both
   deliberately unpulled for now:
   - *Corner filtering.* If §2.4's padding is not good enough at face corners,
     put point lights back on fixed-resolution cubemaps and keep the atlas for
     sun and spot. A `ShadowRecord.Flags` bit selects the path. The cost is the
     second allocator and second caching path §2.4 exists to avoid, so measure
     before paying it — and note §2.3 makes point lights the minority case in a
     well-authored scene anyway.
   - *Tile count.* Dual-paraboloid halves a point light to 2 tiles instead of 6,
     at the cost of tessellation-dependent artifacts. The lever if point lights
     stay dominant despite spot lights existing.
4. **Cluster Z distribution.** Exponential is standard, but the near/far it is
   fit to interacts with the shadow `farPlane`, a hardcoded `50` in
   `core/app.go:114`. Both should come from the same scene bounds.
5. **Ordering against `BACKEND_DECISION.md`.** Its §9 items 3–8 (Vulkan-native
   clip space, `PipelineSpec`, the pass list, compute) are the substrate Parts B
   onward would be built on. Part A is independent and can land now.
