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

### Lighting — Blinn-Phong, two light types
Defined in `scene/Light.hpp` (`LightType { Sun, Point }`) and evaluated in
`shaders/slang/forward.slang`:
- **Directional ("Sun")** — `calcDirLight`, infinite light along `direction`.
- **Point** — `calcPointLight`, with distance attenuation
  (`kConstant/kLinear/kQuadratic`).
- Both use the Blinn-Phong halfway-vector specular term; ambient + diffuse +
  specular are scaled by per-light `intensity`, `diffuse`, `specular` factors.
- Up to `MAX_LIGHTS` (8) lights in any mix of directional and point — see
  **Multi-light support** below.

### Shadows — both kinds, with PCF
Driven by `Light::renderLight` (`scene/Light.cpp`), rendered in dedicated depth
passes before the main pass:
- **Directional → 2D shadow map.** Orthographic light-space matrix; sampled in
  `shadowCalculation` with a 3×3 PCF kernel and slope-scaled depth bias; clamps
  to lit beyond the far plane. Backed by `createShadowMap2D`.
- **Point → omnidirectional cubemap shadow.** 6 face-view matrices rendered via
  the `depth_cube` geometry-shader path; sampled in `shadowCalculationCube` with
  a 20-tap disk PCF whose radius grows with view distance. Stores linear
  distance / `farPlane`. Backed by `createShadowCubemap`.
- GL↔VK bridging for the shadow passes (positive viewport, CW front face,
  `TO_VK_DEPTH` clip-z remap) is handled per `cpp/BACKEND.md`.

### Multi-light support
The forward pass evaluates up to `MAX_LIGHTS` (= 8) lights per fragment, in any
mix of directional and point lights. How it fits together:

- **Uniform block.** `Uniforms` (`common.slang`) carries `LightData
  lights[MAX_LIGHTS]` plus three ints: `lightCount` (how many entries are live)
  and `shadowDirIndex` / `shadowPointIndex` (which light, if any, owns each
  shadow map; -1 = none). `MAX_LIGHTS` is duplicated as a constant in the two CPU
  mirrors (`vulkan/Uniforms.hpp`, `opengl/Shader.cpp`) and in `scene/Mesh.cpp`,
  all of which must stay in step with the shader. The layout change is guarded
  the usual way: the Vulkan scalar mirror by `static_assert(sizeof(VKUniformBlock)
  == 1288)`, the GL std140 mirror by `kBlockSize` (1536) and the hand-computed
  offset table (`lights[]` ends at byte 1488, the trailing ints follow).

- **Fragment loop.** `forward.slang` loops `l < lightCount`, branches on
  `light.type`, and adds `calcDirLight` / `calcPointLight` for each. The earlier
  hard-coded 2-iteration loop is gone.

- **Shadow budget, decoupled from light order.** Shadows do *not* scale to N
  lights — there is still exactly one 2D shadow map and one cube shadow map.
  Rather than assume a fixed light ordering, the shader applies the 2D shadow
  only to the light at `shadowDirIndex` and the cube shadow only to the light at
  `shadowPointIndex`; every other light is lit but unshadowed. The shadow-test
  helpers were generalised to take the relevant light's direction / position as
  parameters instead of indexing `lights[0]` / `lights[1]` directly.

- **Who casts.** `Scene` (`scene/Scene.cpp`) picks the first directional and the
  first point light as the casters, records their indices (`shadowDirIndex`,
  `shadowPointIndex`) and sets `Light::castsShadow`. Only casters allocate a
  shadow map (`Light::setup` early-returns otherwise) and run a depth pass
  (`App` skips non-casters), so adding more lights costs only the forward-pass
  evaluation, not extra shadow passes or cube-map allocations. `Mesh::draw`
  uploads `lightCount` + the two indices and binds the casters' maps; light
  ordering in the XML no longer matters.

### Materials & textures
- `scene/Material.hpp`: ambient / diffuse / specular / shininess / alpha, plus a
  diffuse texture and a normal-map slot.
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
- The skybox cubemap doubles as a crude **reflection probe** in `forward.slang`
  (reflect view vector, sample cubemap, weight by `1 - matDiffuse`).

### Scene & assets
- XML scene description (`scene/Scene.cpp`) loads camera, meshes, lights,
  skybox. Meshes load from OBJ/MTL via tinyobjloader.
- Per-frame `updateMeshes()` supports moving geometry (Verlet-style movement
  hooks exist from the Go original).
- **Showcase scene** (`assets/showcase.xml`, the default) exercises every
  feature: a normal-mapped paving ground, a metal Suzanne, a brick and a wood
  primitive (all normal-mapped), and a low-Kd chrome sphere that mirrors the
  skybox, lit by a directional sun (2D shadow) + a warm point light (cube
  shadow). PBR colour/normal maps are CC0 from ambientCG, in `cpp/textures/`.
  Note: static meshes render with an identity model matrix, so geometry is baked
  into the `Demo*.obj` vertices (in GL world space) rather than positioned by the
  XML `<position>` tags; the demo objs were generated directly in world space.
  (Light order in the XML is no longer significant — the shadow casters are
  resolved by index at load time; see **Multi-light support**.)

---

## Part 2 — Roadmap

Ordered by value-to-effort. Each item lists the files to touch and the strategy.

### 1. PBR materials (medium-high)
**Why:** Blinn-Phong is the visual ceiling; metallic-roughness is the standard.
**Files:** `scene/Material.hpp` (+ loader in `Mesh.cpp`), `forward.slang`,
`common.slang`, both uniform mirrors.
**Strategy:**
- Add albedo / metallic / roughness / AO + their texture slots to `Material`.
- Swap the lighting functions in `forward.slang` for a Cook-Torrance BRDF
  (GGX distribution, Smith geometry, Fresnel-Schlick).
- For correct image-based lighting, prefilter the skybox into irradiance +
  prefiltered-specular cubemaps and a BRDF LUT (one-time compute/raster pass at
  load) instead of the current raw-cubemap reflection hack.

### 2. HDR + tonemapping + bloom (medium)
**Why:** unlocks intensity values >1 and physically meaningful lighting.
**Files:** `renderer/Backend.hpp` (offscreen HDR target API), both backends, a
new `tonemap`/`bloom` Slang pass, `core/App.cpp` (render-to-texture then
composite).
**Strategy:**
- Render the main pass into an `RGBA16F` framebuffer instead of the swapchain.
- Add a fullscreen post pass: bright-pass + separable Gaussian blur for bloom,
  then ACES/Reinhard tonemap + gamma to the backbuffer.
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
