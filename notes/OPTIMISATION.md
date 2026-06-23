# Overdrive C++ — Performance log

Every performance problem hit in the `cpp/` engine so far, what caused it, how it
was fixed (or deliberately left alone), and how it was measured. Read alongside
`notes/FEATURES.md` (feature context) and `cpp/BACKEND.md` (renderer contract).

Most of these surfaced on an **Intel UHD 620** iGPU under FIFO (vsync) present.
That machine is a **throwaway development box** — the engine targets discrete
GPUs — so a recurring theme below is distinguishing real, portable costs from
Intel-driver (ANV) artifacts that vanish on the target hardware.

---

## Measurement methodology

### GPU timestamp queries (the reliable tool)
Opt-in, off by default. Set the env var `OD_GPU_TIMING=1` and each backend writes
three GPU timestamps per frame — frame start, the shadow→main boundary, and frame
end — then prints averaged **GPU-side** milliseconds for the shadow-bake and main
pass every 120 frames:

```
OD_GPU_TIMING=1 ./build-vk/overdrive     # Vulkan
OD_GPU_TIMING=1 ./build/overdrive        # OpenGL
```

- **Vulkan** (`vulkan/Backend.cpp`): a `VK_QUERY_TYPE_TIMESTAMP` query pool, 3
  queries per frame-in-flight. Results are host-read at the next `beginFrame`,
  after that frame's fence is already waited, so the read never stalls. The
  boundary timestamp uses **`BOTTOM_OF_PIPE`**, not `TOP_OF_PIPE`: a top-of-pipe
  timestamp fires when the command is *parsed*, before the shadow work drains, so
  it under-counts the bake and over-counts the main pass. Bottom-of-pipe fires
  after all prior work completes — the true stage boundary. Multiply ticks by
  `limits.timestampPeriod` (83.3 ns on this device) for nanoseconds.
- **OpenGL** (`opengl/Backend.cpp`): `glQueryCounter(GL_TIMESTAMP)` at the same
  three points, double-buffered so results are read one frame late (no stall).
  GL timestamps are already nanoseconds and natively have bottom-of-pipe
  semantics, so the boundary is correct without extra care.

The instrumentation is generic (any GPU/driver) and adds nothing to a normal run
(guarded by the env var).

### Why not just toggle features and read FPS?
FPS subtraction (measure with a stage on, then off, subtract the frame times) is
tempting and was used early — **it lied twice** and cost real debugging time:

1. It attributed the Vulkan cost to the **shadow bake**. Real timestamps showed
   the bake is ~equal on both backends; the gap is the **main pass**.
2. It made OpenGL look much faster than it was, because at the time **GL draws
   were silently failing** (`GL_INVALID_OPERATION`, see below) — so GL wasn't
   doing the shadow work it appeared to skip cheaply.

Lessons: (a) FPS is a whole-frame average contaminated by vsync quantization and
thermal throttling; a difference of two noisy averages has a large error bar.
(b) Always confirm the thing you think you're measuring is actually executing.
(c) Per-stage GPU timestamps cost ~30 lines and remove the guesswork — use them
before drawing conclusions or committing to a fix.

### Thermal caveat
The iGPU throttles hard under sustained load (observed FPS sliding 25→6 within a
single run, package ~67 °C). Absolute numbers below drift run-to-run; trust the
**ratios** between stages measured back-to-back, not the absolutes.

---

## Fixes that landed

### 1. Per-light uniform reads through the BDA pointer (Vulkan)
**Symptom:** first multi-light cut ran the showcase at ~60 fps on OpenGL but only
~30 on Vulkan; going from 2→5 lights pushed Vulkan off a vsync interval.
**Cause:** the per-light loop in `forward.slang` read loop-invariant material
fields (`U.matAmbient`/`matDiffuse`/`matSpecular`/`matShininess`) *inside*
`calcDirLight`/`calcPointLight` — once per light per fragment. On Vulkan `U` is a
`buffer_reference` (BDA) pointer; the compiler can't prove those loads are
loop-invariant (no aliasing guarantee) and re-fetches them every iteration. On
OpenGL the same reads hit a UBO and ride Intel's constant cache for free — hence
the backend-specific cliff.
**Fix:** hoist the material fields into a local `MatParams` struct (and the loop
scalars `lightCount`/`shadowDirIndex` into locals) once at the top of `fsMain`,
pass `MatParams` into the light functions. The loop touches registers, not the
BDA pointer.
**Result:** Vulkan 39 → 53 fps (5 lights); OpenGL unaffected. Structural, not
light-count-specific — the saving applies for any N. This is *why* the per-light
loop never reads material data through `U` directly.

### 2. Shadow taps through bindless descriptors (Vulkan)
**Symptom:** Vulkan ~2× slower than OpenGL on the shadow-heavy showcase (~37 vs
~62 fps), worse under throttle. Gutting the fragment shader showed the cost was
the shadow taps, not PBR/IBL math or CPU submission.
**Cause:** the shadow maps were sampled through the bindless `texturesCube[idx]` /
`textures2D[idx]` arrays. Intel's ANV driver re-fetches a *dynamically-indexed*
descriptor on every tap → 20 cube taps = 20 descriptor fetches.
**Fix:** give the shadow maps plain **dedicated bound descriptors** (set 0,
binding 2 = `Sampler2D`, binding 3 = `SamplerCube[MAX_SHADOW_CUBES]`) — the same
fixed-sampler model OpenGL already uses. `VKBackend` mirrors each caster's map
into the binding in `bindTexture2D`/`bindCubemap`, rewriting a slot only when its
caster changes (`writeDedicatedTexture`, guarded by `shadow2DHandle` /
`shadowCubeHandles[]`). Material textures stay bindless.
**Result:** ~37 → ~43 fps. (See FEATURES.md for the descriptor-layout detail.)

### 3. Full PCF kernel on every fragment
**Symptom:** still tap-bound after fix #2.
**Cause:** every fragment ran the full 9-tap (2D) / 20-tap (cube) PCF kernel,
even though almost all fragments are unambiguously fully lit or fully shadowed.
**Fix:** **early-bail PCF** — take 4 spread taps first; if they unanimously
agree, return immediately and skip the full kernel. Only penumbra fragments pay
full price. Quality unchanged.
**Result:** ~43 → ~46 fps, and it throttles less (less total GPU work). Helps both
backends, disproportionately the tap-bound Vulkan path.

### 4. OpenGL drew nothing but the skybox (correctness, but it poisoned perf data)
**Symptom:** after multi-cube shadows landed, OpenGL showed only the skybox; the
scene was invisible. (Listed here because it also made GL's perf numbers a lie —
see methodology.)
**Cause:** two unit-binding regressions in `Mesh::draw` from a texture-unit
refactor. (a) `shadowCubeMap` became a `samplerCube[4]` **array**, but the GL
sampler-location enumeration in `opengl/Shader.cpp` only recorded the array as one
mangled name — so `setInt("shadowCubeMap[1..3]", unit)` matched nothing and those
samplers stayed at GL default unit 0, which holds the *2D* shadow map → sampler
**type conflict** → `GL_INVALID_OPERATION` on every `glDrawElements` (only the
skybox, a separate program, survived). (b) the normal map was bound to the old
unit 4, now a cube-shadow unit, a second 2D/cube collision.
**Fix:** enumerate array samplers per element (query `glGetUniformLocation` for
each `[e]`, store under the logical `shadowCubeMap[e]`); bind diffuse/normal via
the `Settings::UNIT_*` constants; bind a valid cube (skybox) to unused shadow
slots so the array stays complete.
**Lesson:** a draw that silently no-ops looks like a fast draw. Always check
`glGetError` / validation when a stage "gets cheaper" unexpectedly.

---

## Deliberate non-fix

### Dynamically-indexed cube-shadow descriptor (Vulkan, multi-cube)
**Finding (real GPU timestamps, least-throttled):**

| stage        | OpenGL  | Vulkan |
|--------------|---------|--------|
| shadow bake  | ~14.8 ms| ~16 ms |
| main pass    | ~12.3 ms| ~32 ms |
| — cube PCF within main | (folded in) | ~16 ms |
| total        | ~27 ms (≈37 fps) | ~48 ms |

The bake is ~equal on both backends, so it is **not** the gap (this killed an
earlier plan to rewrite the cube bake with multiview — it would have bought
nothing). The gap is the **main pass**: Vulkan spends ~16 ms on cube PCF alone,
which OpenGL does inside its ~12 ms whole-main-pass budget. Isolation test
(`return 0` from `shadowCalculationCube`) confirmed it: Vulkan main pass halved.

**Cause:** extending point shadows to `MAX_SHADOW_CUBES` made the cube sampler a
descriptor array indexed by a runtime slot (`shadowCubeMap[slot]`). Intel's ANV
driver re-fetches that dynamically-indexed descriptor per tap — exactly the cost
fix #2 removed for the single (non-indexed) cube, reintroduced by the array index.
(`[ForceUnroll]` on the slot scan did **not** help: slang kept `slot` a runtime
value, so the descriptor stayed dynamic. That dead attribute was removed.)

**Why we don't fix it:** the fix would be to constant-fold the index — a
`switch(slot)` calling `Sample` with literal `[0]..[3]`, or four single
`SamplerCube` bindings — so each tap hits a compile-time-known descriptor. But:
- The cost is an **Intel-iGPU (ANV) artifact**. Discrete NVIDIA/AMD cache
  descriptors in hardware and make dynamic indexing ~free, so the fix wins
  ~nothing on the GPUs this engine actually targets.
- This iGPU is a **throwaway dev box**; the engine won't ship on it.
- The fix adds a VK/GL `#ifdef` split and `switch` boilerplate to a hot path —
  complexity paid to chase a number we can't even see on real hardware.

The generic array (`shadowCubeMap[slot]`) is kept: cleanest, portable, spec-legal
(`slot` is dynamically uniform). The dynamic-index pattern is *correct* — only
ANV makes it slow. **Revisit only if a target GPU profiles the same way** (use
`OD_GPU_TIMING` to check); the fix itself is generic best practice ("avoid dynamic
descriptor indexing in a hot loop"), just not worth it now.

---

## Open levers (if a real target ever needs them)
- Fewer base cube PCF taps, or a screen-space shadow cache.
- Ray-traced shadows (Vulkan only, roadmap §3 in FEATURES.md) — removes the
  shadow-map taps entirely.
- Constant-index cube sampling (above) — only if profiled to matter on target HW.
