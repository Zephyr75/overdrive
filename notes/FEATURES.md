# Overdrive C++ — Feature Report & Roadmap

Status of the `cpp/` engine beyond bare-bones mesh rasterization, plus a
prioritized plan for what comes next and how to build it. Read alongside
`cpp/BACKEND.md` (renderer contract), `notes/VULKAN.md` (Vulkan techniques), and
`notes/RAYTRACING_PLAN.md` (the longer-horizon ray-tracing design).

---

## Part 1 — Implemented features

### Dual backend, one shader source
- Backend-agnostic renderer: scene layer (`cpp/scene/`) makes zero graphics-API
  calls; everything goes through `renderer/Backend.hpp` + `renderer/Shader.hpp`,
  implemented twice in `opengl/` and `vulkan/`.
- Shaders authored once in Slang (`cpp/shaders/slang/*.slang`) and compiled per
  backend at configure time: GLSL 4.10 for OpenGL, SPIR-V for Vulkan.
- Vulkan path follows the modern stack: 1.3 dynamic rendering, buffer-device
  address + scalar layout for uniforms, bindless descriptor indexing, 2 frames
  in flight (see `cpp/BACKEND.md`).

### Lighting — Cook-Torrance PBR, two light types
Defined in `scene/Light.hpp` (`LightType { Sun, Point }`) and evaluated in
`shaders/slang/forward.slang` with a metallic-roughness microfacet BRDF (see
**PBR materials** below for the BRDF itself):
- **Directional ("Sun")** — `calcDirLight`, infinite light along `direction`;
  radiance = `color · diffuse · intensity`.
- **Point** — `calcPointLight`, inverse-square falloff
  (`1 / (kConstant + d²)`); radiance scaled by that attenuation.
- Each light builds an incoming-radiance term and feeds the shared
  `cookTorrance` evaluator; per-light `intensity` / `diffuse` set its strength.
- Up to `MAX_LIGHTS` (8) lights in any mix of directional and point — see
  **Multi-light support** below.

### Shadows — both kinds, with PCF
Driven by `Light::renderLight` (`scene/Light.cpp`), rendered in dedicated depth
passes before the main pass:
- **Directional → 2D shadow map.** Orthographic light-space matrix; sampled in
  `shadowCalculation` with a 3×3 PCF kernel and a **normal-offset bias** (see
  below); clamps to lit beyond the far plane. Backed by `createShadowMap2D`.
- **Point → omnidirectional cubemap shadow.** 6 face-view matrices rendered via
  the `depth_cube` geometry-shader path; sampled in `shadowCalculationCube` with
  a 20-tap disk PCF whose radius grows with view distance. Stores linear
  distance / `farPlane`. Backed by `createShadowCubemap`.
- GL↔VK bridging for the shadow passes (positive viewport, CW front face,
  `TO_VK_DEPTH` clip-z remap) is handled per `cpp/BACKEND.md`.

#### Shadow-sampling performance (the GL/Vulkan parity fix)
The PCF kernels tap the shadow maps a lot per fragment (9× for the 2D map, 20×
for the cube), which made the **Vulkan backend run ~2× slower than OpenGL** on an
Intel UHD 620 (≈37 vs ≈62 fps, and worse once the iGPU throttled). Profiling by
gutting the fragment shader showed the cost was entirely the shadow taps, not the
PBR/IBL math or CPU submission. Two fixes closed most of the gap (to ≈46 vs ≈61):

- **Dedicated shadow descriptors instead of bindless.** The shadow maps were
  sampled through the bindless `texturesCube[idx]` / `textures2D[idx]` arrays.
  Intel's Vulkan driver re-fetches a *dynamically-indexed* descriptor on every
  tap, so 20 cube taps = 20 descriptor fetches. The shadow maps now get plain
  bound descriptors (set 0, bindings 2 = `Sampler2D`, 3 = `SamplerCube[
  MAX_SHADOW_CUBES]`) — the same fixed-sampler model the OpenGL backend already
  uses. `VKBackend` mirrors the directional caster's 2D map (texture unit 0) into
  binding 2 and each point caster's cube map (units `SHADOW_CUBE_UNIT_BASE`..)
  into the binding-3 array in `bindTexture2D` / `bindCubemap`, only rewriting a
  slot when its caster changes (`writeDedicatedTexture`, guarded by
  `shadow2DHandle` / `shadowCubeHandles[]`). Material textures stay bindless.
  (≈37 → ≈43 fps.)
- **Early-bail PCF.** Both shadow tests first take 4 spread taps; if they
  unanimously agree (fully lit or fully shadowed — true for almost every fragment
  outside a penumbra) they return immediately, skipping the full 9-/20-tap
  kernel. Only soft edges pay full price. Quality is unchanged; this helps both
  backends and disproportionately the Vulkan path that was tap-bound. (≈43 → ≈46
  fps, and it throttles far less because the GPU does less work.)

The residual gap is the iGPU still being shadow-tap-bound below the 60 fps vsync
cap (OpenGL has headroom under it). Further levers if needed: fewer base cube
taps, a screen-space shadow cache, or the ray-traced-shadow path (roadmap §3),
which removes the shadow-map taps entirely on Vulkan.

**Multi-cube update (and a deliberate non-fix).** Extending point shadows from
one cube to up to `MAX_SHADOW_CUBES` (4) made the cube sampler a *descriptor
array* indexed by a runtime slot (`shadowCubeMap[slot]`). Re-profiling with real
GPU timestamp queries (not FPS subtraction — see `OPTIMISATION.md` for why that
matters) showed the Vulkan main pass spends ~16 ms on cube PCF alone vs OpenGL's
~12 ms for the *entire* main pass; the shadow bake is ~equal on both backends
(~15 ms), so it is **not** the gap. The cause is Intel's ANV driver re-fetching a
dynamically-indexed descriptor per tap — the same class of cost the dedicated
bindings fixed for the single cube, reintroduced by the array index. We
**deliberately do not** constant-fold the index (e.g. `switch(slot)` or 4 single
bindings): that cost is an Intel-iGPU artifact, near-free on the discrete GPUs
this engine actually targets, and the iGPU here is a throwaway dev box. Keeping
the generic array avoids contorting the shader for hardware we won't ship on. The
full rationale, measurements, and the opt-in GPU-timing instrumentation
(`OD_GPU_TIMING`) live in `OPTIMISATION.md`; revisit only if a target GPU profiles
the same way.

#### Shadow bias — normal-offset (and how to change it later)
Shadow-map filtering needs a bias to escape **shadow acne** (the receiver
self-shadowing from depth-map quantization). The tradeoff is **peter-panning**:
too much bias detaches the shadow from the object's base, leaving a lit *gap* at
the contact point.

Both shadow tests use a **normal-offset bias** rather than a depth bias: instead
of offsetting the compared depth, the receiver sample point is pushed along its
surface normal in world space (`NORMAL_OFFSET_2D` / `NORMAL_OFFSET_CUBE` in
`forward.slang`), more at grazing light angles, *before* projecting into the
shadow map. The 2D path re-projects the offset world position in the fragment
shader (so the old precomputed `fragPosLightSpace` varying is gone); the cube
path offsets the `fragToLight` origin. This escapes acne geometrically, so the
residual constant depth bias is tiny (2D `0.0015`, cube `0.04`) and contact
shadows stay attached.

The offset constants are tuned for the showcase's ~10-unit scene scale; rescale
them if the scene scale changes (too small → acne returns; too large →
peter-panning comes back). **Alternatives if you want to revisit this:**
- **Front-face culling in the shadow pass** (render only back faces into the
  depth map): the cleanest fix for *closed, solid* meshes — the bias hides
  inside the geometry — but a flat/single-sided ground plane has no back face,
  so it can't cover the showcase ground on its own. Would need
  `glCullFace(GL_FRONT)` (GL) / a CW-vs-CCW cull flip (VK) around the depth pass.
- **Slope-scaled depth bias** (`glPolygonOffset`) — cheap, but on its own it was
  what caused the original peter-panning.
- A robust production setup usually pairs **front-face culling (solids) +
  normal-offset (everything, incl. flat receivers)**, which is the natural next
  step here if shadows need to be tighter.

### Multi-light support
The forward pass evaluates up to `MAX_LIGHTS` (= 8) lights per fragment, in any
mix of directional and point lights. How it fits together:

- **Uniform block.** `Uniforms` (`common.slang`) carries `LightData
  lights[MAX_LIGHTS]` plus `lightCount` (how many entries are live), `shadowDirIndex`
  (which light, if any, owns the 2D shadow map; -1 = none) and `pointShadowLights[
  MAX_SHADOW_CUBES]` (the light index owning each cube-shadow slot, or -1).
  `MAX_LIGHTS` and `MAX_SHADOW_CUBES` are duplicated as constants in the two CPU
  mirrors (`vulkan/Uniforms.hpp`, `opengl/Shader.cpp`), `settings/Settings.hpp`,
  and `scene/Mesh.cpp`, all of which must stay in step with the shader. The layout
  change is guarded the usual way: the Vulkan scalar mirror by
  `static_assert(sizeof(VKUniformBlock) == 1312)`, the GL std140 mirror by
  `kBlockSize` (1600) and the hand-computed offset table (`lights[]` ends at byte
  1488; the trailing ints, PBR scalars, then the std140 `pointShadowLights[4]`
  array at 1536 follow).

- **Fragment loop.** `forward.slang` loops `l < lightCount`, branches on
  `light.type`, and adds `calcDirLight` / `calcPointLight` for each. The earlier
  hard-coded 2-iteration loop is gone.

- **Shadow budget, decoupled from light order.** Shadows are bounded but no
  longer capped at one of each kind: one 2D shadow map (directional) plus up to
  `MAX_SHADOW_CUBES` (= 4) cube shadow maps (point). Rather than assume a fixed
  light ordering, the shader applies the 2D shadow only to the light at
  `shadowDirIndex`, and for each point light scans `pointShadowLights[]` — if a
  cube slot owns it, it samples that slot's `shadowCubeMap[s]`; every other light
  is lit but unshadowed. The shadow-test helpers take the relevant light's
  direction / position (and, for the cube path, the slot) as parameters instead
  of indexing `lights[0]` / `lights[1]` directly.

- **Who casts.** `Scene` (`scene/Scene.cpp`) picks the first directional light
  plus the first `MAX_SHADOW_CUBES` point lights as the casters, records the
  directional in `shadowDirIndex` and assigns each point caster a cube slot in
  `pointShadowLights[]`, and sets `Light::castsShadow`. Only casters allocate a
  shadow map (`Light::setup` early-returns otherwise) and run a depth pass
  (`App` skips non-casters; each caster already owns its own FBO + cube map, so
  the bake loop scaled to N casters for free) — so adding more lights costs only
  the forward-pass evaluation, not unbounded shadow passes. `Mesh::draw` uploads
  `lightCount` + the indices and binds each caster's map to its unit; light
  ordering in the XML no longer matters.

- **Vulkan per-light uniform-read optimisation.** The first multi-light cut ran
  the showcase at ~60 fps on OpenGL but only ~30 on Vulkan (FIFO vsync, Intel
  UHD 620). Root cause: the per-light loop in `forward.slang` read the
  loop-invariant material fields (`U.matAmbient` / `matDiffuse` / `matSpecular` /
  `matShininess`) *inside* `calcDirLight` / `calcPointLight`, i.e. once **per
  light per fragment**. On Vulkan `U` is a `buffer_reference` (BDA) pointer, so
  the compiler cannot prove those loads are loop-invariant (no aliasing
  guarantee) and re-fetches them from memory every iteration; on OpenGL the same
  reads hit a UBO and ride Intel's constant cache for free — hence the
  backend-specific cliff. Going from 2 to 5 lights pushed the Vulkan frame past
  the 16.6 ms vsync boundary, so it dropped cleanly to the next interval (30 fps).
  Fix: hoist the material fields into a local `MatParams` struct (and the loop
  scalars `lightCount` / `shadowDirIndex` into locals)
  once at the top of `fsMain`, and pass `MatParams` into the light functions
  instead of reading `U` inside them. The loop now touches registers, not the
  BDA pointer. Measured (Intel UHD 620, 5 lights): Vulkan 39 → 53 fps, back to
  the old 2-light baseline; OpenGL unaffected (a marginal win at most). The
  lighting math is unchanged. Note this is structural, not light-count: the
  saving applies for any N, and is the reason the per-light loop never reads
  material data through `U` directly.


### PBR materials — metallic-roughness Cook-Torrance
The forward shader uses a physically-based microfacet BRDF instead of
Blinn-Phong. Implemented entirely in `forward.slang` with three new material
scalars threaded through the uniform block.

- **BRDF.** `cookTorrance` (in `forward.slang`) is the textbook Cook-Torrance
  specular term — GGX/Trowbridge-Reitz normal distribution (`distributionGGX`),
  Smith height-correlated geometry via Schlick-GGX with the direct-lighting
  `k = (r+1)²/8` (`geometrySmith`), and Fresnel-Schlick (`fresnelSchlick`) —
  plus a Lambertian diffuse lobe. Energy is conserved: the diffuse weight is
  `kD = (1 - F)(1 - metallic)`, so the Fresnel reflectance and metalness steal
  from the diffuse term, and metals have no diffuse at all.
- **Material model.** `matDiffuse` is reused as the **base colour / albedo**
  (sampled albedo texture × tint, linearised from sRGB with `pow(·, 2.2)`).
  `Material` (`scene/Material.hpp`) gains `metallic`, `roughness`, `ao` scalars;
  loaded from the `.mtl` PBR extension keys (`Pm`, `Pr`) in `scene/Mesh.cpp`,
  defaulting to dielectric/matte (`metallic 0`, `roughness 1`) for legacy
  materials. `F0 = lerp(0.04, albedo, metallic)`: dielectrics reflect ~4%, metals
  tint their reflectance with the albedo.
- **Uniform plumbing.** Three floats (`matMetallic` / `matRoughness` / `matAo`)
  were appended to the shared `Uniforms` (`common.slang`) and both CPU mirrors.
  Appending at the end keeps the change cheap: the GL std140 block still ends at
  1536 bytes (`kBlockSize` unchanged), the VK scalar block grows 1288 → 1300
  (size `static_assert` bumped). `Mesh::draw` uploads them per submesh next to
  the existing `material.*` setters; `vkUniformFields()` / `glUniformOffsets()`
  carry the three new offsets.
- **Image-based ambient.** The old raw-reflection hack is replaced by a
  Fresnel-weighted split-sum approximation: the skybox cubemap stands in for both
  the diffuse irradiance (sampled along `N`) and the prefiltered specular
  environment (sampled along `reflect(-V, N)`), mixed by
  `fresnelSchlickRoughness` and scaled by `ao`. So metals mirror the sky and
  dielectrics pick up a soft tint, with no separate reflection term. (A real
  prefiltered-mip + BRDF-LUT IBL is still roadmap — see below.)
- **Tonemapping.** PBR radiance is unbounded; `fsMain` applies a Reinhard
  tonemap + gamma at the end so it stays displayable in the LDR backbuffer until
  the dedicated HDR/bloom pass (roadmap) lands. Showcase light intensities were
  retuned for the inverse-square falloff and this LDR path.

### Materials & textures
- `scene/Material.hpp`: ambient / diffuse (= albedo) / specular / shininess /
  alpha / metallic / roughness / ao, plus a diffuse texture and a normal-map
  slot.
- **Bindless textures** in both backends (`sampler2D[256]` + `samplerCube[64]`);
  texture handle 0 is a built-in white pixel. Sampler uniforms keep GL
  texture-unit semantics and resolve to array slots at draw time.
- Texture paths are now resolved portably: the loader (`scene/Mesh.cpp`) strips
  any baked Blender path to its basename and loads from the project-local
  `cpp/textures/` directory, so the project moves across machines/folders.

### Normal mapping
- Tangent-space normal maps are sampled in `forward.slang` (`perturbNormal`).
  The TBN basis is derived per-fragment from screen-space derivatives of
  `fragPos` and uv (Schüler's cotangent frame) — no tangents in the vertex
  layout, so the existing pos/normal/uv VBO and both backends' `createMesh` are
  untouched.
- Driven by a `useNormalMap` flag in the uniform block: `scene/Mesh.cpp` binds
  the material's normal map to texture unit 4 and sets the flag per submesh;
  meshes without one fall back to the geometric normal. The map loads from a
  `.mtl` `map_Bump` / `bump` entry (`Material::normalMapPath`), resolved through
  the same portable basename → `textures/` path logic.
- The `texNormalMap` (Vulkan bindless slot) and `useNormalMap` fields were added
  to the shared `Uniforms` block and to both CPU mirrors (`vulkan/Uniforms.hpp`,
  `opengl/Shader.cpp`), kept byte-compatible via the existing size asserts.

### Environment & reflection
- **Skybox** (`scene/Skybox.*`, `shaders/slang/skybox.slang`): cubemap rendered
  behind the scene.
- The skybox cubemap doubles as a crude **reflection probe** in `forward.slang`,
  now consumed by the PBR ambient term (Fresnel/metallic-weighted env sample)
  rather than the old `1 - matDiffuse` hack — see **PBR materials**.

### Scene & assets
- XML scene description (`scene/Scene.cpp`) loads camera, meshes, lights,
  skybox. Meshes load from OBJ/MTL via tinyobjloader.
- Per-frame `updateMeshes()` supports moving geometry (Verlet-style movement
  hooks exist from the Go original).
- **Showcase scene** (`assets/showcase.xml`, the default) exercises every
  feature: a normal-mapped paving ground, a metal Suzanne (`Pm 1`), a brick and a
  wood primitive (dielectric, all normal-mapped), and a fully-metallic low-
  roughness chrome sphere (`Pm 1, Pr 0.08`) that mirrors the skybox through the
  PBR ambient term, lit by a directional sun (2D shadow) + a warm point light
  (cube shadow). The per-mesh `.mtl` files carry the `Pm`/`Pr` PBR scalars; light
  intensities are tuned for the inverse-square falloff. Colour/normal maps are
  CC0 from ambientCG, in `cpp/textures/`.
  Note: static meshes render with an identity model matrix, so geometry is baked
  into the `Demo*.obj` vertices (in GL world space) rather than positioned by the
  XML `<position>` tags; the demo objs were generated directly in world space.
  (Light order in the XML is no longer significant — the shadow casters are
  resolved by index at load time; see **Multi-light support**.)

---

## Part 2 — Roadmap

Ordered by value-to-effort. Each item lists the files to touch and the strategy.

### 1. PBR materials — **done** (scalar metallic-roughness)
The Cook-Torrance metallic-roughness BRDF shipped — see **PBR materials** in
Part 1. What landed: per-material `metallic`/`roughness`/`ao` scalars (`.mtl`
`Pm`/`Pr`), GGX/Smith/Fresnel BRDF in `forward.slang`, a Fresnel-weighted skybox
ambient term, and an inline Reinhard tonemap.

**Still open (PBR follow-ups):**
- **Texture-driven PBR.** Add albedo/metallic/roughness/AO *map* slots (new
  bindless textures + `map_Pm`/`map_Pr` loading) so values vary per-texel, not
  just per-material. Today the maps in `cpp/textures/` are colour + normal only.
- **Proper IBL.** Prefilter the skybox into an irradiance cubemap + a
  roughness-mip prefiltered-specular cubemap and a BRDF LUT (one-time
  compute/raster pass at load), replacing the current single-sample skybox
  ambient approximation.

### 2. HDR + tonemapping + bloom (medium)
**Why:** unlocks intensity values >1 and physically meaningful lighting.
**Files:** `renderer/Backend.hpp` (offscreen HDR target API), both backends, a
new `tonemap`/`bloom` Slang pass, `core/App.cpp` (render-to-texture then
composite).
**Strategy:**
- Render the main pass into an `RGBA16F` framebuffer instead of the swapchain.
- Add a fullscreen post pass: bright-pass + separable Gaussian blur for bloom,
  then ACES/Reinhard tonemap + gamma to the backbuffer. (A stopgap Reinhard +
  gamma already runs inline at the end of `forward.slang` for the PBR path; move
  it here once there is a real HDR target.)
- This needs a real offscreen-color-target abstraction; today `beginPass(0,…)`
  only distinguishes backbuffer vs shadow FBOs. Generalize framebuffer creation.

### 3. Ray-traced shadows (high — Vulkan only)
**Why / how:** already designed in detail. The entry point is **ray query
(`VK_KHR_ray_query`)** dropped into `forward.slang`'s shadow test, replacing the
shadow-map passes; it reuses the existing forward pass and the current light
loop. OpenGL stays on shadow maps (GL 4.1 cannot participate). See
`notes/RAYTRACING_PLAN.md` for acceleration-structure plumbing, the ray-query
vs RT-pipeline trade-off, and the optional-capability stub strategy for keeping
`GLBackend` a one-liner. Follow-ups: RT AO → reflections → one-bounce GI.

---

## Quick reference — where things live

| Concern | File(s) |
|---|---|
| Backend contract | `cpp/renderer/Backend.hpp`, `cpp/renderer/Shader.hpp` |
| GL / VK impls | `cpp/opengl/`, `cpp/vulkan/` |
| Shaders (source of truth) | `cpp/shaders/slang/*.slang` |
| Uniform layout (must stay in sync) | `common.slang` ↔ `vulkan/Uniforms.hpp` ↔ `opengl/Shader.cpp` |
| Lights & shadows | `cpp/scene/Light.{hpp,cpp}` |
| Materials & textures | `cpp/scene/Material.hpp`, `cpp/scene/Mesh.cpp` |
| Scene / XML / skybox | `cpp/scene/Scene.cpp`, `cpp/scene/Skybox.*` |
| Frame loop | `cpp/core/App.cpp` |

**Always rebuild shaders after editing `.slang`** — both backends read only the
compiled GLSL/SPIR-V, never the Slang source.
