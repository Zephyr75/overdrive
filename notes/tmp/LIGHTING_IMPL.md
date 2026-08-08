# Lighting implementation — Parts A–H

**Status: not started.** The build order for `LIGHTING_PLAN.md`, split so each
part is a session's work that leaves the tree running.

Scope: what to edit, in what order, and how to know each part landed. Every
`§n` below points into `LIGHTING_PLAN.md`.

Not here: *why* the design looks like this — the decisions, the atlas layout,
the capacity arithmetic and the rejected alternatives all live in
`LIGHTING_PLAN.md`.

---

## Parts

| part | theme | risk | unlocks |
|---|---|---|---|
| [A](#part-a--light-model) | light model, spot lights | struct layout | ~64 lights |
| [B](#part-b--atlas-plumbing) | atlas plumbing | image layouts | tiles drawable |
| [C](#part-c--records-and-atlas-sampling) | records, atlas sampling | tile bleeding | one sampler, N tiles |
| [D](#part-d--the-allocator) | quadtree allocator | fragmentation | variable resolution |
| [E](#part-e--staticdynamic-split) | static/dynamic, caching | classification | the shadow budget |
| [F](#part-f--depth-prepass) | depth prepass | MSAA + `EQUAL` | overdraw, AO input |
| [G](#part-g--clustered-forward) | clustered forward | Z distribution | 1000s of lights |
| [H](#part-h--quality-tiers) | quality tiers | dead knobs | the low-end story |

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

1. Append four fields to `LightData`, **68 → 84 bytes**. Nothing existing moves,
   and scalar layout means no padding members:

   ```go
   Constant, Linear  float32
   Quadratic, Cutoff float32   // Cutoff is the inner cone cosine
   Type              int32
   OuterCutoff       float32   // outer cone cosine, for the spot falloff
   Radius            float32   // attenuation cutoff, §5.1
   ShadowFirst       int32     // first ShadowRecord, -1 = unshadowed (Part C)
   ShadowCount       int32     // 1 sun/spot, 6 point (Part C)
   ```

   Mirror it in `common.slang`.
2. `MaxLights` 8 → 16 in `renderer/uniforms.go`, `MAX_LIGHTS` in
   `common.slang`.
3. Update the two `init()` guards: `LightData` 68 → **84**, `FrameUniforms`
   1184 → **1984**. The arithmetic: the light array goes 8 × 68 = 544 to
   16 × 84 = 1344, so +800.
4. Add `LightSpot = 2` beside `LightSun` / `LightPoint`. Add `Cutoff` and
   `OuterCutoff` to `scene.Light`; add `<cutoff>` and `<outerCutoff>` to
   `LightXml`; parse `"spot"` in `LightXml.toLight` (`scene/light.go:55`).
   Delete the hardcoded `cos45` in `scene/scene.go` — the cutoff stops being a
   constant.
5. Derive `Radius` once in `toLight`: the distance at which
   `intensity / (constant + linear·d + quadratic·d²)` falls below `1/255`. Solve
   the quadratic; clamp to something sane when `quadratic` is 0.
6. `forward.slang`: add `calcSpotLight` — `calcPointLight` times a smoothstep
   between `outerCutoff` and `cutoff` on `dot(-L, direction)`. Switch on
   `light.type` in `fsMain`.
7. `forward.slang`: the attenuation early-out (§6), before both the BRDF and any
   shadow lookup, skipped for `LIGHT_SUN`.
8. `plugin/xml_export.py`: export Blender `SPOT` lamps, mapping `spot_size` and
   `spot_blend` to the two cosines. Add a spot light to `assets/showcase.xml`.

**Gate.** Standard gate, plus: the showcase renders a visible cone, and
`MaxLights` 16 does not regress FPS — the early-out should make it *faster* than
8 lights without one.

**Risk.** The `LightData` growth is the most dangerous edit in the plan. Do not
batch it with anything else in one commit.

---

## Part B — Atlas plumbing

**Goal.** The two new `Backend` methods and the atlas render target, with
nothing yet using them.

**Touches.** `renderer/backend.go`, `vulkan/backend.go`, `vulkan/draw.go`,
`../ENGINE_FLOW.md`, and `go-vulkan`.

**Steps.**

1. **`go-vulkan` first:** bind `vkCmdCopyImage` and `VkImageCopy`. Neither
   exists today and neither is listed in `BINDINGS_GAP.md` — one function plus
   one struct, alongside batch 5's `CmdBlitImage`. `CmdSetViewport` and
   `CmdSetScissor` are already bound (`vk/cmd.go:225,237`).
2. Add `SetViewportScissor(x, y, w, h int)` and
   `CopyDepthRegion(src, dst RenderTargetHandle, srcX, srcY, dstX, dstY, w, h int)`
   to the `Backend` interface (§8).
3. Implement both on Vulkan: `vkCmdSetViewport` + `vkCmdSetScissor`, both
   already dynamic state; `vkCmdCopyImage` with the depth aspect and the layout
   transitions around it.
4. Allocate the atlas through the existing `RenderTargetSpec` with
   `Cube: false` — it is an ordinary large 2D depth target, no new spec field.
5. Record the invariant-2 amendment in `../ENGINE_FLOW.md` §5: a viewport is set
   by `BeginPass`, or narrowed by `SetViewportScissor` within a pass on an atlas
   target, and by nothing else.

**Gate.** Standard gate. A throwaway test that bakes the existing sun shadow
into a corner of a 4096 atlas and samples it back proves both methods without
any of Part C.

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

1. Define `ShadowRecord` (§5.2, 96 bytes) in `renderer/`. Put the array in a
   storage buffer reached by device address and push it as a **third pointer**
   beside the two `PushConstants` already carries (§2.5) — no new descriptor, no
   texel buffer, no bindings work. `PushConstants` goes 16 → 24 bytes, well
   inside the 128-byte minimum.
2. Rework `FrameUniforms` per §5.3, to:

   ```go
   View, Projection mgl32.Mat4            // 128
   BakeMatrix       mgl32.Mat4            // 64   → 192
   ViewPos          [3]float32            // 12   → 204
   LightCount       int32                 //  4   → 208
   Lights           [16]LightData         // 1344 → 1552
   TexShadowStatic  TextureHandle         //  4   → 1556
   TexShadowDynamic TextureHandle         //  4   → 1560
   TexSkybox        TextureHandle         //  4   → 1564
   ```

   Expected size **1564**, down from Part A's 1984: the six-matrix array and
   the cube bookkeeping leave and nothing fixed-size replaces them. Only 380
   bytes above today's 1184, for twice the lights and a bigger `LightData`.
   Update the `init()` guard.
3. Replace `Light.RenderShadowMap` (`scene/light.go:99`) with a tile bake: one
   `BeginPass` on the atlas for the whole frame, `SetViewportScissor` per tile,
   one `BakeMatrix` per tile (§4.4's loop). A point light becomes six ordinary
   tile bakes at 90° with the `+2 texel` FOV widening from §4.2. Delete
   `Light.setup`'s per-light `CreateRenderTarget` and the `shadowTarget` /
   `depthMap` / `depthCubeMap` fields with it.
4. In `core/app.go`, the two-iteration `for _, i := range [2]int32{dirCaster,
   pointCaster}` loop becomes one call into the tile bake.
5. Delete `depth_cube.slang` and its `CreateShader("depth_cube")` call. The
   backend probes for `<name>.geo.spv`, so nothing else references it. If
   `BACKEND_DECISION.md` §9 item 3 has not landed yet, the Vulkan
   `FrontFace = Clockwise` shadow-pass case goes here too — it exists only to
   match that geometry stage's memory layout.
6. `forward.slang`: one `shadowLookup(record, fragPos, normal)` replacing
   `shadowCalculation` and `shadowCalculationCube`. Cube face selection is the
   major axis of `fragPos - light.position`, indexing
   `records[light.shadowFirst + face]`. Keep the 4-tap early-bail, and **clamp
   every tap to the record's rect inset by one texel** (§4.2).

   Keep storing **linear radial distance to the light**, as the current cube
   path does, rather than switching to per-face projected depth — that is what
   `ShadowRecord.FarPlane` carries. Radial distance is face-independent, so
   depth is continuous across a face boundary and the bias tunes once rather
   than per face (§2.4).
7. `common.slang`: `shadowMap2D` and `shadowCubeMap[]` collapse to two
   `Sampler2D`; the dedicated cube descriptors and `MAX_SHADOW_CUBES` go.
8. Interim allocation: hardcode a fixed partition — sun tile plus N point-light
   tile groups — so the part is testable before Part D exists.

**Gate.** Standard gate, and specifically: the showcase scene's shadows are
indistinguishable from before this part. It is a pure refactor of *where* shadow
data lives.

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
   `ShadowFirst = -1`. Running out of budget must cost shadow quality and never
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
   `FrameUniforms` into the same storage buffer, at which point `MaxLights = 16`
   becomes the per-*cluster* cap rather than the scene cap and `FrameUniforms`
   drops to roughly 236 bytes.
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
