# Backend Abstraction

The engine is now backend-independent. Graphics API selection happens at startup via the `createBackend()` factory.

## Structure

```
renderer/
  Shader.hpp      — abstract Shader interface (use, setInt, setFloat, setVec3, setMat4)
  Backend.hpp     — abstract Backend interface + createBackend() factory declaration

opengl/
  Shader.hpp/.cpp — GLShader : Shader (OpenGL implementation)
  Backend.hpp/.cpp — GLBackend : Backend (OpenGL implementation, defines createBackend())
  Texture.hpp/.cpp — GL texture helpers used internally by GLBackend
```

## Adding a new backend (e.g. Vulkan)

1. Create `vulkan/` directory with `VKShader` and `VKBackend` implementing the abstract interfaces.
2. Define `createBackend()` in `vulkan/Backend.cpp` returning `std::make_unique<VKBackend>()`.
3. Update `CMakeLists.txt`: replace `opengl/*.cpp` with `vulkan/*.cpp` in `SOURCES` (or add a CMake option to select between them).
4. Write Vulkan-specific SPIR-V shaders.

Only one backend is compiled at a time. The scene layer (`Mesh`, `Light`, `Skybox`, `Scene`) contains no graphics API calls.

## Key changes from the original

- `opengl/Shader` renamed to `GLShader`, now derives from abstract `Shader`.
- `scene/Material` texture fields changed from `GLuint` to `uint32_t`; texture paths stored separately and GPU-loaded in `Mesh::setup()`.
- `Mesh`, `Light`, `Skybox` each store a `Backend*` set at setup time; all draw/render methods call through it.
- `Scene` constructor takes `Backend&`; setup calls (`mesh.setup`, `light.setup`, `skybox.setup`) forwarded with the backend.
- `App::run` creates the backend via `createBackend()`, initialises it, then passes it to Scene.
- `Input::framebufferSizeCallback` no longer calls `glViewport` directly; the viewport is set each frame in the main loop via `backend->setViewport`.
- `opengl/Texture.hpp/.cpp` are internal to `GLBackend` and no longer included by scene code.
