# Lighting implementation — Parts A–H

**Status: A, B and C landed.** The build order for `LIGHTING_PLAN.md`, split so
each part is a session's work that leaves the tree running.

The "As each part lands" sync below **has been done through C**:
`../FEATURES.md`, `../ENGINE_FLOW.md`, `../ARCHITECTURE.md`, `../OVERVIEW.md`,
`../TODO.md` and `../../CLAUDE.md` describe the atlas rather than per-light
shadow targets. A–C are not struck from this file yet, since the deviations
recorded under each are the only account of _why_ the shipped shape differs from
`LIGHTING_PLAN.md`.

Scope: what to edit, in what order, and how to know each part landed. Every
`§n` below points into `LIGHTING_PLAN.md`.

Not here: _why_ the design looks like this — the decisions, the atlas layout,
the capacity arithmetic and the rejected alternatives all live in
`LIGHTING_PLAN.md`.

---

## Parts

| part                                                | theme                    | risk           | unlocks              |
| --------------------------------------------------- | ------------------------ | -------------- | -------------------- |
| [A](#part-a--light-model) _(landed)_                | light model, spot lights | struct layout  | ~64 lights           |
| [B](#part-b--atlas-plumbing) _(landed)_             | atlas plumbing           | image layouts  | tiles drawable       |
| [C](#part-c--records-and-atlas-sampling) _(landed)_ | records, atlas sampling  | tile bleeding  | one sampler, N tiles |
| [D](#part-d--the-allocator)                         | quadtree allocator       | fragmentation  | variable resolution  |
| [E](#part-e--staticdynamic-split)                   | static/dynamic, caching  | classification | the shadow budget    |
| [F](#part-f--depth-prepass)                         | depth prepass            | MSAA + `EQUAL` | overdraw, AO input   |
| [G](#part-g--clustered-forward)                     | clustered forward        | Z distribution | 1000s of lights      |
| [H](#part-h--quality-tiers)                         | quality tiers            | dead knobs     | the low-end story    |

Three positions in that order are fixed:

- **Spot lights first (A).** Six times cheaper per shadowed light (§4.5), and
  every capacity figure assumes they exist. Zero coupling to any other part —
  pure `scene/` plus `forward.slang`, no backend work. Adding them after the
  allocator means tuning the allocator twice.
- **Depth prepass at F**, after the shadow work, immediately before clustering.
  Earlier it returns little — with the Part A early-out the forward pass is not
  yet overdraw-bound. Later, and Part G pays to run a full per-cluster light
  loop on hidden fragments. It also lands §10's depth buffer before any AO work.
- **Config knobs last (H).** Every part before it hardcodes its constant in
  `settings/settings.go`; H moves them all to TOML in one pass, once their real
  ranges are known.

B–E are the shadow budget in dependency order: plumbing, records, allocation,
caching. G is the light-count headline.

**Prerequisite.** `BACKEND_DECISION.md` §9 items 3–8 (Vulkan-native clip space,
`PipelineSpec`, the pass list, compute) are the substrate B onward sits on, and
item 3 in particular deletes the shadow-pass winding special case that Part C
would otherwise have to reason about. Part A is independent and can land now.

---

## The gate

Every part ends with the same check, from `src/`:

```sh
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh
go build ./... && go test ./...
go run .                                       # eyeball the showcase scene
OVERDRIVE_VK_VALIDATION=1 go run .             # clean log
```

The eyeball step is not optional for any part touching `common.slang` or
`renderer/uniforms.go`. The `init()` size guard catches a member added, removed
or resized; **two members swapped leaves every size identical and renders silent
garbage**, and the cross-backend diff that used to catch that is gone. To read
the offsets the compiler actually emitted:

```sh
spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate
```

---

## Part A — Light model

**Goal.** Spot lights, per-light radius, and a `LightData` big enough for
everything later parts need, so the layout is disturbed exactly once.

**Touches.** `renderer/uniforms.go`, `shaders/slang/common.slang`,
`shaders/slang/forward.slang`, `scene/light.go`, `scene/scene.go`,
`plugin/xml_export.py`, `assets/showcase.xml`.

**Steps.**

1. ~~Grow `LightData` and add the spot fields.~~ **Done.** Three dead members
   (`Specular`, `Linear`, `Quadratic`) came out and four went in, so the struct
   is **68 → 72 bytes** and `FrameUniforms` **1184 → 1216** at `MaxLights = 8`.
   `Radius`, `ShadowIndex` and `ShadowCount` are present but unread — grouped in
   so the layout is disturbed once rather than once per part.
2. ~~`MaxLights` 8 → **64**~~ **Done**, in `renderer/uniforms.go` and
   `common.slang`. `FrameUniforms` goes 1216 → **5248**: the non-light part is
   640 bytes, and the array 8 × 72 = 576 becomes 64 × 72 = 4608.

   64 rather than 16 because that is what the step 7 early-out actually buys
   (§6), and because `MaxLights` costs nothing at runtime — `forward.slang:267`
   loops to `lightCount`, the scene's real count, never to `MaxLights`. The only
   price is the memcpy: ~5 KB per pass, three passes a frame. There is no
   uniform-block ceiling in play either, since the block is read through a
   device address rather than bound as a UBO. Capping at 16 would leave Part D's
   allocator untestable near the atlas's real capacity until Part G.

3. ~~Add `LightSpot`, the scene and XML plumbing, delete `cos45`.~~ **Done.**
   The XML carries Blender's own spot terms rather than the cosines —
   `<cone>` (full angle, degrees) and `<coneBlend>` (0..1 soft-edge fraction) —
   and `LightXml.toLight` converts them, so the export round-trips and the
   scene file stays readable.
4. ~~Derive `Radius` at load.~~ **Done**, in `scene.lightRadius`. It solves the
   falloff `forward.slang:215` really implements — `1/(kConstant + d²)` — so:

   ```
   Radius = sqrt(max(0, peak/lightCutoff - lightConstant))
   peak   = max(Color.r, Color.g, Color.b) * Diffuse * Intensity
   ```

   Four things that were not obvious going in:

   - **`peak` uses the brightest channel**, not a flat 1.0. Ignoring `Color`
     inflates the radius of a tinted light, which is safe but wastes the very
     culling the field exists for. Averaging instead would cull a saturated
     light while its strong channel is still visible.
   - **It runs after the `/1000` intensity scaling**, not before. The raw
     Blender energy gives a radius √1000 ≈ 32× too large.
   - **`kConstant` was a bare `1.0` inside `FillFrameUniforms`**, invisible from
     `toLight`. It is now `scene.lightConstant`, shared by both, so the radius
     cannot drift out of step with the attenuation it inverts.
   - **A sun gets 0**, since a directional light does not attenuate. That makes
     the type guard in step 6 load-bearing: drop it and the sun disappears
     rather than degrading.

5. ~~`forward.slang`: add `calcSpotLight`.~~ **Done.** `calcPointLight` times a
   smoothstep from `outerCutoff` to `cutoff`, with `fsMain` switching on
   `light.type` through the new `LIGHT_*` defines in `common.slang`.

   The cone term is `dot(L, direction)`, **not** `dot(-L, direction)` as written
   above: in this engine `direction` points _toward_ the light. `LightXml.toLight`
   negates Blender's shine vector (`scene/light.go:79`) and `calcDirLight` then
   uses `direction` directly as the incoming `L`. A spot has no shadow path yet —
   `pickShadowCasters` only ever picks a sun and a point light.

6. ~~`forward.slang`: the attenuation early-out (§6).~~ **Done**, at the top of
   the light loop, before the BRDF and before any shadow lookup, guarded on
   `type != LIGHT_SUN` because a sun's `Radius` is 0.
7. ~~`xml_export.py`: export Blender `SPOT` lamps.~~ **Done** — it writes
   `spot_size` and `spot_blend` straight through. ~~Add a spot to the
   showcase.~~ **Done**: `SpotViolet`, GL (-2, 6, 4.5), straight down, 40° cone,
   `coneBlend` 0.25. `TestShowcaseSpotLight` in `scene/showcase_test.go` guards
   the derived cosines and the radius, since a malformed cone degrades silently
   to `cutoff == outerCutoff == 1`.

> `xml_export.py` and `assets/` are at the **repository root**, not under
> `src/`. `CLAUDE.md` says `src/plugin/` and `src/assets/`; both are stale.

**Gate.** Standard gate, plus: the showcase renders a visible cone, and
`MaxLights` 64 does not regress FPS — the early-out should make a 64-light scene
_faster_ than 8 lights without one.

**The showcase cannot show that.** Its radii, as computed:

| light      | intensity | radius          |
| ---------- | --------- | --------------- |
| PointWarm  | 0.2       | **7.07**        |
| PointRed   | 20        | 71.4            |
| PointGreen | 30        | 87.5            |
| PointBlue  | 20        | 71.4            |
| Sun        | 4.5       | 0 (directional) |

The scene is ~10–20 units across, so four of the five lights reach every
fragment in it and the early-out culls almost nothing. Step 6 needs a scene of
many _dim, localised_ lights to measure at all — the same layered test scene
Part F wants for overdraw, so build it once and use it for both.

That is a property of the showcase's authoring, not of the formula: `PointWarm`
at intensity 0.2 gets a 7-unit radius and does cull.

**Risk.** The `LightData` growth is the most dangerous edit in the plan. Do not
batch it with anything else in one commit.

---

## Part B — Atlas plumbing

**Goal.** The two new `Backend` methods and the atlas render target, with
nothing yet using them.

**Touches.** `renderer/backend.go`, `vulkan/backend.go`, `vulkan/draw.go`,
`../ENGINE_FLOW.md`, and `go-vulkan`.

**Steps.**

1. ~~**`go-vulkan` first:** bind `vkCmdCopyImage` and `VkImageCopy`.~~ **Done**,
   in `vk/cmd.go` beside `CmdCopyBufferToImage`, and listed in §3 of
   `BINDINGS_GAP.md`. `ImageCopy` carries one `AspectMask` and one `LayerCount`
   rather than a pair of each, Vulkan requiring the two subresources to agree;
   offsets are `Offset2D` with an implied z, as `BufferImageCopy` already does.
2. ~~Add `SetViewportScissor` and `CopyDepthRegion` to `Backend`.~~ **Done.**
3. ~~Implement both on Vulkan.~~ **Done.** Three things the plan did not say:

   - **The y-flip had to be extracted first.** `BeginPass` built its viewport
     inline in three places; `SetViewportScissor` would have been a fourth copy
     of the rule. They now share `vulkan/backend.go:viewportFor(pass, x, y, w, h)`,
     which negates the height for `passMain` and `passOffscreenColor` only. The
     refactor is value-for-value identical to what the three sites emitted.
   - **`passActive` is a new field.** `currentTarget == 0` means both "backbuffer
     pass" and "no pass", and `CopyDepthRegion` has to tell them apart: a copy
     inside `CmdBeginRendering` is invalid.
   - **Depth targets gained `TransferSrc | TransferDst`** usage in
     `vulkan/texture.go`. Without it the copy is a validation error, and one
     atlas is both a source and a destination.

   `CopyDepthRegion` returns **both** images to `ShaderReadOnlyOptimal` rather
   than leaving them in a transfer layout: the static atlas stays sampleable
   without a further barrier, and the destination's next `BeginPass` starts from
   a layout it can name. It also rejects an out-of-range region outright — an
   out-of-bounds `CmdCopyImage` is a device loss, not a clipped copy.

4. ~~Allocate the atlas through the existing `RenderTargetSpec`.~~ Nothing to
   do: a 4096 `TargetDepth` with `Cube: false` already allocates.
5. ~~Record the invariant-2 amendment in `../ENGINE_FLOW.md` §5.~~ **Done**, plus
   the two methods in §0's frequency index and §4.8.

**Gate.** Standard gate, run clean. The throwaway probe is kept as
`.claude` scratch `partB-atlas-probe.diff`: it bakes the sun into a 1024 tile at
(1024, 512) of a 4096 atlas, copies that tile to (0,0) of a second atlas, and
remaps `shadowCalculation`'s uv by ×0.25 to sample it back. Applied, it builds
and runs validation-clean at the same frame rate; **it has not been eyeballed**,
so "the shadow still looks right" is unconfirmed. Re-apply with `git apply` to
check that by hand.

**Risk.** Image layout transitions around `vkCmdCopyImage` are the usual
validation trap. Run with `OVERDRIVE_VK_VALIDATION=1` throughout this part, not
only at the end.

---

## Part C — Records and atlas sampling

**Goal.** One shadow map for the whole scene; every light samples a rect of it.

**Touches.** `renderer/uniforms.go`, `common.slang`, `forward.slang`,
`scene/light.go`, `scene/scene.go`, `core/app.go`, `vulkan/draw.go`; delete
`shaders/slang/depth_cube.slang`.

**Steps.**

1. ~~Define `ShadowRecord` (§5.2, 96 bytes) and push it as a third pointer.~~
   **Done.** `PushConstants` is 16 → 24 bytes, and `pushConstantSize` is now
   `unsafe.Sizeof(pushAddresses{})` rather than a literal at the two call sites
   that had to agree.

   The array rides the **existing per-frame ring**, not a separate storage
   buffer: the ring is already `BufferUsageShaderDeviceAddress` and already
   reset per frame, so `writeRing` only had to grow a slice form
   (`writeRingSlice`). `CreateStorageBuffer` (`BACKEND_DECISION.md` §9 item 8)
   is still worth having, but nothing here needed it.

   That took a **third `Backend` method**, which §8 did not list:
   `BindShadowRecords([]ShadowRecord)`, frame-scoped rather than pass-scoped.
   `BeginFrame` seeds the ring with one empty record so a frame that never calls
   it still pushes a dereferenceable address.

2. ~~Rework `FrameUniforms` per §5.3.~~ **Done, at 4844 bytes rather than the
   4828 the plan predicted.** §5.3 removed `FarPlane` and `LightPos` on the
   grounds that "the record carries both now" — true of the _sampling_ side, and
   wrong about the _bake_ side: `depth_point.slang` writes radial distance and
   never reads the record array, so it needs the light position and far plane
   from somewhere. They came back as `BakeLightPos` / `BakeFarPlane` beside
   `BakeMatrix`, +16 bytes:

   ```go
   View, Projection mgl32.Mat4            // 128
   BakeMatrix       mgl32.Mat4            // 64   → 192
   BakeLightPos     [3]float32            // 12   → 204
   BakeFarPlane     float32               //  4   → 208
   ViewPos          [3]float32            // 12   → 220
   LightCount       int32                 //  4   → 224
   Lights           [64]LightData         // 4608 → 4832
   TexShadowStatic  TextureHandle         //  4   → 4836
   TexShadowDynamic TextureHandle         //  4   → 4840
   TexSkybox        TextureHandle         //  4   → 4844
   ```

   Still a net shrink from Part A's 5248. `spirv-dis` confirms slangc emits
   those exact offsets.

3. ~~Replace `Light.RenderShadowMap` with a tile bake.~~ **Done** in the new
   `scene/shadowatlas.go`: one `BeginPass` on the atlas, then per tile a
   `SetViewportScissor`, a `BakeMatrix` and a full mesh loop. `Light.setup`,
   `shadowTarget`, `depthMap`, `depthCubeMap` and the `backend` field are gone —
   a light owns no GPU resource at all now, only a `shadowIndex`/`shadowCount`
   pair into the record array.
4. ~~`core/app.go`: one call into the tile bake.~~ **Done**, and the order is
   load-bearing: `UpdateShadows` (allocate + build records) → `BindShadowRecords`
   → `FillFrameUniforms` (which copies each light's `shadowIndex` out) →
   `BakeShadows`.
5. ~~Delete `depth_cube.slang`.~~ **Done**, replaced by `depth_point.slang`:
   vertex plus a fragment stage writing `SV_Depth`, no geometry stage. The
   `FrontFace = Clockwise` case stays for now — `passShadow2D` and
   `passShadowCube` were already identical pipeline state, so retiring the
   geometry stage did not touch it, and `BACKEND_DECISION.md` §9 item 3 is still
   what deletes it.
6. ~~`forward.slang`: one `shadowLookup`.~~ **Done**, as `shadowLookup` +
   `pcfTile` + `tileSample` + `cubeFace`. Radial depth for face tiles, projected
   depth for a sun or spot, normalised so both compare in the tile's own [0, 1].

   Two things the plan did not say:

   - **The clamp-to-border sampler stopped meaning anything.** The sun's map got
     "outside the frustum reads fully lit" free from `BorderColorOpaqueWhite`;
     inside an atlas the neighbours _are_ the border. `shadowLookup` now tests
     the tile-local uv explicitly and returns 0 before sampling.
   - **The point-light kernel changed shape**, from 20 taps on a 3D disk to the
     same 4-then-3×3 the 2D path uses. It is the one place the image is not a
     pure refactor: penumbra width on point shadows will differ slightly.

7. ~~`common.slang`: two `Sampler2D`.~~ **Done.** Bindings 2 and 3 are
   `shadowAtlasStatic` and `shadowAtlasDynamic`; `MAX_SHADOW_CUBES` and
   `renderer.MaxShadowCubes` are gone, and the descriptor pool lost 3 of its 4
   cube-shadow descriptors. `samplerShadowCube` survives only because
   `CreateRenderTarget` still handles a cube depth spec nothing asks for.
8. ~~Interim allocation.~~ **Done**: a 4096² atlas as a row-major grid of 16
   tiles of `settings.ShadowWidth` (1024), handed out in order, with the caster
   set left exactly as it was — first sun, first point light — so the gate has
   something to compare against. A point light takes its six tiles all or
   nothing; five would leave a lit wedge.

**Gate.** Standard gate: builds, `go test ./...` green, `spirv-val
--scalar-block-layout` clean on all ten modules, and
`OVERDRIVE_VK_VALIDATION=1 go run .` runs silent at ~180 FPS on the 5070 Ti.

**The eyeball step has not been done** — no screenshot path on this Wayland
session — so "the showcase's shadows are indistinguishable" is _unverified_.
Standing in for it: `scene/shadowatlas_test.go` checks on the CPU that
`cubeFaceDirs`, `cubeFace()` and the six matrices agree (a point along each face
direction projects inside that face's own tile), that a tile's `AtlasRect` and
`TexelSize` match the pixels the bake's viewport wrote, and that the allocator
hands out non-overlapping tiles and refuses to wrap. That covers the two silent
failures in the Risk note; it does not cover bias, penumbra or bleeding.

**Also unresolved, and now more visible.** The point-light path multiplies its
shadow by **5.0**, so `Lo += contrib * (1.0 - shadow)` goes _negative_ on a fully
shadowed fragment and subtracts light that other lights put there. That predates
this part and is preserved verbatim rather than silently fixed, since the gate is
an unchanged image — but it is a bug, not a tuning constant, and Part D or E
should delete the factor and re-tune.

**Risk.** Tile bleeding (missing rect clamp) shows as shadows from the wrong
light; cube seams (missing FOV widening) show as hairline cracks at face
boundaries. Both are subtle enough to ship by accident — check a point light
against a wall corner deliberately.

---

## Part D — The allocator

**Goal.** Tile sizes chosen per frame from screen-space importance.

**Touches.** new `scene/shadowatlas.go`, `scene/scene.go`.

**Steps.**

1. Quadtree allocator over the atlas: split 4096 → 2048 → … → 128, with
   `Alloc(size)` and `Free(rect)` coalescing freed siblings.
2. Per-light score and tier, thresholds from §4.3.
3. Hysteresis: a tier change requires the score to cross its threshold by more
   than 20%. Without it a light on a boundary reallocates every frame and forces
   a full re-bake every frame — the exact opposite of Part E's goal.
4. Sort by score, allocate greedily, demote what does not fit to
   `ShadowIndex = -1`. Running out of budget must cost shadow quality and never
   frame time.
5. Delete `Scene.pickShadowCasters`, `Scene.casts` and `Scene.ShadowCasters` —
   the fixed 1-dir + 1-point budget they encode is what this part replaces.

**Gate.** Standard gate, plus: walking the camera toward a light visibly
sharpens its shadow and walking away coarsens it, without popping every frame.
Log one frame's allocation map and check it against §4.1.

**Risk.** Quadtree fragmentation over a long session — many alloc/free cycles at
mixed sizes leaving no contiguous slot. Coalescing on free is what prevents it;
write that unit test specifically, it is cheap and CPU-only.

---

## Part E — Static/dynamic split

**Goal.** The shadowed-light budget. Static lights bake once; dynamic lights
bake within a texel allowance.

**Touches.** `scene/`, `core/app.go`, `vulkan/` (`CopyDepthRegion` from Part B).

**Steps.**

1. Two atlases, `staticAtlas` and `dynamicAtlas` (§2.1). `ShadowRecord.Flags`
   bit 0 selects which one the shader samples.
2. Classify meshes static vs movable — the ECS/physics entities are the movable
   set, baked OBJ geometry is static.
3. Bake `staticAtlas` at load from static casters only. Never re-bake unless a
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
   camera frustum), §4.4.
7. Re-bake budget in **texels** per frame, queued by score (§4.5). Overflow
   resolves on later frames.

**Gate.** Standard gate, plus a bake counter printed beside the FPS: a static
showcase scene must settle at **zero** bakes per frame. Then move one physics
entity and confirm only the lights that see it wake up.

**Risk.** The static/dynamic classification will be wrong first — a mesh
classified static that later moves leaves a shadow behind, with no crash and no
log line. Make the counter and a debug atlas view part of the work, not an
afterthought.

---

## Part F — Depth prepass

**Goal.** Draw depth first, shade only what survives, and put the buffer §10's
AO work needs in place.

**Touches.** `renderer/backend.go`, `vulkan/`, `core/app.go`,
`shaders/slang/depth.slang`.

**Steps.**

1. `BeginPass` always clears depth, which would erase the prepass result at the
   start of the main pass. Add a `keepDepth bool` (or a small `PassOptions`)
   giving the depth attachment a `Load` op rather than `Clear`. Only interface
   change in the part — and if `BACKEND_DECISION.md` §9 item 7 has landed, it
   belongs on the `Pass` description instead.
2. Prepass: `BeginPass` on the backbuffer, depth only, `depth.slang`, every
   scene mesh with its real model matrix.
3. Main pass: `BeginPass(..., keepDepth: true)`, then `SetDepthCompare` to
   `CompareEqual` for the forward scene draws. The skybox keeps `LessEqual`, the
   UI is unchanged. `CompareEqual` is a new enum value in `renderer/` and may
   need the matching `go-vulkan` constant.
4. MSAA: the prepass must run at the same sample count as the main pass or the
   `EQUAL` test fails along every geometric edge. Verify with
   `[antialiasing] samples = 4` and again with `1`.

**Not taken here.** The packed-normal attachment (§10) needs MRT in
`RenderTargetSpec`, which nothing else in this plan requires. It belongs to the
AO work; depth alone is what Part G benefits from, and normals can be
reconstructed from depth in the meantime.

**Gate.** Standard gate, plus: identical image with the prepass on and off, and
a measurable FPS gain in a scene with real overdraw — the showcase may be too
flat to show one, so build a deliberately layered test scene if needed.

**Risk.** `EQUAL` depth is unforgiving of any difference between the two passes'
vertex transforms. Both must use the same matrices from the same uniform block,
not a recomputed copy.

---

## Part G — Clustered forward

**Goal.** Thousands of lights. Each fragment shades only the lights in its
froxel.

**Touches.** `scene/`, `renderer/uniforms.go`, `forward.slang`.

**Steps.**

1. Froxel grid, default 16 × 9 × 24, exponential in Z. `ClusterGrid [4]int32` in
   `FrameUniforms` carries the dimensions and `maxPerCluster`.
2. CPU build per frame: every light's bounding sphere against every froxel,
   producing `clusterOffsets` (offset, count per cluster) and `clusterIndices`
   (flat light indices). Upload both into the storage buffer from Part C and
   push a fourth pointer for them.
3. `forward.slang`: derive the cluster from `gl_FragCoord.xy` and view depth,
   read offset and count, loop only those lights. The Part A early-out stays as
   the inner guard.
4. The scene light array outgrows `MaxLights` here — move `Lights[]` out of
   `FrameUniforms` into the same storage buffer. `MaxLights` stops being the
   scene cap and `maxPerCluster` (16) takes over as the per-fragment cap;
   `FrameUniforms` drops to roughly 236 bytes.
5. Feed the cluster result into Part D's allocator: a light intersecting zero
   clusters skips tile allocation entirely (§2.2). This is the synergy the
   ordering was chosen for.

**Follow-up, not required here.** A compute cluster build is a good first user
of `Dispatch` (`BACKEND_DECISION.md` §9 item 8). Build on the CPU first — it is
simpler and not obviously the bottleneck.

**Gate.** Standard gate, plus a stress scene with 200+ unshadowed lights holding
frame rate, and the froxel grid visualised as a debug overlay at least once.

**Risk.** Z-slice distribution interacts with the shadow `farPlane`, still a
hardcoded `50` in `core/app.go:114`. Fit both to the same scene bounds in this
part or the two disagree at range.

---

## Part H — Quality tiers

**Goal.** One scene, one code path, from a discrete GPU down to an integrated
laptop one.

**Touches.** `settings/settings.go`, `settings/config.go`, `configs/*.toml`.

**Steps.**

1. Move every constant the earlier parts hardcoded into `settings`, then into
   the TOML schema of §9.
2. `dynamicAtlas = 0` must disable the dynamic pass entirely — every record
   falls back to `staticAtlas`, moving objects cast nothing, per-frame shadow
   cost goes to zero. This is the low-end switch and it is worth an explicit
   test.
3. Ship `configs/low.toml` beside `vulkan.toml`: 2048 atlas, no dynamic atlas,
   8 × 5 × 12 clusters, `maxPerCluster` 16, cheap PCF.
4. Extend `settings`' existing test coverage to the new keys and their defaults.

**Gate.** Standard gate across every config.

**Risk.** Knobs that silently do nothing. Confirm each one's visible effect once,
by hand, at the extremes of its range.

---

## As each part lands

Move its content into `../FEATURES.md` (why it is built that way) and
`../ENGINE_FLOW.md` (how a frame runs), then strike the part from here. When
both this file and `LIGHTING_PLAN.md` are empty, delete them.
