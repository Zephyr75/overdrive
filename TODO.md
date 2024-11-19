# V1

- [X] Overlay Gutter on Overdrive
- [X] Add support for multiple shadows
- [X] Clean up fragment shader 
- [ ] Integrate skybox reflection nicely to the color computation
- [X] Make it usable from a simple ECS script
- [X] Get colliders position, size and rotation from Blender scene
- [ ] Debug mode
- [ ] Add multiple lights of the same type
- [ ] Add proper box colliders
- [ ] Add audio support

[Cubemap from HDRI](https://matheowis.github.io/HDRI-to-CubeMap/)

# Extensions

- [ ] Add bloom
- [ ] Fix lighting casters
- [X] Add anti-aliasing
- [ ] Add ambient occlusion (SSAO)
- [ ] Add blend (transparency)
- [ ] Add geometry shader for fur
- [ ] Add normal mapping
- [X] Add framebuffers (post-proc + gutter)
- [ ] Add instancing
- [ ] Add HDR to fix too much light
- [ ] Ray tracing on specific objects?
- [ ] Ray marching for basic shapes?
- [ ] Bushes
- [ ] Grass
- [ ] Wave Function Collapse
- [ ] Isotropic remeshing
- [ ] Noise terrain
- [ ] Navmesh
- [ ] Bezier paths


GOPROXY=proxy.golang.org go list -m github.com/Zephyr75/gutter@v0.1.2


Using OBJ conventions: Y is Up

    Blender to OBJ:
    pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
