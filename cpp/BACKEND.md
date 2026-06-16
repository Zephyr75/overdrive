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

vulkan/
  Shader.hpp/.cpp — VKShader : Shader (uniform emulation over a BDA block)
  Backend.hpp/.cpp — VKBackend : Backend (Vulkan 1.3 implementation)
  Uniforms.hpp    — CPU mirror of the shader uniform block + name->offset maps
  ThirdParty.cpp  — VMA + stb_image implementation TU

shaders/slang/   — Slang shader sources (the single source of truth)
shaders/gl/      — generated GLSL 4.10 for OpenGL   (git-ignored)
shaders/vk/      — generated SPIR-V for Vulkan       (git-ignored)
```

## Shaders (Slang, one source, two backends)

Every shader is written once in `shaders/slang/*.slang` and compiled by CMake to
both targets:

- **OpenGL** — `slangc -target glsl -profile glsl_410 -preserve-params`, then
  `shaders/slang/downgrade.cmake` rewrites slangc's ~GLSL-4.50 output down to the
  4.10 core profile (the macOS ceiling): clamps `#version`, strips
  `layout(binding=)` and the SSBO `layout(row_major) buffer;` line, and converts
  `{…}` array initialisers to the `type[](…)` constructor form. Output → `shaders/gl/`.
- **Vulkan** — `slangc -target spirv -emit-spirv-directly -fvk-use-scalar-layout`.
  Output → `shaders/vk/`. (`-preserve-params` is *not* used here — it crashes the
  direct-SPIR-V path, and the VK shaders reach the UBO through a `Uniforms*`
  pointer whose full struct layout slangc keeps regardless of field usage.)

`common.slang` selects the resource model per target with `-DTARGET_VK`: Vulkan
uses the BDA push-constant pointer + bindless arrays; OpenGL uses a std140
`ConstantBuffer` UBO + named samplers. The shading math is shared. `createShader`
takes a logical name (e.g. `"forward"`); each backend resolves its own per-stage
files. `slangc` is found on PATH or the SDK is fetched at configure time.

## Selecting a backend

Only one backend is compiled at a time; `createBackend()` is defined by whichever is built. The scene layer (`Mesh`, `Light`, `Skybox`, `Scene`) contains no graphics API calls.

```sh
cmake -B build                      # OpenGL (default)
cmake -B build-vk -DUSE_VULKAN=ON   # Vulkan
cmake --build build-vk -j
./build-vk/overdrive                # run from cpp/
```

Vulkan build requirements: `vulkan-headers`, `vulkan-memory-allocator`, and `vulkan-validation-layers` (enabled automatically when installed; messages go to stderr). Shaders are compiled by `slangc` (found on PATH, otherwise the Slang SDK is fetched at configure time).

## How the Vulkan backend implements the interface

Targets Vulkan 1.3 with `dynamicRendering`, `synchronization2`, `bufferDeviceAddress`, `scalarBlockLayout` and descriptor indexing (see notes/VULKAN.md).

- **Uniforms** — the GL-style named setters write into a single CPU-side block
  (`VKUniformBlock`, scalar layout, shared by all shaders). Each draw snapshots
  the block into a per-frame host-visible ring buffer and passes its GPU
  address as a push constant; shaders read it through a `buffer_reference`
  pointer. No descriptor sets for buffers. The block matches the `Uniforms`
  struct in `shaders/slang/common.slang` byte-for-byte under scalar layout.
  (The OpenGL backend mirrors the same struct as a std140 UBO in `GLShader`;
  same logical names, std140 offsets.)
- **Textures** — one bindless descriptor set: `sampler2D[256]` + `samplerCube[64]`,
  partially bound, update-after-bind. Texture handles resolve to array slots at
  draw time; sampler uniforms keep their GL "texture unit" meaning. 2D slot 0 is
  a white pixel (= the engine's `whiteTexture()`/"no texture" handle 0).
- **Pipelines** — built lazily per (shader, pass type, vertex layout); cull mode
  and depth compare are dynamic state (core 1.3), so `setCullFace`/`setDepthFunc`
  map directly.
- **GL conventions** — the main pass renders with a negative-height viewport,
  which also cancels Vulkan's y-down winding flip, so the main pass keeps GL's
  counter-clockwise front face; shadow passes use a positive viewport (so the
  shadow-map memory layout matches GL and lookup math is unchanged) and
  therefore declare a clockwise front face. Vertex shaders remap clip-space z
  from GL's [-w,w] to Vulkan's [0,w].
- **Shadow passes** — depth-only dynamic rendering; the cube pass renders all 6
  faces in one go via the geometry shader and `layerCount = 6`. Image layout
  transitions happen in `beginPass`/`endPass`.
- **Frames in flight** — 2; per-frame command buffer, fence, acquire semaphore
  and uniform ring; per-image render-finished semaphores.

Known simplifications: no MSAA (GL build uses 4x), no texture mipmaps
(GL generates them), and `updateBuffer` drains the GPU before writing
(mesh moves are rare; switch to per-frame buffers or a model matrix if that
changes).

## Key changes from the original

- `opengl/Shader` renamed to `GLShader`, now derives from abstract `Shader`.
- `scene/Material` texture fields changed from `GLuint` to `uint32_t`; texture paths stored separately and GPU-loaded in `Mesh::setup()`.
- `Mesh`, `Light`, `Skybox` each store a `Backend*` set at setup time; all draw/render methods call through it.
- `Scene` constructor takes `Backend&`; setup calls (`mesh.setup`, `light.setup`, `skybox.setup`) forwarded with the backend.
- `App` owns the backend (created in the constructor, before the window, so the backend chooses its own window hints). `App.cpp` no longer touches glad or GL; `glfwSwapBuffers` lives in `GLBackend::endFrame()`.
- `Input::framebufferSizeCallback` no longer calls `glViewport` directly; the viewport is set per pass via `backend->beginPass`.
- `opengl/Texture.hpp/.cpp` are internal to `GLBackend` and no longer included by scene code.
