# Overdrive :speedboat:

Overdrive is a game engine, not just a renderer. It is written in Go and its
graphics layer runs on Vulkan 1.3, behind an abstraction that keeps every other
package free of graphics calls.

You build scenes in Blender and export them to the engine's XML format with a
custom add-on. The export covers meshes, camera, lights and materials.

![Overdrive showcase scene](demo.png)

## Features

### Rendering

* Modern Vulkan setup. Vulkan 1.3 dynamic rendering, buffer device address with
  scalar layout uniforms, bindless descriptors, synchronization2, VMA, and 2
  frames in flight.
* Shaders written once in [Slang](https://github.com/shader-slang/slang) and
  compiled to SPIR-V. The scene code never calls a graphics API directly — it
  talks to an abstract `Backend` interface.
* Physically based shading. A metallic-roughness Cook-Torrance BRDF with
  GGX distribution, Smith geometry and Fresnel-Schlick, energy conserving, with
  Reinhard tone mapping.
* Directional and point lights, up to 8 at once, each with its own colour,
  intensity and falloff.
* Shadows for both light types. Directional lights use a 2D shadow map and point
  lights use a cube map for shadows in all directions. Both are softened with
  PCF and use a normal-offset bias so contact shadows stay attached.
* Normal mapping in tangent space, worked out per pixel so meshes need no extra
  tangent data.
* Materials and textures, with colour and normal maps and bindless texture
  arrays.
* Skybox that doubles as the ambient environment, so metals reflect it and
  dielectrics pick up a soft tint.
* OBJ and MTL mesh loading with XML scene files.

### Engine and tools

* An entity component system for game objects.
* Verlet particle physics.
* Game and menu UI built on [Gutter](https://github.com/zephyr75/gutter),
  composited over the scene as a fullscreen pass.
* A Blender add-on that turns a full Blender scene into a ready to use XML scene
  with meshes, camera, lights and materials.

## Roadmap

The ordered plan lives in [`notes/tmp/BACKEND_DECISION.md`](notes/tmp/BACKEND_DECISION.md)
§9. In short:

* Compute shaders, pipeline objects and a pass list — the substrate that makes
  everything below a new file rather than engine surgery.
* HDR, tone mapping and bloom, plus support for many dynamic lights.
* Texture driven PBR. Metallic, roughness and AO maps so the values vary per
  texel instead of per material.
* Real image based lighting. Prefilter the skybox into an irradiance map, a
  roughness mip chain and a BRDF lookup table, replacing today's single sample
  approximation.
* Reflection probes and volumetric rendering.
* Ray tracing. A software BVH first, then hardware ray queries where the GPU has
  them. See [`notes/FEATURES.md`](notes/FEATURES.md).
* A physics engine with rigid bodies and collisions, built out from the existing
  Verlet base.

## Build and run

### Requirements

Tested on Arch Linux. You need Go 1.26 or newer, plus:

```sh
sudo pacman -S base-devel glfw
sudo pacman -S vulkan-icd-loader
sudo pacman -S vulkan-validation-layers   # optional, for OVERDRIVE_VK_VALIDATION=1
```

You also need the Slang shader compiler (`slangc`) to build the shaders. It is
taken from `$SLANGC`, then your `PATH`. On Arch it is the AUR package
`shader-slang-bin`, which installs to `/opt/shader-slang-bin/bin/slangc` and does
**not** put it on `PATH` — so either add that directory to `PATH` or pass
`SLANGC=` as below. Arch's `slang` package is the unrelated S-Lang library.
Prebuilt SDKs for other systems are on the
[Slang releases page](https://github.com/shader-slang/slang/releases).

### Quick start

```sh
cd src                    # the Go module
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh   # or just ./build_shaders.sh if slangc is on PATH
go build ./...
go run .
```

`go` commands run from `src/`, the module root. Where you launch the binary from
does not matter — assets, textures, shaders and configs are resolved against the
project root, found by walking up from the working directory. Set
`OVERDRIVE_ROOT` to point somewhere else.

### Settings

Resolution, shadow-map resolution and anti-aliasing come from a TOML file, so
one build covers every combination:

```sh
go run .                        # reads configs/vulkan.toml
go run . -config low_end.toml   # or any other file
```

```toml
[window]
width = 1920
height = 1080

[shadows]
width = 1024
height = 1024

[renderer]
backend = "vulkan"

[antialiasing]
mode = "msaa"         # or "none"
samples = 4           # 2, 4 or 8

[textures]
anisotropy = 8        # 1 (off), 2, 4, 8 or 16
```

Every key is optional, an absent one keeping its default. An unknown key or
value is an error rather than a shrug, since a settings file that is silently
ignored is worse than one that fails. The file is the engine's only
configuration input, so what a run was configured with is always readable from
the file it was given.

`[renderer] backend` accepts only `"vulkan"`. It is kept so a config naming
another backend fails with an explanation, and so a second one has somewhere to
be named later.

### Validation layers

```sh
OVERDRIVE_VK_VALIDATION=1 go run .
```

A debugging switch, not a setting: it changes nothing about what is rendered.

### Shaders

Shader sources live in `shaders/slang/`. The generated `shaders/vk/` directory is
not checked in, so run `./build_shaders.sh` before your first build and after
every shader edit — the backend does not read the `.slang` files at runtime.

### Tests

```sh
go test ./...
```

No GPU needed. They check that the showcase scene loads and parses.

## Repository layout

| Path | Contents |
|------|----------|
| `src/` | The engine, and the Go module root. |
| `src/renderer/` | The backend abstraction: one interface, opaque handles, the two typed uniform structs. |
| `src/vulkan/` | The backend. Every graphics call lives here. |
| `src/shaders/slang/` | Shader sources, compiled to SPIR-V by `build_shaders.sh`. |
| `src/scene/`, `src/ecs/`, `src/physics/` | Scene graph, entity component system, Verlet physics. No graphics calls. |
| `src/plugin/` | The Blender add-on that exports a scene to XML. |
| `notes/` | Engine documentation and graphics cheatsheets — see [`notes/README.md`](notes/README.md). |

## Documentation

| Document | What it covers |
|----------|----------------|
| [`ENGINE_FLOW.md`](notes/ENGINE_FLOW.md) | **Start here for the renderer.** One frame from `main()` to the GPU, then the `Backend` contract method by method. §0 indexes all 25 methods by how often they run. |
| [`ARCHITECTURE.md`](notes/ARCHITECTURE.md) | The code map: repository layout, the dependency rule, scene loading, the package-by-package symbol reference, the scene format. |
| [`FEATURES.md`](notes/FEATURES.md) | What is implemented and why it is built that way, the roadmap, and the performance history. |
| [`BACKEND_DECISION.md`](notes/tmp/BACKEND_DECISION.md) | Why Vulkan only, what the abstraction cannot yet express, and the ordered work to fix that. |
| [`TODO.md`](notes/TODO.md) | The working list. |
| [`cheatsheets/`](notes/README.md) | Reference notes on PBR, ray tracing, OpenGL, Vulkan, linear algebra and real-time technique in general. |
