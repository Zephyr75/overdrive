# Next-Level Features — Design Notes

Forward-looking design notes for taking Overdrive past its current forward+PBR
renderer. Each section explains **how the technique works** before listing the
**files to touch** and a **strategy** for slotting it into this engine. Nothing
here is implemented — this is a menu of next steps, ordered loosely by
value-to-effort within each theme.

Read alongside:
- `notes/FEATURES.md` — what already ships (PBR, shadow maps, multi-light, IBL).
- `notes/RAYTRACING_PLAN.md` — the detailed acceleration-structure + ray-query
  plan this file extends.
- `cpp/BACKEND.md` — the pass-based backend contract every new pass must obey.
- `notes/VULKAN.md` — Vulkan techniques the VK backend must follow.

Two cross-cutting constraints shape every choice below:

1. **Two backends, one shader source.** Shaders are authored once in Slang and
   compiled to GLSL 4.10 (OpenGL 4.1) and SPIR-V (Vulkan 1.3). Anything needing
   compute, ray tracing, or storage images is **Vulkan-only**; GL 4.1 has no
   compute shaders (4.3+) and no RT. Use the existing `#if TARGET_VK` split and
   the "optional capability, GL stubs to a no-op" pattern from the RT plan.
2. **The uniform block is mirrored three ways.** `common.slang` ↔
   `vulkan/Uniforms.hpp` ↔ `opengl/Shader.cpp` must stay byte-compatible
   (guarded by `static_assert` on the VK side, the offset table on the GL side).
   Every new uniform field pays that tax — prefer appending at the end.

---

## 1. Ray tracing — the family of effects

All of these share the **one foundation** in `notes/RAYTRACING_PLAN.md`: a
two-level acceleration structure (BLAS per mesh, TLAS per scene) plus inline
**ray queries** (`VK_KHR_ray_query`) fired from the existing `forward.slang`
fragment shader. Build that once and each effect below is mostly new shader
code, not new plumbing. **Vulkan only**; the GL backend keeps shadow maps and
SSAO.

A ray is just `origin + t · direction`. "Tracing" means asking the BVH *what is
the first triangle this ray hits, and at what `t`*. Every effect here is a
different choice of where rays start, which way they point, and what you do with
the hit.

### 1a. Ray-traced hard + soft shadows (first target)
**How it works.** For each light, fire a *shadow ray* from the surface point
toward the light. If it hits any geometry before reaching the light, the point
is in shadow. This is an exact visibility test — no shadow map, so no acne, no
peter-panning, no cube-face seams, no resolution limit.
- **Hard shadows:** one ray straight at the light. Binary in/out.
- **Soft shadows (penumbrae):** real lights have area. Treat the light as a disc
  or sphere and fire N rays at jittered points across its surface; the fraction
  that reach the light is the soft visibility. More rays = smoother penumbra,
  more cost. 1 ray/pixel + a denoiser (§1e) is the production trick.

**Files:** `forward.slang` (replace `shadowCalculation` / `shadowCalculationCube`
with `traceShadow` under `TARGET_VK`), `renderer/Backend.hpp`
(`buildAccelerationStructure` / `refitAccelerationStructure`, already sketched in
the RT plan), `vulkan/` (AS build, TLAS descriptor in set 0).
**Strategy.** Exactly the rollout in `RAYTRACING_PLAN.md` §"Rollout order":
A/B behind a toggle vs the shadow-map path, then delete the depth passes on the
RT path once at parity. Use `RAY_FLAG_TERMINATE_ON_FIRST_HIT` — shadow rays only
need *any* hit, not the closest.

### 1b. Ray-traced ambient occlusion (RTAO)
**How it works.** Ambient occlusion darkens creases and contact points by
asking "how much of the surrounding hemisphere is blocked by nearby geometry?"
Fire N short rays into the cosine-weighted hemisphere around the surface normal,
each with a small `TMax` (the AO radius, e.g. 0.5–2 scene units). The occlusion
factor is `hits / N`; multiply it into the ambient/IBL term. Because the rays
are short and need only first-hit, this is cheap relative to reflections.

This is the **ground-truth** version of the screen-space AO in §3 — it sees
geometry that is off-screen or behind the camera, which SSAO physically cannot.

**Files:** `forward.slang` (new `traceAO` applied to the IBL ambient term, which
already exists and is scaled by `matAo`), reuses the TLAS from §1a.
**Strategy.** Replace (on the RT path) the per-material `ao` scalar with traced
AO; keep the scalar as a multiplier. 1–4 rays/pixel + temporal accumulation
(§1e). Cosine-weight the hemisphere sampling so you match the Lambert term and
don't need to divide by pi twice.

### 1c. Ray-traced reflections
**How it works.** For a reflective surface, reflect the view direction about the
normal (`reflect(-V, N)`) and trace that ray into the scene. Whatever it hits,
**shade that hit point** (its own albedo, lights, even its own shadow rays) and
return the color as the reflection. This replaces the current skybox-only
reflection approximation with true inter-object reflections — the chrome sphere
would mirror the Suzanne next to it, not just the sky.
- **Glossy/blurry reflections:** rough surfaces don't reflect a single ray.
  Importance-sample the GGX lobe (the same distribution the BRDF already uses) to
  spread reflection rays into a cone whose width grows with `roughness`. Mirror =
  1 tight ray; rough metal = many spread rays (or 1 ray + denoise).

**Why this needs the geometry-descriptor indirection.** A shadow ray needs no
material data, but a reflection ray must *shade* its hit point. So you need the
`instanceCustomIndex` → geometry-descriptor table from `RAYTRACING_PLAN.md`
(BDA array of `{vertexBufferAddr, indexBufferAddr, materialIndex}`) to fetch the
hit triangle's normals, UVs, and bindless material at the hit. Build that
descriptor buffer now even if only shadows ship first (the plan says so).

**Files:** `forward.slang` (reflection ray + a `shadeHit` helper that re-runs the
BRDF at the hit; or move to a deferred/`RT-pipeline` model if recursion gets
deep), `vulkan/` (geometry-descriptor BDA array, bindless vertex/index buffers).
**Strategy.** Start with **one bounce, mirror-only** for low-roughness metals
(cheap, high visual payoff on the chrome sphere). Add GGX cone sampling for
glossy next. Recursion (reflections-of-reflections) is where the inline ray
query gets awkward — that is the trigger to move to the **RT pipeline + SBT**
(`VK_KHR_ray_tracing_pipeline`) per the RT plan's trade-off table.

### 1d. One-bounce diffuse GI (stretch)
**How it works.** Global illumination = light that bounces off surfaces and
lights other surfaces (the reason a red wall tints a nearby white floor pink).
For each shaded point, fire rays into the hemisphere, shade what they hit
(including *its* direct lighting), and add that incoming radiance as extra
"ambient". One bounce already kills the flat look of constant ambient.

**Files / strategy.** This is RTAO (§1b) but you *shade* the hit instead of just
counting it — same rays, more work per hit. Needs the geometry descriptor (§1c)
and heavy denoising/accumulation (§1e). This is the point where a true **path
tracer** (RT pipeline, raygen/miss/closest-hit stages, Russian-roulette
termination) becomes the cleaner architecture than bolting bounces onto the
forward shader. Treat as a separate "reference path tracer" mode, not a forward
add-on.

### 1e. The thing that makes 1a–1d usable: accumulation + denoising
**How it works.** All the above are noisy at 1–few rays/pixel (Monte Carlo
sampling has variance). Two standard fixes:
- **Temporal accumulation.** Reproject last frame's result into this frame using
  motion vectors and blend (exponential moving average). A static camera
  converges to a clean image over a few frames; moving regions fall back to
  spatial filtering.
- **Spatial denoise.** An edge-aware (à-trous / bilateral) blur guided by depth
  and normals (a "G-buffer") so it smooths noise without crossing real edges.
- SVGF (spatiotemporal variance-guided filter) is the well-documented combo of
  both; or wire in a vendor denoiser later.

**Files:** needs the HDR offscreen target from §2 (you accumulate in float),
motion vectors (reproject the previous view-projection per pixel), and a new
denoise Slang pass. **Strategy.** Don't ship RT reflections/AO without at least
temporal accumulation, or the noise will dominate. This is the unglamorous
prerequisite for §1b–1d looking good.

---

## 2. HDR pipeline + tonemapping + bloom (prerequisite for almost everything)
**How it works.** Real lighting has unbounded intensity; the current path
clamps with an inline Reinhard tonemap in `forward.slang` straight to an 8-bit
backbuffer, which crushes highlights and forecloses bloom, exposure, and clean
RT accumulation. The fix: render the scene into a **floating-point** color
target (`RGBA16F`), where values can exceed 1.0, then a fullscreen post pass maps
that HDR range down to displayable LDR.
- **Bloom:** isolate pixels brighter than 1.0 (bright-pass), blur them with a
  separable Gaussian (horizontal then vertical, cheap), add back. Simulates light
  bleeding in a lens/eye.
- **Tonemap:** ACES or Reinhard curve compresses HDR → [0,1], then gamma encode.

**Files:** `renderer/Backend.hpp` (a real offscreen-color-target abstraction —
today `beginPass(0,…)` only knows backbuffer vs shadow FBOs), both backends, new
`tonemap`/`bloom` Slang passes, `core/App.cpp` (render-to-texture then
composite). **Strategy.** This is roadmap §2 in `FEATURES.md` and the gateway to
RT accumulation (§1e), volumetrics (§4), and glass (§5b) — all want a float
target. **Do this early.** Move the inline Reinhard out of `forward.slang` into
the post pass once the float target exists.

---

## 3. Screen-space ambient occlusion (SSAO) — the both-backends AO
**How it works.** The cheap, GL-compatible cousin of RTAO. After a depth (+
normal) prepass, for each pixel sample a few neighboring depths in a hemisphere
around its normal; if neighbors are closer to the camera than expected, the
pixel is occluded. Pure screen-space, no BVH, runs on both backends. Limitation:
it only sees what's on screen — off-screen and back-facing occluders are missed
(exactly what RTAO fixes).

**Files:** depth/normal prepass (or reuse a G-buffer if §1e lands first), a new
`ssao` Slang pass + a blur pass, feed the result into the ambient term in
`forward.slang`. **Strategy.** On TODO.md already (`Add ambient occlusion
(SSAO)`). Ship this for the OpenGL backend and low-end GPUs; let Vulkan prefer
RTAO when `supportsRayTracing()`. Needs the offscreen-target work from §2.

---

## 4. Volume rendering

"Volumes" = things without a hard surface: fog, smoke, clouds, god-rays, and
SDF-defined shapes. Three distinct strategies, each suited to different content.

### 4a. SDF ray marching (procedural shapes, blobs, terrain detail)
**How it works.** A **signed distance field** is a function `f(p)` returning the
distance from point `p` to the nearest surface (negative inside). You render it
**without triangles** by *sphere tracing*: start a ray at the camera, evaluate
`f` at the current point — that value is a safe distance you can step without
overshooting any surface — jump that far, repeat. When `f(p) ≈ 0` you've hit the
surface; the normal is the gradient of `f` (finite differences). SDFs compose
analytically: `min` = union, `max(-a,b)` = subtraction, and `smin` (smooth min)
gives organic metaball blends impossible with meshes. This is how
demoscene/Shadertoy scenes and Dreams-style sculpting work.

**Files:** a new fullscreen `raymarch_sdf` Slang pass that runs *after* the
raster forward pass and composites by **comparing its hit distance against the
depth buffer** (so SDF objects and meshes occlude each other correctly), a small
SDF library in Slang (primitives + ops), scene hooks to feed SDF primitive
params. **Strategy.** Self-contained and both-backends (it's just a fragment
shader marching a math function — no compute, no BVH). High wow-factor, low
plumbing. Already flagged on TODO.md (`Ray marching for basic shapes`). Watch
the cost: marching is per-pixel loop-heavy; cap iteration count and use a coarse
bounding volume to skip empty space.

### 4b. Participating media — volumetric fog / god-rays (ray marching a density)
**How it works.** Instead of marching to a *surface*, march through a *volume*
and accumulate light. Step along the view ray; at each step sample a density
(constant fog, or a 3D noise for smoke/clouds), and ask "how much light reaches
*this* point?" — i.e. march a shadow ray toward the sun (or sample the shadow
map / fire an RT shadow ray). Accumulate in-scattered light and attenuate by
transmittance (Beer–Lambert: light falls off exponentially with density·distance).
The visible shafts through gaps in geometry ("god-rays") fall out of this for
free because shadowed steps contribute no in-scatter.

**Files:** a `volumetric` Slang pass between the forward and tonemap passes,
reading the existing shadow map / cube (or the TLAS on the RT path) for the
light-visibility term, compositing into the HDR target from §2. **Strategy.**
Needs §2 (HDR) so the bright in-scatter doesn't clip. March at *half-res* +
upsample (volumetrics are low-frequency) and use the light's shadow data you
already render. Animate density with scrolling 3D noise for drifting fog.

### 4c. Cloud / smoke as a 3D texture (voxel volumes)
**How it works.** Same ray-march-the-density loop as §4b, but density comes from
a baked or simulated **3D texture** (a voxel grid) instead of analytic noise —
this is how big fluffy volumetric clouds (à la Horizon/Nubis) and smoke sims are
rendered. The 3D texture can be authored (Blender VDB-style), procedurally
generated once, or written each frame by a fluid sim (§5c link). Sampling a 3D
texture is a hardware trilinear fetch, so it's cheaper per-step than evaluating
heavy noise inline.

**Files:** 3D-texture support in `renderer/Backend.hpp` + both backends (GL has
`GL_TEXTURE_3D`; VK has 3D images — both fine), the §4b march pass sampling it,
optional VDB/`.vol` loader in `scene/`. **Strategy.** TODO.md already wants
`Ray marching for clouds`. Build §4b first (analytic density), then swap the
density source to a 3D texture. Writing the 3D texture from a compute sim is
Vulkan-only.

---

## 5. Materials: glass, transparency, advanced surfaces

### 5a. Order-independent transparency / alpha blend (the foundation)
**How it works.** The forward path is opaque-only; transparent surfaces need
back-to-front blending or the result depends on draw order. Two approaches:
- **Sorted blending:** sort transparent meshes back-to-front per frame, draw
  after opaques with depth-test-on/depth-write-off and alpha blending. Simple,
  breaks on intersecting/overlapping transparents.
- **Weighted blended OIT** (McGuire): accumulate weighted color + revealage in
  two float targets, resolve in one pass. Order-independent, no sorting, good
  enough for most real-time glass/foliage.

**Files:** blend-state control in `renderer/Backend.hpp` + both backends (it's
mostly pipeline state), a transparent-pass split in `core/App.cpp`, `Material`
already has `alpha`. **Strategy.** On TODO.md (`Add blend (transparency)`). Ship
sorted blending first (trivial), upgrade to WBOIT if intersecting transparents
matter. Prerequisite for proper glass (§5b).

### 5b. Glass — refraction, Fresnel, roughness, absorption
**How it works.** Glass is what makes transparency *interesting*. The physics:
- **Fresnel:** glass reflects more at grazing angles, less head-on (you already
  have `fresnelSchlick`). So glass is part reflection, part transmission, mixed
  by the Fresnel term — same split-energy idea as the existing PBR.
- **Refraction:** light bends entering a denser medium (Snell's law). `refract()`
  in Slang gives the bent ray from the surface normal and the **index of
  refraction** (IOR ≈ 1.5 for glass). The view "through" the glass is sampled
  along that bent direction.
- **Roughness:** frosted glass scatters transmission like rough metal scatters
  reflection — blur the refracted sample by roughness.
- **Absorption / tint:** colored glass attenuates light by Beer–Lambert through
  its thickness (thicker = more saturated) — the same law as volumetric fog.
- **Chromatic dispersion (optional):** IOR varies per wavelength, so refract R/G/B
  slightly differently for prism edge color.

**Two ways to get the "behind" color to refract:**
1. **Screen-space refraction** (both backends): render opaques to an HDR
   texture (you have it from §2), then the glass pass samples that texture at the
   pixel offset given by the refracted direction. Cheap, but can't refract things
   off-screen or behind other transparents.
2. **Ray-traced refraction** (Vulkan, §1c machinery): trace the refracted ray
   into the TLAS and shade the true hit. Correct, handles arbitrary depth, the
   path to caustics. More expensive.

**Files:** a `glass`/transmission branch in `forward.slang` (Fresnel mix +
`refract` + absorption), driven by new material flags (IOR, transmission,
thickness — append to the uniform block per the layout rule), the §2 HDR target
for screen-space refraction, `Material.hpp` + `.mtl` loading for the new params.
**Strategy.** Screen-space refraction first (works on both backends, looks great
on the chrome-sphere-style showcase), RT refraction as the Vulkan upgrade. This
plugs straight into the existing Cook-Torrance frame — glass is "PBR with a
transmission lobe", not a separate shading model.

### 5c. Other material upgrades worth a line
- **Texture-driven PBR** (already a `FEATURES.md` follow-up): albedo/metal/rough/
  AO *maps* instead of per-material scalars — `map_Pm`/`map_Pr` loading + bindless
  slots. Low effort, big quality jump; do before exotic materials.
- **Proper IBL** (also a `FEATURES.md` follow-up): prefilter the skybox into an
  irradiance cubemap + roughness-mip prefiltered-specular cubemap + BRDF LUT, one
  compute/raster pass at load. Replaces the single-sample skybox ambient hack.
- **Clearcoat / anisotropy / subsurface:** extra BRDF lobes for car paint,
  brushed metal, skin/wax. Additive once texture-driven PBR exists.
- **Parallax occlusion mapping:** ray-march a height map in the surface tangent
  space (you already build the TBN in `perturbNormal`) so bricks/cobbles get real
  depth at silhouette-interior, not just normal-map lighting. Cheap depth illusion.

---

## 6. Physics — from Verlet particles to rigid bodies, cloth and rope

The Go prototype had Verlet particle physics (`go_deprecated/physics/verlet.go`);
the C++ engine kept Verlet-style movement hooks (`updateMeshes()` /
`Mesh::moveTo`) but no real solver. Here's how to grow it.

### 6a. Verlet integration — the shared substrate
**How it works.** Verlet stores each point's *current* and *previous* position;
the next position is `2·cur − prev + accel·dt²` (velocity is implicit in the
positional difference). It's stable, cheap, and — crucially — makes
**constraints** trivial: to satisfy a constraint (e.g. "these two points stay
distance L apart"), just *move the points* to fix it, and the implicit velocity
updates for free. Iterate constraints a few times per frame and the system
relaxes toward a valid state. This one idea drives ropes, cloth, and soft bodies.

**Files / strategy.** Port `verlet.go` to `cpp/physics/` (new dir), step it in
`Scene::updateMeshes`. This is the base for §6b–6d. CPU is fine for hundreds of
points; move to a Vulkan compute solver only if you need tens of thousands.

### 6b. Rope / chain
**How it works.** A rope is a line of Verlet points joined by **distance
constraints** ("each link stays length L from the next"). Pin the top point; let
gravity pull the rest; iterate the distance constraints ~10×/frame and it hangs
and swings like a real rope. Add bending constraints (between point i and i+2)
for stiffness. Render as a tube/strip following the points.

**Files:** `cpp/physics/` rope type, a tube-mesh generator in `scene/`, hook into
`updateMeshes`. **Strategy.** The "hello world" of Verlet constraints — ship it
first to validate the solver; visually striking for ~30 lines of solver.

### 6c. Cloth / flags
**How it works.** Cloth is a 2D *grid* of Verlet points with distance
constraints: structural (right/down neighbors), shear (diagonals), and bend
(skip-one neighbors). Pin two corners, apply gravity + a wind force, relax the
constraints — you get a waving flag. The same solver as rope, just a grid instead
of a line. Add sphere/plane collision by projecting points out of colliders.

**Files:** `cpp/physics/` cloth type, a dynamically-updated mesh (the existing
`updateMeshes()` already supports moving geometry — drive vertices from the point
grid), normals recomputed per frame for lighting. **Strategy.** Natural step
after rope. Self-collision is the hard part — skip it initially.

### 6d. Rigid bodies — cubes, boxes, stacking
**How it works.** Unlike a deformable blob, a rigid body has a fixed shape and
moves by **linear** (position/velocity) + **angular** (orientation/angular
velocity) state. The loop each frame:
1. **Integrate** forces → velocities → positions/orientations.
2. **Broad phase:** cheaply cull pairs that can't touch (AABB sweep / grid) so
   you don't test every box against every box.
3. **Narrow phase:** for surviving pairs, find actual contacts. Box-box uses the
   **Separating Axis Theorem** (if any axis separates the two shapes' projections,
   they don't intersect; otherwise the axis of least overlap gives the contact
   normal + penetration depth).
4. **Solve contacts:** apply impulses so bodies stop interpenetrating and bounce/
   slide per restitution + friction. Iterate (sequential impulses) for stable
   stacks.

**Files:** a real `cpp/physics/` rigid-body world (or integrate a library — Jolt
or Bullet — if you don't want to write the solver), box colliders from the
Blender export (TODO.md: `Add proper box colliders`, and collider transforms are
already exported per TODO.md line 8), drive `Mesh` transforms from body state.
**Strategy.** This is the biggest single item here. Decision up front: **write a
small impulse solver** (educational, full control, weeks of work, the engine's
"from scratch" ethos) **vs integrate Jolt** (production-grade, days, less to
learn). Recommend a small from-scratch box-only solver to match the project's
hand-rolled character, with the option to swap in Jolt if scope grows. The
RT-transform model in `RAYTRACING_PLAN.md` (transform in the TLAS instance, not
baked into vertices) pairs naturally with rigid bodies — a moving body refits the
TLAS instead of re-uploading vertices.

### 6e. Soft bodies (stretch)
**How it works.** Verlet again: fill a shape's volume/shell with points and a
dense web of distance constraints; stiff constraints → jelly, loose → cloth-like.
Pressure (a volume-preservation constraint) makes inflatable/squishy bodies.
**Strategy.** Only after rope+cloth+rigid; shares the §6a solver.

---

## 7. World generation & geometry (from TODO.md)

Grouped because they're CPU-side mesh/data generation, mostly backend-agnostic,
and largely independent of the rendering work above.

- **Instancing** (TODO.md): draw thousands of copies (grass, bushes, rocks) with
  one call + a per-instance transform buffer. Prerequisite for grass/foliage at
  scale; both backends support it (`glDrawElementsInstanced` / Vulkan instanced
  draws). Likely the highest value-to-effort item in this section.
- **Grass / bushes / fur** (TODO.md): instanced billboards or geometry-shader-
  generated blades (the Go path had a fur geometry shader). Add wind by animating
  vertices with noise. Needs §7-instancing.
- **Noise terrain** (TODO.md): generate a heightfield from fractal noise (fBm),
  triangulate to a mesh, with LOD/chunking for large worlds. Feeds navmesh.
- **Wave Function Collapse** (TODO.md): constraint-based tile/module placement
  for procedural levels — pure CPU data gen, outputs a set of placed meshes.
- **Isotropic remeshing** (TODO.md): post-process meshes to uniform triangle
  size — a tooling/quality step, lower priority.
- **Bezier paths** (TODO.md): spline curves for camera moves, object motion, and
  rope/cable routing. Cheap, useful for the demo/cutscene polish.
- **Navmesh** (TODO.md): walkable-surface graph for AI pathfinding — a gameplay
  system, belongs after the physics/ECS layer is back.

---

## 8. Suggested sequencing

A rough critical path that respects dependencies:

1. **HDR + tonemap + bloom (§2).** Unblocks RT accumulation, volumetrics, and
   glass refraction. Do this first — most things below assume a float target.
2. **Transparency/blend (§5a) → glass screen-space refraction (§5b).** High
   visual payoff, both backends, leans on §2.
3. **SDF ray marching (§4a).** Self-contained, both backends, big wow, already
   on TODO.md.
4. **RT shadows (§1a)** → the AS foundation, then **RTAO (§1b)** and
   **RT reflections (§1c)**, with **accumulation/denoise (§1e)** alongside.
   Vulkan only; everything else keeps working on GL.
5. **Volumetric fog/clouds (§4b/§4c).** After §2; reuses shadow/RT data.
6. **Physics: Verlet (§6a) → rope (§6b) → cloth (§6c) → rigid bodies (§6d).**
   Largely orthogonal to rendering; can proceed in parallel by a separate effort.
7. **Instancing + foliage/terrain (§7)** as world-building demands grow.

Per the cross-cutting rules: keep every Vulkan-only feature behind a
`supportsRayTracing()`-style capability with a GL no-op stub, author shaders once
in Slang with `#if TARGET_VK` splits, and append new uniform fields at the end of
the block to keep the three CPU/GPU mirrors byte-compatible.
