# TODO — the working list

> **Scope** small, concrete open items, split by priority. Anything needing a paragraph of reasoning lives in `FEATURES.md` §9.
>
> `[ ]` open · `[~]` started · `[-]` dropped on purpose

---

## Now — a stable, easy engine for small games

The goal: build small games quickly on proper shadows, clean image quality, basic PBR shading, a working UI and base physics, with objects placed and moved from Blender. Breakout (`src/breakout/`) is the yardstick: everything its README lists under "Engine gaps it works around" belongs here.

The sections are in priority order, and so are the items inside each.

### 1. Silent wrongness a game will hit

Each of these renders or simulates something wrong with nothing logged, which is the opposite of stable.

- [x] **A moved light re-bakes its shadow**: each allocation remembers the `shadowPose` (position, direction, cone; `scene/light.go`) its tiles were baked from, and `markDirtyTiles` invalidates them when it changes, whether the game called `Light.Move` or set `Pos`/`Dir` directly. The static atlas is all-or-nothing, so **a light that moves every frame re-bakes the whole static atlas every frame**: fine for a few lights, and the reason a "movable light" path (all its casters in the dynamic tile) is the next step if it shows up in frame time
- [x] **The sun's shadow box follows the camera** (`Light.followCamera`): centred on the camera in steps of half the box, re-centred only once the camera is ¾ of a step away, so it moves (and re-bakes the static atlas) a few times per 20 m walked rather than every frame. Its size is `[shadows] sunHalfExtent` (10 m, as before); the sun's `position` no longer matters. Cascades only if one tile is not sharp enough
- [x] **`NewSphereFromMesh` takes half the largest side of `Mesh.Bounds()`** rather than vertex 0's distance. Same radius for the football; the showcase's Suzanne.001 goes from 0.108 to 0.159 (ear to ear), so it rests about 5 cm higher
- [x] **`Mesh.SetVisible`**: a hidden mesh is skipped by every draw and every bake. Toggling dirties its dynamic tiles like a move, and re-bakes the static atlas for a static mesh. Breakout uses it for broken bricks
- [x] **A static mesh made movable re-bakes the static atlas**: `Mesh.Place` on a mesh the scene did not mark `movable` used to leave its old shadow baked where it loaded
- [x] **Driven fixed bodies push** (`physics/verlet.go` `Fixed`): set `Pos` before a step and the distance moved is the speed it hits with, the collision worked out relative to it; `World.Step` zeroes it afterwards. `Teleport` still moves one without speed. The world now also places a fixed body's mesh when it moves (`Verlet.attach` keeps the centre-to-pivot offset)
- [x] Swapchain-acquire write-after-read under `validate_sync`: the guessed cause was right. A just-acquired image is now `useAcquired` (`vulkan/barrier.go`), whose stage is the one the acquire semaphore is waited at. 10 reports a run → 0
- [-] **Phase 1b and the ceiling**: not a bug. Phase 1 already gives each light the largest free pool within its ceiling and free counts only fall, so a capped phase 1b could never do anything: exceeding the ceiling with leftovers is its whole job. `FEATURES.md` now says so
- [x] `useColorAttachment` declares read as well as write access, as `useDepthAttachment` does: a loaded or blended attachment reads. Pixel-identical, and no validation report

### 2. Transforms and Blender export

Today the OBJ is exported in world space, `rotation` is written but ignored (`MeshToml.Rotation`), scale is not exported, and `Mesh.Place` turns the world-space vertices about `initialPosition`. This is the biggest ease-of-use item: it is what makes "place it in Blender, move it in Go" work for any object, rather than only for ones built about the origin like breakout's.

- [ ] Export each OBJ in **local space** (no object transform applied) and write position, rotation and scale to the scene file
- [~] A model matrix from position/rotation/scale in `DrawUniforms.Model`: position and rotation done (`Mesh.Place`), scale and reading the exported transform at load still to come; then `initialPosition` goes
- [x] Moves update the matrix only, no vertex re-upload (`Mesh.Place`)
- [~] Shadows with the transformed bounds: `boundsCenter` already follows `Place`, so culling and dirtying are right under move and rotation. `boundsRadius` must scale with the largest scale axis once scale exists
- [ ] Check the normal matrix (`inverse3` in `surface.slang`'s `vsMain`) under rotation and non-uniform scale
- [ ] Export `movable` from Blender (a custom property): the exporter writes only `castsShadow`, which is why breakout's TOML is written by hand
- [ ] Re-export `showcase.toml` and `stress.toml`, and compare with `-screenshot` before and after

### 3. Physics a game can rely on

- [x] Physics bodies drive the mesh's model matrix, rotation included (`World.PlaceAndRotateMeshes`)
- [x] Contacts reported to the game (`World.Contacts`, `ContactsOf`), bodies removable (`World.Remove`), velocity in m/s (`Velocity`, `SetVelocity`, `Teleport`), step rate on the world (`World.Rate`)
- [x] Box colliders, oriented and fitted from face normals (`NewBoxFromMesh`)
  - [ ] **Only fixed**: `Box.Collide` does nothing, so only a sphere responds to contacts. A crate that falls needs box–box and box–plane, and a box that can rotate needs an inertia tensor
- [ ] Colliders from the mesh's transform once transforms land (section 2): `NewBoxFromMesh` reads raw vertices, which is only right while the OBJ is in world space
- [ ] Mark a mesh's collider type and fixed/dynamic in Blender, exported to the scene file, so `createWorld`-style code becomes one call over the scene
- [ ] Simple queries a game asks constantly: a ray cast (what is under the cursor, line of sight, is the player grounded) and an overlap test (trigger volumes)
- [ ] Verlet distance constraints

### 4. Game-facing API

What breakout had to work around or write itself. Small changes each, but they are what "easy to use" means day to day.

- [ ] **Spawn and clone meshes at runtime**: every brick is its own `[[mesh]]` loading the same OBJ. A `Scene.Clone(mesh)` sharing the vertex buffer and textures, and an `Add`/`Remove` that the shadow allocator's per-mesh state follows
- [ ] **Input with edges**: keys and buttons read as "pressed this frame" / "released this frame", rather than every game keeping its own `launchHeld`. One small `input` state polled once a frame
- [ ] **`NewApp` options**: positional `nil` callbacks, a default scroll callback the game has to unset (`input.ScrollCallback`), a cursor callback skipped under `lockCamera`. An options struct (or setters) with no hidden defaults beyond the fly camera
- [ ] **Textures resolve only beside their scene** (`scene/mesh.go:texturePath`), hence breakout's symlink. Allow a shared asset folder, or a path relative to the MTL
- [ ] Audio: play a sound, loop music, set volume. Was under Later, but a small game feels broken without it; any plain-Go player (`oto`/`beep`) keeps it out of `renderer/`
- [ ] Loading errors as errors: a missing OBJ/MTL/texture should name the file and the scene entry, not panic through `utils.HandleError`

### 5. Visuals that add a lot

Not eye candy: these change what a game can show at all, or fix something visibly wrong.

- [x] **Alpha, one rule for every material**: a fragment's alpha is MTL `d` × its `map_d` texel; at or above `OPAQUE_ALPHA` (0.99, `scene.OpaqueAlpha` on the CPU) it is opaque, below it blended, at 0 absent. `Material.opaquePart`/`translucentPart` decide which passes draw a group at all: the opaque ones and the bakes keep the opaque fragments through `clipAlpha` in the `*_cutout.slang` twins (plain groups stay on the discard-free pipelines and keep early-Z), and `Scene.RenderTransparent` draws the rest after the opaque meshes with `transparent.slang`, `CompareLess`, no depth write, sorted far to near per mesh. An alpha-mapped foliage material is drawn by both, its solid texels writing depth and casting shadows and its fringe blending. Alpha maps get coverage-preserving mips (`preserveCoverage` in `scene/image.go`), so a distant canopy keeps its opaque share instead of averaging into the transparent pass. Blender's render method is no longer exported. Blender's OBJ exporter writes no maps for a material built inside a node group (Poly Haven foliage), so `xml_export.py:warn_grouped_materials` prints a warning to ungroup it. Still open: leaves are culled from behind (no double-sided pipeline), per-mesh sorting (interpenetrating meshes, and a mesh's own translucent fringes), blending after tonemapping until HDR lands, no runtime `Mesh.SetAlpha` for fades, no shadow from translucent fragments
- [ ] **HDR + tonemapping**: the forward pass writes straight to the 8-bit swapchain, so bright lights clip. The one post effect worth doing now; bloom on top is cheap once it exists, and it is all outside `vulkan/` (the test of the interface)
- [ ] Filter albedo in linear space (`FormatRGBA8Srgb` + drop the shader's `pow 2.2`): the box filter averages gamma bytes today, which darkens distant textures
- [ ] A deferred (mid-frame) `UpdateImage` keeps one pending copy per image, so two mips written in one frame lose the first
- [ ] Slope-scaled depth bias, for walls and floors that must cast (needs `vkCmdSetDepthBias` in `go-vulkan`)

### 6. Housekeeping

- [~] **`src/main`, a 6 MB binary, was committed in `54cfb0c`**: `src/main` and `src/breakout/breakout` are now in `.gitignore`, but the file stays tracked until `git rm --cached src/main`
- [x] `gofmt -w .`: 33 files, mostly a trailing space after `{`. Keep `gofmt -l .` at zero
- [ ] First tests: `shadowAtlas.allocateAtlas` is pure CPU logic over `[]Light` (layout packing, no shared slot, slots stable across frames, rank order); then physics (a ball settling on a box, an elastic bounce, a driven paddle's push)
- [x] Renamed `[shadows] bakeBudgetMiB` to `bakeBudgetMegatexels`: it counts texels in units of 2^20, not memory
- [x] Removed the shadow sampler's white border: clamp to edge instead, since `shadowLookup` returns lit before sampling outside a tile. Locked-camera screenshots are pixel-identical before and after
- [ ] `movableCasterInRange` carries `// TODO check if moving lights are supported`: answered by the moved-light item in section 1, delete it with that fix

---

## Later — cool, not needed now

### Rendering

- [ ] Texture-driven PBR: an AO map, for crevice detail finer than SSAO (roughness `map_Pr` and metallic `map_Pm` are done)
- [x] Real IBL: prefiltered specular mips and irradiance, as equirect maps; baked by Filament's `cmgen` in the exporter since 2026-10-10, before that in Go (`scene/ibl.go`)
- [ ] **The HDRI's sun as a shadow-casting light.** The image-based light casts no shadow, so an HDRI sun lights the inside of every shadow. `<clamp>` was removed with the Go bake, so this now belongs in the exporter, before `cmgen` runs:
  - [ ] In `write_environment`, cap the image at a limit (about 20: outdoor skies stay below it; cedar_bridge peaks at 19.4 outside the sun) and sum `(L − limit)·dω` per channel over the texels above it, direction weighted by that energy
  - [ ] Run `cmgen` on the capped image, and write the removed energy as a sun `[[light]]`: `Color` = irradiance / its largest channel, `Intensity` = that channel, `Dir` only: the shadow box follows the camera (`Light.followCamera`) and ignores `Pos`
  - [ ] Then lower or drop `environmentLightScale` (`scene/skybox.go`), which partly compensates for this
- [ ] **Material textures prepared by Filament's tools in the exporter** (in the release `install_cmgen.sh` downloads). Each writes one file per mip, so all of them first need `loadTexture` (`scene/mesh.go`) to load a precomputed chain instead of calling `createMipLevels`:
  - [ ] `roughness-prefilter`: raises roughness in the far mips where the normal map is bumpy, which stops highlights shimmering on distant normal-mapped surfaces; the reason to build the chain loading at all
  - [ ] `mipgen`: Lanczos filtering in linear space (replaces the gamma-space item in Now, section 5) and `--kernel=normals`, which renormalises normal-map mips that the box filter flattens today
  - [ ] `basisu`: KTX2 GPU-compressed textures, about 4× less VRAM and faster loads than 4K PNG/JPEG; needs a KTX2 loader and BC upload (`renderer.FormatBC7`/`FormatBC7Srgb` already map to Vulkan formats, but nothing creates one yet)
- [ ] Bloom, once HDR + tonemapping (Now, section 5) exists
- [ ] Post-process AA (FXAA/TAA), for specular and normal-map shimmer MSAA cannot fix
- [x] Ambient occlusion (SSAO), reading the prepass depth (`core/ao.go`)
- [ ] Half-resolution AO: two full-screen passes at full resolution today
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
