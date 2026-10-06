# TODO — the working list

> **Scope** small, concrete open items. Anything needing a paragraph of reasoning lives in `FEATURES.md` §9.
>
> `[ ]` open · `[~]` started · `[-]` dropped on purpose

---

## Engine

- [ ] Fix Gutter on Overdrive: `main.go` still calls `App.Run` with a nil widget
- [ ] Clustered forward: removes the fixed `MaxLights = 64` and stops a light behind the camera scoring like one in front
- [ ] Proper box colliders (only sphere and plane exist)
- [ ] Verlet distance constraints
- [ ] Audio support

## Backend

- [ ] **Vulkan-native clip space**: build projections y-flipped with `[0, 1]` depth, then delete `TO_VK_DEPTH`, the negative-height viewport and the atlas passes' `WindingClockwise`
- [ ] Reverse-Z, once the above lands
- [ ] Bind `ReloadPipelines` (shader hot reload) to a key or a file watcher
- [ ] Ray queries: `go-vulkan` acceleration-structure bindings, then `CreateAccel`/`BuildAccel` and their specs
- [ ] **Prove the stack**: HDR + tonemap + bloom built entirely in `scene/` or a new `effects/` package, touching nothing under `vulkan/`
- [ ] Score physical devices instead of taking `devices[0]`
- [ ] Split `BufferCopyDst`'s second meaning ("the CPU reads this back", which picks cached memory in `vulkan/buffer.go`) into a `Readback` flag

## Rendering

- [ ] Post-process AA (FXAA/TAA): needs the scene rendered offscreen, which `PassSpec` already expresses
- [ ] HDR + tonemapping + bloom: `FormatRGBA16F` exists and `Capacities().Formats` probes it; a mip chain of views plus `BlendAdd` is all bloom needs
- [ ] Ambient occlusion (SSAO), reading the prepass depth
- [ ] Blending / transparency: a second pipeline drawn after the opaque `EQUAL` batch, skipped in `Scene.RenderDepth`, `CompareLess`, no depth write, sorted back to front. `Material.Alpha` is parsed but never reaches `DrawUniforms`
- [ ] Alpha cutout: stays in the prepass, but `prepass.slang` must `discard` exactly like `forward.slang` or `EQUAL` speckles
- [ ] Instancing
- [~] **Mipmaps**, which is what makes anisotropy pay off. `go-vulkan` has `CmdBlitImage` and the linear-filter format probe; still needed:
  - [ ] Mip count on texture upload (`floor(log2(max(w, h))) + 1`), the 6-layer cubemap path included, plus `GenerateMips` and the mip-range barrier back
  - [ ] The material sampler's `MaxLod` following the chain length
- [ ] Shadow cascades for the sun (one 2048 tile today)
- [ ] Slope-scaled depth bias, for a flat surface that must cast
- [ ] Ray-traced shadows (`FEATURES.md` §9)
- [ ] Ray marching for basic shapes and clouds
- [ ] Geometry shader for fur

## Tooling and tests

- [ ] **Any test at all**: there is not one `_test.go`. Start with `shadowAtlas.allocate`, pure CPU logic over `[]Light`
- [~] Image regression test: `go run . -screenshot out.png` writes frame 90, nothing compares two runs yet
- [ ] Rename `[shadows] bakeBudgetMiB`: it counts texels in units of 2^20, not memory
- [ ] Remove the shadow sampler's white border (`scene/shadowatlas.go`): `shadowLookup` returns before sampling outside a tile, so it is never reached
- [-] GPU timestamp queries: built, then deleted unused; RenderDoc profiles per pass
- [-] Debug object names and the validation messenger: validation output comes from a `VK_LAYER_SETTINGS_PATH` file instead (`CLAUDE.md`)

## Shading experiments

- [ ] Glass shader (needs transparency)
- [ ] Watercolor shader: [reference](https://x.com/TheMirzaBeig/status/2016702324576579644), [Blender version](https://www.reddit.com/r/blender/comments/1hcfb8x/realtime_watercolor_shader_in_blender/)

## Procedural / world

- [ ] Wave Function Collapse
- [ ] Noise terrain
- [ ] Grass, bushes
- [ ] Isotropic remeshing
- [ ] Navmesh
- [ ] Bezier paths

## Reference

- [Cubemap from HDRI](https://matheowis.github.io/HDRI-to-CubeMap/)
- Blender → engine coordinates: `pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}`
- `GOPROXY=proxy.golang.org go list -m github.com/Zephyr75/gutter@v0.1.2`
