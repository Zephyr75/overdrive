# Ray Tracing Integration Plan

How hardware ray tracing would slot into the Overdrive C++ engine. **Design
note only — nothing here is implemented.** Read alongside `cpp/BACKEND.md` and
`notes/VULKAN.md`. For the ray-tracing-vs-path-tracing vocabulary, see
`notes/RAYTRACING.md`.

## TL;DR: Vulkan only

Ray tracing lands in the **Vulkan backend only**. The OpenGL backend cannot
participate, for a hard reason: the GL backend targets the **OpenGL 4.1 core
profile** (the macOS ceiling, see `cpp/BACKEND.md` — shaders are even
downgraded to GLSL 4.10). GL 4.1 has:

- no compute shaders (added in 4.3),
- no `GL_*_ray_tracing` extension path on the Apple stack,
- no acceleration-structure or shader-binding-table concept at all.

So there is no portable "both backends" story. The realistic options are:

1. **HW ray tracing, Vulkan only** (this plan) — `GLBackend` keeps the existing
   raster path unchanged; the feature is unavailable there.
2. A **software/raster approximation** (SSR, screen-space shadows, raster
   reflection probes) both backends could share — but that is *not* ray tracing
   and is out of scope.

The abstraction already tolerates option 1: `createBackend()` compiles exactly
one backend, and the scene layer never names an API. We add ray-tracing entry
points to the `Backend` interface as **optional capabilities** that default to
"not supported", so `GLBackend` stays a one-line stub.

## What ray tracing buys this engine first

The engine is a forward renderer with shadow-map shadows (2D + cube), a skybox,
Blinn-Phong materials (`scene/Material.hpp`), and bindless textures. Highest
value / lowest disruption first target: **ray-traced shadows** — replace the
shadow-map passes (`createShadowMap2D` / `createShadowCubemap` + the `depth` /
`depth_cube` shaders) with a ray query against the scene BVH. Removes
acne/peter-panning, cube-face seams, and the 6-face geometry-shader pass, and
reuses the existing light loop in `forward.slang`.

Follow-ups by increasing effort: ray-traced AO → reflections → one-bounce GI /
path tracing. The plan below builds the foundation (acceleration structures +
ray query in the forward shader) that all of these share.

## Two ways to trace in Vulkan

| | Ray query (inline) | Ray-tracing pipeline |
|---|---|---|
| Extension | `VK_KHR_ray_query` | `VK_KHR_ray_tracing_pipeline` |
| Rays fire | inside existing frag/compute shaders | dedicated raygen/miss/closest-hit stages |
| New plumbing | **none** — no SBT, no new pipeline type | shader binding table, RT pipeline, `vkCmdTraceRays` |
| Best for | shadows, AO, simple reflections | full path tracing, recursive bounces |

**Recommendation: start with ray query.** Drops straight into `forward.slang`'s
fragment shader as a shadow test, no shader-binding table, reuses the current
forward pass and pipeline machinery. Move to the RT pipeline only when recursive
multi-bounce GI is on the table.

Both paths share one prerequisite: **acceleration structures**
(`VK_KHR_acceleration_structure`), which need `bufferDeviceAddress` (already
enabled) and `VK_KHR_deferred_host_operations`.

## Device feature/extension changes (`VKBackend::init`)

Today the device enables 1.3 dynamic rendering, sync2, BDA, scalar layout,
descriptor indexing (`notes/VULKAN.md`). Add, gated behind a runtime probe:

```
Device extensions:
  VK_KHR_acceleration_structure
  VK_KHR_ray_query                 // inline path
  VK_KHR_deferred_host_operations  // dependency of accel structure
  (VK_KHR_ray_tracing_pipeline)    // only for the pipeline path, later

Feature structs chained into vkCreateDevice:
  VkPhysicalDeviceAccelerationStructureFeaturesKHR.accelerationStructure
  VkPhysicalDeviceRayQueryFeaturesKHR.rayQuery
```

Probe with `vkGetPhysicalDeviceFeatures2` first. If absent (older GPU, software
queue, or MoltenVK/macOS), set internal `rayTracingSupported = false` and fall
back to the existing shadow-map path. The engine must still run on a non-RT GPU
via that fallback.

## Geometry → acceleration structures

A two-level BVH, the standard layout:

- **BLAS** (bottom level) — one per unique `Mesh` geometry, built from the
  *same* vertex/index buffers the raster path uploads. `SubMesh`
  (`scene/Mesh.hpp`) holds `vao/ebo` + index list; the buffers additionally need
  `VK_BUFFER_USAGE_ACCELERATION_STRUCTURE_BUILD_INPUT_READ_ONLY_BIT |
  SHADER_DEVICE_ADDRESS_BIT`. `createBuffer`/`createMesh` are GL-flavoured
  (`vbo/vao/ebo` handles) but the Vulkan backend maps them to real `VkBuffer`s
  internally — add the usage flags there and feed device addresses to
  `vkCmdBuildAccelerationStructuresKHR`.
- **TLAS** (top level) — one per scene, one instance per `Mesh`, each carrying:
  - the BLAS device address,
  - a 3×4 transform from the mesh's world position,
  - `instanceCustomIndex` = index into a parallel geometry-descriptor array so
    hits can fetch material + normals.

Transform model: the engine currently bakes `position` into vertices
(`rebuildAndUpload`). For RT it is cheaper to keep geometry local and put the
transform in the TLAS instance, so a moved mesh triggers only a TLAS refit, not
a vertex re-upload.

Build timing:

- BLAS: once in `Mesh::setup`, immutable unless topology changes.
- TLAS: rebuilt/refit whenever a mesh moves — hook into `Scene::updateMeshes` /
  `Mesh::moveTo`. Animated meshes refit per frame; static meshes never rebuild.

This is the bulk of new backend code: scratch sizing via
`vkGetAccelerationStructureBuildSizesKHR`, a build command buffer, a barrier
before first use.

## Backend interface additions

Keep the scene layer API-agnostic. Add capability methods to
`renderer/Backend.hpp`, all defaulting to "unsupported" in `GLBackend`:

```cpp
virtual bool supportsRayTracing() const { return false; }

// Build/refit the scene BVH. Called from Scene after meshes set up and
// whenever geometry transforms change. Backend owns the AS handles.
virtual void buildAccelerationStructure(const Scene &scene) {}
virtual void refitAccelerationStructure(const Scene &scene) {}
```

The TLAS handle does not leak into scene code. In Slang the acceleration
structure is a descriptor (`RaytracingAccelerationStructure`), so it gets **one
new binding** in the existing bindless set 0 (alongside `sampler2D[]` /
`samplerCube[]`), not the BDA uniform block.

## Shader changes (`forward.slang`, ray-query path)

`common.slang` already branches on `TARGET_VK`. Add, VK-only:

```slang
[[vk::binding(2, 0)]] RaytracingAccelerationStructure tlas;
```

In the forward fragment shader, replace the shadow-map lookup with a ray query
toward each light (the directional/point loop already exists, driven by
`LightData`):

```slang
// pseudo — per light, instead of sampling TEX_SHADOWMAP / TEX_SHADOWCUBE
float traceShadow(float3 worldPos, float3 N, float3 toLight, float tMax) {
    RayDesc ray;
    ray.Origin = worldPos + N * 1e-3;   // surface offset replaces depth bias
    ray.Direction = normalize(toLight);
    ray.TMin = 0.0; ray.TMax = tMax;    // = distance to point light; large for directional

    RayQuery<RAY_FLAG_TERMINATE_ON_FIRST_HIT> q;
    q.TraceRayInline(tlas, RAY_FLAG_NONE, 0xFF, ray);
    q.Proceed();
    return q.CommittedStatus() == COMMITTED_TRIANGLE_HIT ? 0.0 : 1.0; // 0 = shadowed
}
```

`#else` (OpenGL) keeps the existing `TEX_SHADOWMAP` / `TEX_SHADOWCUBE` sampling
unchanged, so one source still feeds both backends. `lightSpaceMatrix` /
`shadowMatrices` / `farPlane` uniforms stay for the GL build; the VK build
ignores them.

Opaque shadow queries need no material data. Reflections/AO later need the
`instanceCustomIndex` → geometry-descriptor indirection to read hit-point
normals and bindless texture slots; design that descriptor buffer now (BDA array
of `{vertexBufferAddr, indexBufferAddr, materialIndex}`) even if only shadows
ship first.

## Render-loop integration

Minimal disruption to the pass-based lifecycle in `cpp/BACKEND.md`:

- **AS build/refit** happens outside `beginPass`/`endPass`, in `beginFrame` (or
  once at load) — it is compute/transfer work, not a render pass. Barrier its
  output before the forward pass reads the TLAS.
- The **shadow passes disappear** on the RT path: no shadow-framebuffer
  `beginPass`, no `depth`/`depth_cube` draws. The forward pass is otherwise
  unchanged — same dynamic-rendering target, same bindless set (now + TLAS), same
  BDA push constant.
- `setCullFace` etc. stay; ray queries ignore raster cull (shadows want
  double-sided occlusion anyway).

## Rollout order

1. Probe + enable extensions; real `supportsRayTracing()`, fallback intact.
2. BLAS/TLAS build in the Vulkan backend; validate in RenderDoc (shows
   acceleration structures) — no shader change yet.
3. Bind TLAS into the forward descriptor set; add `traceShadow` behind a
   toggle so RT vs shadow-map is A/B comparable.
4. Delete shadow passes on the RT path once parity is confirmed.
5. (Later) ray-traced AO → reflections → switch to RT pipeline + SBT for
   recursive GI if wanted.

## Risks / open questions

- **macOS**: MoltenVK ray-tracing support is partial; the fallback path is
  required, not optional, for the engine's cross-platform target.
- **Transform model**: moving world transform from baked vertices into TLAS
  instances diverges the VK path from the GL path's `rebuildAndUpload` — keep it
  behind the backend boundary.
- **Memory**: BLAS/TLAS + scratch are extra VRAM; size with
  `vkGetAccelerationStructureBuildSizesKHR`, reuse one scratch buffer.
- **Two frames in flight**: TLAS is GPU-only, so not duplicated per frame
  (`notes/VULKAN.md`), but a per-frame refit must not overwrite a TLAS the
  previous frame's GPU still reads — gate refit on the frame fence, or
  double-buffer the TLAS if refit-per-frame becomes normal.
</content>
