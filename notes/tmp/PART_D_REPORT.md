# Part D — implementation report

Everything changed in the session of **2026-08-16**, in the order it was built,
with the reasoning that is not visible from the diff.

**Scope:** `LIGHTING_IMPL.md` Part D — the per-frame shadow-atlas allocator —
plus three things it dragged in: a spot-light shadow projection and two bug
fixes.

**Not here:** the design rationale for the atlas itself (`LIGHTING_PLAN.md` §4)
and the account of Parts A–C (`LIGHTING_IMPL.md`).

> ### The allocator below was replaced the same day
>
> The per-frame buddy quadtree this report describes gave way to a slot layout
> carved once at load — `slotLayout` / `buildLayout` in `scene/shadowatlas.go`.
> `quadNode` survives only to *place* the slots, losing `release`, `free` and
> sibling coalescing with it. The tier became a ceiling rather than an
> allocation, `tierHysteresis` was renamed `nextTierThreshold`, and a second
> margin `slotStickiness` was added on a different axis. The layout is now
> `LIGHTING_PLAN.md` §4.1's partition exactly (1×2048 + 16×512 + 64×256 +
> 256×128), and the tiers dropped a step to 512 / 256 / 128 to match, so the same
> `stress.xml` scene reads 44.9% of a 77-light layout rather than §7's 87.1% of a
> fitted one — fewer texels per light, no light lost.
>
> This document remains the record of *why the quadtree was built and what it
> measured*, which is what made the replacement's case. `LIGHTING_PLAN.md` §4.1
> and §4.3, and `../FEATURES.md`, describe what runs now.

---

## 0. Summary

| | before | after |
| --- | --- | --- |
| who casts a shadow | first sun + first point light, fixed at load | every light the atlas can fit, scored per frame |
| tile size | one constant, `settings.ShadowWidth` (1024) | 2048 / 1024 / 512 / 256 / 128, from screen-space importance |
| allocator | row-major grid of 16 equal cells | buddy quadtree with coalescing free |
| showcase | 6 lights, 2 casting, 7 tiles (44% of atlas) | 9 lights, 9 casting, 19 tiles (81.2%) |
| frame rate | ~180 FPS | ~181 FPS |

Gate: `go build ./...` and `go test ./...` green, `spirv-val
--scalar-block-layout` clean on all ten SPIR-V modules, `go run .` with
`[debug] validation = true` silent.

---

## 1. The allocator — `src/scene/shadowatlas.go`

### 1.1 The quadtree

`quadNode` is a buddy tree over the 4096² atlas: a node is free, allocated
whole, or split into four quadrants. `alloc(size)` walks down splitting as it
goes and takes the first free leaf of exactly that size; `release(x, y, size)`
marks it free and then collapses the parent's four children back to `nil` once
nothing below it is live.

A quadtree rather than a shelf packer or a free list because every tile is a
power of two and the sizes are few: splitting *is* the allocation and merging
*is* the free, both `O(log n)`, and there is no packing heuristic to get wrong.
First-fit rather than best-fit for the same reason — every node at a given depth
is the same size, so there is no better fit to find.

**The coalesce is the load-bearing part.** Without it, an atlas that has cycled
through many small tiles ends up with every texel free and no contiguous slot, so
a light promoting to a large tier can never be served again. That failure never
raises an error; the allocator quietly starts refusing large tiers and every
shadow degrades.

### 1.2 Scoring, tiers, hysteresis

```
score = Radius / distance to camera        // rough screen-space footprint
tier  = 1024 if score > 0.50
        512  if score > 0.20
        256  if score > 0.08
        none otherwise
```

`Radius` is Part A's per-light attenuation cutoff, which is why Part A had to
land first: it is the only thing that makes "how much of the view does this light
cover" answerable on the CPU.

A tier change requires the score to cross its threshold by 20% (`tierHysteresis`).
Promotion needs the score to clear the *new* tier's threshold by that margin;
demotion needs it to have fallen the same margin below the threshold of the tier
currently held. Between the two the light keeps what it has.

### 1.3 Four things the plan did not say

**The tree persists across frames, and it has to.** Hysteresis is meaningless
against an allocator that resets every frame — the point of not changing tier is
not changing *pixels*, which only means something if the tiles survive. So
`shadowAtlas` carries `allocs map[int32]*lightAlloc` and only touches a light
whose tier actually moved. This is also the precondition for Part E baking a tile
once and leaving it alone.

**Two tier states, not one.** A point light takes one step below its scored tier,
because six faces cost 6× the texels of a spot at the same size — without that a
handful of point lights spend the whole atlas and the plan's "10 point lights in
a 4096" does not come out. But feeding the halved size back in as `cur` next
frame reads as a demotion, and the light oscillates. So `lightAlloc` keeps three
numbers: `tier` (the hysteresis state, in `shadowTiers`' units), `want` (the
per-face size that tier buys) and `size` (what it actually got).

**Allocation is three passes, not one.**

1. Free everything whose tier moved, *before* allocating anything, so a light
   promoting this frame can be served out of the space a demoting one just gave
   back.
2. Allocate in score order, degrading a step at a time — 512 → 256 → 128 — and
   only then falling to `ShadowIndex = -1` and lighting unshadowed. Running out
   of atlas costs the least important light its resolution and never costs frame
   time.
3. Retry the degraded ones at full size. Without this a light that lost a tier
   during one crowded frame stays coarse for the rest of the session. The retry
   takes the new tiles *before* releasing the old, so a failed upgrade leaves
   what it has alone.

**A sun is not scored at all.** A directional light has `Radius = 0` (Part A step
4), so `Radius / distance` is 0 and the sun would be the first light demoted to
unshadowed. It takes a fixed 2048, which is also `LIGHTING_PLAN.md` §4.1's
drawing — it is the one light every pixel sees.

### 1.4 Deleted

`Scene.pickShadowCasters`, `Scene.casts`, `Scene.ShadowCasters` and the fields
`shadowDirIndex` / `shadowPointIndex`, plus the old grid allocator
(`shadowAtlas.tile`, `.perRow`, `.used`, `.alloc`).

---

## 2. A spot light needed its own projection

`Light.shadowRecord` had two branches: a cube face, and "everything else = the
sun's orthographic box". A spot falling into the second branch is simply wrong —
a cone is a perspective frustum — and nothing had ever noticed, because
`pickShadowCasters` only ever picked a sun and a point light.

Part D makes every light a candidate, so the branch had to exist:

```go
fov := 2 * acos(clamp(l.OuterCutoff, -1, 1))   // the cone's own full angle
fov *= (tile.size + 2) / tile.size             // +2 texels, as a cube face is
proj := mgl32.Perspective(clamp(fov, 0.01, 3.0), 1, nearPlane, farPlane)
rec.WorldToTile = proj.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), spotUp(l.Dir)))
```

The two-texel widening is for the same reason a cube face gets it: the PCF kernel
must stay inside the tile at the cone's rim. `spotUp` returns `{0,0,1}` when the
cone points within 8° of straight up or down, because `LookAtV` produces NaNs
when its up vector is parallel to its forward vector — and "aimed straight down"
is the single most likely thing anyone authors.

---

## 3. Two bugs

### 3.1 The ×5 point-shadow scale (`forward.slang`)

`shadowLookup` multiplied the point-light branch by `5.0`, so
`Lo += contrib * (1.0 - shadow)` went *negative* on a fully shadowed fragment and
subtracted light that other lights had put there. It predates the atlas; Part C
preserved it verbatim so its gate could be an unchanged image, and flagged it for
"Part D or E".

It had to be Part D. With one point light casting it was an invisible darkening.
With every point light casting it is black blotches.

### 3.2 The ground plane self-shadowing — the one that mattered

A point light above the ground baked the ground plane into its own shadow map,
and the ground then shaded against itself. Acne across the entire plane. Under
one casting point light that was a slight darkening nobody had noticed; under
nine casting lights the showcase rendered **almost black** — mean pixel value
9–19 out of 255.

**How it was found.** Not by reading the shader. `[debug] noShadows` — every
`ShadowIndex` forced to -1 while the tiles still bake — rendered the same scene
bright, which said in one run that the lights were fine and the occlusion was
wrong. Two failures that produce an identical picture and have nothing in common
in the code.

**The first fix was wrong**, and worth recording because it is the obvious one.
`BakeShadows` set `CullFront` for the 2D tiles only, so cube faces baked
`CullBack`; making every tile `CullFront` cured the acne immediately — a
single-sided plane has no back face, so front-face culling drops it out of the
map entirely. Mean went 9–19 → 108–144.

It also introduced **peter-panning**. Front-face culling bakes the *far* side of
a closed caster, so the depth stored is a whole thickness too far, and every
sphere floated above a lit disc of its own diameter. Obvious in the rendered
image once someone looked at a contact point.

**The real fix is `Mesh.CastsShadow`** — `<castsShadow>` in the XML, default
true, written by the exporter from Blender's own "Shadow" ray visibility. The
ground opts out and every tile goes back to `CullBack`, which keeps contact
shadows welded to their objects.

The reasoning generalises past this scene: a single-sided plane with the whole
scene above it can only ever occlude *itself*. Nothing is below it to receive its
shadow, so every texel it writes is a chance to shade against itself — and at the
grazing angles a high light gives a large plane, the depth gradient across one
texel dwarfs any constant or normal-offset bias.

Still open: a flat surface that legitimately must cast, such as a wall or a floor
with a room under it. That wants slope-scaled depth bias (`vkCmdSetDepthBias`),
which `go-vulkan` does not bind yet.

---

## 4. The showcase

The old scene had six lights placed for *colour* — four point lights at roughly
eye level beside the props, a spot over bare ground, a bright sun. Under Part D
all six cast, and the render still showed essentially one shadow.

Rebuilt as **six spots + two point lights + one sun**, all nine shadowed: 6 tiles
of 1024, 2 × 6 tiles of 512, 1 tile of 2048 — 19 tiles, 81.2% of the atlas. That
is the allocator near its limit, which is the point.

Three placement rules, every one of them learnt by rendering the scene and
looking at it:

1. **A light casts a visible shadow only when it is well above the props and off
   to one side.** Lights at eye level beside a prop throw their shadows away
   horizontally, where no visible ground catches them. This is what made the
   first version look like the feature was broken.
2. **Spots beat point lights for this.** A spot contributes exactly zero outside
   its cone, so its pool is a region where it dominates and its shadow is most of
   the light lost there. A point light lights everything a little, and that
   pedestal is what no shadow can cut through.
3. **Read the shadows by hue, not by brightness.** `fsMain` ends on a Reinhard
   curve, which compresses hard above 1: with several lights on one fragment,
   blocking one barely moves the luminance. Blocking a *saturated* one leaves a
   strongly tinted shadow, which is unmistakable. Hence six near-primary spots
   rather than six pastel ones.

Also: the camera moved back and up (GL `(0, 6, 14)`, pitch 19°) so the whole set
including the cube is in frame, and the sun dropped 4.5 → 0.5, because at its old
intensity it drowned every coloured shadow.

---

## 5. Tests

The tests that guard the part, as they stand under the slot layout:

| test | what it guards |
| --- | --- |
| `TestSlotLayoutPacksTheAtlas` | every declared slot is inside the atlas and no two overlap — a layout carved once and trusted for the whole session |
| `TestSlotsDoNotMoveWhileHeld` | a light keeps the very same rect while its plan does not change, which is what Part E will cache |
| `TestTierHysteresis` | a score drifting inside the band keeps one tier; crossing by >20% moves it |
| `TestTileSizeTracksCameraDistance` | walking toward a light sharpens it, walking away coarsens it, walking far enough drops it |
| `TestShowcasePointLightsGetSixFaces` | a point light holds six tiles or none — five leaves a lit wedge |
| `TestShowcaseLoads` | rewritten: it asserted the deleted fixed-caster pick, and now checks one light of each type exists, since each has a distinct bake path |

---

## 6. Every file touched

**`src/scene`**
- `shadowatlas.go` — the allocator: scoring, hysteresis, three-phase allocation, the spot projection
- `scene.go` — fixed-caster budget deleted; the `noShadows` switch
- `shadowatlas_test.go`, `showcase_test.go`

**`src/core`**
- `app.go` — `lockCamera`

**`src/shaders/slang`**
- `forward.slang` — the ×5 scale deleted

**assets and docs**
- `assets/showcase.xml` — the nine-light rig, the camera, the placement rules
- `CLAUDE.md`, `notes/OVERVIEW.md`, `notes/ENGINE_FLOW.md`, `notes/ARCHITECTURE.md`, `notes/FEATURES.md`, `notes/TODO.md`, `notes/tmp/LIGHTING_IMPL.md`

---

## 7. How the result compares to `LIGHTING_PLAN.md` §4.1

§4.1 draws a representative 4096² partition — 1 tile of 2048 for the sun, 16 of
512, 64 of 256, 256 of 128, holding 77 lights in 337 tiles — and says the
allocator rebalances it per frame from the §4.3 score.

**The showcase cannot reach that shape, and the reason is informative.** Its nine
lights all sit within ~15 units of the camera with radii over 100, so every score
lands between 4.7 and 62 against a top-tier threshold of **0.50**. The tiering
saturates: every light takes the biggest tile it is entitled to, and the partition
comes out as one 2048, six 1024 and twelve 512 — three sizes, 19 tiles, no
quadtree depth below level 3.

So `assets/stress.xml` exists to show the other case. Its 41 lights are placed in
rings and their intensities are *derived from the tier they should land in*:
`scene.lightRadius` inverts the shader falloff, so solving it backwards gives the
intensity that puts a light at a chosen distance into a chosen tier. Rings at
scores 0.75 / 0.30 / 0.13 land in 1024 / 512 / 256, and the point-light rings
halve one step from there. `go run . -scene stress.xml`:

| tile size | count | texels | % atlas | §4.1 draws |
| --- | --- | --- | --- | --- |
| 2048 | 1 | 4.19 M | 25.0 | 1 (the sun) |
| 1024 | 4 | 4.19 M | 25.0 | — |
| 512 | 8 | 2.10 M | 12.5 | 16 |
| 256 | 48 | 3.15 M | 18.8 | 64 |
| 128 | 60 | 0.98 M | 5.9 | 256 |
| | **121** | **14.6 M** | **87.1** | 337 |

41 lights, all casting, still ~181 FPS. The shape matches: the sun's 2048
quadrant exactly as drawn, then a descending cascade of tile sizes filling the
rest, and the quadtree going four levels deep. The counts differ because §4.1
spends its whole atlas on the two smallest tiers and this scene spreads across
all five — 121 tiles at 87% against 337 at 100%.

Two things the comparison actually turned up:

- **There is no 128 scoring tier.** `shadowTiers` has three entries (1024, 512,
  256); `minTileSize` is 128 and is reachable only by a point light halving from
  the 256 tier, or by degradation when the atlas is full. The plan's §4.1 drawing
  and its §9 `tierHigh`/`tierMid`/`tierLow` disagree about whether 128 is a tier
  or a granularity. It is currently a granularity.
- **The thresholds are calibrated for lights that are far relative to their
  radius**, which is what §4.3 implies and what the showcase is not. A scene of
  ordinary room-scale lights sits at the top of the range and never exercises the
  allocator's reason for existing. That is the same observation Part A made about
  the early-out — "the showcase cannot show that" — and the same fix: a scene of
  many dim, localised lights, which `stress.xml` now is.

---

## 8. What Part D did not do

- **The score ignores whether a light is on screen.** That is
  `LIGHTING_PLAN.md` §2.2's cluster gate and belongs to Part G.
- **Every tile still re-bakes every frame**, the allocator's persistence
  notwithstanding. Using it is Part E.
- **No cascades.** The sun is one 2048 ortho tile over a hardcoded `[-10, 10]`
  box.
- **Tier thresholds and tile sizes are constants**, not config. Part H.
