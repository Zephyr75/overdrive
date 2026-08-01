# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Overdrive is a Go game engine whose graphics layer runs on **both OpenGL 4.1 and Vulkan 1.3** from one shader set, picked at startup by `OVERDRIVE_BACKEND`. It also carries an ECS, Verlet physics, a gutter-based UI overlay and a Blender XML export plugin.

## Build & run

The Go module root is **`src/`** (module `github.com/Zephyr75/overdrive`). Run everything from there — asset, shader and texture paths are relative to it.

```sh
cd src
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh   # required; see note below
go build ./...
go test ./...        # std140 layout + showcase-scene checks; no GPU needed
go run .             # OpenGL by default

OVERDRIVE_BACKEND=gl     go run .   # OpenGL 4.1 core (default)
OVERDRIVE_BACKEND=vulkan go run .
OVERDRIVE_VK_VALIDATION=1 OVERDRIVE_BACKEND=vulkan go run .

go test ./opengl/                        # after any common.slang edit
go test ./scene/ -run TestShowcaseLoads   # single test
```

Stale paths to ignore: `GO_BACKEND.md` and the two top-level scripts (`overdrive.sh`, `overdrive_build.sh`) still say `go/` or `cpp/`. The C++ tree was deleted; the Go tree moved to `src/`. Fix references as you touch them rather than following them. (`README.md` and `notes/` were corrected on 2026-08-01.)

`slangc` comes from the AUR `shader-slang-bin` package, which installs to `/opt/shader-slang-bin/bin/slangc` and **does not put it on PATH** — so `build_shaders.sh` needs `SLANGC=/opt/shader-slang-bin/bin/slangc` unless that directory has been added to PATH. (Arch's `slang` package is the unrelated S-Lang library.) `src/shaders/gl/` and `src/shaders/vk/` are git-ignored, so a fresh clone builds and tests fine but cannot run until the script has been run once.

The Vulkan backend links against the `vk` package in the sibling repo `../../go-vulkan` (a `replace` directive in `go.mod`, resolving to `/home/zeph/GitHub/go-vulkan`). Missing bindings are catalogued in `GO_BACKEND.md` Part 6.

## Architecture

```
main.go            builds an App, loads a Scene, builds an ECS World
core/              NewApp (window + backend), App.Run (the frame loop), renderUI
scene/ ecs/        meshes, lights, camera, skybox, materials, physics entities
input/ physics/    — plain Go, zero graphics calls
renderer/          the abstraction: Backend interface, opaque handles, Uniforms struct
opengl/ vulkan/    the only packages that may import gl.* / vk.*
```

Four invariants hold the two backends together. Breaking any of them is how this engine goes wrong:

1. **Nothing above `renderer/` imports a graphics API.** Scene/core/ecs/input/physics own opaque handles (`renderer.MeshHandle`, `TextureHandle`, …) that each backend interprets in its own table. `createBackend` lives in `core/` only because `renderer` can't import the backends back.

2. **Clears and viewports exist only inside `Backend.BeginPass`.** Never add a free-floating clear to scene or core code.

3. **Uniforms are one typed struct.** `renderer.Uniforms` (1312 bytes) mirrors the block in `shaders/slang/common.slang` field for field. Vulkan memcpys it straight into a ring buffer — Go's struct packing *is* scalar layout, guarded by an `init()` panic in `renderer/uniforms.go`. OpenGL hand-marshals it into a std140 UBO in `opengl/uniforms.go`; those offsets are the only hand-written layout in the engine, and `opengl/uniforms_test.go` re-derives them from the generated GLSL and fails on drift. **After editing `common.slang`, rebuild shaders and run `go test ./opengl/`.**

4. **Shaders are authored once in Slang** (`src/shaders/slang/`) and compiled per backend. Neither backend reads `.slang` at runtime, so `build_shaders.sh` must run before the first build and after every shader edit.

Frame shape (`core/App.Run`): physics + mesh re-upload → input → `BeginFrame` → ≤2 shadow passes (depth-only, no color clear) → main backbuffer pass (skybox with LEQUAL depth, scene forward, UI fullscreen quad) → `EndFrame`. Shadow budget is fixed at load: `Scene.pickShadowCasters` gives a 2D map to the first directional light and a cube map to the first point light; other lights still light the scene, they just cast nothing.

Cross-backend gotchas that would silently produce a mirrored or inside-out image: Vulkan's main pass uses a **negative-height viewport** to match GL's y-up NDC, which also flips winding (so it keeps CCW front faces), while the shadow passes use a positive viewport and declare `FrontFace = Clockwise` so the shadow map's memory layout matches GL's. `ENGINE_FLOW.md` §5 is the full list, §6 is a symptom→file table.

## Documentation map

- `ENGINE_FLOW.md` — **read this first when touching the renderer.** Operational: one frame from `main()` to the GPU, then the `Backend` contract method by method with what each backend does. §0 indexes all 28 methods by call frequency (startup / load / per-frame / per-pass / per-draw); §7 is the Vulkan object-ownership tree and the five lifetime classes.
- `GO_BACKEND.md` — the design doc (why the abstraction is shaped this way), plus the go-vulkan bindings wishlist.
- `GO_PARITY.md` — the live TODO: known gaps (single point-shadow cube, 1024² shadow maps, no GL mipmaps, `devices[0]` physical-device pick, no image comparison between backends).
- `ABSTRACTION_REVIEW.md` — review of the decomposition: three extensibility limits (no colour render targets, the per-draw uniform god-struct, per-object-kind draws) and the order to fix them in.
- `notes/README.md` — index of `notes/`, marking each file current / C++-era-but-still-the-reason-why / personal reference. Superseded engine docs live in `notes/archive/`. `notes/VULKAN.md` is the prescribed Vulkan technique list (1.3, dynamic rendering, BDA + scalar layout, bindless descriptor indexing, synchronization2, VMA, 2 frames in flight).

## Conventions

Comments here are one-line, sentence-case, no trailing period, placed above the declaration and explaining *why* or *what invariant*, not what the code literally does. Match that density — it is deliberate and consistent across the tree.

Scenes are XML (`src/assets/*.xml`) referencing OBJ/MTL in `assets/meshes/`, produced by the Blender add-on in `src/plugin/xml_export.py`. Static mesh geometry is baked into the OBJ vertices (identity model matrix), so `<position>` is unused for them.
