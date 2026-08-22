# TODO.md — the working list

Small, concrete items. Anything that needs a paragraph of reasoning lives in
`FEATURES.md` Part 2 instead.

## Engine

- [x] Overlay Gutter on Overdrive
- [ ] Fix Gutter on Overdrive — `main.go` still calls `App.Run` with a nil widget
- [x] Support multiple shadows (one 2D map + one cube) — superseded by the atlas
- [x] Retire the cube-shadow slots — every shadow is a tile of one atlas now, `tmp/LIGHTING_IMPL.md` Part C
- [x] Per-frame atlas allocation — a buddy quadtree scored by `radius / distance`, `tmp/LIGHTING_IMPL.md` Part D
- [x] Replace the per-frame quadtree with a fixed slot layout — rank picks the slot, the tier caps it; rects never move, so Part E can cache a baked tile. The layout is `tmp/LIGHTING_PLAN.md` §4.1's partition exactly: 1x2048 + 16x512 + 64x256 + 256x128, 337 slots, 100% of the atlas
- [x] Make `atlasSize` a `[shadows]` quality knob — `tmp/LIGHTING_IMPL.md` Part H. The bias problem was real: `FrameUniforms.ShadowNormalScale` carries `4096 / atlasSize` and `shadowLookup` multiplies the world-space offsets by it, 1.0 at the default so the shipped image did not move
- [x] Check whether `[shadows] width`/`height` in the TOML still drive anything — they did not, nor did `settings.ShadowAspectRatio()`. All three deleted in Part H
- [ ] Split the per-tile bake state out of `FrameUniforms` — `CurWorldToTile`/`CurLightPos`/`CurFarPlane` are the only fields the shadow pass changes per tile, but it republishes all 4844 bytes, so a tile costs 4864 ring bytes instead of ~100. That overflowed the 1 MiB ring at 259 tiles (silent wrong depth, one stderr line); `ringSize` is 4 MiB now as a stopgap, and a full 337-slot atlas would still cost ~1.6 MiB per frame
- [x] Delete the ×5 point-shadow scale in `forward.slang` — it made a shadowed fragment subtract light, invisible with one caster and blotchy with six
- [x] Clean up the fragment shader
- [x] Integrate the skybox reflection into the colour computation (PBR ambient term)
- [x] Usable from a simple ECS script
- [x] Read collider position, size and rotation from the Blender scene
- [x] Quality tiers — the whole `[shadows]` section is config now (`atlasSize`, `slotDivisors`/`slotCounts`, `tierScores`, `dynamicAtlas`, `bakeBudgetMiB`, `pcf`, `nearPlane`/`farPlane`), plus `configs/low.toml`. `tmp/LIGHTING_IMPL.md` Part H
- [ ] Clustered forward — the last unbuilt part of the lighting plan, lifted out to `tmp/CLUSTERED_FORWARD.md`. Also what fixes `lightScore` ranking a light behind the camera as highly as one in front of it, and what removes the fixed `MaxLights = 64`
- [~] Debug mode — a `[debug]` TOML section so far: `lockCamera`, `noShadows`, `validation`. Images are inspected in RenderDoc; no in-engine overlay yet
- [x] Multiple lights of the same type (up to `MAX_LIGHTS` = 64)
- [ ] Proper box colliders — `physics/box.go` is empty, `box_old.go` is commented out
- [ ] Verlet distance constraints — `physics/link.go` is commented out
- [ ] Audio support

## Backend

The ordered plan is `tmp/BACKEND_DECISION.md` §9. These are its first items.

- [x] Delete the OpenGL backend — 2026-08-05
- [x] Drop the dead 16-byte cell rule from `renderer/uniforms.go` and `common.slang` — `tmp/BACKEND_DECISION.md` §5.3
- [x] Drop the GL-shaped interface members: `CreateBuffer`'s `dynamic`, `WhiteTexture`, `CreateShader`'s `hasGeometry` — §5.4
- [x] Replace the GL-shaped semantics: cull/depth enums, `CreateTexture` taking pixels, `BindShader` + `Draw`, `BeginPass` without w/h — §5.6
- [ ] **Vulkan-native clip space** — build projections y-flipped with `[0,1]` depth, then delete `TO_VK_DEPTH`, the negative-height viewport and the shadow-pass `FrontFace = Clockwise` case — §5.5
- [ ] Reverse-Z, once the above and `CompareOpGreater` land — §9 item 9
- [ ] Shader hot-reload — the biggest single velocity win, and independent of everything else
- [ ] `go-vulkan`: formats, barrier rework, compute, storage images, blit — `go-vulkan/BINDINGS_GAP.md` §7 batches 1-6
- [ ] `PipelineSpec`, replacing `CreateShader` + `SetCullMode` / `SetDepthCompare`
- [ ] `Pass` interface and a pass list, replacing the hardcoded frame in `core/app.go`
- [ ] Compute in `Backend` — `Dispatch`, `CreateStorageBuffer`, `CreateStorageImage`

## Rendering

- [x] Anti-aliasing — MSAA on the backbuffer, `[antialiasing]` in the config file choosing the mode and count
- [ ] Post-process AA (FXAA/TAA) — needs the scene rendered offscreen, which needs a depth attachment on colour render targets (`passOffscreenColor` has none)
- [x] Framebuffers — generalised into `CreateRenderTarget(RenderTargetSpec)`
- [x] Normal mapping (tangent-space, per-fragment TBN)
- [ ] HDR + tone mapping + bloom — needs a half-float format binding, `go-vulkan/BINDINGS_GAP.md` §7 batch 1. See `FEATURES.md` §2
- [ ] Ambient occlusion (SSAO)
- [x] Depth prepass — `[renderer] depthPrepass`, `BeginDepthPrepass` + `prepass.slang`, main pass shading with `CompareEqual`. `tmp/LIGHTING_IMPL.md` Part F
- [ ] Blending / transparency — blocked on `PipelineSpec`, since there is no blend state in the interface. **Transparent geometry must be excluded from the depth prepass**, in three parts: skip it in `Scene.RenderDepthPrepass`, draw it after the opaque `CompareEqual` batch with `CompareLess` and depth write off, and sort it back-to-front. Also needs `Material.Alpha` to actually reach `DrawUniforms` (it is parsed and dropped today, and the struct is size-guarded at 128 bytes), and `DepthWriteEnable` to become dynamic state — it is baked per pipeline in `vulkan/shader.go` from the vertex layout
- [ ] Alpha cutout, if it lands before the above — a `discard`ing material stays opaque and stays in the prepass, but `prepass.slang` must then run the identical `discard`, or the depth it writes disagrees with `forward.slang` and `EQUAL` rejects the wrong fragments. Silent, and it looks like speckled geometry
- [ ] Instancing
- [x] Anisotropic filtering — `[textures] anisotropy` in the config file (1/2/4/8/16), clamped to the device limit in `createSamplers`. **Does almost nothing until mipmaps land** — see the next item
- [ ] **Mipmaps**, which is what makes anisotropy and `SamplerMipmapModeLinear` pay off. In order:
  - [ ] `CmdBlitImage` + `ImageBlit` bindings in `go-vulkan` — `BINDINGS_GAP.md` §5.2. `ImageUsageTransferSrc` and `ImageLayoutTransferSrcOptimal` are already bound
  - [ ] `FormatFeatureSampledImageFilterLinear` binding, to probe the format before blitting — the spec requires linear-filter support for a linear blit, and R8G8B8A8_UNORM is not guaranteed to have it
  - [ ] Generalise `imageBarrier` — it hardcodes `LevelCount: 1` (`vulkan/backend.go:990`), so it cannot transition a mip chain. Needs a base level + count
  - [ ] `uploadTexture`: `MipLevels = floor(log2(max(w,h))) + 1`, add `ImageUsageTransferSrc`, then blit level i-1 → i down the chain, ending with the whole chain in `SHADER_READ_ONLY`. Must handle the 6-layer cubemap path too
  - [ ] Image view `LevelCount` and sampler `MaxLod` follow the chain length; both are pinned at 1 today
- [ ] Shadow cascades — currently fixed at 1024², no CSM
- [ ] Geometry shader for fur
- [ ] Ray-traced shadows — `FEATURES.md` §3, `tmp/BACKEND_DECISION.md` §8
- [ ] Ray marching for basic shapes and clouds — existed in the pre-Slang GLSL tree, not ported; `shaders/slang/` has only `forward`, `depth`, `depth_point`, `skybox`, `ui`

## Tooling and cleanup

- [ ] GPU timestamp queries, to profile one pass against another — blocked on query-pool bindings, `go-vulkan/BINDINGS_GAP.md` §5.4
- [ ] Debug object names (`SetDebugUtilsObjectNameEXT`) so validation and RenderDoc show names, not handles — `go-vulkan/BINDINGS_GAP.md` §5.5
- [ ] A rendered-image regression test — nothing checks the frame today, only that the scene parses
- [ ] Score physical devices instead of taking `devices[0]`
- [ ] Delete or implement the dead files listed in `ARCHITECTURE.md` §8
- [ ] `overdrive.sh` and `overdrive_build.sh` at the repo root are still cmake wrappers for the deleted C++ tree

## Procedural / world

- [ ] Wave Function Collapse — `algorithms/wfc.go` is an empty placeholder
- [ ] Noise terrain
- [ ] Grass, bushes
- [ ] Isotropic remeshing
- [ ] Navmesh
- [ ] Bezier paths

## Reference

- [Cubemap from HDRI](https://matheowis.github.io/HDRI-to-CubeMap/)
- Coordinate conversion, Blender → engine (OBJ convention, Y up):
  `pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}`
- `GOPROXY=proxy.golang.org go list -m github.com/Zephyr75/gutter@v0.1.2`
