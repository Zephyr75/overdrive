# TODO — the working list

> **Scope** small, concrete open items, split by priority. Anything needing a paragraph of reasoning lives in `FEATURES.md` §9.
>
> `[ ]` open · `[~]` started · `[-]` dropped on purpose

---

## Now — a clean engine for tiny games

The goal: build small games quickly on proper shadows, clean image quality, basic PBR shading, a working UI and base physics, with objects placed and moved from Blender.

### Transforms and Blender export

Today the OBJ is exported in world space, `<rotation>` is written but ignored, scale is not exported, and a move rebuilds and re-uploads every vertex on the CPU (`Mesh.updateVertices`).

- [ ] Export each OBJ in **local space** (no object transform applied) and write position, rotation and scale to the XML
- [ ] Give each mesh a model matrix built from position/rotation/scale, sent in `DrawUniforms.Model` (the shaders already multiply by it)
- [ ] `MoveTo` / `MoveBy` (and a new rotate) update the matrix only, no vertex re-upload; drop `initialPosition` and the per-move buffer rewrite
- [ ] Bake and cull shadows with the transformed bounds (`boundsCenter`, `casterInRange`), and keep a moved mesh dirtying its dynamic tiles
- [ ] Check the normal matrix (`inverse3` in `surface.slang`'s `vsMain`) under rotation and non-uniform scale
- [ ] Re-export `showcase.xml` and `stress.xml`, and compare with `-screenshot` before and after

### Physics

- [ ] Build colliders from the mesh's bounds and transform instead of raw vertices (`NewSphereFromMesh` takes vertex 0's distance to the position)
- [ ] Box colliders (only sphere and plane exist)
- [ ] Mark a mesh's collider type and static/dynamic in Blender, exported to the XML
- [ ] Physics bodies drive the mesh's model matrix, rotation included
- [ ] Verlet distance constraints

### UI

- [ ] Fix Gutter on Overdrive: `main.go` still calls `App.Run` with a nil widget, so only the crosshair draws

### Shadows

- [ ] **Sun shadows that follow the camera**: the sun's tile covers a fixed `Ortho(-10, 10, -10, 10)` box around the origin. Fit it to the view frustum, then cascades if one tile is not sharp enough
- [ ] A moving light dirties its own tiles (nothing moves a light yet; a game will)
- [ ] Slope-scaled depth bias, for walls and floors that must cast (needs `vkCmdSetDepthBias` in `go-vulkan`)

### Image quality

- [x] **Mipmaps**: `ImageSpec.MipLevels` and `ImageData.Mip`, the chain built on the CPU (`scene/image.go` `mipChain`) rather than by blits, so `go-vulkan` needed no `CmdBlitImage`
  - [ ] Filter albedo in linear space (`FormatRGBA8Srgb` + drop the shader's `pow 2.2`): the box filter averages gamma bytes today
  - [ ] A deferred (mid-frame) `UpdateImage` keeps one pending copy per image, so two mips written in one frame lose the first
- [ ] Alpha cutout (foliage, fences): stays in the prepass, but `prepass.slang` must `discard` exactly like `forward.slang` or `EQUAL` speckles

### Housekeeping

- [ ] Swapchain-acquire write-after-read under `validate_sync`, 10 reports a run before and after AO. Likely cause, unverified: the transition out of `useNone` has no source stage, so it does not wait on the stage the acquire semaphore is waited at (`vulkan/frame.go`, the swapchain image's first `recordUseImage`)
- [ ] `useColorAttachment` declares write access only: a colour attachment that loads (no `Clear`) or blends also reads, as `useDepthAttachment` now declares. The AO passes clear to avoid it

- [ ] A first test: `shadowAtlas.allocate` is pure CPU logic over `[]Light`
- [ ] Rename `[shadows] bakeBudgetMiB`: it counts texels in units of 2^20, not memory
- [ ] Remove the shadow sampler's white border (`scene/shadowatlas.go`): `shadowLookup` returns before sampling outside a tile, so it is never reached

---

## Later — cool, not needed now

### Rendering

- [ ] Texture-driven PBR: metallic and AO maps (roughness is done, `map_Pr`)
- [x] Real IBL: prefiltered specular mips and irradiance, as equirect maps; baked by Filament's `cmgen` in the exporter since 2026-10-10, before that in Go (`scene/ibl.go`)
- [ ] **The HDRI's sun as a shadow-casting light.** The image-based light casts no shadow, so an HDRI sun lights the inside of every shadow. `<clamp>` was removed with the Go bake, so this now belongs in the exporter, before `cmgen` runs:
  - [ ] In `write_environment`, cap the image at a limit (about 20: outdoor skies stay below it; cedar_bridge peaks at 19.4 outside the sun) and sum `(L − limit)·dω` per channel over the texels above it, direction weighted by that energy
  - [ ] Run `cmgen` on the capped image, and write the removed energy as a sun `<light>`: `Color` = irradiance / its largest channel, `Intensity` = that channel, `Pos` = mesh bounds centre + 25 m along `Dir` (the shadow camera is `Ortho(±10, 1, 50)` from `Pos`)
  - [ ] Then lower or drop `environmentLightScale` (`scene/skybox.go`), which partly compensates for this
- [ ] **Material textures prepared by Filament's tools in the exporter** (in the release `install_cmgen.sh` downloads). Each writes one file per mip, so all of them first need `loadTexture` (`scene/mesh.go`) to load a precomputed chain instead of calling `createMipLevels`:
  - [ ] `roughness-prefilter`: raises roughness in the far mips where the normal map is bumpy, which stops highlights shimmering on distant normal-mapped surfaces; the reason to build the chain loading at all
  - [ ] `mipgen`: Lanczos filtering in linear space (replaces the gamma-space item under Image quality) and `--kernel=normals`, which renormalises normal-map mips that the box filter flattens today
  - [ ] `basisu`: KTX2 GPU-compressed textures, about 4× less VRAM and faster loads than 4K PNG/JPEG; needs a KTX2 loader and BC upload (`renderer.FormatBC7`/`FormatBC7Srgb` already map to Vulkan formats, but nothing creates one yet)
- [ ] HDR + tonemapping + bloom, built entirely outside `vulkan/` (the test of the interface)
- [ ] Post-process AA (FXAA/TAA), for specular and normal-map shimmer MSAA cannot fix
- [x] Ambient occlusion (SSAO), reading the prepass depth (`core/ao.go`)
- [ ] Half-resolution AO: two full-screen passes at full resolution today
- [ ] Blending / transparency: a pass after the opaque `EQUAL` batch, `CompareLess`, no depth write, sorted back to front; `Material.Alpha` has to reach `DrawUniforms`
- [ ] Clustered forward: removes the 64-light cap and the score's blindness to off-screen lights
- [ ] Instancing
- [ ] Ray-traced shadows through ray queries (acceleration-structure bindings in `go-vulkan` first)
- [ ] Ray marching for basic shapes and clouds
- [ ] Geometry shader for fur

### Backend

- [ ] Vulkan-native clip space: y-flipped `[0, 1]` projections, then delete `TO_VK_DEPTH`, the negative-height viewport and the atlas passes' `WindingClockwise`
- [ ] Reverse-Z, once the above lands
- [ ] Bind `ReloadPipelines` (shader hot reload) to a key or a file watcher
- [ ] Score physical devices instead of taking `devices[0]`
- [ ] Split `BufferCopyDst`'s second meaning ("the CPU reads this back") into a `Readback` flag
- [ ] Image regression test comparing two `-screenshot` runs automatically

### Engine

- [ ] Audio

### Shading experiments

- [ ] Glass shader (needs transparency)
- [ ] Watercolor shader: [reference](https://x.com/TheMirzaBeig/status/2016702324576579644), [Blender version](https://www.reddit.com/r/blender/comments/1hcfb8x/realtime_watercolor_shader_in_blender/)

### Procedural / world

- [ ] Wave Function Collapse
- [ ] Noise terrain
- [ ] Grass, bushes
- [ ] Isotropic remeshing
- [ ] Navmesh
- [ ] Bezier paths

---

## Dropped

- [-] GPU timestamp queries: built, then deleted unused; RenderDoc profiles per pass
- [-] Debug object names and the validation messenger: validation output comes from a `VK_LAYER_SETTINGS_PATH` file instead (`CLAUDE.md`)

## Reference

- [Cubemap from HDRI](https://matheowis.github.io/HDRI-to-CubeMap/)
- Blender → engine coordinates: `pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}`
- `GOPROXY=proxy.golang.org go list -m github.com/Zephyr75/gutter@v0.1.2`
