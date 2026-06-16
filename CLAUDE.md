# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

Two implementations of the Overdrive engine:

- `go/` — original Go + OpenGL 4.1 engine (ECS, Verlet physics, UI, Blender export plugin). Documented in `ARCHITECTURE.md`.
- `cpp/` — C++17 rewrite with a backend-agnostic renderer (OpenGL and Vulkan backends). Documented in `cpp/BACKEND.md`. **Active development happens here.**
- `notes/` — design notes; `notes/VULKAN.md` prescribes the Vulkan techniques the C++ backend must follow (Vulkan 1.3, dynamic rendering, BDA + scalar layout, bindless descriptor indexing, synchronization2, VMA, 2 frames in flight).

## C++ build & run

```sh
cd cpp
cmake -B build                      # OpenGL backend (default)
cmake -B build-vk -DUSE_VULKAN=ON   # Vulkan backend
cmake --build build-vk -j
./build-vk/overdrive                # MUST run from cpp/ — asset/shader paths are relative
```

- Only one backend is compiled per build tree; `createBackend()` is defined by whichever backend is built.
- Shaders are authored once in Slang (`cpp/shaders/slang/*.slang`) and compiled per backend by CMake: GLSL 4.10 into `cpp/shaders/gl/` for OpenGL, SPIR-V into `cpp/shaders/vk/` for Vulkan (both git-ignored). Always rebuild after a shader edit — neither backend reads the `.slang` files at runtime. `slangc` is used from PATH if present, else the Slang SDK is fetched automatically at configure time. See `cpp/shaders/slang/` and `cpp/BACKEND.md`.
- Vulkan deps (Arch): `vulkan-headers`, `vulkan-memory-allocator`; `vulkan-validation-layers` auto-enables when installed (messages on stderr). (Shaders no longer use glslc/shaderc.)
- Asset loading takes several seconds at startup (unoptimized OBJ parsing) — allow for that when running with a timeout.

## C++ architecture

The scene layer (`scene/` — Mesh, Light, Skybox, Scene) contains no graphics API calls; everything goes through the abstract interfaces in `renderer/` (`Backend`, `Shader`), implemented in `opengl/` and `vulkan/`. Read `cpp/BACKEND.md` before touching the renderer — it defines the pass-based lifecycle (`beginFrame` → `beginPass`/`endPass` per render target → `endFrame`) and the rule that clears happen only at pass boundaries.

Key Vulkan-backend conventions (details in `cpp/BACKEND.md`):

- GL-style named uniforms are emulated: setters write a CPU `VKUniformBlock` (`vulkan/Uniforms.hpp`), snapshotted per draw into a host-visible ring buffer and read by shaders through a `buffer_reference` push-constant pointer. `VKUniformBlock` field offsets must byte-match the `Uniforms` struct in `cpp/shaders/slang/common.slang` (scalar layout; static_asserts guard the C++ side, `spirv-dis` verifies the SPIR-V side). The OpenGL backend mirrors the same struct in a std140 UBO (`GLShader`, `opengl/Shader.cpp`) — same logical names, different (std140) offsets.
- Textures are bindless (`sampler2D[256]` + `samplerCube[64]`); sampler uniforms keep GL "texture unit" semantics and resolve to array slots at draw time. Texture handle 0 = built-in white pixel.
- GL↔Vulkan bridging: main pass uses a negative-height viewport, which cancels Vulkan's y-down winding flip, so it keeps GL's CCW front face; shadow passes use a positive viewport (shadow-map layout matches GL) and declare a CW front face; vertex shaders remap clip z via `TO_VK_DEPTH`.

## Go build & run

```sh
cd go
go build ./...
go run main.go    # run from go/ — relative asset paths
```
