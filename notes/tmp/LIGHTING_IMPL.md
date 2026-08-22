# Lighting implementation — Parts A–H

**Status: A–F and H landed. G is deferred to [`CLUSTERED_FORWARD.md`](CLUSTERED_FORWARD.md).** The build order for `LIGHTING_PLAN.md`, split so
each part is a session's work that leaves the tree running.

The "As each part lands" sync below **has been done through E** for
`../../CLAUDE.md` and `../ENGINE_FLOW.md`, and **through D** for the rest:
`../FEATURES.md`, `../ENGINE_FLOW.md`, `../ARCHITECTURE.md`, `../OVERVIEW.md`,
`../TODO.md` and `../../CLAUDE.md` describe the atlas and its per-frame allocator
rather than per-light shadow targets and a fixed caster pick. A–D are not struck
from this file yet, since the deviations recorded under each are the only account
of _why_ the shipped shape differs from `LIGHTING_PLAN.md`.

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
| [D](#part-d--the-allocator) _(landed)_              | slot-layout allocator    | contention thrash | variable resolution  |
| [E](#part-e--staticdynamic-split) _(landed)_        | static/dynamic, caching  | classification | the shadow budget    |
| [F](#part-f--depth-prepass) _(landed)_              | depth prepass            | MSAA + `EQUAL` | overdraw, AO input   |
| [G](CLUSTERED_FORWARD.md) _(deferred)_              | clustered forward        | Z distribution | 1000s of lights      |
| [H](#part-h--quality-tiers) _(landed)_              | quality tiers            | dead knobs     | the low-end story    |

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
go run . -config <a copy with [debug] validation = true>   # clean log
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
validation trap. Run with `[debug] validation = true` throughout this part, not
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
`go run .` with `[debug] validation = true` runs silent at ~180 FPS on the 5070 Ti.

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

## Part D — The allocator _(landed)_

**Goal.** Tile sizes chosen per frame from screen-space importance.

**Touches.** `scene/shadowatlas.go`, `scene/scene.go`, and — not in the plan —
`shaders/slang/forward.slang`, `assets/showcase.xml`, plus the atlas readback in
`renderer/backend.go`, `vulkan/texture.go`, `scene/shadowdebug.go`,
`core/app.go` and `go-vulkan`.

**Steps.**

1. ~~Quadtree allocator over the atlas.~~ **Done**, then **superseded**: the
   per-frame tree became a fixed slot layout (`slotLayout` / `buildLayout`),
   carved once at load. `quadNode` still places the slots — largest size first,
   which cannot fragment — but `release`, `free` and the sibling coalescing are
   gone with the per-frame use that needed them.
2. ~~Per-light score and tier.~~ **Done**, `lightScore` and `shadowTiers`, at
   §4.3's thresholds exactly. The tier is now the **ceiling** on what a light may
   hold rather than the size it is handed: rank picks the slot, the tier caps it.
3. ~~Hysteresis.~~ **Done**, now `nextTierThreshold = 1.2` in `tierFor`, plus a
   second margin `slotStickiness = 1.2` on the ranking — fixed pools let two
   near-equal lights trade a contended pool's last slot, which the tier margin
   cannot damp because it guards a threshold rather than a comparison.
4. ~~Sort by score, allocate greedily, demote what does not fit.~~ **Done**, in
   three phases rather than one; see below.
5. ~~Delete `Scene.pickShadowCasters`, `Scene.casts` and `Scene.ShadowCasters`.~~
   **Done.**

**Six things the plan did not say.**

- **The tree persists across frames**, and it has to. §4.3's hysteresis is
  pointless against an allocator that resets every frame — the whole point of not
  changing tier is not changing _pixels_, which only means something if the tiles
  survive. So `shadowAtlas` carries `allocs map[int32]*lightAlloc` and only
  touches a light whose tier actually moved.

- **Two tier states, not one.** A point light takes one step below its scored
  tier, because six faces cost 6× the texels of a spot at the same size. Feeding
  that halved size back in as `cur` next frame reads as a demotion and the light
  oscillates, so `lightAlloc` keeps `tier` (the hysteresis state, in
  `shadowTiers`' units) separate from `want` (the per-face size it buys) and
  `size` (what it actually got).

- **Allocation is three passes.** Free everything whose tier moved _before_
  allocating anything, so a promoting light can be served out of what a demoting
  one just gave back; then allocate in score order, degrading a step at a time;
  then retry the degraded ones at full size. Without the third pass a light that
  lost a tier during one crowded frame stays coarse for the rest of the session.
  That retry takes the new tiles before releasing the old, so a failed upgrade
  leaves what it has alone.

- **A sun is not scored at all.** It has no `Radius` — Part A step 4 gives a
  directional light 0 — so `radius / distance` is 0 and the sun would be the
  first light demoted to unshadowed. It takes a fixed 2048, which is also §4.1's
  drawing.

- **A spot needed its own projection.** `shadowRecord` had two branches, a cube
  face and "everything else = the sun's ortho box". Nothing noticed because
  `pickShadowCasters` never picked a spot. Now that every light is a candidate,
  a spot bakes through `mgl32.Perspective` at `2·acos(outerCutoff)`, widened by
  two texels for the same reason a cube face is, with `spotUp` avoiding the
  degenerate `LookAtV` when the cone points straight down.

- **The ×5 point-shadow scale had to go here**, not in Part E. Part C preserved
  it deliberately so its gate was an unchanged image; with every point light
  shadowed, `Lo += contrib * (1 - shadow)` going negative is black blotches
  rather than an invisible bug.

- **A caster flag was needed, and front-face culling is not a substitute.** With
  every light casting, the ground plane baked into every point light's map and
  shaded against itself — acne over the whole plane, which renders as a black
  scene. Making every tile `CullFront` cures that (a single-sided plane has no
  back face) and immediately peter-pans every closed caster by its own thickness.
  `Mesh.CastsShadow` / `<castsShadow>` is the fix; the cull mode stays `CullBack`.

**The atlas dump.** The eyeball step has had no answer since the OpenGL backend
went — this session has no screenshot path at all, and the atlas is a target
nothing draws to the screen anyway. So Part D also built one:
`renderer.DepthReader` (optional, outside `Backend`, because it blocks on an idle
queue), `VKBackend.ReadDepthTarget` on top of a new `vk.CmdCopyImageToBuffer`
binding, and `Scene.DumpShadowAtlas` / `ShadowAllocationMap`. `F9` or
`[debug] dumpAtlas = N` writes `shadow_atlas.png` with each tile
contrast-stretched to its own range — the three bakes do not share an encoding —
and outlined by light type.
>
> **Removed on 2026-08-17.** All of it: the two reader interfaces, the readback
> in `vulkan/texture.go`, `scene/shadowdebug.go`, the `dumpFrame`/`dumpAtlas`
> settings and the `F9`/`F10` keys. Images are inspected in RenderDoc instead,
> which needs no engine code and does not put a pipeline stall in the
> abstraction. `lockCamera` and `noShadows` stayed.

**Gate.** Standard gate: builds, `go test ./...` green, `spirv-val
--scalar-block-layout` clean, `go run .` with `[debug] validation = true` silent at
~181 FPS on the 5070 Ti — **the same rate as Part C's two casters**, with six
lights shadowed instead of two, which is the early-out and the tile budget both
doing their job.

The allocation map on the showcase, which is §4.1's shape at showcase scale:

```
  PointWarm    point score  0.875  tier 1024  6 tiles of 512
  PointRed     point score  7.709  tier 1024  6 tiles of 512
  PointGreen   point score  9.442  tier 1024  6 tiles of 512
  PointBlue    point score 11.398  tier 1024  6 tiles of 512
  SpotViolet   spot  score  9.927  tier 1024  1 tile  of 1024
  Sun          sun                 tier 2048  1 tile  of 2048
  11534336 texels of 16777216 used (68.8%)
```

**And the eyeball was actually done**, for the first time since Part B: the dump
shows the sun's ortho tile with the props silhouetted, the spot's cone looking
down at the chrome sphere, and 24 point-light faces each with the ground's
radial-distance horizon. Tests standing in for the rest:
`TestAtlasCoalescesFreedSiblings` (the Risk note's fragmentation case, an
alloc/free cycle repeated eight times finding the same capacity),
`TestTierHysteresis`, `TestTileSizeTracksCameraDistance` and
`TestShowcaseLightsAllFitTheAtlas` (no two tiles overlap, and a point light holds
six or none).

**One showcase change.** `SpotViolet` moved from GL (-2, 6, 4.5) to (-3.8, 6,
0.4) and widened 40° → 50°. Where it was, its cone covered bare ground, and the
2D bake culls front faces — so its atlas tile baked **completely empty** and the
spot shadow path was untested. Aimed at the chrome sphere the tile is 30%
geometry. The dump is what caught that; nothing else would have.

**Risk.** ~~Quadtree fragmentation over a long session.~~ Covered by
`TestAtlasCoalescesFreedSiblings`. Still open: the score ignores whether a light
is on screen at all, which is §2.2's cluster gate and belongs to Part G.

---

## Part E — Static/dynamic split _(landed)_

**Goal.** The shadowed-light budget. Static lights bake once; dynamic lights
bake within a texel allowance.

**Touches.** `scene/`, `core/app.go`, `vulkan/` (`CopyDepthRegion` from Part B).

**Steps.**

1. ~~Two atlases, `staticAtlas` and `dynamicAtlas` (§2.1). `ShadowRecord.Flags`
   bit 0 selects which one the shader samples.~~ **Done**, as `staticTarget` /
   `dynamicTarget` on `shadowAtlas`, over **one** `slotLayout` — so a tile's rect
   is identical in the two and the copy needs no remap. The shader side needed no
   edit at all: `forward.slang` has branched on the bit since Part C.
2. ~~Classify meshes static vs movable.~~ **Done**, but not from the ECS. `Mesh.Movable`
   is an XML `<movable>` element (the `*bool` default trick `CastsShadow` uses),
   **and** `MoveBy`/`MoveTo` set it. The ECS route was rejected: `scene` does not
   import `ecs`, and an entity owning a `*scene.Mesh` is a convention of `main.go`
   rather than a rule. The two together are what defuse the Risk note below.
3. ~~Bake `staticAtlas` at load from static casters only.~~ **Done**, though on
   the first frame rather than at load — the bake needs a live command buffer, so
   it runs where every other pass does.
4. ~~Per frame, `CopyDepthRegion` then draw movable casters on top.~~ **Done.**
5. ~~Dirty tracking.~~ **Done**, off `Scene.movedMeshes`, which `UpdateMeshes`
   fills from the `needsUpdate` flag it was already clearing.
6. ~~Caster cull and cube-face cull.~~ **Caster cull done**, twice over: bounding
   sphere vs light radius decides whether a light needs a dynamic tile at all,
   and sphere vs the tile's own six frustum planes (`frustumPlanes` /
   `sphereInFrustum`, Gribb-Hartmann off `WorldToTile`) decides whether a tile is
   drawn into. A face pointing at empty space now costs six plane tests and no
   state changes. **The cube-face-vs-camera-frustum cull was not taken** — see
   the deviations.
7. ~~Re-bake budget in texels per frame, queued by score.~~ **Done for the
   dynamic side**, `bakeTexelBudget` at 8 MiB, spent in `lightScore` order. The
   static side is deliberately unbudgeted — see the deviations.

**Four things the plan did not say.**

- **`keepDepth` is a Part E prerequisite, not a Part F one.** `BeginPass`
  hardcoded `LoadOp: Clear` on depth, and step 4 is impossible against it: the
  copy must land in the dynamic atlas before the pass that draws over it, and
  that pass would erase it. `CopyDepthRegion` refuses to run inside a pass, so no
  ordering saves it. `BeginPass` therefore took F step 1's third parameter early.
  Offscreen depth targets only: the backbuffer's depth image barriers from
  `Undefined` every frame, which discards what a `Load` would read, so F still
  owns that half. Without `keepDepth` the dynamic atlas is wiped every frame and
  step 5's dirty tracking has nothing to cache.

- **The static side is all-or-nothing, and unbudgeted.** Baking only the slots
  whose light changed looks obviously right and is wrong: a tile whose frustum
  holds no caster writes nothing, so a slot handed to a new light would keep its
  old owner's depth and wear another light's shadow. Since `BeginPass` clears the
  whole target anyway, a static re-bake clears and redraws every allocated light.
  It is a spike, not a frame rate — the allocator's hysteresis is what keeps
  allocation still, and the showcase does it once and never again.

- **Queueing marks a tile current, not the bake.** `staticValid` / `dynamicValid`
  are set in `UpdateShadows` where the decision is made, not in `BakeShadows`
  which merely executes it. That is what lets the whole policy be tested on the
  CPU, and it is safe because `BakeShadows` draws exactly what the queues hold and
  cannot fail partway. It does mean the two must stay paired in the frame loop.

- **A light waiting for its first static bake goes unshadowed**, `ShadowIndex = -1`,
  rather than sampling a slot it has not been baked into. Same philosophy as
  Part D's degrade path: running out of budget costs a light its shadow for a
  frame, never the frame its time.

**Gate.** Standard gate: builds, `go test ./...` green (15 tests in `scene`),
`spirv-val --scalar-block-layout` clean, and `go run .` with
`[debug] validation = true` silent on both `showcase.xml` and `stress.xml`.

The bake counter prints beside the FPS and the showcase **settles at zero
static, zero dynamic**, which is the gate's real question. Frame rate on the
Intel UHD 620 this session runs on went **14 → 42 FPS** on the showcase, which is
the per-frame bake disappearing.

The second half of the gate — "move one physics entity and confirm only the
lights that see it wake up" — was run against a temporary scene, since
`main.go:createWorld` looks for meshes named `Sphere` / `Sphere2` that
`showcase.xml` does not contain, so **the showcase drives no movement at all**.
Renaming two of its spheres so the falling-ball demo picks them up gives: static
queued on frame 1 only, dynamic re-queued every frame for 287 frames while the
ball falls, validation clean throughout. Worth fixing the showcase or the demo
so this is reachable without editing a scene.

**Risk, as written.** The classification being wrong first. Defused two ways:
`MoveBy`/`MoveTo` promote a mesh to movable themselves, so a mesh that moves
without declaring it leaves a shadow behind for one frame rather than for the
session; and `prevCenter` keeps a caster *leaving* a light's range dirtying the
tile it is leaving, which its new position alone would not say.

**Still open.**

- **The cube-face-vs-camera-frustum cull (step 6's second half) was not taken.**
  Every cheap version of it is a heuristic that silently drops a shadow, which is
  exactly the failure mode this part's Risk note is about. The honest form of the
  test is "can any visible fragment sample this face", which is §2.2's cluster
  gate — Part G's, and already noted as Part D's open risk.
- **A light that moves does not dirty its own tiles.** Nothing in the engine
  moves a light today, so there is no path to the bug; the moment `Light.Pos`
  becomes writable, `UpdateShadows` needs to compare it against the position the
  tile was baked at.
- **The debug atlas view the Risk note asks for** does not exist. It was built in
  Part D and removed on 2026-08-17 in favour of RenderDoc; the bake counter is
  what stands in for it.

---

## Part F — Depth prepass _(landed)_

**Goal.** Draw depth first, shade only what survives, and put the buffer §10's
AO work needs in place.

**Touches.** `renderer/backend.go`, `vulkan/`, `core/app.go`,
`shaders/slang/depth.slang`.

**Steps.**

1. ~~`BeginPass` always clears depth.~~ **Done**, in two halves. Part E took the
   `keepDepth bool` for offscreen depth targets; this part finished it for the
   backbuffer, whose depth image barriered from `Undefined` every frame and so
   discarded what a `Load` would read. `VKBackend.depthLayout` now tracks it,
   reset to `Undefined` in `BeginFrame` because the image genuinely does not
   survive one. The `keepDepth`-on-the-backbuffer warning Part E left is gone.
2. ~~Prepass: `BeginPass` on the backbuffer, depth only, `depth.slang`.~~ **Done,
   but neither with `BeginPass` nor with `depth.slang`** — see the deviations.
   `BeginDepthPrepass()` is a 28th `Backend` method and `prepass.slang` a new
   shader.
3. ~~Main pass with `CompareEqual` for the scene draws.~~ **Done**, in
   `Scene.RenderScene`, bracketed the way `RenderSkybox` already brackets
   `LessEqual` — and restoring `CompareLess` afterwards is load-bearing, the UI
   testing depth and failing an EQUAL comparison against the geometry it
   composites over. `renderer.CompareEqual` needed `vk.CompareOpEqual`, which
   `go-vulkan` did not bind.
4. ~~MSAA at the same sample count.~~ **Done**: `passSamples` returns `b.samples`
   for the prepass as well as the main pass. Verified running at
   `samples = 4` and `samples = 1`, both validation-clean.

**Three things the plan did not say.**

- **`depth.slang` is the wrong shader, and reusing it would z-fight.** It projects
  through `FRAME.bakeMatrix`, a single premultiplied matrix, while
  `forward.slang:20` does `mul(projection, mul(view, float4(fragPos, 1.0)))` with
  `fragPos` already through `model`. Same value mathematically, different
  associativity, different rounding — and `EQUAL` compares the bits. So
  `prepass.slang` exists purely to repeat `forward.slang`'s vertex arithmetic
  operation for operation. The Risk note says "the same matrices from the same
  uniform block"; that is necessary and not sufficient. It has to be the same
  *arithmetic*.

- **The prepass binds no colour attachment**, which is why it is its own
  `Backend` method rather than a `BeginPass` flag. Attachments are pass state:
  under dynamic rendering a pipeline declares the formats it will be used with,
  so "backbuffer, depth only" is a fifth `passKind` (`passDepthPrepass`) with its
  own `renderingInfo`, not a variation on `passMain`. The payoff is that the
  prepass writes no colour, blends nothing, and under MSAA **resolves nothing** —
  it costs a geometry pass and a depth write, not a second pass over the
  framebuffer. Reusing `passMain` would have cost a resolve of garbage every
  frame.

- **`StoreOp` differs between the two passes.** The prepass stores its depth,
  the main pass still discards its own — the depth image is ordinary memory
  rather than a transient attachment, so this works, and keeping the main pass on
  `DontCare` means the prepass costs the write-out and nothing else does.

**Not taken here.** The packed-normal attachment (§10) needs MRT in
`RenderTargetSpec`, which nothing else in this plan requires. It belongs to the
AO work; depth alone is what Part G benefits from, and normals can be
reconstructed from depth in the meantime.

**Gate.** Standard gate: builds, `go test ./...` green, `spirv-val
--scalar-block-layout` clean on all 12 modules, and `go run .` with
`[debug] validation = true` silent on `showcase.xml` at `samples = 4` and
`samples = 1`, with the prepass on and off, and on `stress.xml`.

**Two halves of the gate are not met, and neither is a code problem.**

- **"Identical image with the prepass on and off" is unverified.** There is still
  no screenshot path — Part D's readback was removed on 2026-08-17 in favour of
  RenderDoc — so this needs a human with two captures. The indirect evidence is
  weak but real: a wholesale `EQUAL` failure would reject nearly every fragment
  and the frame rate would *jump*, the expensive fragment shader having stopped
  running. It does not.
- **"A measurable FPS gain" is not measurable on the showcase**, exactly as the
  plan predicted. Five meshes on a ground plane is almost no overdraw, and on the
  Intel UHD 620 this session runs on the run-to-run spread (32–48 FPS on
  *identical* settings) swamps any difference. The layered test scene Part A step
  6 also wants is what would answer this; it is still unbuilt.

**Risk, as written.** `EQUAL` being unforgiving. Realised immediately, in the
form the note did not predict — see the first deviation.

**One knob, added early.** `[renderer] depthPrepass`, default true. Part H is
where knobs are supposed to land, but the gate's own A/B requires this one to
exist, so it went in with a `settings` test (`TestDepthPrepassTurnsOff`) rather
than as a constant to be moved later.

---

## Part G — Clustered forward _(deferred)_

**Moved out to [`CLUSTERED_FORWARD.md`](CLUSTERED_FORWARD.md)** on 2026-08-22,
unchanged, so this file could be closed out with Part H. It is the only part of
the plan not built, and nothing else waits on it — Part F was ordered immediately
before it and is already in place.

Three things elsewhere are waiting on it and say so where they will bite:
`lightScore` ranking a light behind the camera as highly as one in front of it,
Part E's untaken cube-face-vs-camera-frustum cull, and `MaxLights` still being a
fixed 64.

---

## Part H — Quality tiers _(landed)_

**Goal.** One scene, one code path, from a discrete GPU down to an integrated
laptop one.

**Touches.** `settings/settings.go`, `settings/config.go`, `configs/*.toml`.

**Steps.**

1. ~~Move every constant the earlier parts hardcoded into `settings`, then into
   the TOML schema of §9.~~ **Done**, as a rewritten `[shadows]` section:
   `atlasSize`, `slotDivisors` / `slotCounts`, `tierScores`, `dynamicAtlas`,
   `bakeBudgetMiB`, `pcf`, `nearPlane` / `farPlane`. `nextTierThreshold` and
   `slotStickiness` deliberately stayed constants — see the deviations.
2. ~~`dynamicAtlas = 0` must disable the dynamic pass entirely.~~ **Done**, as a
   bool rather than a size. It gates `al.dynamic` in `UpdateShadows`, so no light
   takes a dynamic tile, no record sets `Flags` bit 0 and the copy and the second
   pass never run. `TestDynamicAtlasOffKeepsEverythingStatic` is the explicit test
   the step asked for, and it checks the records rather than the pass: a record
   still selecting the second atlas would sample one nothing ever wrote.
3. ~~Ship `configs/low.toml`.~~ **Done**: 2048 atlas, no dynamic atlas, cheap PCF,
   no MSAA, no anisotropy, 1280×720. **No cluster keys** — Part G is deferred and
   there are none to set. `TestLowConfigTurnsThingsDown` asserts it actually turns
   things down, including that the slot counts are *un*touched.
4. ~~Extend `settings`' test coverage.~~ **Done**: `TestShadowKeysReachTheirVariables`
   (every key lands), `TestShippedConfigsLoad` (both files), plus ten new
   rejection cases in `TestInvalidConfigsAreRejected`.

**Four things the plan did not say.**

- **`init()` had to move into `settings`.** `scene/shadowatlas.go` validated the
  layout in an `init()` panic while it was compile-time. A configured layout is
  validated in `settings.checkShadowAtlas` instead, which rejects the file — the
  established behaviour for a bad value, and the right one here: a layout that
  cannot be carved is not an error anywhere else. Allocation simply refuses,
  every light ends up with `ShadowIndex = -1`, and the scene renders unshadowed
  with nothing logged.

- **Tier sizes are derived, not configured.** `shadowTiers` used to name
  `atlasSize/8, /16, /32` in its own list beside `slotLayout`'s. Two lists that
  had to agree is exactly the failure a config multiplies, so a tier's size is
  now `slotLayout[i+1].size` and only the *scores* are a key. A ceiling can no
  longer name a size no pool holds. `TestTierSizesFollowTheSlotRows` guards it.

- **A smaller atlas needed a shader change, or the knob shipped broken.** This is
  the `TODO.md` item that said so: `NORMAL_OFFSET_2D` / `NORMAL_OFFSET_CUBE` in
  `forward.slang` are world-space constants tuned at 4096, and halving the atlas
  halves every tile, doubling a texel's world footprint and re-introducing the
  acne they were tuned to hide. `FrameUniforms` grew a `ShadowNormalScale`
  (4844 → **4848** bytes) carrying `4096 / atlasSize`, and `shadowLookup`
  multiplies by it. **1.0 at the default**, so the default image is byte-identical
  to what Part F shipped — which is the only reason this was safe to do without an
  eyeball. `spirv-dis` confirms the new member at offset 4844.

- **Cheap PCF rides `ShadowRecord.Flags` bit 1** rather than growing a struct.
  `Flags` had one bit in use and 31 spare, so the quality knob cost zero layout
  risk — and it is per-tile for free, should a tier ever want to spend fewer taps
  on a small slot than a large one. Cheap keeps the 4 corner taps and drops the
  3×3 refinement, so a penumbra quantises to quarters instead of ninths.

**Two constants that deliberately stayed constants.** `nextTierThreshold` and
`slotStickiness` are hysteresis margins, not quality: they trade re-bakes against
responsiveness, and a wrong value is a flicker rather than a tier. Exposing them
would be four more keys nobody tunes and two more ways to make the allocator
thrash. `lightConstant` and `lightCutoff` in `scene/light.go` stayed for the same
reason — `lightCutoff` inverts the falloff that `forward.slang` hardcodes, so it
is only meaningful in lockstep with a shader edit.

**Dead keys removed.** `[shadows] width` / `height` and
`settings.ShadowAspectRatio()` had **no callers at all** — they predate the atlas
and drove nothing. `TODO.md` asked whether they were dead; they were.

**Gate.** Standard gate across every config: builds, `go test ./...` green (19
in `scene`, 7 in `settings`), `spirv-val --scalar-block-layout` clean on all 12
modules, and `go run .` with `[debug] validation = true` silent on
`configs/vulkan.toml` and on `configs/low.toml`.

**Risk, as written.** Knobs that silently do nothing, "confirm each one's visible
effect once, by hand, at the extremes of its range". Done for what a terminal can
see: `atlasSize` runs clean at 1024 and 8192, `low.toml` runs clean end to end,
and a bad value (`pcf = "medium"`) rejects the file with a message naming the key.
**What a terminal cannot see is the visible half** — that a 1024 atlas looks
coarser rather than acne-ridden, and that cheap PCF looks harder rather than
broken. Both need RenderDoc or an eyeball; neither is asserted here.

---

## As each part lands

Move its content into `../FEATURES.md` (why it is built that way) and
`../ENGINE_FLOW.md` (how a frame runs), then strike the part from here. When
both this file and `LIGHTING_PLAN.md` are empty, delete them.
