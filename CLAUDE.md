# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Overdrive is a Go game engine whose graphics layer runs on **Vulkan 1.3**, behind
an abstraction (`renderer/`) that keeps every other package free of graphics
calls. It also carries an ECS, Verlet physics, a gutter-based UI overlay and a
Blender XML export plugin.

An OpenGL 4.1 backend existed until 2026-08-05 and was deleted; `notes/tmp/BACKEND_DECISION.md`
records why, and is the roadmap for what the abstraction grows into next.

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
go test ./...        # uniform layout + showcase-scene checks; no GPU needed
go run .             # reads configs/vulkan.toml

go run . -config configs/vulkan.toml     # the same, named explicitly
go run . -config configs/low.toml        # the low quality tier: 2048 atlas, no
                                         # dynamic atlas, cheap PCF, no MSAA
go run . -scene stress.xml               # 64 lights (MaxLights), all casting: the allocator scene

go test ./scene/ -run TestShowcaseLoads   # single test
```

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
| `validation` | Vulkan validation layers |

**Images are inspected in RenderDoc, not by the engine.** There is no readback
path and no PNG dump: capture a frame and read the swapchain image and the
shadow atlas out of the capture. `lockCamera = true` is what makes two captures
comparable.

`go vet ./...` reports two pre-existing `possible misuse of unsafe.Pointer` in
`vulkan/backend.go`; they are the device-address arithmetic and are not new.

To validate the generated SPIR-V, pass the layout flag — plain `spirv-val` is
wrong here:

```sh
for f in shaders/vk/*.spv; do spirv-val --scalar-block-layout "$f"; done
```

Stale paths to ignore: the two top-level scripts (`overdrive.sh`, `overdrive_build.sh`) are still cmake wrappers naming `go/` or `cpp/`. The C++ tree was deleted; the Go tree moved to `src/`. Fix references as you touch them rather than following them.

`slangc` comes from the AUR `shader-slang-bin` package, which installs to `/opt/shader-slang-bin/bin/slangc` and **does not put it on PATH** — so `build_shaders.sh` needs `SLANGC=/opt/shader-slang-bin/bin/slangc` unless that directory has been added to PATH. (Arch's `slang` package is the unrelated S-Lang library.) `src/shaders/vk/` is git-ignored, so a fresh clone builds and tests fine but cannot run until the script has been run once.

The backend links against the `vk` package in the sibling repo `../../go-vulkan` (a `replace` directive in `go.mod`, resolving to `/home/zeph/GitHub/go-vulkan`). `go-vulkan/BINDINGS_GAP.md` inventories what those bindings cover and what has to be added for compute, storage images, HDR and ray tracing.

## Architecture

```
main.go            builds an App, loads a Scene, builds an ECS World
core/              NewApp (window + backend), App.Run (the frame loop), renderUI
scene/ ecs/        meshes, lights, camera, skybox, materials, physics entities
input/ physics/    — plain Go, zero graphics calls
renderer/          the abstraction: Backend interface, opaque handles, the three uniform structs
vulkan/            the only package that may import vk.*
```

Four invariants hold the engine together. Breaking any of them is how it goes wrong:

1. **Nothing above `renderer/` imports a graphics API.** Scene/core/ecs/input/physics own opaque handles (`renderer.MeshHandle`, `TextureHandle`, …) that the backend interprets in its own table. This is the rule that keeps `go test ./...` runnable without a GPU, and it is why the abstraction is kept despite there being one backend.

2. **Clears and viewports exist only inside `Backend.BeginPass`** — or are narrowed by `SetViewportScissor` within a pass on an atlas target, which is the one amendment. `BeginPass`'s `keepDepth` argument suppresses the depth clear so a pass can add to what a target already holds; it works on offscreen depth targets only, the backbuffer's depth being discarded every frame. Never add a free-floating clear to scene or core code.

3. **Uniforms are three typed structs, split by update frequency.** `renderer.FrameUniforms` (4848 bytes, camera/lights/the tile being baked) is published once per pass by `BindFrameUniforms`; `renderer.DrawUniforms` (128 bytes, model matrix + material) goes out per draw; `renderer.ShadowRecord` (96 bytes, one shadow tile) is a variable-length **array** published once per frame by `BindShadowRecords`, its length being a property of the scene. All three mirror `shaders/slang/common.slang` field for field, and the backend **uploads them by memcpy** into a ring buffer plus three pushed device addresses.

   That works because Slang compiles with `-fvk-use-scalar-layout` and scalar layout is exactly Go's packing for `float32`/`int32` structs — so **keeping the field order in step is the whole requirement**. No 16-byte cells, no `vec3`-plus-scalar pairing, no vectors standing in for scalar arrays; that was std140's rule and it went with the OpenGL backend on 2026-08-05. Use only `float32`, `int32`, arrays of those, and `mgl32` matrices — anything with a wider alignment breaks the correspondence.

   The guard is an `init()` size panic in `renderer/uniforms.go`. It catches a member added, removed or resized — it cannot catch two members swapped, which leaves the size identical and renders silent garbage. **After editing `common.slang`, rebuild shaders and look at the scene.** To check a layout by hand, `spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate` prints the offsets the compiler actually emitted.

   `ScalarBlockLayout` is load-bearing: `LightData` is 72 bytes, so `lights[]` has a non-16-aligned stride that the standard layout rules reject. `spirv-val` fails on these modules unless given `--scalar-block-layout`.

4. **Shaders are authored once in Slang** (`src/shaders/slang/`) and compiled to SPIR-V. The backend does not read `.slang` at runtime, so `build_shaders.sh` must run before the first build and after every shader edit.

Frame shape (`core/App.Run`): physics + mesh re-upload → input → `BeginFrame` → tile allocation, dirty tracking and records → the static atlas pass when allocation moved → a `CopyDepthRegion` per dirty dynamic tile → the dynamic atlas pass (both depth-only, no color clear, one viewport per tile) → depth prepass on the backbuffer (`BeginDepthPrepass`, depth only, no colour attachment) → main backbuffer pass, keeping that depth (skybox with LEQUAL, scene forward with **EQUAL**, UI fullscreen quad) → `EndFrame`. The prepass is `[renderer] depthPrepass`, on by default; turning it off is the A/B that checks the two passes agree — **`prepass.slang` must combine `projection`, `view` and `model` in exactly the order `forward.slang`'s `vsMain` does**, since EQUAL rejects a difference in the last bit and shows it as speckle rather than as an error. **A settled scene runs neither bake pass**, which is what the `bakes:` counter beside the FPS is for.

**Every shadow in the scene is a sub-rect of a 4096² depth texture** — of two of them, since Part E: `staticAtlas` holds the casters that cannot move and is re-baked only when allocation moves, `dynamicAtlas` is a `CopyDepthRegion` of that plus the movable casters drawn on top, and `ShadowRecord.Flags` bit 0 is which one a light samples. A mesh is movable when the scene says `<movable>` or `MoveBy`/`MoveTo` has been called on it, and a light takes a dynamic tile only while a movable caster is inside its radius. The static side is all-or-nothing (a tile whose frustum holds no caster writes nothing, so baking only the changed slots would leave one wearing its last owner's depth); the dynamic side is per-tile and capped at `bakeTexelBudget` a frame, spent in score order, a light that misses out keeping the tile it has. Both atlases carve the same `slotLayout`, so a tile's rect is identical in the two and the copy needs no remap.

Below that split the tiling is unchanged — a sun or spot takes one tile, a point light six 90° tiles rather than a cubemap — so the pass count does not grow with the light count. Each tile is described by a `ShadowRecord` whose `LightSpace` matrix both bakes it and samples it. Two rules are silent corruption if broken: **clamp every PCF tap to the tile inset by one texel** (an atlas has no border; the neighbour is another light's shadow), and **widen each cube face to `90° + 2 texels`** (no cross-face filtering, so the kernel must stay inside its own tile). The atlas is carved into a **fixed slot layout** at load (`slotLayout` / `buildLayout` in `scene/shadowatlas.go`) — so many slots at 2048, 512, 256 and 128, which is `notes/tmp/LIGHTING_PLAN.md` §4.1's partition quadrant for quadrant — and those rects never move again, which is what lets Part E cache a baked tile. Every size is a division of `atlasSize` rather than a pixel count, so the atlas size buys sharpness and the slot counts buy light budget: two separate knobs, both `[shadows]` keys.
A smaller atlas also scales `forward.slang`'s world-space normal-offset bias
through `FrameUniforms.ShadowNormalScale` (`4096 / atlasSize`, so 1.0 at the
default) — without it, halving the atlas doubles a texel's world footprint and
re-introduces the acne those constants were tuned to hide. Slots are typeless, only size matters, because a point light's six faces each carry their own rect and are never filtered across, so they need not be adjacent. Who occupies a slot is decided **per frame**: every light scores `radius / distance to camera`, lights sort by score, and each takes the best free slot no larger than the ceiling its score earns (512 / 256 / 128, a sun capped at 2048; a point light's six faces each take a slot of that size, not a smaller one — the pool bounds it, so no extra rationing is needed). **Rank picks the slot, the tier caps it** — the ceiling stops an unimportant light claiming a slot it would waste, and phase 1b then re-offers whatever is spare to whoever ended up under their ceiling, so an idle slot size is never wasted on a scene whose light mix differs from the layout's. Running out of slots costs the least important light its resolution and never costs frame time — it walks down a pool at a time and finally leaves `ShadowIndex = -1`, which lights it unshadowed. Two hysteresis margins guard two different axes: `nextTierThreshold` (20%) keeps a boundary score from changing a light's ceiling every frame, and `slotStickiness` (20%) keeps two near-equal lights from trading a contended pool's last slot every frame — a failure fixed pools have and a splitting tree does not.

Conventions that would silently produce a mirrored or inside-out image: the main pass uses a **negative-height viewport** so clip space comes out y-up, which is what the projection matrices in `scene/` assume; that also flips winding, so it keeps CCW front faces. The shadow pass uses a positive viewport and declares `FrontFace = Clockwise`. Projections are the OpenGL convention (clip z in `[-w, w]`), so every vertex stage calls `TO_VK_DEPTH`. `notes/ENGINE_FLOW.md` §5 is the full list, §6 is a symptom→file table.

## Documentation map

- `notes/OVERVIEW.md` — **the whole engine in one read.** Layers, startup order, one frame, how uniforms and textures reach a shader, and the record-vs-submit model. Start here if the context is cold; it is deliberately the one file that restates the others.
- `notes/ENGINE_FLOW.md` — **read this first when touching the renderer.** Operational: one frame from `main()` to the GPU, then the `Backend` contract method by method. §0 indexes all 27 methods by call frequency (startup / load / per-frame / per-pass / per-draw); §5 is the rendering conventions, §6 a symptom→file table, §7 the Vulkan object-ownership tree and the five lifetime classes.
- `notes/ARCHITECTURE.md` — the code map: repository layout, the dependency rule (with the diagram), scene loading, physics/ECS, a package-by-package symbol reference, the XML/OBJ scene format and the Blender add-on, and §8 the list of dead files.
- `notes/FEATURES.md` — what is implemented and _why it is built that way_ (shadow bias, early-bail PCF, bindless vs dedicated descriptors), Part 2 the roadmap and known gaps, plus the performance history.
- `notes/tmp/BACKEND_DECISION.md` — **the current plan.** Why Vulkan only, what the `Backend` interface cannot yet express, and the ordered work items to fix that.
- `notes/TODO.md` — the working list.
- `notes/README.md` — index of `notes/`, and the shared conventions the cheatsheets follow.
- `notes/cheatsheets/` — engine-independent reference: `GRAPHICS.md` (real-time techniques, procedural generation, physics simulation, AI, compression, optimisation, GPGPU, emulation), `PBR.md`, `RAYTRACING.md`, `OPENGL.md`, `VULKAN.md`, `ALGEBRA.md`. All English, all opening with a Scope / Not here / Source block. `OPENGL.md` is kept deliberately — it is API reference, not a description of this engine.

## Conventions

Comments here are one-line, sentence-case, no trailing period, placed above the declaration and explaining _why_ or _what invariant_, not what the code literally does. Match that density — it is deliberate and consistent across the tree.

Scenes are XML (`src/assets/*.xml`) referencing OBJ/MTL in `assets/meshes/`, produced by the Blender add-on in `src/plugin/xml_export.py`. Static mesh geometry is baked into the OBJ vertices (identity model matrix), so `<position>` is unused for them.
