# FEATURES.md — what the engine does, and what comes next

What is implemented beyond bare mesh rasterisation, why each piece is built the
way it is, and the roadmap in value-to-effort order.

Read alongside `ENGINE_FLOW.md` (the renderer contract, operationally) and
`ARCHITECTURE.md` (where the code lives). Paths are relative to `src/`.

---

## Contents

- [Part 1 — implemented](#part-1--implemented)
  - [The abstraction](#the-abstraction)
  - [Lighting](#lighting--cook-torrance-pbr-three-light-types)
  - [Shadows](#shadows--one-atlas-one-lookup-with-pcf)
  - [Multi-light support](#multi-light-support)
  - [PBR materials](#pbr-materials--metallic-roughness-cook-torrance)
  - [Materials and textures](#materials-and-textures)
  - [Normal mapping](#normal-mapping)
  - [Environment and reflection](#environment-and-reflection)
  - [Scene and assets](#scene-and-assets)
  - [UI overlay](#ui-overlay)
  - [Depth prepass](#depth-prepass--shade-each-visible-pixel-once)
  - [Quality tiers](#quality-tiers--one-code-path-from-a-discrete-gpu-down)
  - [Anti-aliasing](#anti-aliasing--msaa-on-the-backbuffer)
- [Part 2 — roadmap](#part-2--roadmap)
- [Performance notes](#performance-notes)

---

## Part 1 — implemented

### The abstraction

- The scene layer makes zero graphics-API calls. Everything goes through
  `renderer.Backend`/`Frame`/`Pass`/`Compute` (24 methods), implemented in
  `vulkan/`. An OpenGL 4.1
  backend existed until 2026-08-05; `tmp/BACKEND_DECISION.md` §1–2 is why it went and
  why the abstraction stayed
- Shaders are authored in Slang (`shaders/slang/*.slang`) and compiled to SPIR-V
  by `build_shaders.sh`
- The modern Vulkan stack, matching howtovulkan.com: 1.3 dynamic rendering,
  buffer device address with scalar layout for uniforms, bindless descriptor
  indexing, synchronization2, VMA, 2 frames in flight
- **What it cannot express yet** — compute, pipeline objects, a pass list,
  per-material shaders, HDR formats — is `tmp/BACKEND_DECISION.md` §6, and the
  ordered work to fix it is §9

### Lighting — Cook-Torrance PBR, three light types

Declared in `scene/light.go` (`renderer.LightSun`, `LightPoint`, `LightSpot`) and
evaluated in `shaders/slang/forward.slang` with a metallic-roughness microfacet
BRDF:

- **Directional (`sun`)** — `calcDirLight`, infinite light along `direction`,
  radiance `color · diffuse · intensity`
- **Point** — `calcPointLight`, inverse-square falloff, radiance scaled by that
  attenuation. Import divides Blender's intensity by 1000 to match
- **Spot** — `calcSpotLight`, the point falloff times a `smoothstep` between the
  outer and inner cone cosines. The XML carries Blender's own `<cone>` (degrees)
  and `<coneBlend>` (0..1) so the export round-trips; `LightXml.toLight` converts
- All three feed the shared `cookTorrance` evaluator
- Up to `MAX_LIGHTS` (64) lights in any mix, with a per-light `Radius` (derived
  at load in `scene.lightRadius`) letting the fragment loop skip a light before
  the BRDF or any shadow tap. That early-out is what makes 64 affordable

### Shadows — one atlas, one lookup, with PCF (Percentage-Closer Filtering)

Driven by `Scene.BakeShadows` (`scene/shadowatlas.go`), in one depth pass before
the main pass. **Every shadow in the scene is a sub-rect of one 4096² depth
texture**, and the tile is selected by viewport rather than by binding a
different image:

- **Directional → one tile.** Orthographic light-space matrix, baked with
  `depth.slang`, holding ordinary projected depth
- **Point → six tiles.** Six 90° perspective matrices, one per face, baked with
  `depth_point.slang`, holding **linear radial distance / `farPlane`**. Radial
  distance is face-independent, so depth stays continuous across a face boundary
  and one bias covers all six
- **Spot → one tile.** A perspective frustum at the cone's own full angle,
  widened by two texels like a cube face is, baked with `depth.slang`. It shared
  the sun's ortho branch until Part D, which nothing noticed because no spot was
  ever picked as a caster
- **One `shadowLookup`** in `forward.slang` serves all of them. It projects
  through the tile's own matrix, maps tile-local uv through `AtlasRect`, and
  forks only on `Face` (`-1` projected depth, `0..5` radial)
- The winding convention for this pass (positive viewport, clockwise front face)
  is in `ENGINE_FLOW.md` §5

#### Why an atlas rather than a texture per light

A cubemap gave two things free that an atlas must pay for, and the trade was
still worth it:

- **Seamless cross-face filtering.** Gone. Each cube face's frustum is widened to
  `90° + 2 texels` (`cubeFaceFov`) so a PCF kernel at a face edge still finds its
  neighbourhood inside its own tile. It fixes edges exactly and corners only
  approximately — three faces meet there and no single widened frustum covers it
- **A border.** Gone too: a tile's neighbours are the adjacent texels of the same
  image, so `BorderColorOpaqueWhite` no longer means "outside the frustum".
  `shadowLookup` tests the tile-local uv explicitly, and clamps every tap to the
  tile inset by one texel. Miss either and one light shows another's shadow

What that buys: the pass count stops scaling with the light count (N target binds
→ N viewport changes, and a viewport change is nearly free where a target bind is
a render-pass boundary), tile size stops being one compile-time constant shared
by every light, and the static/dynamic caching in Part E gets a single texel
currency to budget in. `tmp/LIGHTING_PLAN.md` §2.4 is the full argument.

#### Shadow bias — normal-offset

Shadow filtering needs a bias to escape **acne** (self-shadowing from depth-map
quantisation). The trade-off is **peter-panning**: too much bias detaches the
shadow from the object's base.

`shadowLookup` uses a **normal-offset** bias rather than a depth bias: the
receiver sample point is pushed along its surface normal in world space
(`NORMAL_OFFSET_2D = 0.08` scaled by the incidence angle, `NORMAL_OFFSET_CUBE =
0.10` flat), _before_ projecting into the tile. Escaping acne geometrically means
the residual constant depth bias is tiny and contact shadows stay attached.

The offsets are tuned for the showcase's ~10-unit scene scale — rescale them if
the scene scale changes. Alternatives if this needs revisiting:

- **Front-face culling in the shadow pass.** Tried, and **rejected**. It escapes
  acne by hiding the bias inside the geometry, but it bakes the _far_ side of a
  closed mesh, so the depth stored is a whole thickness too far: a sphere floats
  above a lit disc of its own diameter. Textbook peter-panning, and severe on
  anything round. Every tile bakes `CullBack`, and `Mesh.CastsShadow` handles the
  case front-face culling was reached for — see below
- **Slope-scaled depth bias** (`vkCmdSetDepthBias`) — the missing piece, and the
  one that would cover a flat surface which legitimately must cast. It scales the
  bias by the depth gradient, which is exactly the quantity that blows up at
  grazing incidence. Not bound in `go-vulkan` yet

#### Early-bail PCF

`pcfTile` first takes 4 corner taps. If they unanimously agree — fully lit or
fully shadowed, true for almost every fragment outside a penumbra — it returns
immediately and skips the 3×3 kernel. Only soft edges pay full price and quality
is unchanged.

The atlas cost the point-light path its old filter: a 20-tap 3D disk whose radius
grew with view distance became the same 4-then-3×3 as everything else, since a
tile is sampled in 2D. Penumbra width on point shadows is narrower for it.

### Multi-light support

The forward pass evaluates up to `MAX_LIGHTS` (64) lights per fragment in any mix.

- **Uniform block.** `renderer.FrameUniforms` carries `Lights [64]LightData` plus
  `LightCount` and the two atlas handles. `MaxLights` is duplicated in
  `renderer/uniforms.go` and `common.slang` and must stay in step
- **Fragment loop.** `forward.slang` loops `l < lightCount`, skips any light whose
  `radius` does not reach the fragment, branches on `light.type`, and adds
  `calcDirLight` / `calcPointLight` / `calcSpotLight`
- **Shadows are decoupled from light order**, and from light _type_. Each
  `LightData` carries its own `shadowIndex` / `shadowCount` into the record
  array — `-1` means unshadowed — so the shader needs no side table of which
  light owns which map. A point light's six records are consecutive, and the
  face is picked from the major axis of `fragPos - light.position`
- **The atlas is carved once, not repartitioned per frame.** `slotLayout` in
  `scene/shadowatlas.go` declares how many slots exist at each size, `buildLayout`
  places them at scene load, and those rects never move again. Every size is a
  division of `atlasSize` rather than a pixel count, so changing the atlas
  rescales the whole layout instead of changing how many lights fit — **atlas
  size buys sharpness, the slot counts buy light budget**, and they are separate
  knobs. That is what a quality setting wants: turning shadows down must not stop
  lights casting

- **Slots are typeless; only size matters.** A point light's six faces each carry
  their own `atlasRect` and are never filtered across, so they need not be
  adjacent — a point light takes any six slots of one size, from wherever they
  are. That is what stops a fixed layout from being rigid, and it is visible in
  the atlas: a point light's tiles are scattered across it rather than adjacent

- **Who casts is decided per frame**, by `shadowAtlas.allocate`. Every light
  scores `Radius / distance to camera` — its rough screen-space footprint, which
  is why Part A's `Radius` had to exist first. **Rank picks the slot, the tier
  caps it**: lights sort by score and take the best free slot no larger than
  their ceiling, where `shadowTiers` sets that ceiling at 512 above 0.50, 256
  above 0.20, 128 above 0.08 and nothing below. A sun skips the score entirely
  and is capped at 2048: it has no radius, and it is the one light every pixel
  sees. The 2048 slot is the sun's by construction, nothing else being capped
  that high, so a scene with no sun should trade that row for four 1024s

- **A point light takes six slots at its tier's size, not a smaller one.** It was
  capped a tier below for a while, so six faces would not cost 6× a spot's texels
  at the same score — rationing that made sense against an allocator which could
  hand the whole atlas to whoever asked first, and is redundant against fixed
  pools, where a pool holds only the slots it holds. Keeping it had a cost no
  reasoning surfaced and one atlas dump did: **the largest scored tier and the
  largest scored pool are the same size**, so halving locked every point light out
  of that pool, which then stood empty behind lights entitled to it while
  everything below shuffled a tier down. Removing it took `stress.xml` from 74.2%
  to 91.8% and filled the 512 quadrant 16/16. Its six tiles stay all-or-nothing —
  five would leave a lit wedge, which reads as a hole rather than a coarser shadow

- **The ceiling stops a light competing for a slot, not using an idle one.**
  Phase 1b re-offers whatever is still spare to whoever ended up under their
  ceiling, largest first and still in rank order. Without it, a layout tuned for
  one light mix wastes its unused sizes on a scene with a different one — the
  measured cost on `stress.xml`, which has no near point lights, was 11 points of
  occupancy and six far spots stuck at 128. Only degraded lights move, so a light
  already at its ceiling never churns

- **Degradation is the failure mode, never frame time.** A light that finds no
  slot at its ceiling walks down a pool at a time and finally holds nothing,
  which leaves `ShadowIndex = -1` and lights it unshadowed. Nothing gets slower

- **Two hysteresis margins, on two different axes.** `nextTierThreshold` (20%) is
  the margin against a fixed score threshold, so a light hovering on a tier
  boundary keeps its ceiling. `slotStickiness` (20%) is the margin on the ranking
  itself, and it exists because a fixed pool has a failure a splitting tree does
  not: once a pool is empty, two lights with near-equal scores trade its last slot
  every frame, and the loser cannot be served a step smaller out of the same
  space. That is a forced re-bake plus a visible flicker.
  `TestSlotStickinessSurvivesContention` is the guard

- **The layout quadtree survives, halved.** `quadNode` still places the slots,
  because filling largest size first can never fragment and there is no packing
  heuristic to get wrong. But it runs once at load and never frees, so `release`
  and the sibling coalescing went with the per-frame allocator — that was the
  subtle half, and the half that failed silently

Measured on `stress.xml` — 64 lights, which is `MaxLights` exactly: 1 sun, 24
spots and 16 point lights in coloured rings, plus 23 dim white point lights as
fill. All 64 shadowed, 11 of them degraded a step, **259 tiles filling 91.8% of the atlas**.
The remaining 25.8% cannot be claimed by this scene: `MaxLights` runs out before
the atlas does, which is `tmp/LIGHTING_PLAN.md` §4.5's "77 against 64" arriving
in practice. See §"Reading the shadow atlas" below for how those numbers were
read off a capture.

No frame-rate figure goes with that, and none can: the swapchain is
`PresentModeFifoKHR` and this machine's display is 180.03 Hz, so 9 lights and 64
lights both read ~181 FPS. See "Measuring by FPS subtraction does not work here"
under Performance notes — GPU timestamp queries are the instrument, and they are
blocked on query-pool bindings.

Filling that headroom by letting lights climb above their ceiling was tried and
reverted. With this much spare, every light reaches the largest pool there is and
the score stops selecting a resolution at all — a lone spot held the *sun's* 2048
slot at every distance. `TestTileSizeTracksCameraDistance` is the guard, and the
surplus pass stays restricted to genuinely degraded lights.

#### Retuning the layout

The atlas is 16 cells of 1024², and §4.1's partition spends all 16: the sun's
2048 is one quadrant, and the 512, 256 and 128 rows are one quadrant each. Cost
per light, in cells:

| light | cells |
| --- | --- |
| spot @1024 | 1.0 |
| spot @512 | 0.25 |
| spot @256 | 0.0625 |
| spot @128 | 0.015625 |
| point @512/face | 1.5 |
| point @256/face | 0.375 |
| point @128/face | 0.09375 |

**Why there is no 1024 tier.** A 1024 row of four slots costs 4 cells — a whole
quadrant — and the only quadrant available is the 128 row. So a 4096² atlas can
have §4.3's high tier *or* the 40 far point lights §4.1 puts in the bottom right,
never both. The drawing chose light count; the engine follows it. An 8192 atlas
is what buys both.

Pure-population capacity after a sun, everything at one tier:

| tier | spots | point lights |
| --- | --- | --- |
| top (score > 0.50, i.e. distance < 2× radius) | 48 | 32 |
| mid (> 0.20) | 192 | 128 |
| far (> 0.08) | 768 | 512 |

Those numbers are **resolution-independent** — halving `atlasSize` halves every
slot uniformly and the same lights still cast, which is the whole point of
declaring the layout in divisions. What the atlas size does decide is texels per
face: at 4096 a far point light's face is 128², at 2048 it is 64².

**The real ceiling is draw calls, not atlas space.** Every tile re-draws every
casting mesh, so `stress.xml`'s 121 tiles against 4 casting meshes is 484 draws —
nothing — but the same layout in a scene with 200 casting meshes is 26,600. That
is what stops the slot counts simply being raised, and Part E's static/dynamic
split is the answer to it.

**Lighting a scene so its shadows are visible turned out to be its own problem**,
and the showcase's comment block records the three rules it took to get right:
a light must be high and off to one side or its shadow lands where no visible
ground catches it; a spot beats a point light because it contributes exactly zero
outside its cone, where a point light lights everything a little and that pedestal
is what a shadow cannot cut through; and with several lights on one fragment the
Reinhard curve compresses hard enough that the shadow has to be read by **hue**
rather than by brightness, which is why the six spots are near-primaries.

#### `Mesh.CastsShadow` — the acne fix that is not a bias

A mesh with `<castsShadow>false</castsShadow>` is skipped by `BakeShadows`.
Default true; the exporter writes it only when Blender's own "Shadow" ray
visibility is off.

The showcase ground uses it, and the reasoning generalises: **a single-sided
plane with the whole scene above it can only ever occlude itself.** Nothing is
below it to receive its shadow, so every texel it writes into a shadow map is a
chance to shade against itself — and at the grazing angles a high light gives a
large plane, the depth gradient across one texel dwarfs any constant or
normal-offset bias. That is acne over the entire surface.

This was found the expensive way. `BakeShadows` originally set `CullFront` for
the 2D tiles only, so cube faces baked `CullBack` and the ground went into every
point light's map. With one point light casting it was a slight darkening nobody
noticed; with every light casting, the showcase rendered **almost black** — which
reads as "the lights broke", not "the bias is wrong". The first fix was
`CullFront` everywhere, which cured the acne and introduced peter-panning: a lit
disc under every sphere, the size of the sphere. Excluding the caster is what
fixes the actual problem, and it leaves `CullBack` free to keep contact shadows
welded to their objects.

The remaining general gap is a flat surface that legitimately must cast — a wall,
a floor with a room below. That wants slope-scaled depth bias
(`vkCmdSetDepthBias`), which `go-vulkan` does not bind yet.

> **Bug, now fixed.** The point-light branch used to scale its shadow by **5.0**,
> so `Lo += contrib * (1 - shadow)` went negative on a fully shadowed fragment
> and _subtracted_ light other lights contributed. Harmless while one point light
> cast; with every point light shadowed it is black blotches, so the factor came
> out here rather than in Part E.

### Reading the shadow atlas

The atlas is the one render target nothing ever puts on screen, so a wrong tile
rect, a missing bake or a light baking into another's pixels stays invisible
until it shows up as a shadow in the wrong place. It is read out of a **RenderDoc
capture** — the engine has no readback path, deliberately: the only call that
could provide one blocks on an idle queue, and nothing that stalls the pipeline
belongs in the abstraction for a debug feature's sake.

Two things about the image that are not obvious when reading it:

- **The three bakes do not share a depth encoding**, so no single contrast range
  reads all of them: an ortho sun tile is linear in z, a cube face is radial
  distance over the far plane, and a spot is projected perspective depth crowded
  against 1. A range that shows the sun renders every spot tile white.
- **A tile's depth looks plausible whatever rect the record claims**, so a rect
  bug shows up only by holding the tile positions in the image against the
  `AtlasCoords` in the `ShadowRecord` array.

### Two scenes, and why the showcase is not enough

`assets/showcase.xml` is the beauty shot. `assets/stress.xml`
(`go run . -scene stress.xml`) is the allocator's.

The showcase **saturates the top tier**: its nine lights all sit within ~15 units
of the camera with radii over 100, so every score lands between 4.7 and 62
against a 0.50 threshold, and every one of them is entitled to the largest tile a
scored light can hold — 512, the point lights six of them each — so the 512 pool
saturates and the 256/128 pools stay empty for want of anything far enough away.
Variable resolution goes untested here.

The stress scene derives each light's intensity **from the tier it should land
in** — `scene.lightRadius` inverts the shader falloff, so solving it backwards
gives the intensity that puts a light at a chosen distance into a chosen tier.
41 coloured lights in rings at scores 0.75 / 0.30 / 0.13 occupy 121 tiles and
44.9% of the atlas. A further 23 dim white point lights — the whole remaining
`MaxLights` budget — take it to **64 lights, 259 tiles, 91.8%**, nothing
unshadowed and 11 degraded a step. Point lights rather than spots for the fill, because six
faces each means 23 lights buy 138 tiles where spots would buy 23, and tile count
is the bake cost. What stops it reaching 100% is `MaxLights`, not the atlas.

The frame rate is ~181 either way and that is the vsync ceiling, not a result;
the per-frame quadtree this layout replaced packed the 41-light version into
87.1% of a partition it rebalanced to fit.

That is the same gap Part A recorded about the attenuation early-out ("the
showcase cannot show that") and it has the same fix: a scene of many dim,
localised lights.

### Seeing the frame itself

The presented frame is inspected in RenderDoc too. Two `[debug]` switches exist
because the frame alone is ambiguous:

- **`[debug] lockCamera`** skips the input handler and never installs the
  cursor callback. With the cursor captured, the compositor delivers a position
  event of its own choosing during the first frames and the view drifts
  differently every run, which makes two captures incomparable.
- **`[debug] noShadows`** forces every `ShadowIndex` to -1 while still
  baking every tile. A scene that is dark because its lights are dim and a scene
  that is dark because every light is wrongly occluded are the same picture and
  have nothing in common in the code; this is the A/B that tells them apart. It
  is what found the self-shadowing bug below in one run.

### PBR materials — metallic-roughness Cook-Torrance

- **BRDF.** `cookTorrance` in `forward.slang`: GGX/Trowbridge-Reitz normal
  distribution (`distributionGGX`), Smith geometry via Schlick-GGX with the
  direct-lighting `k = (r+1)²/8` (`geometrySmith`), Fresnel-Schlick
  (`fresnelSchlick`), plus a Lambertian diffuse lobe. Energy conserving: the
  diffuse weight is `kD = (1 - F)(1 - metallic)`, so metals have no diffuse
- **Material model.** `MatDiffuse` doubles as base colour / albedo (sampled
  texture × tint, linearised with `pow(·, 2.2)`). `scene.Material` carries
  `Metallic`, `Roughness`, `Ao`, loaded from the MTL PBR extension keys `Pm` and
  `Pr` in `scene/mesh.go`, defaulting to dielectric and matte for legacy
  materials. `F0 = lerp(0.04, albedo, metallic)`
- **Tonemapping.** PBR radiance is unbounded, so `fsMain` ends with a Reinhard
  tonemap plus gamma to stay displayable in the LDR backbuffer, until a real
  HDR pass lands (roadmap §2)

The theory behind all of this is in `cheatsheets/PBR.md`.

### Materials and textures

- `scene.Material`: ambient, diffuse (= albedo), specular, shininess, alpha,
  metallic, roughness, ao, plus a diffuse texture and a normal map
- **Bindless textures** (`sampler2D[256]` + `samplerCube[64]`,
  `PartiallyBound | UpdateAfterBind`); handle 0 is a built-in white pixel, and a
  texture handle travels to the shader as a slot index inside the uniform block
- **The two shadow atlases are the exception**: they get dedicated `Sampler2D`
  descriptors (bindings 2 and 3) rather than bindless slots. See
  [performance notes](#performance-notes)
- Texture paths are portable: `texturePath` strips Blender's baked absolute path
  to a basename and resolves against the project-local `textures/`

### Normal mapping

- Tangent-space normal maps are sampled in `forward.slang` (`perturbNormal`).
  The TBN basis is derived per fragment from screen-space derivatives of
  `fragPos` and uv (Schüler's cotangent frame) — no tangents in the vertex
  layout, so the 32-byte `pos|normal|uv` vertex buffer and `CreateMesh` are
  untouched
- Driven by `UseNormalMap` in `DrawUniforms`, set per face group in `Mesh.draw`;
  meshes without a map fall back to the interpolated geometric normal. The map
  loads from an MTL `map_Bump` / `bump` entry

### Environment and reflection

- **Skybox** (`scene/skybox.go`, `shaders/slang/skybox.slang`): a cubemap drawn
  behind the scene with `LEQUAL` depth, from a copy of the frame block whose view
  translation has been stripped
- The same cubemap doubles as a crude **reflection probe** consumed by the PBR
  ambient term: sampled along `N` for irradiance and along `reflect(-V, N)` for
  specular, mixed by `fresnelSchlickRoughness` and scaled by `ao`. So metals
  mirror the sky and dielectrics pick up a soft tint, with no separate reflection
  term. Real prefiltered-mip IBL is roadmap §1

### Scene and assets

- XML scene description (`scene/scene.go`) loading camera, meshes, lights and
  skybox; meshes from OBJ/MTL parsed in `scene/mesh.go`
- Per-frame `Scene.UpdateMeshes` reuploads geometry the physics step moved
- **Showcase scene** (`assets/showcase.xml`, the default) exercises every
  feature: a normal-mapped paving ground, a metal Suzanne (`Pm 1`), brick and
  wood primitives (dielectric, normal-mapped), and a fully metallic low-roughness
  chrome sphere (`Pm 1, Pr 0.08`) mirroring the skybox, lit by a directional sun
  (one atlas tile) plus a warm point light (six tiles) and an unshadowed violet
  spot. Colour and normal maps are CC0 from ambientCG
- Static mesh geometry is baked into the OBJ vertices in world space and rendered
  with an identity model matrix, so `<position>` is unused for static meshes
- `scene/showcase_test.go` loads the scene with no GPU and asserts its contents

### UI overlay

Widget trees from [Gutter](https://github.com/Zephyr75/gutter) are rasterised on
the CPU into an RGBA image, uploaded with `Backend.UpdateImage`, and composited
as an ordinary fullscreen mesh built once by `core.newOverlay`. It redraws only
when the tree or the hover state changed.

On Vulkan the upload is _staged_ and copied at the top of the next frame, because
a copy cannot be recorded inside a render pass — one frame of latency, no queue
stall. `main.go` currently passes a nil widget, so only the debug crosshair draws.

### Depth prepass — shade each visible pixel once

`[renderer] depthPrepass`, on by default. `Scene.RunDepthPrepass` opens a pass of
its own — a depth attachment and no colour — and draws every mesh through
`prepass.slang` (position in, empty fragment stage), filling the backbuffer's
depth with the nearest surface per pixel. The main pass then keeps that depth (its
depth `Attachment.Clear` is nil, so it loads) and the forward pipeline is built
with `CompareEqual`, so only the frontmost fragment survives to run `fsMain`.

**Why it is worth a whole extra geometry pass.** `forward.slang`'s `fsMain` is
the most expensive shader in the engine: per fragment it loops `lightCount`
lights, each running Cook-Torrance plus a `shadowLookup` of 4 to 13 PCF taps. On
`stress.xml` that is 64 lights. Without a prepass, every surface drawn over
later pays that in full and throws the result away — the ground under Suzanne is
shaded, then overwritten. `Scene.RenderScene` draws in XML order and sorts
nothing, so the overdraw is whatever the scene file happened to list.

**It is not buying depth testing.** Early-Z already rejects a hidden fragment
before the shader runs — `fsMain` neither writes `SV_Depth` nor discards, so the
hardware is free to do it. What the prepass buys is the *right draw order*
without sorting: the depth buffer knows the final nearest surface before any
shading starts, so rejection is exact, per pixel, and correct for
interpenetrating geometry that no sort can order.

The trade is one cheap geometry pass against `(overdraw − 1)` expensive fragment
shaders, so it wins on depth complexity and loses on flat scenes. **The showcase
is a flat scene** — five meshes on a plane, overdraw barely over 1.0 — and shows
no measurable gain, which is expected rather than broken. The real payoff is
Part G: clustered forward adds a per-cluster light loop to the same fragment
shader, so running it on hidden fragments gets more expensive, not less. §10's
ambient-occlusion work also wants this depth buffer.

**`EQUAL` is unforgiving, and that shaped the code.** `prepass.slang` cannot
reuse `depth.slang`: that one projects through a single premultiplied
`BAKE.worldToTile`, while `forward.slang:20` does
`mul(projection, mul(view, float4(fragPos, 1.0)))` with `fragPos` already
through `model`. Same value mathematically, different associativity, different
rounding — and `EQUAL` compares bits, so the difference shows as speckle along
every edge rather than as an error. `prepass.slang` exists only to repeat that
arithmetic operation for operation. Keep them in step.

The prepass binds **no colour attachment**, which is a `PassSpec` with `Color`
empty and `Depth` set — no special method, since attachments are pass state and
under dynamic rendering a pipeline just declares the formats it will be used
with. The payoff is that it writes no colour, blends nothing and — under MSAA —
resolves nothing.

It is also handed the *same uploaded `FrameUniforms` address* the forward pass
gets, so the two read the same bytes rather than two independently rebuilt
copies. That removes one of the two ways the arithmetic could drift; keeping
`prepass.slang` in step with `forward.slang` removes the other.

**Transparency has to stay out of it.** Nothing in the engine is transparent
today (`Material.Alpha` is parsed and dropped; `fsMain` returns alpha 1.0), but
the constraint is structural: a transparent surface has no single nearest depth,
and `CompareEqual` shades one fragment per pixel where blending needs several.
Whenever transparency lands it is a third pass — opaque prepass, opaque main with
`EQUAL`, then transparent sorted back-to-front with depth write off and
`CompareLess`. Alpha *cutout* is the opposite case: it is opaque and belongs in
the prepass, but `prepass.slang` must then run the same `discard` as
`forward.slang` or the depth it writes is wrong. See `TODO.md`.

### Anti-aliasing — MSAA on the backbuffer

`[antialiasing] mode` and `samples` in the config file (`none`, or `msaa` at
2/4/8) are read once when the backend initialises, because the count is baked
into a multisampled colour + depth pair resolved into the swapchain image at the
end of the main pass. The request is clamped to
`framebufferColor/DepthSampleCounts`, so an unsupported 8× steps down to 4×
rather than failing device-side; 1 and 4 are guaranteed by the spec.

MSAA rather than a post-process filter because it needs no new pass and no new
render target: FXAA or TAA would mean rendering the scene offscreen, which
`PassSpec.Color []Attachment` plus `Depth` now expresses — nothing structural is
in the way any more, it is simply not built. It also only smooths geometric edges — shader aliasing (specular
highlights, normal-map shimmer) is untouched, which is what a post-process pass
would buy.

The cost is real and worth measuring on the target GPU: on an Intel UHD 620 at
1920×1080 the showcase scene runs ~49 FPS off, ~44 at 4×, ~28 at 8×.

### Quality tiers — one code path, from a discrete GPU down

`configs/low.toml` ships beside `vulkan.toml` and sets no key the default file
does not also have. There is no low-end code path — that is the point.

The whole shadow system is `[shadows]`: `atlasSize`, the `slotDivisors` /
`slotCounts` layout, `tierScores`, `dynamicAtlas`, `bakeBudgetMiB`, `pcf`, and
the projection planes. **Two of those are orthogonal on purpose** — `atlasSize`
buys sharpness and the slot counts buy light budget — so turning shadows down
must never stop lights casting. `low.toml` halves the atlas and leaves the counts
alone: the same 337 slots, the same lights casting, each at half the resolution.

`dynamicAtlas = false` is the low-end switch and it is a real one: no light takes
a dynamic tile, no record sets `Flags` bit 0, and the per-frame copy and second
bake pass never run. Per-frame shadow cost goes to zero and movers cast nothing.

`pcf = "cheap"` keeps the four corner taps and drops the 3×3 refinement, so a
penumbra quantises to quarters rather than ninths. It reaches the shader on
`ShadowRecord.Flags` **bit 1** rather than through a struct field — `Flags` had
31 spare bits, so the knob cost no layout risk, and it is per-tile for free
should a tier ever want to spend fewer taps on a small slot than a large one.

**A bad value rejects the file.** `settings.checkShadowAtlas` is where the
`init()` panics in `scene/shadowatlas.go` went when the layout stopped being
compile-time. That matters more here than for most settings: a layout that cannot
be carved is silent everywhere else — allocation refuses, every light ends up
with `ShadowIndex = -1`, and the scene renders unshadowed with nothing logged.

The two hysteresis margins, `nextTierThreshold` and `slotStickiness`, are
deliberately **not** knobs. They trade re-bakes against responsiveness, so a
wrong value is a flicker rather than a tier — two more ways to make the allocator
thrash and nothing gained.

---

## Part 2 — roadmap

Ordered by value to effort. Each item lists what to touch, and *why* it is wanted
— which is what this section is for. What each one is blocked on, and which
interface method expresses it, is `tmp/INTERFACE_PLAN.md` §4 and §5; that is not
repeated here.

### 1. Texture-driven PBR and real IBL

**Why** today's material values are per-material scalars and the ambient term is
a single skybox sample.

- **Texture-driven PBR.** Add albedo/metallic/roughness/AO _map_ slots — new
  bindless textures plus `map_Pm` / `map_Pr` loading in `scene/mesh.go` — so
  values vary per texel. Today `textures/` holds colour and normal maps only
- **Proper IBL.** Prefilter the skybox into an irradiance cubemap plus a
  roughness-mip prefiltered specular cubemap and a BRDF LUT, as a one-time pass
  at load, replacing the current single-sample approximation.
  `cheatsheets/PBR.md` §9 is the derivation

### 2. HDR, tonemapping, bloom

**Why** unlocks intensities above 1 and physically meaningful lighting.

**Files** a new `tonemap` / `bloom` Slang pass, `scene/` or a new `effects/`, `core/app.go`. Not `vulkan/`.

- Render the main pass into a colour image instead of the swapchain.
  `CreateImage(ImageSpec{Format: FormatRGBA16F, Usage: UsageSampled |
  UsageColorAttachment})` is all it takes now — the half-float binding landed
  with `go-vulkan` batch 1, and `Caps().Formats(FormatRGBA16F)` probes the device
  rather than assuming. **Nothing under `vulkan/` has to change**, which is the
  point: `INTERFACE_PLAN.md` §6 nominates this as the proof of that
- Add a fullscreen post pass: bright-pass plus separable Gaussian blur for bloom,
  then ACES/Reinhard tonemap and gamma to the backbuffer. The stopgap Reinhard at
  the end of `forward.slang` moves here

### 3. Ray-traced shadows

**Why / how** the entry point is a **ray query** (`VK_KHR_ray_query`) dropped
into `forward.slang`'s shadow test, replacing the shadow-map passes and reusing
the existing forward pass and light loop. `cheatsheets/RAYTRACING.md` §5 covers
the ray-query vs RT-pipeline trade-off and the acceleration-structure plumbing;
`tmp/BACKEND_DECISION.md` §8 is the decision context. Follow-ups: RT ambient
occlusion → reflections → one-bounce GI.

Hardware ray tracing is **not** universal — a GTX 1080 or most laptop iGPUs have
no RT cores — so the compute BVH is the baseline and `Supports(FeatureRayTracing)`
is the fork, not a backend gate.

### 4. Known gaps

Smaller items, all of them deliberate for now:

| Gap                                                                          | Where                                                                        |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| No cascades: the sun is one 2048 ortho tile over a hardcoded [-10, 10] box  | `Light.shadowRecord` in `scene/shadowatlas.go`                                |
| A static re-bake redraws every allocated tile, not the slots that changed   | `Scene.UpdateShadows` — a tile with no caster in frustum writes nothing      |
| The score ignores whether a light is on screen at all                       | `lightScore` — the cluster gate is Part G                                    |
| `MaxLights` is a fixed 64, and the score ignores what is off screen         | both are `tmp/CLUSTERED_FORWARD.md`, the one deferred part                   |
| A light that moves does not dirty its own tiles                             | nothing moves a light yet; `Scene.UpdateShadows` when one can                |
| ~~The prepass image is unverified against the prepass-off image~~           | verified: `-screenshot` on and off differ in 6 pixels of 2.07 M              |
| `GeometryShader` is enabled and unused                                       | `depth_cube.slang` retired with the atlas; the feature stays for `PassSpec.Layers` |
| No mipmaps on any texture                                                    | every image is one level; `Frame.GenerateMips` and the mip fields on the specs were deleted as unused and come back with the first caller |
| Physical device is `devices[0]`, not scored                                  | `vulkan/backend.go`                                                          |
| No rendered-image regression test                                            | `-screenshot` makes one possible; nothing automates the comparison           |
| The uniform structs still obey the dead 16-byte cell rule                    | `tmp/BACKEND_DECISION.md` §5.3                                               |

---

## Performance notes

Every fix below was found by measurement, not by reading the code. Several were
found by comparing against the OpenGL backend while it still existed — that
comparison is gone, which is what makes the timestamp-query gap at the end of
this section matter more than it used to.

**Shadow taps dominate the fragment shader.** The PCF kernel taps up to 13× per
fragment (4 corners, then 3×3 only where they disagree — it was 9× for the 2D map
and 20× for the cube before the atlas unified them). On an Intel UHD 620 this ran
roughly 2× slower than the OpenGL backend did. Two fixes closed most of the gap:

- **Dedicated shadow descriptors instead of bindless.** The shadow maps used to
  be sampled through the bindless arrays. Intel's driver re-fetches a
  _dynamically indexed_ descriptor on every tap, so 20 cube taps meant 20
  descriptor fetches. They now get plain bound descriptors (set 0, bindings 2 and
  3); material textures stay bindless
- **Early-bail PCF**, described above

**Loop-invariant reads through a BDA pointer.** An earlier multi-light cut read
the material fields (`matAmbient`, `matDiffuse`, …) _inside_ `calcDirLight` /
`calcPointLight`, i.e. once per light per fragment. On Vulkan those live behind a
`buffer_reference` (BDA) pointer, so the compiler cannot prove the loads are
loop-invariant and re-fetches them every iteration. (Through an OpenGL UBO the
same reads rode the constant cache for free, which is how the cliff was spotted —
it dropped Vulkan a whole vsync interval.) The fix was to hoist them into a local
`MatParams` struct once at the top of `fsMain` and pass it into the light
functions. This is structural, not light-count dependent, and it is why the
per-light loop never reads material data through `FRAME` / `DRAW` directly.

**A deliberate non-fix, now moot.** Making the cube sampler a descriptor _array_
(`shadowCubeMap[slot]`) reintroduced the dynamic-index cost on Intel's ANV
driver, and we chose not to constant-fold the index (a `switch(slot)`, or four
single bindings): that cost is an Intel-iGPU artifact, near-free on the discrete
GPUs this engine targets. The atlas removed the array entirely — bindings 2 and 3
are two plain `Sampler2D`, and which tile a fragment reads is a uv offset rather
than a descriptor index, so the dynamic-index question does not arise. Kept here
because it is the reason the shadow bindings are dedicated at all.

**The uniform split** (`FrameUniforms` per pass, `DrawUniforms` per draw) cut the
per-draw payload from 1312 to 128 bytes. It did **not** measurably move the frame
rate on this iGPU — the win is structural.

**The 16-byte cell rule** that followed it was never a bandwidth optimisation
either; it existed so std140 came out byte-identical to scalar layout and neither
backend had to marshal. With OpenGL gone the rule buys nothing — scalar layout
matches Go's packing unconditionally — and it is now dead weight the uniform
structs still carry. `tmp/BACKEND_DECISION.md` §5.3 is the removal.

**Measuring by FPS subtraction does not work here.** Under vsync a frame that
crosses 16.6 ms drops cleanly to the next interval, so frame-rate deltas hide the
real cost, and run-to-run variance on this machine is wide enough to swamp a 10%
difference. The honest instrument is GPU timestamp queries, blocked on
query-pool bindings — `go-vulkan/BINDINGS_GAP.md` §5.4, five functions and about
four hours. With the second backend gone this is the only way left to tell a slow
pass from a slow frame, so it has moved up the list.
