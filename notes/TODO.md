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

- [ ] **A moved light keeps its old shadow.** `Light.Move` (`scene/light.go`) only changes `Pos`: nothing dirties its static or dynamic tiles, so the shadow stays where it was baked. Track a `prevPos` like `Mesh.prevCenter`, and have a moved light force its tiles (static side: `runStatic`) the way a moved caster does
- [ ] **Sun shadows only cover 20 m around the origin**: the tile is a fixed `Ortho(-10, 10, -10, 10)` (`scene/shadowatlas.go`, the sun branch of the tile build), so any level larger than breakout's loses sun shadows at its edges. Fit the box to the view frustum, snapped to whole texels so it does not shimmer; cascades only if one tile is not sharp enough
- [ ] **`NewSphereFromMesh` takes its radius from vertex 0** (`physics/sphere.go`): right only for a UV sphere whose first vertex is on the surface. Take it from the mesh's bounds (`boundsRadius`, or half the largest extent)
- [ ] **No way to hide a mesh**: breakout `Place`s broken bricks to y = −1000. A `Mesh.Visible` checked by `RenderScene`, `RenderDepth` and the bake loops, with a toggle dirtying the tiles it was in like a move
- [ ] **A fixed body that the game moves gives what it hits no speed** (a paddle, a door, a lift): `Teleport` keeps `Pos − PrevPos`, but `inverseMass` is 0 and the collision response ignores the fixed side's motion. Make a fixed body's velocity count in `resolve`, i.e. a kinematic body
- [ ] Swapchain-acquire write-after-read under `validate_sync`, 10 reports a run. Likely cause, unverified: the transition out of `useNone` has no source stage, so it does not wait on the stage the acquire semaphore is waited at (`vulkan/frame.go`, the swapchain image's first `recordUseImage`)
- [ ] **Phase 1b ignores the ceiling**: `offerSpareSlots` gives a light under its ceiling the largest pool with enough free slots, with no upper bound, so a light whose score earns 128 can end on a 512 slot (and a spot on the sun's 2048 when there is no sun). `FEATURES.md` says spares go to lights "under their ceiling", which reads as *up to* it. Decide which is meant: cap the scan at `sizeWanted`, or document that spares may exceed it. `TestAllocateRespectsCeiling` is skipped until then
- [ ] `useColorAttachment` declares write access only: a colour attachment that loads (no `Clear`) or blends also reads, as `useDepthAttachment` declares. Nothing hits it today (the AO passes clear to avoid it), but transparency will

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

- [ ] **Alpha cutout** (foliage, fences, grates): stays in the prepass, but `prepass.slang` must `discard` exactly like `forward.slang` or `EQUAL` speckles
- [ ] **Transparency** (glass, water, fading UI-in-world, particles): a pass after the opaque `EQUAL` batch, `CompareLess`, no depth write, sorted back to front; `Material.Alpha` has to reach `DrawUniforms`. Needs the `useColorAttachment` fix in section 1
- [ ] **HDR + tonemapping**: the forward pass writes straight to the 8-bit swapchain, so bright lights clip. The one post effect worth doing now; bloom on top is cheap once it exists, and it is all outside `vulkan/` (the test of the interface)
- [ ] Filter albedo in linear space (`FormatRGBA8Srgb` + drop the shader's `pow 2.2`): the box filter averages gamma bytes today, which darkens distant textures
- [ ] A deferred (mid-frame) `UpdateImage` keeps one pending copy per image, so two mips written in one frame lose the first
- [ ] Slope-scaled depth bias, for walls and floors that must cast (needs `vkCmdSetDepthBias` in `go-vulkan`)

### 6. Housekeeping

- [~] **`src/main`, a 6 MB binary, was committed in `54cfb0c`**: `src/main` and `src/breakout/breakout` are now in `.gitignore`, but the file stays tracked until `git rm --cached src/main`
- [x] `gofmt -w .`: 33 files, mostly a trailing space after `{`. Keep `gofmt -l .` at zero
- [x] First tests, no GPU needed: `scene/shadowatlas_test.go` (layout packing at both tiers, no shared slot, a sun on the largest slot, slots stable across frames, rank order) and `physics/world_test.go` (box fitting, a ball settling on a box, an elastic bounce, a head-on exchange, `Remove`)
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
  - [ ] Run `cmgen` on the capped image, and write the removed energy as a sun `[[light]]`: `Color` = irradiance / its largest channel, `Intensity` = that channel, `Pos` = mesh bounds centre + 25 m along `Dir` (the shadow camera is `Ortho(±10, 1, 50)` from `Pos`)
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
