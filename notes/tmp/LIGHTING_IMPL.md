# Lighting implementation plan — Parts A–H

**Status: proposal, under iteration. Nothing here is implemented yet.**

> **Stale as of 2026-08-05.** The OpenGL backend was deleted
> (`BACKEND_DECISION.md`), so every "backend work: both" cell below is now a
> single implementation, and the GL 4.1 limits that shaped `LIGHTING_PLAN.md`
> are void. Read that file's banner first. Part A remains correct and
> independently useful; Parts B onward should be re-sequenced after
> `BACKEND_DECISION.md` §9 items 4-6 (pipeline objects, pass list, compute),
> which are the substrate they would now be built on.

The step-by-step build order for `LIGHTING_PLAN.md`, split out so each file
stays readable. Eight parts, meant to be picked up one at a time.

Scope: what to change, in what order, and how to know each part landed.

Not here: *why* the design looks like this — the architecture, the capacity
numbers, the atlas layout and the rejected alternatives all live in
`LIGHTING_PLAN.md`, which every `§n` reference below points into.

never left half-landed: the tree always runs.

## Parts

| part | theme | backend work | risk | unlocks |
|---|---|---|---|---|
| [A](#part-a--the-light-model) | light model, spot lights | none | layout | ~64 lights |
| [B](#part-b--atlas-plumbing) | atlas plumbing | both | VK layouts | tiles drawable |
| [C](#part-c--records-and-atlas-sampling) | records, atlas sampling | both | bleeding | one sampler, N tiles |
| [D](#part-d--the-allocator) | allocator | none | fragmentation | variable resolution |
| [E](#part-e--staticdynamic-split-and-caching) | static/dynamic, caching | both | classification | the shadow budget |
| [F](#part-f--depth-prepass) | depth prepass | both | MSAA + `EQUAL` | overdraw, AO input |
| [G](#part-g--clustered-forward) | clustered forward | none | Z distribution | 1000s of lights |
| [H](#part-h--quality-tiers) | quality tiers | none | dead knobs | the low-end story |

---

## Ordering rationale

Three of the eight have a fixed position in the sequence, for reasons worth
stating rather than rediscovering:

**Spot lights go first (Part A).** A spot light is one tile, a point light six
(§5.5) — so a spot is six times cheaper in atlas texels, bake time and re-bake
budget, for the case that dominates real scenes. Every capacity figure in this
document assumes spots exist. Adding them after the allocator means tuning the
allocator twice, and adding them after the atlas means a second pass over the
shadow record code. They are also the one part with zero coupling to any of the
rest: pure `scene` + `forward.slang`, no backend work.

**The depth prepass goes at Part F** — after all shadow work, immediately before
clustering. Not earlier: with ≤16 lights and the Part A early-out, the forward
pass is not yet overdraw-bound, so a prepass would cost a geometry pass and
return little. Not later: clustered shading (Part G) is exactly what makes
overdraw expensive, since every hidden fragment would otherwise run the full
per-cluster light loop. Landing the prepass one part before clustering means
Part G's cost is measured against a scene that already skips hidden fragments,
and it puts the depth buffer §10 needs in place before any AO work begins.

**Config knobs go last (Part H)** because a knob for a system that does not yet
exist is a guess. Every part before it hardcodes its constant in
`settings/settings.go`; Part H moves them all to TOML in one pass, once their
real ranges are known.

Parts B–E are the shadow budget, in dependency order — plumbing, then records,
then allocation, then caching. Part G is the light-count headline. Deferred
shading appears nowhere, on purpose: it costs MSAA, an MRT rewrite of both
backends, and the clean ambient/direct split §10 depends on, in exchange for a
bandwidth-versus-ALU trade that only pays at light counts this design reaches by
other means.

## The gate

Every part ends with the same check, run from `src/`:

```sh
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh
go build ./... && go test ./...
go run .                                       # eyeball the showcase scene
OVERDRIVE_VK_VALIDATION=1 go run .             # clean log
```

The validation log is the one that matters most now. The cross-backend image
diff that used to catch a mislaid uniform field no longer exists, so any part
touching `common.slang` or `renderer/uniforms.go` has to be eyeballed against
the showcase scene deliberately — see `../ENGINE_FLOW.md` §6.

---

## Part A — The light model

**Goal.** Spot lights, per-light radius, and a `LightData` big enough for
everything the rest of the plan needs, so the struct layout is disturbed exactly
once.

**Why first.** Six times cheaper shadowed lights (§2.3, §5.5); no backend work;
no dependency on any other part. The early-out alone lifts the usable light
count from 8 to roughly 64, which makes every later part measurable against a
scene that is actually lit.

**Touches.** `renderer/uniforms.go`, `shaders/slang/common.slang`,
`shaders/slang/forward.slang`, `scene/light.go`, `scene/scene.go`,
`plugin/xml_export.py`, `assets/showcase.xml`, the `init()` size guard in `renderer/uniforms.go`.

**Steps.**

1. Grow `LightData` 80 → 96 bytes. Three `Reserved` slots cannot hold the four
   fields the plan needs, so add one 16-byte cell rather than squeezing:

   ```go
   Constant, Linear   float32
   Quadratic, Cutoff  float32   // Cutoff is the inner cone cosine
   Type               int32
   OuterCutoff        float32   // outer cone cosine, for the spot falloff
   Radius             float32   // attenuation cutoff, §4.1
   ShadowFirst        int32     // first ShadowRecord, -1 = unshadowed (Part C)
   ShadowCount        int32     // 1 sun/spot, 6 point (Part C)
   Reserved0, Reserved1, Reserved2 float32
   ```

   Two full cells, three slots spare. Mirror it in `common.slang` and update
   both `init()` guards: `LightData` 80 → 96, `FrameUniforms` 1280 → 2176
   (the light array goes 8×80 → 16×96).
2. `MaxLights` 8 → 16 in `renderer/uniforms.go` and `MAX_LIGHTS` in
   `common.slang`.
3. Add `LightSpot = 2` beside `LightSun` / `LightPoint`. Add `Cutoff`,
   `OuterCutoff` to the `scene.Light` struct and `<cutoff>`, `<outerCutoff>` to
   `LightXml`; parse `"spot"` in `LightXml.toLight`. Delete the `cos45` constant
   in `scene/scene.go` — the cutoff stops being hardcoded.
4. Derive `Radius` once, at load, in `toLight`: the distance at which
   `intensity / (constant + linear·d + quadratic·d²)` falls below `1/255`.
   Solve the quadratic; clamp to something sane when `quadratic` is 0.
5. `forward.slang`: add `calcSpotLight` — `calcPointLight` times a smoothstep
   between `outerCutoff` and `cutoff` on `dot(-L, direction)`. Switch on
   `light.type` in `fsMain`.
6. `forward.slang`: the attenuation early-out from §6, before both the BRDF and
   any shadow lookup, skipped for `LIGHT_SUN`.
7. `plugin/xml_export.py`: export Blender `SPOT` lamps, mapping `spot_size` and
   `spot_blend` to the two cosines. Add a spot light to `assets/showcase.xml`.

**Gate.** The standard gate, plus: the showcase scene renders a visible cone;
`MaxLights` 16 does not regress FPS materially (the early-out should make it
*faster* than 8 lights without one).

**Risks.** The `LightData` growth is the single most dangerous edit in the whole
plan — a mislaid field silently renders garbage. The `init()` size guard catches
a wrong *size*, not a wrong *order*, so eyeball the scene and do not batch this
step with anything else in one commit.

**Unlocks.** ~64 lights. Every capacity number in §5.5.

---

## Part B — Atlas plumbing

**Goal.** The three new `Backend` methods and the atlas render target, with
nothing yet using them.

**Why here.** Pure backend work, symmetric across the two APIs, verifiable on
its own. Landing it separately keeps Part C's shader and scene changes from
being debugged simultaneously with fresh Vulkan object lifetimes.

**Touches.** `renderer/backend.go`, `vulkan/backend.go`, `vulkan/draw.go`,
`../ENGINE_FLOW.md`.

**Steps.**

1. Add `SetViewportScissor`, `CopyDepthRegion` and
   `CreateTexelBuffer`/`UpdateTexelBuffer` to the `Backend` interface (§8, with
   the per-API mapping table).
2. Implement it: `CmdSetViewport` + `CmdSetScissor` on the sub-rect
   enabled for the atlas pass; `glBlitFramebuffer` with `GL_DEPTH_BUFFER_BIT`;
   buffer + `glTexBuffer` with `GL_R32UI`.
3. Implement on Vulkan: `vkCmdSetViewport`/`vkCmdSetScissor` (both already
   dynamic state — note the shadow passes use a *positive* height viewport,
   unlike the main pass); `vkCmdCopyImage` with the depth aspect and the layout
   transitions around it; a uniform texel buffer plus its buffer view, and a
   descriptor slot for it.
4. Allocate the atlas via the existing `RenderTargetSpec` with `Cube: false` —
   it is an ordinary large 2D depth target, no new spec field needed.
5. Record the invariant-2 amendment in `../ENGINE_FLOW.md` §5: a viewport is set by
   `BeginPass`, or narrowed by `SetViewportScissor` within a pass on an atlas
   target, and by nothing else.

**Gate.** The standard gate. A throwaway test that bakes the existing sun shadow
into a corner of a 4096 atlas and samples it back proves all three methods
without any of Part C.

**Risks.** Vulkan image layout transitions around `vkCmdCopyImage` are the usual
validation-layer trap. Run with `OVERDRIVE_VK_VALIDATION=1` throughout this
part, not just at the end.

**Unlocks.** Tiles are drawable and copyable; records have somewhere to live.

---

## Part C — Records and atlas sampling

**Goal.** One shadow map for the whole scene. Every light samples a rect of it.

**Why here.** Needs Part B's plumbing; everything after needs this data model.

**Touches.** `renderer/uniforms.go` (and its `init()` size guard), the shaders,
`scene/light.go`, `scene/scene.go`, `core/app.go`, `vulkan/draw.go`,
delete `shaders/slang/depth_cube.slang`.

**Steps.**

1. Define `ShadowRecord` (§4.2) and put the array in the texel buffer from Part
   B, not in `FrameUniforms`. `FrameUniforms` gets `TexShadowRecords`.
2. Rework `FrameUniforms` per §4.3: `LightSpaceMatrix` + `ShadowMatrices[6]` out,
   `BakeMatrix` in; `ShadowDirIndex`, `PointShadowLights[4]`,
   `TexShadowCubeMap` out; `TexShadowStatic`, `TexShadowDynamic` in. Expected
   size 1792 — *smaller* than today's 1280+ despite 16 lights, because the
   six-matrix array leaves. Update the `init()` guard and the std140 test.
3. Replace `Light.RenderShadowMap` with a tile bake: one `BeginPass` on the
   atlas for the whole frame, `SetViewportScissor` per tile, one `BakeMatrix`
   per tile (§5.4's loop). A point light becomes six ordinary tile bakes at 90°
   with the `+2 texel` FOV widening from §5.2.
4. Delete `depth_cube.slang`, its `CreateShader("depth_cube", true)` call, and
   the Vulkan `FrontFace = Clockwise` shadow-pass declaration that existed only
   to match its memory layout (§11).
5. `forward.slang`: one `shadowLookup(record, fragPos, normal)` replacing
   `shadowCalculation` and `shadowCalculationCube`. Cube face selection is the
   major axis of `fragPos - light.position`, indexing
   `records[light.shadowFirst + face]`. Keep the 4-tap early-bail; **clamp every
   tap to the record's rect inset by one texel** (§5.2).
6. `common.slang`: `shadowMap2D` and `shadowCubeMap[]` collapse to two
   `Sampler2D`, and the dedicated cube descriptors go.
7. Interim allocation: hardcode a fixed partition (sun tile + N point-light tile
   groups) so the part is testable before Part D exists.

**Gate.** The standard gate, and specifically: the showcase scene's shadows are
pixel-wise indistinguishable from before this part. This is a
pure refactor of *where* shadow data lives.

**Risks.** Tile bleeding (missing rect clamp) shows as shadows from the wrong
light; cube seams (missing FOV widening) show as hairline cracks at face
boundaries. Both are subtle enough to ship by accident — check a point light
against a wall corner deliberately.

**Unlocks.** One sampler, N tiles, no per-light textures.

---

## Part D — The allocator

**Goal.** Tile sizes chosen per frame from screen-space importance.

**Why here.** Needs Part C's records; Part E's caching needs to know when a tile
was reallocated.

**Touches.** new `scene/shadowatlas.go`, `scene/scene.go`.

**Steps.**

1. Quadtree allocator over the atlas: split 4096 → 2048 → … → 128, `Alloc(size)`
   and `Free(rect)`, coalescing freed siblings.
2. Per-light score and tier, with the thresholds of §5.3.
3. Hysteresis: a tier change requires the score to cross its threshold by more
   than 20%. Without it a light on a boundary reallocates every frame and forces
   a full re-bake every frame — the exact opposite of Part E's goal.
4. Sort by score, allocate greedily, demote what does not fit to
   `ShadowFirst = -1`. Running out of budget must cost shadow quality and never
   frame time.
5. Delete `Scene.pickShadowCasters`, `Scene.casts` and `Scene.ShadowCasters` —
   the fixed 1-dir + 1-point budget they encode is what this part replaces.

**Gate.** The standard gate, plus: walking the camera toward a light visibly
sharpens its shadow, and walking away coarsens it, without popping every frame.
Log the allocation map for one frame and check it against §5.1.

**Risks.** Quadtree fragmentation over a long session — many alloc/free cycles
at mixed sizes leaving no contiguous slot. Coalescing on free is what prevents
it; write the unit test for that specifically, it is cheap and CPU-only.

**Unlocks.** Variable resolution, which is the whole point of an atlas.

---

## Part E — Static/dynamic split and caching

**Goal.** The shadowed-light budget. Static lights bake once; dynamic lights
bake within a texel allowance.

**Why here.** The single largest win in the plan, and it needs D's
reallocation signal to know when a cached tile is invalid.

**Touches.** `scene/`, `core/app.go`, `vulkan/` (`CopyDepthRegion` from
Part B).

**Steps.**

1. Two atlases, `staticAtlas` and `dynamicAtlas` (§2.1). `ShadowRecord.Flags`
   bit 0 selects which one the shader samples.
2. Classify meshes static vs movable — the ECS/physics entities are the movable
   set, baked OBJ geometry is static.
3. Bake `staticAtlas` at load, from static casters only. Never re-bake unless a
   tile is reallocated or the scene reloads.
4. Per frame, for each light with a movable caster in range: `CopyDepthRegion`
   its static tile into its `dynamicAtlas` tile, then draw only movable casters
   on top with an ordinary depth test. The union falls out; one sample at
   shading time.
5. Dirty tracking: a light is dirty when it moved, its tile was reallocated, or
   a movable caster in range moved. `Scene.UpdateMeshes` already knows which
   meshes a physics step touched — feed that set in rather than adding a second
   mechanism.
6. Caster cull (mesh bounds vs light radius) and cube-face cull (face frustum vs
   camera frustum, §5.4).
7. Re-bake budget in **texels** per frame, queued by score (§5.5). Overflow
   resolves on later frames.

**Gate.** The standard gate, plus a bake counter printed beside the FPS: a
static showcase scene must settle at **zero** bakes per frame. Then move one
physics entity and confirm only the lights that see it wake up.

**Risks.** The static/dynamic classification is the part that will be wrong
first — a mesh classified static that later moves leaves a shadow behind, with
no crash and no log line. Make the counter and a debug atlas view part of the
work, not an afterthought.

**Unlocks.** The §5.5 budgets. Static lights become effectively free.

---

## Part F — Depth prepass

**Goal.** Draw depth first, shade only what survives. And put the buffer §10's
AO work will need in place.

**Why here.** See "Ordering rationale" above — after shadows, before clustering.

**Touches.** `renderer/backend.go`, `vulkan/`, `core/app.go`,
`shaders/slang/depth.slang`.

**Steps.**

1. `BeginPass` currently always clears depth, which would erase the prepass
   result at the start of the main pass. Add a `keepDepth bool` (or a small
   `PassOptions`) that gives the depth attachment a `Load` op rather than
   `Clear`. This is the only interface change in the part.
2. Prepass: `BeginPass` on the backbuffer, depth only, `depth.slang`, every
   scene mesh with its real model matrix.
3. Main pass: `BeginPass(..., keepDepth: true)`, then `SetDepthFunc` to `EQUAL`
   for the forward scene draws. The skybox keeps `LEQUAL`, the UI is unchanged.
4. MSAA: the prepass must run at the same sample count as the main pass or the
   `EQUAL` test fails along every geometric edge. Verify with
   `[antialiasing] samples = 4` and again with `1`.

**Steps deliberately not taken here.** The packed-normal attachment (§10) needs
MRT support in `RenderTargetSpec`, which nothing else in this plan requires. It
belongs to the AO work, not to this part; depth alone is what Part G benefits
from, and normals can be reconstructed from depth in the meantime.

**Gate.** The standard gate, plus: identical image with the prepass on and off,
and a measurable FPS gain in a scene with real overdraw (the showcase may be too
flat to show one — build a deliberately layered test scene if so).

**Risks.** `EQUAL` depth is unforgiving of any difference between the two
passes' vertex transforms. The prepass and the forward pass must use the same
matrices from the same uniform block, not a recomputed copy.

**Unlocks.** Overdraw elimination; the depth buffer for AO, SSR, volumetrics.

---

## Part G — Clustered forward

**Goal.** Thousands of lights. Each fragment shades only the lights in its
froxel.

**Why here.** Last of the rendering parts, and the one that pays best on top of
Part F.

**Touches.** `scene/`, `renderer/uniforms.go`, `forward.slang`.

**Steps.**

1. Froxel grid, default 16 × 9 × 24, exponential in Z. `ClusterGrid` in
   `FrameUniforms` carries the dimensions and `maxPerCluster`.
2. CPU build per frame: every light's bounding sphere against every froxel,
   producing `clusterOffsets` (offset, count per cluster) and `clusterIndices`
   (flat light indices). Upload both through Part B's texel buffer.
3. `forward.slang`: derive the cluster from `gl_FragCoord.xy` and view depth,
   read offset and count, loop only those lights. The Part A early-out stays as
   the inner guard.
4. The scene light array outgrows `MaxLights` here — move it to the texel buffer
   too, at which point `MaxLights = 16` becomes the per-*cluster* cap rather
   than the scene cap.
5. Feed the cluster result into Part D's allocator: a light intersecting zero
   clusters skips tile allocation entirely (§2.2). This is the synergy the
   ordering was chosen for.

**No longer blocked.** This part originally ruled out a GPU cluster build,
because GL 4.1 had neither compute shaders nor SSBOs and keeping both backends on
one path was a stated goal. Neither reason survives. Build on the CPU first
anyway — it is simpler and it is not obviously the bottleneck — but a compute
build is now a legitimate follow-up rather than an impossibility, and would make
a good first user of `Dispatch` (`BACKEND_DECISION.md` §9 item 6).

**Gate.** The standard gate, plus a stress scene with 200+ unshadowed lights
holding frame rate, and the froxel grid visualised as a debug overlay at least
once.

**Risks.** Z-slice distribution interacts with the shadow `farPlane`, still a
hardcoded 50 in `core/app.go` (§13.5). Fit both to the same scene bounds in this
part or the two disagree at range.

**Unlocks.** 1000s of lights. The froxel grid is also reusable as-is for
volumetrics later.

---

## Part H — Quality tiers

**Goal.** One scene, one code path, from a discrete GPU down to an integrated
laptop one.

**Why last.** Every knob's sensible range is only known once the system it
controls exists.

**Touches.** `settings/settings.go`, `settings/config.go`, `configs/*.toml`.

**Steps.**

1. Move every constant the earlier parts hardcoded into `settings`, then into
   the TOML schema of §9.
2. `dynamicAtlas = 0` must disable the dynamic pass entirely — every record
   falls back to `staticAtlas`, moving objects cast nothing, per-frame shadow
   cost goes to zero. This is the low-end switch, and it is worth an explicit
   test.
3. Ship `configs/low.toml` beside the two existing files: 2048 atlas, no dynamic
   atlas, 8×5×12 clusters, `maxPerCluster` 16, cheap PCF.
4. Extend `settings`' existing test coverage to the new keys and their defaults.

**Gate.** The standard gate across all three configs — three
runs.

**Risks.** Knobs that silently do nothing. Each one needs a visible effect
confirmed once, by hand, at the extremes of its range.

**Unlocks.** The low-end story, and the ability to state a minimum spec.

---

## Where the value is

Parts A, D, G and H need no backend work at all; B, C, E and F are where the two
implementations have to be kept honest against each other — see the risk column
in [Parts](#parts).

Parts E and G answer the two halves of the original question — E for how many
*shadowed* lights, G for how many lights at all.
