# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

- `go/` — **the engine; this is the main implementation and all active development happens here.** Go, backend-agnostic across OpenGL 4.1 and Vulkan 1.3, plus ECS, Verlet physics, UI and the Blender export plugin. `GO_BACKEND.md` Phases 0–4 are done: scene/core code has zero graphics imports, everything goes through the `renderer.Backend` interface + typed `renderer.Uniforms` struct, both backends are implemented, and shaders are single-source Slang. Read `GO_BACKEND.md` before touching the renderer.
- `notes/` — design notes; `notes/VULKAN.md` prescribes the Vulkan techniques both Vulkan backends must follow (Vulkan 1.3, dynamic rendering, BDA + scalar layout, bindless descriptor indexing, synchronization2, VMA, 2 frames in flight). `notes/FEATURES.md`, `notes/BACKEND.md`, `notes/PIPELINE.md` and `notes/OPTIMISATION.md` carry the renderer design and the measurements behind it — they outlived the C++ tree they were written against, and are still the reference for why the renderer is shaped the way it is.
- `GO_PARITY.md` — the remaining feature checklist, inherited from the now-deleted C++ engine. The renderer is at parity; what's left is backend polish and a few scene-layer items.

## Build & run

```sh
cd go
./build_shaders.sh   # Slang -> shaders/gl/*.glsl + shaders/vk/*.spv (required)
go build ./...
go test ./...        # std140 layout checks; no GPU needed
go run .             # run from the module root — relative asset paths
                     # OVERDRIVE_BACKEND=gl (default) | vulkan
                     # OVERDRIVE_VK_VALIDATION=1 enables the validation layers
```

Shaders are authored **once in Slang**, in `go/shaders/slang/`, and
compiled per backend by `build_shaders.sh` into `shaders/gl/` (GLSL 4.10) and
`shaders/vk/` (SPIR-V). Both output dirs are git-ignored, so the script must run
before the first build and after every shader edit — neither backend reads
`.slang` at runtime. Its only external tool is `slangc`, taken from `$SLANGC`,
then PATH. The GLSL 4.10 downgrade is done inline with `sed`, so no cmake is
needed. **`slangc` is not currently installed on this machine** — Arch's `slang`
package is the unrelated S-Lang library; shader-slang comes from its GitHub
releases or the AUR. The generated shaders are committed-to-disk but git-ignored,
so the engine builds and runs without it; only editing a `.slang` file needs it.

The Vulkan backend lives in `vulkan/` and links against the `vk` package in the
sibling `go-vulkan` repo (a `replace` directive points at `../../go-vulkan`).

Uniforms travel as one `renderer.Uniforms` struct mirroring the block in
`common.slang`. Vulkan memcpys it straight into its ring buffer (Go's struct
packing *is* scalar layout); OpenGL marshals it into a std140 UBO by hand in
`opengl/uniforms.go`. Those std140 offsets are the only hand-written layout in
the engine — `opengl/uniforms_test.go` re-derives them from the generated GLSL
and fails on drift, so run `go test ./opengl/` after touching `common.slang`.

Pass-based frame rule (same as C++): clears and viewports exist only inside
`Backend.BeginPass`; never add free-floating clear calls to scene/core code.
