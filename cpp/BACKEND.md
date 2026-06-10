# Backend Abstraction

The engine is now backend-independent. Graphics API selection happens at startup via the `createBackend()` factory.

## Lifecycle

The backend owns everything API-specific, including context/device creation and presentation:

1. `App` constructor calls `createBackend()`, then `glfwInit()`.
2. `backend->configureWindow()` — sets API-specific GLFW window hints (GL context version, or `GLFW_NO_API` for Vulkan), called before `glfwCreateWindow()`.
3. `backend->init(window)` — GL: make context current + load glad. Vulkan: instance, surface, device, swapchain.
4. Per frame: `beginFrame()` → render passes → `endFrame()` (GL: swap buffers; Vulkan: acquire happens in `beginFrame`, submit + present in `endFrame`).

Rendering is organised into passes — clears happen only at pass boundaries (required for Vulkan render passes / dynamic rendering):

- `beginPass(framebuffer, w, h, clearColor, r, g, b, a)` — binds the target (`0` = backbuffer/swapchain), sets the viewport, always clears depth, clears color if requested.
- `endPass()` — returns to the backbuffer.

There are no free-floating `clear`/`setViewport`/`bindFramebuffer` calls. `setCullFace`/`setDepthFunc` remain immediate state: Vulkan 1.3 covers them with dynamic state (`vkCmdSetCullMode`, `vkCmdSetDepthCompareOp`).

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
- `App` owns the backend (created in the constructor, before the window, so the backend chooses its own window hints). `App.cpp` no longer touches glad or GL; `glfwSwapBuffers` lives in `GLBackend::endFrame()`.
- `Input::framebufferSizeCallback` no longer calls `glViewport` directly; the viewport is set per pass via `backend->beginPass`.
- `opengl/Texture.hpp/.cpp` are internal to `GLBackend` and no longer included by scene code.
