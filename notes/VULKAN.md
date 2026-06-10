# Getting Started with Vulkan (for OpenGL devs)

A longer walkthrough based on *How to Vulkan in 2026* (Sascha Willems). Assumes you know OpenGL, GLSL, matrices, lighting, depth testing, etc. I skip anything that carries over unchanged and instead dwell on what's genuinely different or non-obvious.

---

## 1. Why Vulkan looks the way it does

OpenGL is a giant state machine with a very smart driver. You set blend modes, bind textures, call `glDrawElements`, and the driver figures out memory, synchronization, format conversions, shader recompilation, etc. That convenience has a cost: the driver has to assume worst-case behavior, and a lot of optimization happens at runtime behind your back. Two consequences:

- Performance is inconsistent and opaque. The same code behaves differently across vendors.
- The driver is huge, hard to maintain, and full of game-specific hacks.

Vulkan inverts this. It exposes a thin, explicit interface to what the GPU actually does. You manage memory, layouts, synchronization, and state as explicit objects. In exchange, the driver becomes much simpler, performance is predictable, and you can multithread command generation.

The price is verbosity. A basic triangle is ~1000 lines of setup. But almost every line corresponds to something OpenGL was secretly doing for you.

Keep this mental model: **everywhere Vulkan feels verbose, it's exposing something that existed in OpenGL too, you just weren't aware of it.**

---

## 2. Vulkan 1.3 is effectively a different API than Vulkan 1.0

A lot of older tutorials teach Vulkan 1.0 idioms that are genuinely painful. Target **Vulkan 1.3** and enable these features on the logical device — each one eliminates a category of boilerplate:

- **Dynamic rendering.** Render passes and framebuffers are gone. You describe attachments at draw time with `vkCmdBeginRendering`.
- **Buffer device address (BDA).** Buffers become raw 64-bit pointers inside shaders. Uniform/storage buffer descriptors become unnecessary for most use cases.
- **Descriptor indexing.** One giant array of textures, indexed in the shader ("bindless"). No more recreating descriptor sets per material.
- **Synchronization2.** Cleaner barrier API with stages and access flags consolidated. Harder to misuse than the original.

Without these four, you write roughly twice the code and deal with coupling you don't need. With them, Vulkan becomes tractable.

Vulkan 1.3 covers essentially everything made in the last several years on desktop and most recent mobile. Check `vulkan.gpuinfo.org` to verify for specific targets.

---

## 3. The object hierarchy — with the "why"

```
Instance  ← process-wide connection to the Vulkan loader
  PhysicalDevice  ← handle to a GPU (there can be several)
    Device  ← your "context"; the thing you make calls against
      Queue  ← where you submit work
      Allocator (VMA)  ← memory
      Surface  ← platform-specific window connection
        Swapchain  ← a ring of images the OS compositor reads from
      CommandPool
        CommandBuffer  ← where you record work before submitting
      DescriptorPool
        DescriptorSet  ← handles referring to shader resources
      PipelineLayout
        Pipeline  ← frozen state object (shaders + blend + depth + ...)
      ShaderModule  ← compiled SPIR-V
      Sync objects (Fence, Semaphore)
      Images, Buffers, ImageViews, Samplers
```

Three things are easy to miss:

**Instance vs Device.** The instance knows about *Vulkan* — which loader is present, what instance extensions are installed (surface creation, debug utilities). The device knows about *your GPU* — what features it supports, what queues it has. Instance extensions are global; device extensions live on a specific GPU.

**PhysicalDevice vs Device.** The physical device is a read-only handle you query capabilities from. The logical device is what you actually create with the feature set and queues you want. You can in principle create multiple logical devices from one physical device, though you rarely need to.

**Queue families.** A GPU exposes several queues grouped into families. Each family advertises what it supports: graphics, compute, transfer, video, presentation. On most desktop GPUs, family 0 supports all of the above and that's the one you use. On some hardware (mobile especially) you have to pick carefully. You submit command buffers to a queue, and queues in the same family are equivalent.

---

## 4. Memory: types, heaps, and why VMA is basically mandatory

In OpenGL, you call `glBufferData` and the driver picks where memory goes. In Vulkan, you pick. A GPU exposes several **memory heaps** (physical pools: VRAM, system RAM) and several **memory types** (logical properties of chunks within those heaps).

The relevant properties:

- **`DEVICE_LOCAL`** — lives in VRAM, fast for the GPU, potentially inaccessible from the CPU.
- **`HOST_VISIBLE`** — the CPU can `memcpy` into it.
- **`HOST_COHERENT`** — writes from the CPU are automatically visible to the GPU (no flush needed).
- **`HOST_CACHED`** — reading back from it is fast on the CPU.

Historically the rule was: vertex data, textures, depth buffers live in `DEVICE_LOCAL` only, and you need a staging buffer (a `HOST_VISIBLE` scratch area) to upload. Small per-frame data like uniforms live in `HOST_VISIBLE | HOST_COHERENT` so you can write them directly each frame.

Modern systems with ReBAR/SAM expose a memory type that is *both* `DEVICE_LOCAL` and `HOST_VISIBLE` — VRAM that the CPU can also map. VMA picks this automatically when you let it.

**Why use VMA:**

- It picks the right memory type from your usage flags.
- It sub-allocates from large chunks instead of one allocation per resource (allocations are expensive and limited — sometimes to 4096 total per device).
- It handles persistent mapping, defragmentation hooks, and the auto-fallback between memory types.
- It has first-class support for buffer device address.

The mental model you want:

```c
VmaAllocationCreateInfo ci {
    .flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT
           | VMA_ALLOCATION_CREATE_HOST_ACCESS_ALLOW_TRANSFER_INSTEAD_BIT
           | VMA_ALLOCATION_CREATE_MAPPED_BIT,
    .usage = VMA_MEMORY_USAGE_AUTO
};
```

`AUTO` + describing your access pattern lets VMA pick. `MAPPED_BIT` gives you a pointer you can `memcpy` into forever. `ALLOW_TRANSFER_INSTEAD_BIT` tells VMA: if the ideal memory isn't host-visible on this GPU, fall back to a staging buffer silently.

---

## 5. Synchronization — the actually hard part

This is where beginners drown. Three primitives, each for a different job:

### Fences — GPU signals CPU

"Is the GPU done with this batch of work yet?" A fence is created in either signaled or unsignaled state. You pass it to `vkQueueSubmit`; the GPU signals it when that submission completes. The CPU waits on it with `vkWaitForFences`.

You use fences to know when it's safe to reuse a resource the GPU was reading/writing. Most commonly: "before I overwrite this frame's uniform buffer, wait for the fence from two frames ago."

### Semaphores — GPU signals GPU

"Is the GPU done with stage A before stage B begins?" Binary semaphores can only be waited on inside queue submissions — the CPU cannot wait on them. They enforce ordering between GPU-side operations.

Primary use: gating presentation. You acquire a swapchain image with a `presentSem`, tell `vkQueueSubmit` to wait on `presentSem` before doing color attachment output, have it signal `renderSem` when done, and tell `vkQueuePresentKHR` to wait on `renderSem` before showing the image.

Timeline semaphores (a newer variant) replace fences + binary semaphores with a single counter-based object. They're cleaner but not universal, so most tutorials stick with the binary form.

### Pipeline barriers — ordering within a command buffer

"Before this work in this command buffer can read/write X, wait for the previous work in this command buffer to finish." Barriers are commands you record (`vkCmdPipelineBarrier2`), not objects.

Every barrier has four critical fields:

- **`srcStageMask`** — which pipeline stages need to finish first.
- **`srcAccessMask`** — what kind of memory access from those stages needs to be flushed to be **available**.
- **`dstStageMask`** — which pipeline stages need to wait.
- **`dstAccessMask`** — what kind of memory access in those stages needs those writes to be **visible**.

"Available" and "visible" sound synonymous but are distinct. Available means "drained from the writer's cache to the shared memory system." Visible means "loaded from the shared memory system into the reader's cache." Both must happen, and they're modeled separately because GPUs have tiered caches.

Image memory barriers additionally transition the **image layout** (see next section).

**The beginner shortcut:** when in doubt, pick broader stages and access masks. `ALL_COMMANDS_BIT` + `MEMORY_READ | MEMORY_WRITE` works but serializes the pipeline. You tighten it later for performance.

**Use validation.** Vulkan Configurator has a *synchronization validation* preset that catches the majority of sync bugs, including ones that happen to work on your GPU but break on others. Run with it on at least once per feature.

---

## 6. Image layouts explained

Every `VkImage` has a current **layout** — an abstract state describing how the image is currently arranged in memory and what operations are legal on it. The GPU may physically reorder texels (tiled layouts, compression metadata) depending on how the image is being used, and layout transitions are how you tell the driver to reshuffle.

The layouts that matter in practice:

- **`UNDEFINED`** — contents are garbage. Valid as a source for transitions when you don't care about the previous data. Always the starting state after creation.
- **`ATTACHMENT_OPTIMAL`** — being written to as a color or depth attachment. (Vulkan 1.3 unified color and depth attachment layouts into this one.)
- **`SHADER_READ_ONLY_OPTIMAL`** / **`READ_ONLY_OPTIMAL`** — sampled in a shader.
- **`TRANSFER_SRC_OPTIMAL`** / **`TRANSFER_DST_OPTIMAL`** — a `vkCmdCopy*` source or destination.
- **`PRESENT_SRC_KHR`** — ready to be handed to the presentation engine.

A typical texture's lifetime:

```
Create  → UNDEFINED
        → (barrier) → TRANSFER_DST_OPTIMAL     # to receive the upload
        → vkCmdCopyBufferToImage
        → (barrier) → SHADER_READ_ONLY_OPTIMAL # for sampling
```

A swapchain image each frame:

```
Acquire → UNDEFINED (we discard the old contents)
        → (barrier) → ATTACHMENT_OPTIMAL        # for rendering
        → rendering commands
        → (barrier) → PRESENT_SRC_KHR           # for the compositor
```

Forgetting a layout transition is the #1 cause of "it works on my GPU, crashes on yours." Validation catches it.

---

## 7. Descriptors — and how to mostly skip them

Descriptors are handles that describe shader resources (buffers, images, samplers) to the pipeline. In vanilla Vulkan 1.0 you'd deal with:

- **Descriptor set layouts** — the *interface*: "slot 0 is a uniform buffer, slot 1 is a sampled image."
- **Descriptor pools** — pre-allocated memory from which sets are drawn.
- **Descriptor sets** — the *instance*: actual buffer handle for slot 0, actual image handle for slot 1.

You bind a descriptor set before drawing, and the shader reads the resources through it. Conceptually a lot like UBO binding points in OpenGL but much more verbose.

**Two 1.3 features make descriptors mostly vestigial:**

### Buffer device address (for buffers)

Instead of referencing a uniform buffer through a descriptor, you get its 64-bit GPU address:

```c
VkBufferDeviceAddressInfo info{ .buffer = myBuffer, ... };
VkDeviceAddress addr = vkGetBufferDeviceAddress(device, &info);
```

You pass `addr` to the shader (most commonly via a push constant), and in the shader you just dereference it:

```slang
[shader("vertex")]
VSOutput main(VSInput input, uniform ShaderData *shaderData) {
    float4x4 m = shaderData->model[instanceIndex];
    ...
}
```

No descriptor sets, no bindings, no slot management. The one gotcha: struct layouts on the CPU and GPU must match. Slang/GLSL default to `std140`-ish rules with awkward padding. The simplest fix is to enable `VK_EXT_scalar_block_layout` (Vulkan 1.2 core) and write structs that look identical on both sides.

### Descriptor indexing (for textures)

You still need descriptors for images (there's no "image device address" equivalent yet), but descriptor indexing turns them into a single large array:

```slang
Sampler2D textures[];  // unbounded array, bindless
...
float3 color = textures[NonUniformResourceIndex(materialIndex)].Sample(uv).rgb;
```

You allocate one big descriptor set with N slots, fill it once with all your textures, and bind it once per frame. Per-draw you just pass an index (push constant, per-instance attribute, storage buffer field, whatever).

`NonUniformResourceIndex` tells the driver that threads in the same warp may index different elements — required when the index comes from per-fragment data.

With BDA for buffers and indexing for textures, descriptor sets become a background detail rather than the center of your renderer.

---

## 8. Pipelines — frozen state objects

A **graphics pipeline** bundles:

- Vertex input layout (attribute formats, stride, binding rate)
- Input assembly (triangle list, strip, etc.)
- Vertex + fragment shaders (and others if used)
- Viewport/scissor (can be dynamic)
- Rasterization state (cull mode, polygon mode, line width)
- Multisample state
- Depth/stencil state
- Color blend state
- Pipeline layout (descriptor set layouts + push constant ranges)
- Attachment formats (with dynamic rendering)

All of this is baked into one immutable object at `vkCreateGraphicsPipelines` time. The driver can then optimize aggressively — compile shaders with full knowledge of the surrounding state, specialize for the specific vertex layout, etc.

**Consequence:** if you want a different blend mode, you create a different pipeline. A real renderer ends up with hundreds or thousands of them. This is why pipeline caches and pipeline libraries exist.

**What can be dynamic** without creating a new pipeline: viewport, scissor (always). Line width, depth bias, blend constants (with flags). A much larger list with `VK_EXT_extended_dynamic_state3` and `VK_EXT_shader_object`, which make Vulkan feel closer to OpenGL's state-machine ergonomics — but those are optional and we're skipping them here.

**Pipeline layout** is a separate object because multiple pipelines often share the same resource interface. It lists the descriptor set layouts and push constant ranges the pipeline will use.

**Push constants** are a small block of data (guaranteed at least 128 bytes) that you inline into the command buffer with `vkCmdPushConstants`. They're the cheapest way to pass per-draw parameters. Perfect for BDA pointers, instance indices, material IDs — anything small that changes per draw call.

---

## 9. Command buffers: CPU timeline vs GPU timeline

This distinction trips people up constantly.

**CPU timeline:** code you write runs in order. `vkCmdBindPipeline` returns immediately — it has *recorded* the command, not executed it.

**GPU timeline:** the work described by your commands happens later, when the command buffer is submitted and the GPU gets around to it.

Anything prefixed `vkCmd*` is a record-for-later operation. Everything else (`vkCreate*`, `vkAllocate*`, `vkGet*`, `vkQueue*`) happens on the CPU timeline and may or may not interact with the GPU timeline depending on the call.

### Command buffer lifecycle

```
Initial → (begin) → Recording → (end) → Executable → (submit) → Pending
                                             ↑                       ↓
                                          (reset) ←── (work complete)
```

You must:
- Not record into a command buffer in the Pending state (the GPU is still using it).
- Reset it (explicitly or via `VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT` + `vkBeginCommandBuffer`) before re-recording.
- Know when the GPU is done — that's what the fence is for.

### Why command pools exist

Allocating command buffers is a hot path, so Vulkan uses pools for cheap block allocation. Each pool is tied to a queue family and can only be used on one thread at a time. For multithreaded recording, use one pool per thread.

---

## 10. The swapchain in slow motion

The swapchain is a ring of images owned by the operating system compositor. Your job is to acquire one, render into it, and give it back. Subtleties that matter:

**Image count vs frames in flight — these are different numbers.** The swapchain might have 2, 3, or 4 images depending on the present mode and driver. Frames in flight is how many frames of CPU-side resources (command buffers, uniform buffers) you keep live. They're related but not equal.

**Acquisition is asynchronous.** `vkAcquireNextImageKHR` returns an image index immediately, but the image may not be ready for the GPU yet — the compositor might still be reading from it. That's what the `presentSem` is for: the GPU waits on it before touching the image.

**Image index is not sequential.** The compositor may give you images 0, 2, 1, 0, 2 in any pattern. This is why you index some resources by `imageIndex` (per-image) and others by `frameIndex` (per-frame-in-flight).

**The two-semaphore pattern:** there's a subtle synchronization trap people fall into. A clean formulation:

- **`presentSemaphores[frameIndex]`** — one per frame in flight. Used to gate command buffer submission on image acquisition.
- **`renderSemaphores[imageIndex]`** — one per swapchain image. Used to gate presentation on rendering.

Why the asymmetry? `vkAcquireNextImageKHR` hasn't told us the image index yet when we need to pick a semaphore to pass in — so that one is indexed by `frameIndex`. `vkQueuePresentKHR` already knows the image index — so that one is indexed by `imageIndex`. Using `frameIndex` for both creates a subtle race where presentation can wait on a semaphore that hasn't been signaled yet. Just copy the pattern; the Vulkan Guide has the long version.

**Present modes:**
- `FIFO_KHR` — v-sync, guaranteed available. Start here.
- `MAILBOX_KHR` — uncapped but tear-free; latest frame wins.
- `IMMEDIATE_KHR` — tears, fastest.

**`VK_ERROR_OUT_OF_DATE_KHR`** — the surface changed size/orientation. Recreate the swapchain, bail out of this frame.

---

## 11. Frames in flight, fleshed out

The OpenGL mental model is "one frame at a time, driver handles everything." Vulkan exposes the pipelined reality.

If `maxFramesInFlight = 2`, then while the GPU renders frame N, the CPU prepares frame N+1. While the GPU renders N+1, the CPU prepares N+2 *and* the monitor displays N. Three things going on at once.

You duplicate resources that CPU and GPU both touch:

- Command buffers (CPU records, GPU reads)
- Uniform / shader-data buffers (CPU writes, GPU reads)
- Fences for completion signaling
- Present semaphores (per-frame, see above)

You **don't** duplicate resources only the GPU touches:

- Depth buffer — GPU writes and reads it, but entirely within one frame
- Textures — read-only from shader
- Vertex/index buffers (after upload)
- The pipeline itself

**Higher `maxFramesInFlight` trades latency for throughput.** Two is a sweet spot. Three is common when you want to smooth frame time spikes. More than three mostly adds input latency.

**The fence wait that gates it all** happens at the *start* of each frame. You're asking: "Is the GPU done with frame N-2's resources, so I can reuse them for frame N?" If the GPU is keeping up, the wait is zero. If the GPU is behind, this is where the CPU naturally throttles.

---

## 12. The render loop, with commentary

```c
while (!quit) {
    // (1) Throttle: wait for frame N-MAX_FIF's GPU work to complete.
    vkWaitForFences(device, 1, &fences[frameIndex], VK_TRUE, UINT64_MAX);
    vkResetFences(device, 1, &fences[frameIndex]);

    // (2) Ask the OS for a swapchain image. Signal presentSem when it's ours.
    vkAcquireNextImageKHR(device, swapchain, UINT64_MAX,
                          presentSemaphores[frameIndex], VK_NULL_HANDLE, &imageIndex);

    // (3) Safe to write per-frame CPU-side data now — the GPU is done with it.
    updateShaderData();
    memcpy(shaderDataBuffers[frameIndex].mapped, &shaderData, sizeof(shaderData));

    // (4) Record the command buffer for this frame.
    VkCommandBuffer cb = commandBuffers[frameIndex];
    vkResetCommandBuffer(cb, 0);
    vkBeginCommandBuffer(cb, &bi);

    // (4a) Layout transitions: UNDEFINED -> ATTACHMENT_OPTIMAL
    vkCmdPipelineBarrier2(cb, &preRenderBarriers);

    // (4b) Start dynamic rendering — no render pass object.
    vkCmdBeginRendering(cb, &renderingInfo);
        vkCmdSetViewport(cb, 0, 1, &vp);
        vkCmdSetScissor(cb, 0, 1, &scissor);
        vkCmdBindPipeline(cb, GRAPHICS, pipeline);
        vkCmdBindDescriptorSets(cb, ...);       // bindless textures
        vkCmdBindVertexBuffers(cb, ...);
        vkCmdBindIndexBuffer(cb, ...);
        vkCmdPushConstants(cb, ..., &bdaPointer); // address of per-frame UBO
        vkCmdDrawIndexed(cb, indexCount, instanceCount, 0, 0, 0);
    vkCmdEndRendering(cb);

    // (4c) Layout transition: ATTACHMENT_OPTIMAL -> PRESENT_SRC_KHR
    vkCmdPipelineBarrier2(cb, &presentBarrier);
    vkEndCommandBuffer(cb);

    // (5) Submit: wait on presentSem, signal renderSem, signal fence.
    vkQueueSubmit(queue, 1, &submitInfo, fences[frameIndex]);

    // (6) Hand image back to compositor after renderSem is signaled.
    vkQueuePresentKHR(queue, &presentInfo);

    frameIndex = (frameIndex + 1) % maxFramesInFlight;
    pollEvents();
    if (resized) recreateSwapchain();
}
```

Things worth staring at:

- The fence wait (1) is how the CPU throttles naturally. Without it you'd pile up unbounded frames.
- The shader data write (3) is safe *only because* we waited on the fence. Skip the fence and you may be writing into a buffer the GPU is still reading.
- The pre-render barrier (4a) transitions *from* `UNDEFINED` because we don't care about the previous content of the swapchain image — it might be N-2's frame, which we're about to overwrite anyway.
- The present barrier (4c) is what lets the compositor use the image. Omit it and you'll see validation errors and possibly nothing on screen.
- Submit signals both the fence (for CPU throttling next iteration) and the render semaphore (for presentation). The same GPU work completion event is observed by both.

---

## 13. Shaders: SPIR-V, Slang, and why it's better now

Vulkan doesn't consume GLSL directly. It consumes **SPIR-V**, a binary intermediate representation. You can generate SPIR-V from:

- **GLSL** — via `glslang` or `glslc`. Familiar but dated.
- **HLSL** — via DXC. Useful if you share shaders with a D3D12 backend.
- **Slang** — Khronos's modern shading language. Single file for all stages, module system, generics, automatic differentiation, better error messages.

**Why Slang specifically:**

- All stages in one file with `[shader("vertex")]` / `[shader("fragment")]` attributes — no more duplicated struct definitions across `.vert` and `.frag`.
- Pointers are first-class — plays perfectly with BDA.
- Can emit SPIR-V, HLSL, GLSL, Metal, CUDA — portable if you target multiple APIs.
- You can integrate the compiler as a library and recompile on file change for hot reload.

A minimal Slang module with both stages:

```slang
struct VSInput { float3 Pos; float3 Normal; float2 UV; };
struct VSOutput { float4 Pos : SV_POSITION; float3 Normal; float2 UV; };

struct ShaderData {
    float4x4 projection;
    float4x4 view;
    float4x4 model[3];
};

Sampler2D textures[];

[shader("vertex")]
VSOutput vsmain(VSInput in, uniform ShaderData *sd, uint iid : SV_VulkanInstanceID) {
    VSOutput o;
    o.Pos = mul(sd->projection, mul(sd->view, mul(sd->model[iid], float4(in.Pos, 1))));
    o.Normal = in.Normal;
    o.UV = in.UV;
    return o;
}

[shader("fragment")]
float4 fsmain(VSOutput in, uint iid : SV_VulkanInstanceID) {
    return textures[NonUniformResourceIndex(iid)].Sample(in.UV);
}
```

The `uniform ShaderData *sd` parameter is the BDA pointer — passed from the application as a push constant, dereferenced as if it were a C pointer.

---

## 14. Textures and why KTX beats PNG/JPEG

You *can* load a PNG, decode it to RGBA8 on the CPU, upload it via a staging buffer, and generate mipmaps with `vkCmdBlitImage`. It works. It's slow and wastes memory.

**KTX2 + Basis Universal** is better for every real use case:

- Stores natively compressed formats (BCn, ASTC, ETC) — 4-8× less VRAM.
- Mipmaps are baked in — no blit dance.
- You `memcpy` the file straight into a staging buffer, no decode.
- The libktx library decides the best GPU format per device (transcoding Basis Universal if needed).

**Upload flow, once:**

```
Create image (UNDEFINED) + staging buffer (HOST_VISIBLE)
memcpy file data into staging
begin one-time command buffer:
    barrier: UNDEFINED -> TRANSFER_DST_OPTIMAL
    vkCmdCopyBufferToImage (one region per mip level)
    barrier: TRANSFER_DST_OPTIMAL -> SHADER_READ_ONLY_OPTIMAL
end + submit + wait fence
free staging
```

**Samplers are separate objects.** A `VkSampler` encapsulates filtering mode, addressing mode, anisotropy, LOD clamps. One sampler can be used by many images — you don't need a sampler per texture.

For the descriptor array, you combine them (`VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER`) for simplicity. A real renderer often separates them (`SAMPLED_IMAGE` + `SAMPLER`) to avoid duplicating sampler state per texture.

**Format caveat:** 3-channel formats (RGB) are often unsupported. Use RGBA. On OpenGL the driver silently padded; on Vulkan it simply fails.

---

## 15. Validation layers — use them obsessively

Validation layers ship with the SDK and enable via `vkconfig` or an env var. They check:

- Spec violations (wrong struct type, missing required field, null pointer)
- Undefined behavior (uninitialized descriptor, wrong image layout)
- Synchronization hazards (write-after-read, race conditions — *with the synchronization preset*)
- Best-practice warnings (redundant barriers, suboptimal usage)
- Shader correctness (out-of-bounds access, uninitialized reads)

Errors print to stderr / `OutputDebugString` / logcat. Enable `VK_EXT_debug_utils` and pass a callback to get them in your own log with severity filtering.

**Turn on synchronization validation periodically.** It's expensive so people leave it off, but it catches the class of bugs that are most likely to bite you when shipping to different GPUs.

**When validation is clean and rendering is still wrong,** it's a logic bug (bad matrix, wrong vertex attribute offset, shader math). That's when you reach for RenderDoc — pixel-level GPU state inspection.

---

## 16. Beginner mistakes and how to avoid them

- **Forgetting an image layout transition.** Validation screams. Always transition swapchain images from `UNDEFINED` to `ATTACHMENT_OPTIMAL` at the start of rendering and to `PRESENT_SRC_KHR` at the end.
- **Calling a `vkCmd*` outside `vkBeginCommandBuffer`/`vkEndCommandBuffer`.** Segfaults, no validation help.
- **Submitting a command buffer before the previous submission's fence was signaled.** The command buffer is still in Pending state. Validation will tell you.
- **Updating a per-frame uniform buffer without waiting on the fence first.** Produces flicker or corruption that vanishes under a debugger.
- **Not checking `vkAcquireNextImageKHR` / `vkQueuePresentKHR` return codes.** They return `VK_SUBOPTIMAL_KHR` or `VK_ERROR_OUT_OF_DATE_KHR` on resize; you need to recreate the swapchain.
- **Mismatched CPU/GPU struct layout.** Enable `VK_EXT_scalar_block_layout` or carefully match `std140` rules. Symptoms: garbage values in shaders, especially with `vec3` and arrays.
- **Recreating the swapchain without `oldSwapchain`.** Causes visible hitches and wastes GPU memory. Always pass the previous swapchain into the create info.
- **Forgetting to enable Vulkan 1.3 core features** (`dynamicRendering`, `synchronization2`, `bufferDeviceAddress`, `descriptorIndexing`). You need to explicitly opt in via `VkPhysicalDeviceVulkan1{2,3}Features` even though they're "core." Missing these causes confusing validation errors like "extension not enabled."
- **Treating `imageIndex` and `frameIndex` as the same thing.** They're not. Semaphore indexing depends on which.

---

## 17. A suggested learning order

Don't try to absorb the whole object graph at once. Build up:

1. **Instance + device + queue.** Print the GPU name. Verify Vulkan 1.3 is active.
2. **Swapchain + clear color.** Transition swapchain image to `ATTACHMENT_OPTIMAL`, use dynamic rendering with `LOAD_OP_CLEAR`, transition to `PRESENT_SRC_KHR`, present. One clear, no shaders, no vertex buffer. This alone is ~500 lines and teaches you 60% of Vulkan.
3. **Hardcoded triangle.** A pipeline, a Slang shader with positions in the shader code. No vertex buffers.
4. **Vertex + index buffer via VMA.** Mesh from `tinyobjloader`. Now you have the basic draw shape.
5. **Per-frame uniform buffer via BDA + push constants.** Animate the triangle. First time you deal with frames in flight and fence throttling.
6. **Depth buffer.** Simple scene with overlapping geometry.
7. **Textures via KTX.** Staging buffer, layout transitions, samplers.
8. **Descriptor indexing.** Multiple textures in a bindless array, indexed from shader.
9. **Resize handling.** Recreate swapchain + depth image on `VK_ERROR_OUT_OF_DATE_KHR`.
10. **Proper synchronization with sync2 barriers.** Go back and tighten barriers from "everything everywhere" to the minimum needed. Turn on synchronization validation.
11. **A second pipeline, a second mesh.** Multi-draw-per-frame. This is where you stress-test whether your abstractions hold up.

Past that, you're into engine territory: pipeline caching, render graphs, GPU-driven rendering, bindless descriptor strategies, mesh shaders, raytracing.

---

## 18. Resources

- **Vulkan Docs Site** — combined spec, Khronos tutorial, samples index.
- **Sascha Willems' samples repo** — canonical reference implementations; the tutorial this summary is based on lives there.
- **vkguide.dev** — another modern Vulkan tutorial, complementary style. Worth reading in parallel.
- **vulkan.gpuinfo.org** — verify feature/format/limit support across real hardware.
- **RenderDoc** — frame debugger. Capture a frame, inspect every draw call, pipeline state, buffer contents.
- **vkconfig** — SDK GUI for validation layer configuration.
- **Arseny Kapoulkine — "Writing an Efficient Vulkan Renderer"** — when you're ready to think about performance.

---

## Bottom line

Vulkan rewards patience. The first triangle takes an afternoon, the next one takes ten minutes, and from then on you're writing a real renderer. Almost everything painful in Vulkan 1.0 has been smoothed over by 1.3 core features — if you target that baseline and use VMA + Slang + SDL + Volk, the gap between "I know OpenGL" and "I ship Vulkan" is weeks, not months. Most of the graphics knowledge transfers directly; you're just learning a more honest plumbing layer underneath.