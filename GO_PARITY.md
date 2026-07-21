# GO_PARITY.md — remaining feature checklist

What the Go engine still owes the C++ engine it replaced. The C++ tree was
deleted on 2026-07-22 once the showcase scene was salvaged; these items were
catalogued against it beforehand, and `notes/FEATURES.md` remains the written
record of what it did. Its source is in git history if an exact detail is ever
needed.

Items that engine also lacked (texture-driven PBR maps, real prefiltered IBL,
HDR/bloom, ray tracing) are **not** listed here — they are roadmap, and live in
`notes/FEATURES.md` Part 2.

Scope note: the renderer itself is at parity. Both engines run the same Slang
shader set through the same pass structure on both backends, so everything that
lives *in the shaders* — Cook-Torrance PBR, the GGX/Smith/Fresnel BRDF, the
Fresnel-weighted skybox ambient, Reinhard tonemapping, normal mapping,
normal-offset shadow bias, early-bail PCF, the per-light uniform hoisting — the
Go engine gets for free and needs no work. What remains is almost entirely in
the **scene layer** and in **backend polish**.

---

## 1. Correctness — worth doing first

- [x] **Shadow casters are selected** (2026-07-22). `Scene.pickShadowCasters`
  takes the first directional and first point light; only those allocate a map
  (`Light.setup` early-returns otherwise) and only those bake a depth pass.
  `ShadowDirIndex` / `PointShadowLights[]` are now set, so the 2D map is applied
  to the light that owns it rather than to whichever sits at index 0. The
  showcase has 5 lights and used to run 5 shadow passes with the sun's map
  applied to a point light; it now runs 2, correctly.

- [ ] **Only one point-shadow cube is supported.** `renderer.Uniforms` has a
  single `TexShadowCubeMap` handle where the shader has
  `shadowCubeMap[MAX_SHADOW_CUBES]`. Both backends bind cube slot 0 and fill
  slots 1..3 with a dummy. Needs `TexShadowCubeMap` to become an array, the
  scene to assign slots, and the two backends' dedicated-binding writes to loop
  (`vulkan/draw.go` `bindShadowMaps`, `opengl/uniforms.go` `applyUniforms`).

- [x] **Texture paths are portable** (2026-07-22). `texturePath`
  (`scene/mesh.go`) keeps only the basename and resolves it against the
  engine's `textures/` directory, so a Blender-baked absolute path from another
  machine still loads. `<mtl>` is also optional now, defaulting to the `.obj`
  basename.

## 2. Rendering polish

- [ ] **Shadow map resolution is 1024²**, C++ uses 2048² (`settings/`).
- [ ] **No mipmaps on GL textures.** `opengl.LoadTexture` never calls
  `gl.GenerateMipmap` and uses a non-mipmapped min filter; C++ GL generates
  them. (The Vulkan side skips mipmaps in *both* engines — that one is shared
  roadmap, needing `CmdBlitImage` per `GO_BACKEND.md` §6.2.)
- [ ] **MSAA asymmetry.** The GL backend requests 4× samples via
  `glfw.Samples`; the Vulkan backend rasterises at `SampleCount1Bit`. C++ has
  the same asymmetry, so this is parity-neutral but still a real difference
  between the two Go backends.

## 3. Vulkan backend polish

- [ ] **Physical device is `devices[0]`.** C++ `pickPhysicalDevice` scores
  candidates and requires API ≥ 1.3; the Go backend takes the first device and
  would fail confusingly on a 1.2 driver or pick an iGPU over a dGPU.
- [ ] **No debug-messenger callback.** Validation output relies on the layers
  writing to stderr; C++ installs `VkDebugUtilsMessengerEXT` so messages are
  routed and can be broken on. Needs bindings.
- [ ] **No BDA capture-replay.** C++ sets the capture-replay flag on
  buffer-device-address allocations when supported, so RenderDoc can capture;
  without it RenderDoc crashes on the BDA. Needs bindings.
- [ ] **No GPU timing.** C++ has opt-in timestamp queries (`OD_GPU_TIMING`)
  reporting bake vs main-pass milliseconds — the tool `notes/OPTIMISATION.md`
  argues you need instead of FPS subtraction. Needs query-pool bindings.

## 4. Verification owed

- [ ] **The two backends have never been image-compared.** Both run clean (GL
  error-free; Vulkan validation-clean) at identical frame rates on the same
  shaders, but no screenshot diff has been done — this dev box is Wayland with
  no working capture path. This is the outstanding Phase 4 exit criterion.
- [ ] **Swapchain resize is untested** on the Vulkan backend, for the same
  reason. The recreate path exists and is wired to `ErrOutOfDateKHR` on both
  acquire and present, and `input.FramebufferSizeCallback` updates the settings
  the next frame's passes read.
- [x] **Showcase scene salvaged** (2026-07-22). `assets/showcase.xml` plus its
  5 meshes and 8 CC0 PBR textures now live in the Go tree, and
  `scene/showcase_test.go` asserts the materials parse with real `Pm`/`Pr`
  values, colour and normal maps, and that every texture it names exists — the
  material path is no longer unexercised. Was: C++ ships `assets/showcase.xml` exercising every
  feature (metal Suzanne, chrome sphere, normal-mapped ground, sun + point
  shadow) with CC0 textures. The other Go scenes (`demo`, `sphere`, `cube`) predate PBR
  and normal mapping.

## 5. Not gaps — the Go engine is ahead here

These exist in Go with no C++ counterpart: the ECS (`ecs/`), Verlet physics (`physics/`), the gutter-based
UI overlay (`core/ui.go` + the `ui` Slang pass), the Blender export plugin
(`plugin/`), and `algorithms/`.

---

### Recently closed

- Backend abstraction, pass-based frame loop, typed uniforms — Phases 1–2.
- Single-source Slang shaders for **both** backends, std140 UBO upload on GL,
  `forward` as the main program — Phase 3 (2026-07-21).
- Full Vulkan backend — Phase 4 (2026-07-21).
- PBR material scalars (`Pm`/`Pr` MTL keys, sane `roughness`/`ao` defaults) and
  normal-map wiring (`TexNormalMap` + `UseNormalMap`) reaching the shader —
  2026-07-21. These were silently zero, which made every material read as a
  perfect mirror with no ambient once `forward.slang` became the main program.
