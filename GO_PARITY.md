# GO_PARITY.md — what the Go engine still owes the C++ engine

Checklist of feature gaps between `go_deprecated/` (the Go engine, becoming the
main implementation) and `cpp/` (the debugged reference), as of **2026-07-21**,
after `GO_BACKEND.md` Phases 0–4 landed.

Source of truth for the C++ side is `notes/FEATURES.md`. Items the C++ engine
also lacks (texture-driven PBR maps, real prefiltered IBL, HDR/bloom, ray
tracing) are **not** listed here — they are roadmap for both and live in
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

- [ ] **Shadow casters are not selected; every light gets a shadow map.**
  `Light.setup` (`scene/light.go`) allocates a 2D map or cubemap for *every*
  light, and `App.Run` runs a depth pass for every light. C++ `Scene` picks the
  first directional plus the first `MAX_SHADOW_CUBES` point lights, sets
  `castsShadow`, and only casters allocate or bake. Cost today is unbounded
  shadow passes as lights are added.

- [ ] **`ShadowDirIndex` and `PointShadowLights[]` are never set** —
  `Scene.FillFrameUniforms` leaves them at their zero values. `forward.slang`
  reads `shadowDirIndex` to decide *which* light the 2D shadow map applies to,
  so index 0 wins by default regardless of that light's type. In
  `assets/sphere.xml` light 0 is the **point** light and light 1 the sun, so the
  directional shadow map is currently applied to a point light. Likewise
  `pointShadowLights[]` is all zeros, so all four cube slots claim light 0.
  Fix alongside caster selection above; the shader side already works.

- [ ] **Only one point-shadow cube is supported.** `renderer.Uniforms` has a
  single `TexShadowCubeMap` handle where the shader has
  `shadowCubeMap[MAX_SHADOW_CUBES]`. Both backends bind cube slot 0 and fill
  slots 1..3 with a dummy. Needs `TexShadowCubeMap` to become an array, the
  scene to assign slots, and the two backends' dedicated-binding writes to loop
  (`vulkan/draw.go` `bindShadowMaps`, `opengl/uniforms.go` `applyUniforms`).

- [ ] **Texture paths are not portable.** `Mesh.setup` loads
  `material.TexturePath` verbatim, so a Blender-baked absolute path breaks on
  any other machine. C++ (`cpp/scene/Mesh.cpp`) strips to the basename and
  resolves against the project-local `textures/` directory.

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
- [ ] **No showcase scene.** C++ ships `assets/showcase.xml` exercising every
  feature (metal Suzanne, chrome sphere, normal-mapped ground, sun + point
  shadow) with CC0 textures in `cpp/textures/`. The Go scenes (`demo`, `sphere`,
  `cube`) predate PBR and normal mapping, so most of the material path is
  currently unexercised — which is also why the gaps above went unnoticed.

## 5. Not gaps — the Go engine is ahead here

These exist in Go with no C++ counterpart, and should not be lost in the
rename to `go/`: the ECS (`ecs/`), Verlet physics (`physics/`), the gutter-based
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
