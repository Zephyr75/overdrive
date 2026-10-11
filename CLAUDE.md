# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Overdrive is a Go game engine whose graphics layer runs on **Vulkan 1.3**, behind
an abstraction (`renderer/`) that keeps every other package free of graphics
calls. It also carries an ECS, Verlet physics, a gutter-based UI overlay and a
Blender XML export plugin.

An OpenGL 4.1 backend existed until 2026-08-05 and was deleted; `notes/FEATURES.md` §9
is the roadmap for what the engine grows into next.

## Working mode

**Explain before you change code.** The owner is writing most of the code by hand
in order to understand it, so the explanation is usually the deliverable and the
diff is not.

When asked for a change:

1. Say what you would do — which files and functions, what the approach is, and
   _why that approach_ rather than an obvious alternative.
2. Flag anything that would break, anything non-obvious, and any ordering the
   change depends on.
3. Then stop, unless the change is trivial or mechanical (a rename, a typo, a
   comment, applying something already agreed in this conversation).

Assume they may implement it themselves from the explanation. Write the
explanation so that is possible: name the exact call sites, not "the backend".

This does not apply to reading, searching, running builds or tests, or answering
questions — do those freely. It applies to edits.

Two habits that fit this mode:

- **Verify rather than assert.** Read the code before describing it; run the
  thing before claiming it works. Several conclusions in this repo's history were
  wrong on first inspection and only caught by checking.
- **Report what you actually found**, including when it contradicts something
  said earlier in the conversation.

## Build & run

The Go module root is **`src/`** (module `github.com/Zephyr75/overdrive`), so `go` commands run from there. The working directory does not otherwise matter: every runtime file is resolved by the `paths` package against a discovered project root, so nothing in the tree may hold a relative path literal.

```sh
cd src
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh   # required; see note below
go build ./...
go run .             # reads configs/vulkan.toml

go run . -config configs/vulkan.toml     # the same, named explicitly
go run . -config configs/low.toml        # the low quality tier: 2048 atlas, no
                                         # dynamic atlas, cheap PCF, no MSAA
go run . -scene stress.xml               # 64 lights (MaxLights), all casting: the allocator scene
go run . -screenshot out.png             # write one PNG of frame 90, then quit

go vet ./...         # two known unsafe.Pointer reports, see below
```

**There are no tests.** `go test ./...` reports `[no test files]` for every
package — there is not one `_test.go` in the tree. So a change is verified by
`go build ./...`, `gofmt -l`, and **looking at the scene** — which
`-screenshot out.png` now makes possible without a compositor screen grab, since
it goes through `Frame.Copy` and `Backend.ReadBuffer`. The `init()` size
guards in `renderer/uniforms.go` fire when the engine *runs*, not when it builds.
`notes/TODO.md` carries the item, and nominates `shadowAtlas.allocate` as the
first thing worth a unit test — it is pure CPU logic over `[]Light` and needs no
GPU.

**Every runtime knob is in the TOML file, including the debug ones.** The shadow
system in particular is fully configured — `[shadows]` carries `atlasSize`, the
`slotDivisors`/`slotCounts` layout, `tierScores`, `dynamicAtlas`,
`bakeBudgetMiB`, `pcf` and the projection planes — and a bad value **rejects the
file** rather than being clamped, in `settings.checkShadowAtlas`. A layout that
cannot be carved is silent everywhere else: allocation refuses, every light gets
`ShadowIndex = -1`, and the scene renders unshadowed with nothing logged. There are
no environment variables — `OVERDRIVE_ROOT` is the single exception and cannot be
otherwise, since it is what locates the settings file. So what a run was
configured with is always readable from the file it was given.

`configs/vulkan.toml` `[debug]`, all off by default:

| key | what it does |
| --- | --- |
| `lockCamera` | freezes the camera where the scene put it, so a capture is reproducible |
| `noShadows` | lights everything unshadowed, tiles still baked. The A/B that separates "the scene is dark" from "every light is wrongly occluded" |
| `noPrepass` | skips the depth prepass and depth-tests the main pass with LESS. The A/B that checks `prepass.slang` and `forward.slang` agree to the bit |
| `validation` | Vulkan validation layers. The engine registers no debug messenger, so the layer needs somewhere to report — see the `vk_layer_settings.txt` recipe below |

**Validation output needs a layer settings file, not engine code.** The layer
reports through a `VkDebugUtilsMessengerEXT` or through its own logger, and the
engine registers no messenger. Point `VK_LAYER_SETTINGS_PATH` at a file holding

```
khronos_validation.debug_action = VK_DBG_LAYER_ACTION_LOG_MSG
khronos_validation.log_filename = stdout
khronos_validation.report_flags = error,warn,perf
```

and run with `[debug] validation = true`. Verified on 2026-09-05: it prints
`Validation Error: [ VUID-… ]` blocks on stdout with no code in the engine.

**Every named `Pass` and `Compute` is a `VK_EXT_debug_utils` label**, which is
what groups a RenderDoc capture into `shadowStatic` / `shadowDynamic` /
`depthPrepass` / `main` instead of a flat draw list. The extension is probed for
in `vulkan/backend.go:createInstance` and enabled only when the loader has it, so
a machine without the layers or RenderDoc still runs — every label call is then a
no-op. Object names are *not* set: images and buffers show as raw handles.

**Images are inspected with `-screenshot` or in RenderDoc.** `-screenshot
out.png` copies the swapchain image into a host buffer and writes a PNG of frame
90 — late enough for the physics and the shadow allocator to settle, so two runs
photograph the same scene and can be diffed. RenderDoc is still what shows the
shadow atlases and the per-pass state; `lockCamera = true` is what makes two
captures comparable.

`go vet ./...` reports two pre-existing `possible misuse of unsafe.Pointer` in
`vulkan/backend.go`; they are the device-address arithmetic and are not new.

To validate the generated SPIR-V, pass the layout flag — plain `spirv-val` is
wrong here:

```sh
for f in shaders/vk/*.spv; do spirv-val --scalar-block-layout "$f"; done
```

`slangc` comes from the AUR `shader-slang-bin` package, which installs to `/opt/shader-slang-bin/bin/slangc` and **does not put it on PATH** — so `build_shaders.sh` needs `SLANGC=/opt/shader-slang-bin/bin/slangc` unless that directory has been added to PATH. (Arch's `slang` package is the unrelated S-Lang library.) `src/shaders/vk/` is git-ignored, so a fresh clone builds and tests fine but cannot run until the script has been run once.

The backend links against the `vk` package in the sibling repo `../../go-vulkan` (a `replace` directive in `go.mod`, resolving to `/home/zeph/GitHub/go-vulkan`). `go-vulkan/BINDINGS_OVERDRIVE_GUTTER.md` lists the bindings Overdrive and gutter use beyond the tutorial, with which of the two calls each, and what is still to be added (ray queries); `go-vulkan/BINDINGS_HOWTO.md` lists the core set the tutorial port also uses.

## Architecture

```
main.go            builds an App, loads a Scene, builds an ECS World
core/              NewApp (window + backend), App.Run (the frame loop), the UI overlay
scene/ ecs/        meshes, lights, camera, skybox, materials, pipelines, physics entities
input/ physics/    — plain Go, zero graphics calls
renderer/          the abstraction: Backend/Frame/Pass/Compute, opaque handles, the uniform blocks
vulkan/            the only package that may import vk.*
```

`vulkan/` is organised by concept: `backend.go` (device and lifetime),
`frame.go` (frames, passes, dispatches, the arena), `barrier.go` (the resource-use
table), `slots.go` (descriptors and the retire queue), `pipeline.go`, `image.go`,
`buffer.go`, `convert.go`, `swapchain.go`.

Four invariants hold the engine together. Breaking any of them is how it goes wrong:

1. **Nothing above `renderer/` imports a graphics API.** Scene/core/ecs/input/physics own opaque handles (`renderer.MeshHandle`, `ImageHandle`, `PipelineHandle`, …) that the backend interprets in its own table. This is the rule that keeps everything above `renderer/` buildable and testable without a GPU, and it is why the abstraction is kept despite there being one backend.

2. **Clears and viewports exist only inside `Frame.Pass`** — or are narrowed by `Pass.Viewport` within a pass on an atlas target, which is the one amendment. An `Attachment` clears when its `Clear` field is non-nil and loads what the target already holds otherwise, which is how the main pass keeps the depth the prepass stored and how the dynamic atlas pass adds to the tiles just copied into it. Never add a free-floating clear to scene or core code.

   The nesting is the ordering rule: a `Pass` value cannot exist outside `Frame.Pass`, so a copy or a dispatch inside a render pass is a compile error rather than a runtime guard. The eleven `frameActive`/`passActive` checks the old interface needed are gone, and so is every barrier call site — `vulkan/barrier.go` tracks one *use* per image and per buffer, and each operation declares what it is about to do (`PassSpec.Reads`, `ComputeSpec.Reads`/`Writes`, the attachments themselves).

3. **Uniforms are opaque bytes to the backend, and typed structs to everyone else.**
   `Frame.Upload(data any) Address` memcpys a block into a per-frame arena and
   returns its device address; `DrawCall.Push [4]Address` carries four of those,
   positionally, in one 32-byte push constant. The backend never reads a field.
   `renderer/uniforms.go` declares the blocks — `FrameUniforms` (4776 bytes,
   camera and lights, once per pass), `BakeUniforms` (80, the one tile a depth
   pass is baking, once per tile), `ShadowTile` (96, a variable-length array once
   per frame) and `DrawUniforms` (104, once per draw) — and
   `shaders/slang/common.slang` names which push slot is which.

   That works because Slang compiles with `-fvk-use-scalar-layout` and scalar layout is exactly Go's packing for `float32`/`int32` structs — so **keeping the field order in step is the whole requirement**. No 16-byte cells, no `vec3`-plus-scalar pairing, no vectors standing in for scalar arrays; that was std140's rule and it went with the OpenGL backend on 2026-08-05. Use only `float32`, `int32`, arrays of those, and `mgl32` matrices — anything with a wider alignment breaks the correspondence.

   The guard is an `init()` size panic in `renderer/uniforms.go`. It catches a member added, removed or resized — it cannot catch two members swapped, which leaves the size identical and renders silent garbage. **After editing `common.slang`, rebuild shaders and look at the scene.** To check a layout by hand, `spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate` prints the offsets the compiler actually emitted.

   The arena is frame-scoped and **panics on overflow** rather than wrapping to
   offset 0, which used to make an overflowed frame wrong silently.

   `ScalarBlockLayout` is load-bearing: `LightData` is 72 bytes, so `lights[]` has a non-16-aligned stride that the standard layout rules reject. `spirv-val` fails on these modules unless given `--scalar-block-layout`.

4. **Shaders are authored once in Slang** (`src/shaders/slang/`) and compiled to SPIR-V. The backend does not read `.slang` at runtime, so `build_shaders.sh` must run before the first build and after every shader edit.

Frame shape (`core/App.Run`): physics + mesh re-upload → input → `Backend.Frame` opens → tile allocation, dirty tracking and records → one `Upload` of `FrameUniforms` and one of the tile array → the static atlas pass when allocation moved → a `Frame.Copy` per dirty dynamic tile → the dynamic atlas pass (both depth-only, one viewport per tile) → depth prepass on the backbuffer (depth attachment only, no colour) → main backbuffer pass, keeping that depth (skybox with LEQUAL, scene forward with **EQUAL**, UI fullscreen quad) → the closure returns and the frame is submitted. The prepass and the forward pass are handed the *same uploaded address*, so they read the same bytes rather than two rebuilt copies. The prepass is always on; `[debug] noPrepass = true` turns it off, which is the A/B that checks the two passes agree — **`prepass.slang` must combine `projection`, `view` and `model` in exactly the order `forward.slang`'s `vsMain` does**, since EQUAL rejects a difference in the last bit and shows it as speckle rather than as an error. **A settled scene runs neither bake pass**, which is what the `bakes:` counter beside the FPS is for.

**Every shadow in the scene is a sub-rect of a 4096² depth texture** — of two of them, since Part E: `staticAtlas` holds the casters that cannot move and is re-baked only when allocation moves, `dynamicAtlas` is a `Frame.Copy` of that plus the movable casters drawn on top, and `ShadowTile.Flags` bit 0 is which one a light samples. The two are the only images created with `ImageSpec.Hot`, which puts them in the four dedicated descriptors `forward.slang` indexes by a literal — a measured 1.7x win over a dynamically indexed bindless descriptor, and the reason binding 3 exists at all. A mesh is movable when the scene says `<movable>` or `MoveBy`/`MoveTo` has been called on it, and a light takes a dynamic tile only while a movable caster is inside its radius. The static side is all-or-nothing (a tile whose frustum holds no caster writes nothing, so baking only the changed slots would leave one wearing its last owner's depth); the dynamic side is per-tile and capped at `settings.ShadowBakeBudget()` a frame (`[shadows] bakeBudgetMiB`), spent in score order, a light that misses out keeping the tile it has. Both atlases carve the same `slotLayout`, so a tile's rect is identical in the two and the copy needs no remap.

Below that split the tiling is unchanged — a sun or spot takes one tile, a point light six 90° tiles rather than a cubemap — so the pass count does not grow with the light count. Each tile is described by a `ShadowTile` whose `WorldToTile` matrix both bakes it and samples it. Two rules are silent corruption if broken: **clamp every PCF tap to the tile inset by one texel** (an atlas has no border; the neighbour is another light's shadow), and **widen each cube face to `90° + 2 texels`** (no cross-face filtering, so the kernel must stay inside its own tile). The atlas is carved into a **fixed slot layout** at load (`slotLayout` / `buildLayout` in `scene/shadowatlas.go`) — so many slots at 2048, 512, 256 and 128 — and those rects never move again, which is what lets Part E cache a baked tile. Every size is a division of `atlasSize` rather than a pixel count, so the atlas size buys sharpness and the slot counts buy light budget: two separate knobs, both `[shadows]` keys.
A smaller atlas also scales `forward.slang`'s world-space normal-offset bias
through `FrameUniforms.ShadowNormalScale` (`4096 / atlasSize`, so 1.0 at the
default) — without it, halving the atlas doubles a texel's world footprint and
re-introduces the acne those constants were tuned to hide. Slots are typeless, only size matters, because a point light's six faces each carry their own rect and are never filtered across, so they need not be adjacent. Who occupies a slot is decided **per frame**: every light scores `radius / distance to camera`, lights sort by score, and each takes the best free slot no larger than the ceiling its score earns (512 / 256 / 128, a sun capped at 2048; a point light's six faces each take a slot of that size, not a smaller one — the pool bounds it, so no extra rationing is needed). **Rank picks the slot, the tier caps it** — the ceiling stops an unimportant light claiming a slot it would waste, and phase 1b then re-offers whatever is spare to whoever ended up under their ceiling, so an idle slot size is never wasted on a scene whose light mix differs from the layout's. Running out of slots costs the least important light its resolution and never costs frame time — it walks down a pool at a time and finally leaves `ShadowIndex = -1`, which lights it unshadowed. Two hysteresis margins guard two different axes: `nextTierThreshold` (20%) keeps a boundary score from changing a light's ceiling every frame, and `slotStickiness` (20%) keeps two near-equal lights from trading a contended pool's last slot every frame — a failure fixed pools have and a splitting tree does not.

Conventions that would silently produce a mirrored or inside-out image: the screen passes set `PassSpec.FlipY`, giving a **negative-height viewport** so clip space comes out y-up, which is what the projection matrices in `scene/` assume; that also flips winding, so their pipelines declare `WindingCCW`. The atlas passes leave `FlipY` false and their pipelines declare `WindingCW`. Projections are the OpenGL convention (clip z in `[-w, w]`), so every vertex stage calls `TO_VK_DEPTH`. `notes/OVERVIEW.md` §5 is the full list, §6 a symptom→file table.

## Documentation map

- `notes/OVERVIEW.md` — **start here.** Layers, the code map, startup, one frame pass by pass, the conventions that silently break the image (§5) and a symptom→file table (§6).
- `notes/RENDERER.md` — **read before touching the renderer.** The 25-method `Backend`/`Frame`/`Pass`/`Compute` contract, how uniforms and textures reach a shader, the backbuffer split between the swapchain and `core/targets.go`, pipelines and buffers, and the Vulkan ownership tree with its lifetime classes.
- `notes/SYNCHRONIZATION.md` — recording vs executing, frames in flight, fences and semaphores, the barrier `use` table, and when a resource may be destroyed.
- `notes/FEATURES.md` — every feature and _why it is built that way_ (lighting, the shadow atlas and its allocator, bias, PCF, prepass, MSAA, UI), the scene format and Blender add-on, the config tiers, the roadmap and known gaps (§9), and the performance history.
- `notes/TODO.md` — the working list.
- `notes/SHOWCASE_PBR.md` — how the showcase scene got PBR, mipmaps and HDR environment lighting: the scene-folder layout, `<environment>`, the CPU IBL bake, and what the exporter does now.
- `notes/cheatsheets/` — reference, all opening with a Scope / Not here / Source block: `GLOSSARY.md` (one-line definitions), `DESCRIPTORS.md` (the engine's descriptor set in depth), and engine-independent `VULKAN.md`, `OPENGL.md` (API reference, kept deliberately), `PBR.md`, `RAYTRACING.md`, `GRAPHICS.md` (techniques, procedural generation, physics, AI, compression, optimisation, GPGPU, emulation), `ALGEBRA.md`.

## Conventions

Comments here are one-line, sentence-case, no trailing period, placed above the declaration and explaining _why_ or _what invariant_, not what the code literally does. Match that density — it is deliberate and consistent across the tree.

Scenes are XML in a folder of their own (`assets/showcase/showcase.xml`, the default `-scene`), referencing OBJ/MTL in `meshes/` and textures and the `.hdr` environment in `textures/` beside the XML, produced by the Blender add-on `xml_export.py` at the repository root. The engine decodes PNG, JPEG and Radiance `.hdr` only; the exporter converts anything else. Static mesh geometry is baked into the OBJ vertices (identity model matrix), so `<position>` is unused for them.
