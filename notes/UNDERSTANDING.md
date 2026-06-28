# Understanding the Codebase

A reading guide for the C++ engine, written for someone who knows OpenGL —
and a Vulkan tutorial that uses this codebase as its running example.
`BACKEND.md` is the spec; this document is the tour and the textbook.

How to use it:

- **Part I** — architecture: layers, interface, frame model. Read first.
- **Part II** — Vulkan from the ground up, in the order the code boots.
  Every concept is anchored to a real function in `vulkan/Backend.cpp`.
- **Part III** — how the backend emulates each OpenGL convenience.
- **Part IV** — reference: GL↔Vulkan table, coordinate bridging, a full
  trace of one frame, shader porting, recipes, performance notes.

---

# Part I — Architecture

## The big picture

The engine is split into three strict layers:

```
scene/  (Mesh, Light, Skybox, Scene, Camera, Material)
   │  knows nothing about graphics APIs — only talks to the interfaces below
   ▼
renderer/  (Backend, Shader — pure virtual interfaces)
   │  one of the two implementations is compiled in
   ▼
opengl/ (GLBackend, GLShader)        vulkan/ (VKBackend, VKShader)
```

The golden rule: **no `gl*` or `vk*` call ever appears outside `opengl/` and
`vulkan/`**. The scene layer holds opaque `uint32_t` handles (textures,
buffers, meshes, framebuffers) and calls `Backend` methods. What a handle
*means* is the backend's business: in GL it's a real `GLuint`; in Vulkan it's
an index into a table inside `VKBackend` (`textures`, `buffers`, `meshes`,
`shadowTargets` — index 0 of each is reserved so that `0` keeps its GL-ish
meaning of "no resource").

Only one backend exists per build (`cmake -B build` vs
`cmake -B build-vk -DUSE_VULKAN=ON`). Each backend defines the
`createBackend()` factory, so there is no runtime switch and no virtual-call
indirection cost beyond the interface itself.

### Recommended reading order

1. `renderer/Backend.hpp` + `renderer/Shader.hpp` — the entire contract,
   ~70 lines. Everything else implements or consumes this.
2. `core/App.cpp` — the frame loop. Shows the pass structure end to end.
3. `scene/Mesh.cpp` (`draw()`), `scene/Light.cpp` (shadow pass),
   `scene/Skybox.cpp` — the three call sites that exercise the interface.
4. `opengl/Backend.cpp` — read this as "the interface, annotated": each
   method is a couple of GL calls you already know. It tells you what the
   interface *means*.
5. `vulkan/Backend.cpp` top to bottom, with Part II of this document open
   next to it.
6. Shaders: `shaders/*.glsl` (GL, loaded at runtime) and
   `shaders/vulkan/*.glsl` (compiled to SPIR-V by the build via glslc).

## The frame, as the scene layer sees it

```cpp
backend->beginFrame();
// one depth-only pass per shadow-casting light:
backend->beginPass(light.depthMapFBO, SHADOW_W, SHADOW_H, /*clearColor=*/false);
...draw scene with depth shader...
backend->endPass();
// main pass to the backbuffer (framebuffer 0):
backend->beginPass(0, windowW, windowH, true, 0.1f, 0.1f, 0.1f, 1.0f);
...skybox, then meshes...
backend->endPass();
backend->endFrame();
```

This is the usual GL multi-pass loop with one deliberate restriction: **clears
only happen at pass boundaries** (`beginPass` always clears depth, clears
color on request). There is no free-floating `glClear`/`glViewport`/
`glBindFramebuffer` equivalent. That restriction costs GL nothing and is
exactly what Vulkan's render-pass model (here: dynamic rendering) requires —
it's the single design decision that makes a Vulkan backend possible without
touching the scene layer.

Two pieces of GL state survive as immediate calls — `setCullFace(bool front)`
and `setDepthFunc(bool lequal)` — because Vulkan 1.3 made cull mode and depth
compare op dynamic pipeline state (`vkCmdSetCullMode`,
`vkCmdSetDepthCompareOp`), so they map 1:1 in both backends.

---

# Part II — Vulkan from the ground up

OpenGL gives you a ready-made magic context: one hidden device, one hidden
queue, implicit synchronization, driver-managed memory. Vulkan hands you the
raw parts and makes you assemble that context yourself. This section walks
the assembly in the exact order `VKBackend::init()` performs it.

## Object glossary

The cast of characters, with their closest GL analogue:

| Vulkan object | What it is | Closest GL concept |
|---|---|---|
| `VkInstance` | connection to the Vulkan loader/driver | the GL library itself |
| `VkSurfaceKHR` | OS window's render target, via GLFW | the default framebuffer's window binding |
| `VkPhysicalDevice` | one GPU, enumerable, queryable | what `glGetString(GL_RENDERER)` hints at |
| `VkDevice` | your logical connection to that GPU | the GL context |
| `VkQueue` | where command buffers are submitted | the implicit GL command stream |
| `VkCommandBuffer` | recorded list of GPU commands | no equivalent — GL calls "just happen" |
| `VkSwapchainKHR` | set of presentable images | default framebuffer + swap behavior |
| `VkImage` / `VkImageView` | texture memory / a typed view of it | texture object / (no equivalent) |
| `VkBuffer` | linear GPU memory | buffer object |
| `VkSampler` | filtering/wrapping state, separate from image | `glTexParameter` state, but standalone |
| `VkDescriptorSet` | table of resource bindings shaders read | texture units + UBO binding points |
| `VkPipeline` | all draw state baked into one immutable object | no equivalent — the whole GL state machine |
| `VkPipelineLayout` | the "function signature" shaders expect (sets + push constants) | no equivalent |
| `VkFence` | GPU→CPU sync ("is the GPU done?") | `glFenceSync`/`glClientWaitSync` |
| `VkSemaphore` | GPU→GPU sync (between queue operations) | no equivalent (driver does it) |
| SPIR-V | binary shader IR, compiled offline | GLSL source compiled at runtime |

## Boot sequence — `VKBackend::init()`

```
createInstance() → surface → pickPhysicalDevice() → createDevice()
→ VMA allocator → command pool → createSwapchain() → createFrameData()
→ createSamplers() → createDescriptors() → createGlobalPipelineLayout()
→ createDefaultTextures()
```

Each step, what it does and why it exists:

### 1. Instance — `createInstance()`

`vkCreateInstance` connects to the Vulkan loader. Two things are decided
here:

- **Instance extensions**: whatever GLFW needs to create a surface
  (`glfwGetRequiredInstanceExtensions` — platform window-system extensions),
  plus `VK_EXT_debug_utils` when validation is on.
- **Layers**: the code probes for `VK_LAYER_KHRONOS_validation` and enables
  it only if installed. Layers are interceptors that sit between you and the
  driver; validation is the one you care about — it's the error checking
  that GL does always and Vulkan does *never* by default. A misuse in Vulkan
  without validation is silent garbage or a crash; with validation it's a
  precise message to the `debugCallback` at the top of the file. **Develop
  with validation installed, always.**

GLFW gets `GLFW_NO_API` in `configureWindow()` — the window has no GL
context; Vulkan attaches to it through the surface instead.

### 2. Surface

`glfwCreateWindowSurface` wraps the platform-specific
`vkCreate*SurfaceKHR`. A `VkSurfaceKHR` is the bridge between the
windowing system and Vulkan — the thing the swapchain will present into.

### 3. Physical device — `pickPhysicalDevice()`

Vulkan exposes every GPU in the machine; you choose. The code filters by:

- API version ≥ 1.3 (the engine relies on 1.3 core features),
- `geometryShader` support (cube shadow pass needs it),
- a **queue family** that supports both graphics and present.

Queue families are the part with no GL analogue: a GPU advertises families
of queues with different capabilities (graphics, compute, transfer-only…).
The engine takes the simple route — one family that does everything, one
queue from it. (Engines chasing async transfers use a separate transfer
queue; not needed here.)

Among the survivors it prefers discrete > integrated > other.

### 4. Logical device — `createDevice()`

`vkCreateDevice` is where you *opt in* to every feature you intend to use —
Vulkan features are off by default. The pNext chain in `createDevice()` is
effectively the engine's manifest:

- `dynamicRendering` (1.3) — render without `VkRenderPass`/`VkFramebuffer`
  objects; you name attachments at record time. Kills the most verbose part
  of classic Vulkan.
- `synchronization2` (1.3) — the saner barrier/submit API
  (`vkCmdPipelineBarrier2`, `vkQueueSubmit2`) used throughout.
- `bufferDeviceAddress` (1.2) — buffers get a raw 64-bit GPU address that
  shaders can dereference. Foundation of the uniform system.
- `scalarBlockLayout` (1.2) — lets GLSL blocks pack like C structs
  (no std140 padding), so a C++ struct can mirror the shader block exactly.
- `descriptorIndexing` family (1.2) — `runtimeDescriptorArray`,
  `descriptorBindingPartiallyBound`, `...UpdateAfterBind`,
  `shaderSampledImageArrayNonUniformIndexing`: everything bindless textures
  need (large descriptor arrays, holes allowed, writable while in use).

The only device extension is `VK_KHR_swapchain` (presenting is an extension
because headless Vulkan exists).

### 5. Memory allocator (VMA)

GL hides memory completely (`glBufferData`, done). Vulkan gives you raw
heaps: device-local (VRAM, fast, often not CPU-visible), host-visible
(CPU-writable, slower for the GPU), and combinations. You're expected to
sub-allocate — `vkAllocateMemory` calls are limited and slow.

Nobody writes that allocator by hand; the engine uses **VulkanMemoryAllocator
(VMA)**, the de-facto standard. Usage pattern throughout the code:

```cpp
VmaAllocationCreateInfo aci{};
aci.usage = VMA_MEMORY_USAGE_AUTO;                       // VMA picks the heap
aci.flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT |
            VMA_ALLOCATION_CREATE_MAPPED_BIT;            // CPU-writable, persistently mapped
vmaCreateBuffer(allocator, &bufferCI, &aci, &buffer, &allocation, &info);
// info.pMappedData is a CPU pointer you can memcpy into — forever
```

`MAPPED_BIT` = persistent mapping (like GL's
`glMapBufferRange(GL_MAP_PERSISTENT_BIT)`). After writing you call
`vmaFlushAllocation` — a no-op on coherent memory, required on
non-coherent; calling it unconditionally is the portable idiom.

### 6. Command pool + command buffers

The deepest difference from GL: **nothing executes when you call it.**
`vkCmdDraw`, `vkCmdBindPipeline` etc. only *record* into a
`VkCommandBuffer`. The GPU sees nothing until the buffer is submitted to a
queue. This is what GL drivers do secretly; Vulkan makes it your job, which
is also what makes multithreaded recording possible (not used here).

Command buffers come from a `VkCommandPool` (created with
`RESET_COMMAND_BUFFER_BIT` so each buffer can be individually re-recorded
every frame).

Two usage patterns in the code:

- **Per-frame buffers** (`FrameData::cb`): re-recorded every frame in
  `beginFrame`, submitted in `endFrame`.
- **`immediateSubmit(record)`**: for one-off work (texture uploads). Records
  a throwaway buffer, submits, `vkQueueWaitIdle`s. Blocking and naive — fine
  at load time, never during the frame loop.

### 7. Swapchain — `createSwapchain()`

GL's default framebuffer + `glfwSwapBuffers`, exploded into parts you
control:

- **Format**: queries supported surface formats, prefers
  `B8G8R8A8_UNORM` / sRGB-nonlinear color space.
- **Extent**: usually dictated by the surface capabilities (window size).
- **Image count**: `minImageCount + 1` (typically 3) — enough that the GPU
  isn't stalled waiting for the image being displayed.
- **Present mode**: `FIFO` — plain vsync, the only mode guaranteed to exist.
  (`MAILBOX` would be uncapped-with-no-tearing where available.)

`vkGetSwapchainImagesKHR` then hands back the actual `VkImage`s; the engine
creates a `VkImageView` per image, plus one shared `D32_SFLOAT` depth image
(the swapchain has no depth — in GL the default framebuffer's depth buffer
is a gift; here you make your own).

**Resize**: when the window changes, present/acquire return
`VK_ERROR_OUT_OF_DATE_KHR` / `VK_SUBOPTIMAL_KHR` and `recreateSwapchain()`
rebuilds it (passing the old swapchain in `oldSwapchain` so the driver can
recycle). GL does all of this invisibly when the window resizes.

### 8. Per-frame data — `createFrameData()`

The "2 frames in flight" kit, one per slot:

- a command buffer,
- a `VkFence`, created **signaled** (else frame 0 would wait forever on a
  fence nobody will signal — classic first-Vulkan-app deadlock),
- an acquire `VkSemaphore`,
- the 1 MiB host-visible **uniform ring buffer**, persistently mapped, with
  usage `SHADER_DEVICE_ADDRESS` so `vkGetBufferDeviceAddress` can fetch the
  GPU pointer once and store it (`ringAddr`).

Why 2 in flight: while the GPU renders frame N, the CPU records frame N+1.
One frame in flight = CPU and GPU take turns (slow); three = more latency.
Two is the usual sweet spot.

Per-*image* (not per-frame) render-finished semaphores live alongside —
see the sync section below for why.

### 9. Samplers — `createSamplers()`

In GL, filtering/wrap state lives in the texture object. Vulkan separates
image data (`VkImageView`) from sampling state (`VkSampler`); the combination
is what shaders consume. The engine creates four, used for every texture:

| Sampler | Filtering | Address mode | Used for |
|---|---|---|---|
| `samplerRepeat` | linear | repeat | regular 2D textures |
| `samplerCubeLinear` | linear | clamp-to-edge | skybox cubemap |
| `samplerShadowCube` | nearest | clamp-to-edge | point-light depth cube |
| `samplerShadow2D` | nearest | clamp-to-border, white border | sun shadow map (white border = "out of map ⇒ lit", same trick as the GL version) |

### 10. Descriptors — `createDescriptors()`

Descriptors are how shaders see resources. A `VkDescriptorSetLayout`
declares the shape ("binding 0: array of 256 combined image samplers,
fragment stage"), a `VkDescriptorPool` provides the storage, and
`vkUpdateDescriptorSets` writes actual image views in.

The engine deliberately avoids descriptor *churn* (the classic Vulkan
pain of allocating/binding sets per draw) by going **bindless**: one global
set, two big arrays —

```
set 0, binding 0: sampler2D   textures2D[256]
set 0, binding 1: samplerCube texturesCube[64]
```

with the descriptor-indexing flags `PARTIALLY_BOUND` (unwritten slots are
legal as long as shaders don't read them) and `UPDATE_AFTER_BIND` (slots can
be written while the set is bound/in use — no sync gymnastics when a texture
loads mid-run). Every texture is written into the next free slot **once**,
at creation (`registerTexture()`), and the set is bound **once per frame**
(`beginFrame`). Draws never touch descriptors again — they pass integer
slot indices in the uniform block instead.

### 11. Pipeline layout — `createGlobalPipelineLayout()`

The `VkPipelineLayout` is the calling convention shared by shaders and
draws: which descriptor set layouts exist (one — the bindless set) and what
push constants there are. Here: a single 8-byte push-constant range holding
one `VkDeviceAddress` — the pointer to this draw's uniform block, visible to
vertex, geometry and fragment stages (`kPushStages`).

Push constants are tiny data (≥128 bytes guaranteed) embedded directly in
the command buffer — the cheapest way to get per-draw data to a shader.
The engine pushes only the 8-byte pointer and keeps the real data behind it.

One layout serves every shader in the engine — which is also what lets all
pipelines share descriptor bindings without rebinding.

### 12. Default textures — `createDefaultTextures()`

2D slot 0 = 1×1 white (doubles as engine handle 0, GL's "no texture →
white" convention); cube slot 0 = black dummy. Unbound or wrongly-bound
samplers resolve to these instead of reading an invalid descriptor.

## Images, layouts and barriers

Every `VkImage` is always in a **layout** — a driver-internal arrangement
optimized for one kind of access: `COLOR_ATTACHMENT_OPTIMAL` (being rendered
to), `SHADER_READ_ONLY_OPTIMAL` (being sampled), `TRANSFER_DST_OPTIMAL`
(being copied into), `PRESENT_SRC_KHR` (being displayed),
`DEPTH_ATTACHMENT_OPTIMAL`, `UNDEFINED` ("contents are garbage, don't
care"). GL drivers shuffle these behind your back; in Vulkan **you**
transition images explicitly with pipeline barriers, and the same barrier
also expresses *when* (which pipeline stages must finish first / wait).

All of it goes through one helper — `imageBarrier()` in
`vulkan/Backend.cpp`, a thin wrapper over `vkCmdPipelineBarrier2`:

```cpp
imageBarrier(cb, image, aspect, layerCount,
             fromLayout, toLayout,
             srcStage, srcAccess,   // what must finish before the transition
             dstStage, dstAccess);  // what waits until after it
```

The three transitions worth studying as examples:

1. **Texture upload** (`uploadTexture`): `UNDEFINED → TRANSFER_DST_OPTIMAL`
   (nothing to wait for; copy waits), copy from staging buffer, then
   `TRANSFER_DST → SHADER_READ_ONLY` (copy must finish; fragment sampling
   waits).
2. **Shadow map per frame** (`beginPass`/`endPass`): `... →
   DEPTH_ATTACHMENT_OPTIMAL` before rendering into it, then
   `→ SHADER_READ_ONLY_OPTIMAL` after, with src = late-fragment-tests
   (depth writes), dst = fragment-shader reads. This pair is the explicit
   version of what GL does invisibly between "render to FBO" and "sample its
   texture".
3. **Swapchain per frame**: `UNDEFINED → COLOR_ATTACHMENT_OPTIMAL` in
   `beginPass(0,...)` (from `UNDEFINED` — we clear anyway, previous contents
   are irrelevant), `→ PRESENT_SRC_KHR` in `endFrame` before submit.

## Buffers and the staging pattern

GPU-optimal memory is often not CPU-writable. The standard upload dance
(`uploadTexture`):

1. create a host-visible **staging buffer**, `memcpy` pixels in, flush;
2. `immediateSubmit`: barrier image to `TRANSFER_DST`,
   `vkCmdCopyBufferToImage`, barrier to `SHADER_READ_ONLY`;
3. destroy the staging buffer.

That's `glTexImage2D`, spelled out.

Vertex/index buffers (`createBufferInternal`) skip the dance: VMA's `AUTO`
gives them host-visible (on this iGPU: also device-local — unified memory),
written directly through the persistent mapping. On a discrete GPU a serious
engine would stage these into VRAM too.

## Synchronization: the frame loop

The two primitives, and the rule for choosing:

- **`VkFence`** — GPU signals, **CPU** waits. Used to throttle the CPU.
- **`VkSemaphore`** — GPU signals, **GPU** waits. Links queue operations.
  The CPU can't observe it.

One frame in `beginFrame`/`endFrame`:

```
CPU:  vkWaitForFences(frame.fence)        ── don't get >2 frames ahead
      vkAcquireNextImageKHR(...)          ── which swapchain image? (async!
                  signals frame.acquireSem when the image is really free)
      reset fence, reset+begin command buffer, bind descriptor set
      ...record all passes and draws...
      submit:
        wait    frame.acquireSem  at COLOR_ATTACHMENT_OUTPUT stage
        signal  renderSems[imageIndex]  +  frame.fence
      vkQueuePresentKHR: waits on renderSems[imageIndex]
```

Three details that are classic Vulkan gotchas, all visible in the code:

- The fence starts **signaled** (`createFrameData`) or frame 0 deadlocks.
- The acquire-semaphore wait happens at the *color-attachment-output* stage,
  not the top of the pipe — vertex work may start before the image is
  available; only writing pixels must wait.
- Render-finished semaphores are **per swapchain image**, not per frame
  (`renderSems[imageIndex]`): present operations consume the semaphore at an
  unknowable time, so it must belong to the image, whose reuse is what
  guarantees the previous present finished.

`waitAllFrames()` (all fences) is the "GPU completely idle" hammer used
before destroying resources; `updateBuffer` uses the even blunter
`vkDeviceWaitIdle`.

## Pipelines — `getPipeline()`

The biggest conceptual gap from GL. In GL, draw state is a global state
machine validated at draw time. In Vulkan, *everything* is baked into an
immutable `VkPipeline` at creation: shader stages, vertex layout, topology,
rasterizer config, MSAA, depth/stencil, blending. Changing any baked field
means a different pipeline object.

`getPipeline(shader, pass, layout)` builds them lazily and caches in
`VKShader::pipelines[3][2]`, keyed by:

- **`VKPass`** (`Main` / `Shadow2D` / `ShadowCube`) → attachment formats
  (color+depth vs depth-only), blending (main only), front-face winding
  (see Part IV), shader stages (cube pass adds the geometry stage);
- **`VKVertexLayout`** (`Mesh`: pos/normal/uv stride 32, attributes 0/1/2 /
  `Skybox`: pos only, stride 12). This replaces the VAO: in Vulkan the
  vertex *format* is pipeline state, only the buffer binding happens at
  draw time. Shadow pipelines declare just attribute 0 — the depth shaders
  consume nothing else.

Walk the `VkGraphicsPipelineCreateInfo` sub-structs in the function once;
each maps to GL state you know:

| Sub-struct | GL equivalent | Engine's choice |
|---|---|---|
| `pStages` | linked program | vert+frag (+geo for cube shadows) |
| `pVertexInputState` | VAO attrib pointers | from `VKVertexLayout` |
| `pInputAssemblyState` | `glDrawArrays(GL_TRIANGLES…)` mode | triangle list |
| `pViewportState` | — | count=1, actual values dynamic |
| `pRasterizationState` | `glCullFace`/`glFrontFace`/`glPolygonMode` | fill; winding per pass; cull dynamic |
| `pMultisampleState` | `glfwWindowHint(GLFW_SAMPLES,4)` | off (known simplification) |
| `pDepthStencilState` | `glEnable(GL_DEPTH_TEST)`/`glDepthMask` | test+write on; compare op dynamic |
| `pColorBlendState` | `glBlendFunc` | srcAlpha/1-srcAlpha on main pass; none on shadow passes |
| `pDynamicState` | (the rest of the state machine) | viewport, scissor, cull mode, depth compare |
| `pNext: VkPipelineRenderingCreateInfo` | FBO attachment formats | swapchain format + D32, or D32 only |

Dynamic state is the escape hatch that keeps the cache small: viewport,
scissor, cull mode and depth-compare are set per command buffer
(`vkCmdSet*`), so they don't multiply pipeline variants. Hence 3×2 per
shader instead of dozens.

`use()` doesn't bind anything — it just marks the shader current;
`recordDraw` binds the right pipeline at the next draw (and skips the bind
if it's already bound — `boundPipeline`).

## Shaders: GLSL → SPIR-V

Vulkan consumes **SPIR-V** binaries, not GLSL source. The build
(`CMakeLists.txt`) runs `glslc` (shaderc) over `shaders/vulkan/*.glsl` with
`--target-env=vulkan1.3`, stage from the file suffix, `-I` for
`#include "common.glsl"`. At runtime `VKShader` just loads `.spv` bytes into
`vkCreateShaderModule` (`spvPath()` maps `shaders/X.glsl` →
`shaders/vulkan/X.spv` so the scene layer can keep passing GL paths).

Consequences vs GL:

- shader compile errors appear at **build time**, not first run;
- editing a Vulkan shader needs `cmake --build build-vk` (GL shaders are
  loaded from source at startup — just rerun);
- you can disassemble what the GPU actually gets:
  `spirv-dis shaders/vulkan/forward.frag.spv` — used in this project to
  verify struct offsets (see Part III).

---

# Part III — How the Vulkan backend emulates OpenGL

Vulkan has no state machine, no texture units, no named uniforms, no default
framebuffer, and no implicit synchronization. `VKBackend` rebuilds each GL
convenience explicitly.

## Named uniforms → one CPU struct + a GPU ring buffer

In GL, `glUniform*` writes into per-program state that persists until
overwritten. Vulkan has nothing like it — only descriptor sets and push
constants.

The emulation (`vulkan/Uniforms.hpp`, `vulkan/Shader.cpp`):

- There is **one** uniform struct for the whole engine, `VKUniformBlock` —
  the union of every uniform any shader uses (matrices, shadow matrices,
  material, `lights[2]`, sampler slots). Each `VKShader` owns a CPU copy.
- `setMat4("model", ...)`, `setFloat("lights[0].intensity", ...)` etc. look
  the name up in a name→{offset,size} map (`vkUniformFields()`) and `memcpy`
  into that CPU copy. Exactly the role of `glGetUniformLocation` +
  `glUniform*`, except the "GPU side" is deferred. Unknown names warn once
  (`VKShader::write`) — the Vulkan equivalent of GL silently returning
  location -1.
- At every draw, `VKBackend::recordDraw` snapshots the 860-byte block into
  the per-frame ring buffer (64-byte aligned chunks — the
  `buffer_reference_align` promised to the shader) and pushes the chunk's
  GPU address as the 8-byte push constant.
- Shaders declare the block as a `buffer_reference` pointer
  (`shaders/vulkan/common.glsl`) and read it via `pc.ubo.<field>` — the
  shader dereferences a raw GPU pointer. No descriptor sets for uniform
  data at all.

The contract: the GLSL `UBO` block and the C++ `VKUniformBlock` must match
**byte for byte**. Both use scalar layout (`scalarBlockLayout` +
`layout(buffer_reference, scalar)`), which packs like C — `vec3` is 12
bytes, no std140 rounding. `static_assert`s on the struct sizes guard the
C++ side; `spirv-dis | grep Offset` verifies the SPIR-V side. If you change
one side, change the other.

Why a ring buffer? The block is snapshotted *per draw* — two draws in the
same frame with different model matrices must not stomp each other, and the
GPU reads the data long after the CPU wrote it. Bump-allocating down a ring
(reset to 0 in `beginFrame`, after the fence guarantees the GPU finished
that frame's old contents) makes every draw's uniforms immutable once
recorded — which is precisely how GL's "uniforms stick until you change
them" illusion survives.

## Texture units → bindless descriptor arrays

GL: `glActiveTexture(GL_TEXTURE0 + unit)` + `glBindTexture`, and sampler
uniforms hold a unit index. Vulkan: descriptors must be written up front.

The emulation keeps the GL programming model:

- Every texture gets a permanent slot in `textures2D[256]` /
  `texturesCube[64]` at creation, never rebound (see Part II, descriptors).
- `bindTexture2D(unit, handle)` / `bindCubemap(unit, handle)` just record
  `unit → handle` in two small CPU arrays (8 units each).
  `Shader::setInt("skybox", 3)` still means "sampler `skybox` reads unit 3",
  exactly like GL — `VKShader::setInt` detects sampler names
  (`vkSamplerSlots()`) and stores the unit instead of writing the block.
- At draw time, `recordDraw` resolves each sampler: unit → bound handle →
  that texture's array slot, and writes the slot index into the uniform
  block (`texSkybox` etc.). The shader indexes the array:
  `texture(texturesCube[pc.ubo.texSkybox], dir)`.

Fallbacks mirror GL's lenience: 2D slot 0 is the white pixel (= engine
handle 0 = "no texture"), cube slot 0 a black dummy; a unit bound to the
wrong kind (2D vs cube) resolves to slot 0 of the right kind rather than
reading an invalid descriptor (which, with partially-bound arrays, would be
undefined behavior — typically a device lost, with no error message unless
validation is running).

## FBOs → dynamic rendering + manual layout transitions

GL: bind FBO, draw, done — the driver tracks image states.

- `beginPass(0, ...)` = backbuffer pass: barrier the acquired swapchain
  image to `COLOR_ATTACHMENT_OPTIMAL`, `vkCmdBeginRendering` with it + the
  shared depth image (loadOp = clear per the `clearColor` flag; depth always
  clears), set viewport/scissor, re-apply the dynamic cull/depth state.
- `beginPass(fbo, ...)` = shadow pass: `fbo` indexes the `ShadowEntry`
  table. Barrier to depth-attachment layout, render depth-only;
  `endPass()` barriers to `SHADER_READ_ONLY_OPTIMAL` for sampling.
- The cube shadow pass renders all 6 faces in one draw: the attachment is a
  2D-**array** view of the cube image with `layerCount = 6`; the geometry
  shader emits each triangle 6× with `gl_Layer = face`; sampling uses a
  separate **cube** view of the same image. Two views, one image — the
  Vulkan version of GL's "bind cubemap to FBO as layered attachment".

No `VkRenderPass`/`VkFramebuffer` objects anywhere (`dynamicRendering`) —
that's why the code stays close to the GL mental model.

## Implicit sync → frames in flight

Covered in detail in Part II. Summary of what replaces GL's invisible
driver work: fences throttle the CPU two frames behind, semaphores order
acquire→render→present on the GPU, the uniform ring is reset only after the
fence proves the GPU is done reading it, and `updateBuffer` (mesh edits)
takes the blunt `vkDeviceWaitIdle` path — correct, slow, rare, documented in
`BACKEND.md`.

---

# Part IV — Reference

## OpenGL ↔ engine ↔ Vulkan equivalents

| OpenGL | Engine interface | Vulkan implementation |
|---|---|---|
| context creation | `init(window)` | instance → surface → device → swapchain (Part II) |
| `glClear` + `glViewport` + `glBindFramebuffer` | `beginPass(fbo, w, h, clear, rgba)` | image barrier + `vkCmdBeginRendering` (loadOp = clear) + `vkCmdSetViewport/Scissor` |
| `glfwSwapBuffers` | `endFrame()` | barrier → `vkQueueSubmit2` → `vkQueuePresentKHR` |
| (driver throttling) | `beginFrame()` | fence wait + `vkAcquireNextImageKHR` + begin command buffer |
| `glUseProgram` | `shader->use()` | marks shader current; pipeline bound lazily at next draw |
| `glGetUniformLocation` + `glUniform*` | `setMat4/Vec3/Float/Int(name, v)` | name→offset map, `memcpy` into CPU block; per-draw snapshot into ring buffer; address via push constant; shader reads through `buffer_reference` |
| `glActiveTexture` + `glBindTexture` | `bindTexture2D/bindCubemap(unit, handle)` | record unit→handle; resolved to bindless array slot at draw |
| `sampler2D u; glUniform1i(u, unit)` | `setInt("name", unit)` | slot index written into uniform block; shader indexes `textures2D[...]` |
| `glTexImage2D` + `glTexParameter` | `loadTexture(path)` | staging buffer → barrier → `vkCmdCopyBufferToImage` → barrier; separate `VkSampler` |
| `glGenBuffers` + `glBufferData` | `createBuffer(data, size, dynamic)` | VMA buffer, persistently mapped, `memcpy` + flush |
| VAO + attrib pointers | `createMesh(vbo, indices, ...)` | nothing GPU-side: vertex layout is baked into pipelines (`VKVertexLayout`) |
| `glDrawElements` | `drawMesh(vao, indexCount)` | resolve samplers + snapshot uniforms + push constant + bind vertex/index buffer + `vkCmdDrawIndexed` |
| `glCullFace` | `setCullFace(front)` | `vkCmdSetCullMode` (dynamic state) |
| `glDepthFunc` | `setDepthFunc(lequal)` | `vkCmdSetDepthCompareOp` (dynamic state) |
| FBO + depth texture (shadow map) | `createShadowMap2D/Cubemap(...)` | `D32_SFLOAT` image, attachment view + sampled view, slot in bindless array |
| runtime `glCompileShader` | `createShader(vert, frag, geo)` | loads prebuilt `.spv` (glslc at build time) |
| `GL_ARB_debug_output` | — | `VK_LAYER_KHRONOS_validation` + debug-utils callback (auto-enabled when installed) |
| default framebuffer, vsync | — | swapchain, FIFO present, 2 frames in flight |

## Coordinate-system bridging (the subtle part)

The scene layer produces GL-convention data: GL projection matrices
(clip z in [-w, w]), GL winding (CCW front), GL texture/framebuffer row
order (row 0 = bottom). Vulkan disagrees on all three: clip z in [0, w],
framebuffer y pointing down, row 0 = top. Three tricks, all invisible to
scene code:

1. **Clip-space depth.** Every Vulkan vertex/geometry shader ends with
   `TO_VK_DEPTH(gl_Position)` (defined in `common.glsl`):
   `z = (z + w) / 2`, remapping GL's [-w, w] to Vulkan's [0, w]. The depth
   *values* that land in shadow maps therefore match GL's, so shadow lookup
   math in the fragment shaders is identical between backends. (The
   alternative — building Vulkan-style projection matrices — would have
   leaked API awareness into the scene layer.)

2. **Y direction + winding, main pass.** The main pass uses a
   **negative-height viewport** (`y = height, height = -height`), flipping
   the image upright. That same flip also cancels Vulkan's winding
   inversion, so main-pass pipelines keep GL's `COUNTER_CLOCKWISE` front
   face. Don't "fix" this to CW — it culls the ground plane and skybox and
   renders only closed meshes' backfaces (this exact bug cost a debugging
   session: flat pale shapes on black).

3. **Y direction + winding, shadow passes.** Shadow passes keep a normal
   positive viewport, so the shadow map's memory layout matches GL's
   (row 0 = light-space y = -1) and `projCoords.xy * 0.5 + 0.5` samples it
   identically in both backends. The cost: no winding cancellation, so
   shadow pipelines declare `CLOCKWISE` front face — which keeps
   `setCullFace(true)` (front-face culling, the standard shadow-acne /
   peter-panning fix) meaning the same faces as in GL.

Rule of thumb: **flip the viewport → keep GL winding; keep the viewport →
flip the winding.**

## One frame, traced through the code

What actually happens for a frame with one sun (2D shadow), one point light
(cube shadow), a skybox and N meshes — functions in call order:

```
App loop
│
├─ backend->beginFrame()                        vulkan/Backend.cpp
│    wait frame fence → acquire swapchain image → reset ring offset
│    → reset+begin command buffer → bind bindless descriptor set
│
├─ Light[0] (point) shadow pass                 scene/Light.cpp
│    depthCubeShader->use()                       marks shader current
│    setMat4("shadowMatrices[0..5]"), setVec3("lightPos"), setFloat("farPlane")
│    backend->beginPass(fbo, 1024, 1024, false)
│      barrier cube image → DEPTH_ATTACHMENT_OPTIMAL
│      vkCmdBeginRendering: depth-only, 2D-array view, layerCount=6
│    setCullFace(true)                            vkCmdSetCullMode(FRONT)
│    for each mesh: drawMesh()
│      recordDraw(Mesh):
│        getPipeline(depthCube, ShadowCube, Mesh)  [cached after 1st frame]
│        snapshot 860-byte block → ring buffer; vkCmdPushConstants(address)
│      vkCmdBindVertexBuffers/IndexBuffer; vkCmdDrawIndexed
│      (geometry shader fans each triangle to 6 layers, gl_Layer = face)
│    backend->endPass()
│      vkCmdEndRendering; barrier → SHADER_READ_ONLY_OPTIMAL
│
├─ Light[1] (sun) shadow pass                   same shape, Shadow2D:
│    lightSpaceMatrix ortho; single-layer D32 image; depth.vert/frag
│
├─ backend->beginPass(0, W, H, true, .1,.1,.1,1)
│      barrier swapchain image UNDEFINED → COLOR_ATTACHMENT_OPTIMAL
│      vkCmdBeginRendering: swapchain view + shared depth view, clear both
│      negative-height viewport
│
├─ Skybox                                       scene/Skybox.cpp
│    skyboxShader->use(); setMat4 view (no translation) / projection
│    setDepthFunc(true)                           vkCmdSetDepthCompareOp(LEQUAL)
│    backend->drawSkybox(vao)
│      recordDraw(Skybox): pipeline (skybox, Main, Skybox); snapshot; push
│      vkCmdDraw(36)                              z = w ⇒ depth 1.0, behind all
│    setDepthFunc(false)                          back to LESS
│
├─ Meshes                                       scene/Mesh.cpp draw()
│    forwardShader->use()
│    per light: setInt/setFloat/setVec3 lights[i].*    (→ CPU block)
│    setVec3 viewPos; setInt shadowMap=0, ourTexture=1, shadowCubeMap=2, skybox=3
│    bindTexture2D(0, sunDepthMap); bindCubemap(2, pointDepthCube);
│    bindCubemap(3, skyboxCubemap)
│    per submesh:
│      setVec3/Float material.*; bindTexture2D(1, texture or white)
│      drawMesh(vao, n)
│        recordDraw(Mesh):
│          resolve 4 sampler units → bindless slots → write into block
│          snapshot block → ring (64-aligned bump); vkCmdPushConstants
│        vkCmdBindVertexBuffers/IndexBuffer; vkCmdDrawIndexed
│
├─ backend->endPass()                             vkCmdEndRendering
│
└─ backend->endFrame()
     barrier swapchain image → PRESENT_SRC_KHR
     end command buffer; flush ring allocation
     vkQueueSubmit2  (wait acquireSem @ color-output;
                      signal renderSems[image] + frame fence)
     vkQueuePresentKHR (waits renderSems[image]); handle OUT_OF_DATE
     frameIndex = (frameIndex + 1) % 2
```

Until `vkQueueSubmit2`, the GPU has done *nothing* — the entire frame is a
recording.

## Shader porting pattern

Each GL shader `shaders/X.glsl` has a Vulkan twin `shaders/vulkan/X.glsl`
with mechanical changes only:

- `#include "common.glsl"` — extensions (`GL_EXT_buffer_reference`,
  `GL_EXT_scalar_block_layout`, `GL_EXT_nonuniform_qualifier`), the
  `LightData` struct, the `UBO` buffer-reference block, the push constant,
  the two bindless arrays, `TO_VK_DEPTH`.
- `uniform mat4 model;` → `pc.ubo.model`. `uniform sampler2D ourTexture;` →
  `#define ourTexture textures2D[pc.ubo.texOurTexture]` so the body reads
  the same as GL.
- Explicit `layout(location = N)` on every `in`/`out` (SPIR-V requires it;
  GL could match by name, SPIR-V matches by number — vert outputs and frag
  inputs must agree).
- `TO_VK_DEPTH(gl_Position)` at the end of vertex/geometry stages.
- Lighting/shadow math: byte-identical to the GL version, on purpose — it
  made GL-vs-Vulkan A/B debugging possible (render the same debug output on
  both and diff the images).

Editing a Vulkan shader requires a rebuild (`cmake --build build-vk`) —
glslc recompiles only changed `.glsl`. GL shaders are loaded at runtime;
just rerun.

## Where to make common changes

- **New uniform**: add to `VKUniformBlock` *and* the `UBO` block in
  `common.glsl` (same position!), update the `static_assert` sizes, add the
  name in `vkUniformFields()`. GL side needs nothing. Verify with
  `spirv-dis ... | grep Offset` if unsure.
- **New sampler**: new `texN` int at the end of both blocks, entry in
  `vkSamplerSlots()`, `#define` in the shader.
- **New vertex layout**: extend `VKVertexLayout`, add a case in
  `getPipeline`'s vertex-input setup, widen the pipeline cache array.
- **New pass type** (e.g. post-processing): extend `VKPass`, handle it in
  `beginPass`/`getPipeline` (attachment formats, winding, blending), add the
  layout transitions for any new render target.
- **Scene features** (new mesh types, lights, materials): scene layer only —
  if you find yourself needing an API call there, extend the `Backend`
  interface instead.

## Is this idiomatic Vulkan?

Mostly yes — the foundations are the current (2023+) recommended stack, not
GL-compat hacks: bindless descriptor indexing, BDA uniforms via push
constant, dynamic rendering, synchronization2, dynamic state, VMA, frames
in flight. A native engine would keep all of that.

The GL-emulation tax, in order of real cost:

1. **Full 860-byte block snapshot per draw** — view/projection/shadow
   matrices (~640 bytes) are re-uploaded per draw though they only change
   per frame. Native: split frame-scope data (written once per frame) from
   draw-scope (model matrix — 64 bytes, would even fit directly in push
   constants).
2. **String-keyed uniforms** — `"lights[0].intensity"` concatenation + hash
   lookup per light per mesh per frame, pure CPU overhead. Native engines
   write structs directly.
3. **Manual PCF in shaders** — plain `texture()` reads instead of
   depth-compare samplers (`compareEnable` + `sampler2DShadow`, free
   hardware 2×2 PCF). Inherited from the GL shaders, fixable in both.
4. **Geometry-shader cube shadows** — GS is the slow path on most GPUs;
   modern alternative: multiview or instanced layered rendering. Also
   inherited from GL.
5. **`vkDeviceWaitIdle` in `updateBuffer`/`destroyFramebuffer`** — full
   drain; fine because mesh edits are rare.

At this scene's scale (~10 draws/frame, FIFO-capped) none of it is
measurable; the items start to matter in the thousands-of-draws range.
Known feature gaps vs the GL build: no MSAA, no mipmaps (`BACKEND.md`).
