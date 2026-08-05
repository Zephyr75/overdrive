# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Overdrive is a Go game engine whose graphics layer runs on **Vulkan 1.3**, behind
an abstraction (`renderer/`) that keeps every other package free of graphics
calls. It also carries an ECS, Verlet physics, a gutter-based UI overlay and a
Blender XML export plugin.

An OpenGL 4.1 backend existed until 2026-08-05 and was deleted; `notes/tmp/BACKEND_DECISION.md`
records why, and is the roadmap for what the abstraction grows into next.

## Build & run

The Go module root is **`src/`** (module `github.com/Zephyr75/overdrive`), so `go` commands run from there. The working directory does not otherwise matter: every runtime file is resolved by the `paths` package against a discovered project root, so nothing in the tree may hold a relative path literal.

```sh
cd src
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh   # required; see note below
go build ./...
go test ./...        # uniform layout + showcase-scene checks; no GPU needed
go run .             # reads configs/vulkan.toml

go run . -config configs/vulkan.toml     # the same, named explicitly
OVERDRIVE_VK_VALIDATION=1 go run .       # validation layers

go test ./scene/ -run TestShowcaseLoads   # single test
```

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
renderer/          the abstraction: Backend interface, opaque handles, the two uniform structs
vulkan/            the only package that may import vk.*
```

Four invariants hold the engine together. Breaking any of them is how it goes wrong:

1. **Nothing above `renderer/` imports a graphics API.** Scene/core/ecs/input/physics own opaque handles (`renderer.MeshHandle`, `TextureHandle`, …) that the backend interprets in its own table. This is the rule that keeps `go test ./...` runnable without a GPU, and it is why the abstraction is kept despite there being one backend.

2. **Clears and viewports exist only inside `Backend.BeginPass`.** Never add a free-floating clear to scene or core code.

3. **Uniforms are two typed structs, split by update frequency.** `renderer.FrameUniforms` (1184 bytes, camera/lights/shadow maps) is published once per pass by `BindFrameUniforms`; `renderer.DrawUniforms` (128 bytes, model matrix + material) goes out per draw. Both mirror `shaders/slang/common.slang` field for field, and the backend **uploads them by memcpy** into a ring buffer plus two pushed device addresses.

   That works because Slang compiles with `-fvk-use-scalar-layout` and scalar layout is exactly Go's packing for `float32`/`int32` structs — so **keeping the field order in step is the whole requirement**. No 16-byte cells, no `vec3`-plus-scalar pairing, no vectors standing in for scalar arrays; that was std140's rule and it went with the OpenGL backend on 2026-08-05. Use only `float32`, `int32`, arrays of those, and `mgl32` matrices — anything with a wider alignment breaks the correspondence.

   The guard is an `init()` size panic in `renderer/uniforms.go`. It catches a member added, removed or resized — it cannot catch two members swapped, which leaves the size identical and renders silent garbage. **After editing `common.slang`, rebuild shaders and look at the scene.** To check a layout by hand, `spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate` prints the offsets the compiler actually emitted.

   `ScalarBlockLayout` is load-bearing: `LightData` is 68 bytes, so `lights[]` has a non-16-aligned stride that the standard layout rules reject. `spirv-val` fails on these modules unless given `--scalar-block-layout`.

4. **Shaders are authored once in Slang** (`src/shaders/slang/`) and compiled to SPIR-V. The backend does not read `.slang` at runtime, so `build_shaders.sh` must run before the first build and after every shader edit.

Frame shape (`core/App.Run`): physics + mesh re-upload → input → `BeginFrame` → ≤2 shadow passes (depth-only, no color clear) → main backbuffer pass (skybox with LEQUAL depth, scene forward, UI fullscreen quad) → `EndFrame`. Shadow budget is fixed at load: `Scene.pickShadowCasters` gives a 2D map to the first directional light and a cube map to the first point light; other lights still light the scene, they just cast nothing.

Conventions that would silently produce a mirrored or inside-out image: the main pass uses a **negative-height viewport** so clip space comes out y-up, which is what the projection matrices in `scene/` assume; that also flips winding, so it keeps CCW front faces. The shadow passes use a positive viewport and declare `FrontFace = Clockwise`. Projections are the OpenGL convention (clip z in `[-w, w]`), so every vertex stage calls `TO_VK_DEPTH`. `notes/ENGINE_FLOW.md` §5 is the full list, §6 is a symptom→file table.

## Documentation map

- `notes/ENGINE_FLOW.md` — **read this first when touching the renderer.** Operational: one frame from `main()` to the GPU, then the `Backend` contract method by method. §0 indexes all 25 methods by call frequency (startup / load / per-frame / per-pass / per-draw); §5 is the rendering conventions, §6 a symptom→file table, §7 the Vulkan object-ownership tree and the five lifetime classes.
- `notes/ARCHITECTURE.md` — the code map: repository layout, the dependency rule (with the diagram), scene loading, physics/ECS, a package-by-package symbol reference, the XML/OBJ scene format and the Blender add-on, and §8 the list of dead files.
- `notes/FEATURES.md` — what is implemented and *why it is built that way* (shadow bias, early-bail PCF, bindless vs dedicated descriptors), Part 2 the roadmap and known gaps, plus the performance history.
- `notes/tmp/BACKEND_DECISION.md` — **the current plan.** Why Vulkan only, what the `Backend` interface cannot yet express, and the ordered work items to fix that.
- `notes/TODO.md` — the working list.
- `notes/README.md` — index of `notes/`, and the shared conventions the cheatsheets follow.
- `notes/cheatsheets/` — engine-independent reference: `GRAPHICS.md` (real-time techniques, procedural generation, physics simulation, AI, compression, optimisation, GPGPU, emulation), `PBR.md`, `RAYTRACING.md`, `OPENGL.md`, `VULKAN.md`, `ALGEBRA.md`. All English, all opening with a Scope / Not here / Source block. `OPENGL.md` is kept deliberately — it is API reference, not a description of this engine.

## Conventions

Comments here are one-line, sentence-case, no trailing period, placed above the declaration and explaining *why* or *what invariant*, not what the code literally does. Match that density — it is deliberate and consistent across the tree.

Scenes are XML (`src/assets/*.xml`) referencing OBJ/MTL in `assets/meshes/`, produced by the Blender add-on in `src/plugin/xml_export.py`. Static mesh geometry is baked into the OBJ vertices (identity model matrix), so `<position>` is unused for them.
