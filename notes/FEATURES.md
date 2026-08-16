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
  - [Anti-aliasing](#anti-aliasing--msaa-on-the-backbuffer)
- [Part 2 — roadmap](#part-2--roadmap)
- [Performance notes](#performance-notes)

---

## Part 1 — implemented

### The abstraction

- The scene layer makes zero graphics-API calls. Everything goes through
  `renderer.Backend` (27 methods), implemented in `vulkan/`. An OpenGL 4.1
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
- **Spot → one tile**, structurally; no spot is given one until Part D's
  allocator
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

- **Front-face culling in the shadow pass.** The cleanest fix for _closed_ meshes
  (the bias hides inside the geometry), but a single-sided ground plane has no
  back face, so it cannot cover the showcase ground alone. A sun tile already
  does this via `SetCullMode(CullFront)`, face tiles do not
- **Slope-scaled depth bias** (`glPolygonOffset`) — cheap, but on its own it is
  what caused the original peter-panning
- A production setup usually pairs **front-face culling (solids) + normal-offset
  (everything, including flat receivers)**, which is the natural next step

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
- **Who casts.** `Scene.pickShadowCasters` still selects the first directional and
  the first point light at load, so 7 of the atlas's 16 tiles are in use. Part D
  replaces that with a per-frame score

> **Gap.** The atlas has room for 16 tiles and the allocator hands out 7, because
> the caster pick is still the fixed load-time one. That is Part D, not a bug.

> **Bug, preserved deliberately.** The point-light branch scales its shadow by
> **5.0**, so `Lo += contrib * (1 - shadow)` goes negative on a fully shadowed
> fragment and _subtracts_ light other lights contributed. It predates the atlas
> and was carried across unchanged so Part C's image would be comparable; it
> should be deleted and the shadows re-tuned.

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
the CPU into an RGBA image, uploaded with `UpdateTexture2D`, and composited as an
ordinary fullscreen mesh built once by `core.createOverlayQuad`. It redraws only
when the tree or the hover state changed.

On Vulkan the upload is _staged_ and copied at the top of the next frame, because
a copy cannot be recorded inside a render pass — one frame of latency, no queue
stall. `main.go` currently passes a nil widget, so only the debug crosshair draws.

### Anti-aliasing — MSAA on the backbuffer

`[antialiasing] mode` and `samples` in the config file (`none`, or `msaa` at
2/4/8) are read once when the backend initialises, because the count is baked
into a multisampled colour + depth pair resolved into the swapchain image at the
end of the main pass. The request is clamped to
`framebufferColor/DepthSampleCounts`, so an unsupported 8× steps down to 4×
rather than failing device-side; 1 and 4 are guaranteed by the spec.

MSAA rather than a post-process filter because it needs no new pass and no new
render target: FXAA or TAA would mean rendering the scene offscreen, and an
offscreen colour target has no depth attachment today
(`passOffscreenColor` is colour-only, for post-processing that reads a finished
image). It also only smooths geometric edges — shader aliasing (specular
highlights, normal-map shimmer) is untouched, which is what a post-process pass
would buy.

The cost is real and worth measuring on the target GPU: on an Intel UHD 620 at
1920×1080 the showcase scene runs ~49 FPS off, ~44 at 4×, ~28 at 8×.

---

## Part 2 — roadmap

Ordered by value to effort. Each item lists what to touch.

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

**Files** `vulkan/`, `go-vulkan`, a new `tonemap` / `bloom` Slang pass, `core/app.go`.

- Render the main pass into a colour render target instead of the swapchain.
  `CreateRenderTarget(RenderTargetSpec{Format: TargetColor})` already exists —
  **but** it allocates `R8G8B8A8_UNORM`, not the `R16G16B16A16_SFLOAT` HDR
  actually needs, because the `go-vulkan` bindings expose no half-float format.
  That is a one-constant change in `vulkan/backend.go` once the binding exists;
  `go-vulkan/BINDINGS_GAP.md` §7 batch 1 is the hour of work that adds it
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
| Only 7 of the atlas's 16 tiles are ever allocated — one sun, one point light | `scene/scene.go` `pickShadowCasters`, replaced by Part D                     |
| Atlas tiles are fixed at 1024², no variable resolution and no cascades       | `scene/shadowatlas.go`, `settings/settings.go`                               |
| `CopyDepthRegion` and `ShadowRecord.Flags` bit 0 exist but nothing sets them | the static/dynamic split is Part E                                           |
| `GeometryShader` and `passShadowCube` are enabled and unused                 | `depth_cube.slang` retired with the atlas                                    |
| No mipmaps on any texture                                                    | `vulkan/texture.go` — needs `CmdBlitImage`, `go-vulkan/BINDINGS_GAP.md` §5.2 |
| Physical device is `devices[0]`, not scored                                  | `vulkan/backend.go`                                                          |
| No rendered-image regression test                                            | nothing checks the frame, only that the scene parses                         |
| No GPU timestamp queries, so a pass cannot be profiled                       | needs query-pool bindings, `go-vulkan/BINDINGS_GAP.md` §5.4                  |
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
