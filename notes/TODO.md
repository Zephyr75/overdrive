# TODO.md — the working list

Small, concrete items. Anything that needs a paragraph of reasoning lives in
`FEATURES.md` Part 2 instead.

## Engine

- [x] Overlay Gutter on Overdrive
- [ ] Fix Gutter on Overdrive — `main.go` still calls `App.Run` with a nil widget
- [x] Support multiple shadows (one 2D map + one cube)
- [ ] Fill the remaining 3 cube-shadow slots — the shader has `MAX_SHADOW_CUBES` (4), `Scene` tracks a single `shadowPointIndex`
- [x] Clean up the fragment shader
- [x] Integrate the skybox reflection into the colour computation (PBR ambient term)
- [x] Usable from a simple ECS script
- [x] Read collider position, size and rotation from the Blender scene
- [ ] Debug mode
- [x] Multiple lights of the same type (up to `MAX_LIGHTS` = 8)
- [ ] Proper box colliders — `physics/box.go` is empty, `box_old.go` is commented out
- [ ] Verlet distance constraints — `physics/link.go` is commented out
- [ ] Audio support

## Rendering

- [~] Anti-aliasing — the GL context is hinted for 4× MSAA, the Vulkan backend has none
- [x] Framebuffers — generalised into `CreateRenderTarget(RenderTargetSpec)`
- [x] Normal mapping (tangent-space, per-fragment TBN)
- [ ] HDR + tone mapping + bloom — needs a half-float format binding in `go-vulkan`, see `FEATURES.md` §2
- [ ] Ambient occlusion (SSAO)
- [ ] Blending / transparency
- [ ] Instancing
- [ ] Mipmaps on the GL backend
- [ ] Shadow cascades — currently fixed at 1024², no CSM
- [ ] Geometry shader for fur
- [ ] Ray-traced shadows on Vulkan — `FEATURES.md` §3
- [ ] Ray marching for basic shapes and clouds — existed in the pre-Slang GLSL tree, not ported; `shaders/slang/` has only `forward`, `depth`, `depth_cube`, `skybox`, `ui`

## Tooling and cleanup

- [ ] GPU timestamp queries, to profile the two backends against each other — blocked on query-pool bindings in `go-vulkan`
- [ ] Image comparison between backends in the test suite
- [ ] Score physical devices instead of taking `devices[0]`
- [ ] Delete or implement the dead files listed in `ARCHITECTURE.md` §8
- [ ] `overdrive.sh` and `overdrive_build.sh` at the repo root are still cmake wrappers for the deleted C++ tree

## Procedural / world

- [ ] Wave Function Collapse — `algorithms/wfc.go` is an empty placeholder
- [ ] Noise terrain
- [ ] Grass, bushes
- [ ] Isotropic remeshing
- [ ] Navmesh
- [ ] Bezier paths

## Reference

- [Cubemap from HDRI](https://matheowis.github.io/HDRI-to-CubeMap/)
- Coordinate conversion, Blender → engine (OBJ convention, Y up):
  `pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}`
- `GOPROXY=proxy.golang.org go list -m github.com/Zephyr75/gutter@v0.1.2`
