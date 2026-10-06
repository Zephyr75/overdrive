# Vulkan — the object model, for OpenGL developers

> **Scope** the Vulkan 1.3 API: objects, memory, swapchain, images and layouts, synchronization, buffers and BDA, descriptors, pipelines, command buffers, the render loop. Assumes OpenGL; only what differs is covered.
>
> **Not here** the OpenGL side → `OPENGL.md`. What Overdrive's backend does with all this → `../RENDERER.md`, `../SYNCHRONIZATION.md`. Ray tracing extensions → `RAYTRACING.md` §5.
>
> **Source** [How to Vulkan in 2026](https://howtovulkan.com) (Sascha Willems).

---

OpenGL is a state machine whose driver manages memory, sync and state behind your back. Vulkan makes all of it explicit objects: predictable performance, multithreaded recording, ~1000 lines for a triangle.

> Everywhere Vulkan feels verbose, it is exposing something OpenGL was secretly doing for you

## 1. Baseline and libraries

Target **Vulkan 1.3** and enable four core features, each removing a category of boilerplate:

- `dynamicRendering` no render pass or framebuffer objects: attachments are described at draw time
- `bufferDeviceAddress` buffers become 64-bit pointers in shaders: no buffer descriptors
- `descriptorIndexing` one bindless texture array: no per-material descriptor sets
- `synchronization2` a cleaner barrier API

> "Core" still means opt-in: chain `VkPhysicalDeviceVulkan1{2,3}Features` into device creation, or get "extension not enabled" errors

Libraries: **Volk** (function loading), **VMA** (memory, effectively mandatory), **SDL**/GLFW (window + surface), **GLM** (maths), **Slang** (shaders), **KTX-Software** (textures), **tinyobjloader** (meshes).

## 2. Object hierarchy

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

## 3. Instance and device

- **Instance** knows about *Vulkan*: loader, surface extensions, debug utils. Extensions here are global.
- **Physical device** is a read-only capability handle: `vkEnumeratePhysicalDevices`, `vkGetPhysicalDeviceProperties` (name, type, limits), `vkGetPhysicalDeviceFeatures2`. `vulkan.gpuinfo.org` has real-world support data.
- **Queues** come in families advertising graphics, compute, transfer, present. On most desktop GPUs family 0 does everything. Command pools are tied to one family.
- **Logical device** is created with the queues, device extensions (`VK_KHR_swapchain`) and features you want:

```c
// FULL FEATURE CHAIN
VkPhysicalDeviceVulkan13Features f13 { .sType = ..., .dynamicRendering = VK_TRUE, .synchronization2 = VK_TRUE };
VkPhysicalDeviceVulkan12Features f12 { .sType = ..., .pNext = &f13,
    .descriptorIndexing = VK_TRUE, .bufferDeviceAddress = VK_TRUE, .scalarBlockLayout = VK_TRUE };
VkDeviceCreateInfo ci { .sType = ..., .pNext = &f12, ... };
```

## 4. Memory and VMA

> **Heap = where the memory physically is. Type = what you may do with it.** One heap exposes several types, so VRAM may appear as device-local only and as device-local + host-visible

- `DEVICE_LOCAL` VRAM, fast for the GPU, possibly CPU-inaccessible
- `HOST_VISIBLE` the CPU can map and memcpy into it
- `HOST_COHERENT` CPU writes visible to the GPU without a flush
- `HOST_CACHED` fast CPU readback

Classic rule: meshes, textures, depth in `DEVICE_LOCAL` (via staging); per-frame uniforms in `HOST_VISIBLE | HOST_COHERENT`. ReBAR/SAM systems expose mappable VRAM and VMA picks it.

VMA picks the memory type from usage, sub-allocates from big blocks (the allocation count can be as low as 4096), maps persistently, supports BDA:

```c
// THE ALLOCATION PATTERN TO REMEMBER
VmaAllocationCreateInfo ci {
    .flags = VMA_ALLOCATION_CREATE_HOST_ACCESS_SEQUENTIAL_WRITE_BIT
           | VMA_ALLOCATION_CREATE_HOST_ACCESS_ALLOW_TRANSFER_INSTEAD_BIT  // silent staging fallback
           | VMA_ALLOCATION_CREATE_MAPPED_BIT,                             // permanent memcpy pointer
    .usage = VMA_MEMORY_USAGE_AUTO
};
vmaCreateBuffer(allocator, &bufferCI, &ci, &buffer, &allocation, &allocInfo);
```

## 5. Surface and swapchain

`vkCreateSwapchainKHR` creates a ring of presentable images; `vkGetSwapchainImagesKHR` returns them (the driver picks the count).

- **Present modes**: `FIFO_KHR` v-sync, always available; `MAILBOX_KHR` uncapped and tear-free; `IMMEDIATE_KHR` tears, fastest.
- `VK_ERROR_OUT_OF_DATE_KHR` from acquire or present means the surface resized: recreate, passing the old swapchain as `oldSwapchain`.

> **imageIndex ≠ frameIndex.** Swapchain image count (2–4, the driver's) and frames in flight (yours, usually 2) differ, and images come back in any order. Index per-image resources by `imageIndex`, per-frame ones by `frameIndex`

> **"Backbuffer"** is a double-buffering word: there is no fixed back image, only the swapchain image for this frame. Overdrive uses the word for exactly that

## 6. Images, views and layouts

Reading a texture involves four things OpenGL fused into one `GLuint`:

| Object | Answers | Analogy |
| --- | --- | --- |
| `VkImage` | where the pixels are | the storage |
| `VkImageView` | which pixels, read as what type | a window onto the storage |
| `VkSampler` | how to read them | the filtering rulebook |
| layout | how the pixels are physically arranged right now | no OpenGL equivalent |

> **Image ≠ view.** You never bind an image, only a view of one

**Layouts.** GPUs reorder texels (tiling, compression) per use; a transition tells the driver to reshuffle.

- `UNDEFINED` garbage contents; the state after creation and a valid "discard" source
- `ATTACHMENT_OPTIMAL` written as colour or depth attachment
- `SHADER_READ_ONLY_OPTIMAL` sampled in a shader
- `TRANSFER_SRC/DST_OPTIMAL` copy source / destination
- `PRESENT_SRC_KHR` ready for the compositor

```
// TEXTURE LIFETIME
Create  → UNDEFINED
        → (barrier) → TRANSFER_DST_OPTIMAL      // receive upload
        → vkCmdCopyBufferToImage
        → (barrier) → SHADER_READ_ONLY_OPTIMAL  // sample forever

// SWAPCHAIN IMAGE, EVERY FRAME
Acquire → UNDEFINED (discard old contents)
        → (barrier) → ATTACHMENT_OPTIMAL        // render
        → (barrier) → PRESENT_SRC_KHR           // hand to compositor
```

> A forgotten transition is the #1 cause of "works on my GPU, breaks on yours". Validation catches it

**Views** pick a view type (2D, 2D_ARRAY, CUBE, 3D), a format (UNORM vs SRGB over the same bits) and a subresource range (mips, layers, aspect). Two views of one image is normal: a cube render target is a 2D_ARRAY view to render into and a CUBE view to sample.

**Vocabulary.** An `attachment` is a view a pass renders into (colour, depth, resolve, input). A `render target` is one attachable image. A `framebuffer` is the set of attachments for a pass: under dynamic rendering a concept, not an object (`VkRenderingInfo` replaced `VkFramebuffer`).

## 7. Synchronization

| Primitive | Syncs | Use |
| --- | --- | --- |
| **Fence** | GPU → CPU | "is the GPU done with frame N-2's resources?" `vkWaitForFences`, `vkResetFences`; create signalled so frame 0 does not deadlock |
| **Semaphore** | GPU → GPU | gate presentation: submit waits on the acquire semaphore, signals the render semaphore; present waits on that |
| **Pipeline barrier** | GPU → GPU, inside a command buffer | ordering, cache flushes and layout transitions |

> **The two-semaphore indexing trap:** acquire semaphores by `frameIndex` (acquire does not know the image yet), render semaphores by `imageIndex` (present does). Using `frameIndex` for both is a race

> Timeline semaphores replace fences and binary semaphores with one counter: cleaner, less universal

### One barrier, three jobs

1. **Execution dependency.** Everything in `srcStageMask` recorded before finishes before anything in `dstStageMask` recorded after starts.
2. **Memory dependency.** Caches are not coherent between stages: `srcAccessMask` flushes writes (*available*), `dstAccessMask` invalidates the reader's cache (*visible*).
3. **Layout transition**, scheduled between the two scopes.

> "The write finished" and "the reader can see it" are **different claims**. Right layouts with sloppy stage masks still render garbage

| Field | Question | If wrong |
| --- | --- | --- |
| `srcStageMask` | which stages of earlier commands must finish | reads land before the write retires |
| `srcAccessMask` | which writes get flushed | reader sees stale data |
| `oldLayout` | the arrangement the image is in now | undefined contents |
| `newLayout` | the arrangement the next user needs | the next access is illegal |
| `dstStageMask` | which stages of later commands wait | races the consumer |
| `dstAccessMask` | which caches get invalidated | reader hits a stale cache line |

- **Only write bits do work in `srcAccessMask`.** A write-after-read hazard is solved by the stage mask alone.
- **`oldLayout` has no getter.** The application tracks every image's layout.
- **`oldLayout = UNDEFINED` means discard**: right before a full overwrite, wrong for a partial copy.
- **The transition reads and writes memory itself**, so both halves need populating even for a read-only source.

A finished shadow map handed to the pass that samples it:

```
oldLayout DEPTH_ATTACHMENT_OPTIMAL  →  newLayout SHADER_READ_ONLY_OPTIMAL
src  LateFragmentTests / DepthStencilAttachmentWrite
dst  FragmentShader    / ShaderSampledRead
```

> Shortcut while learning: `ALL_COMMANDS` + `MEMORY_READ | MEMORY_WRITE` everywhere is correct but serialises. Tighten later and run **synchronization validation** once per feature

### What the `2` means

`VK_KHR_synchronization2`, core in 1.3: the same concepts, redesigned.

| | 1.0 | synchronization2 |
| --- | --- | --- |
| mask width | 32-bit, out of bits | **64-bit**, room for ray tracing and mesh stages |
| stage masks | one pair for the whole call | **per barrier** |
| "nothing" | `TOP_OF_PIPE` / `BOTTOM_OF_PIPE`, easy to get backwards | explicit `STAGE_2_NONE` / `ACCESS_2_NONE` |
| arguments | three arrays | one `VkDependencyInfo` |
| submit | `vkQueueSubmit` + parallel wait-stage array | `vkQueueSubmit2`, stage mask per semaphore |

## 8. Buffers vs images

| | `VkBuffer` | `VkImage` |
| --- | --- | --- |
| shape | linear bytes, meaning is yours | typed grid, `VkFormat` declared |
| memory layout | row-major, guaranteed | opaque, vendor-swizzled tiling |
| layout state | none | `VkImageLayout`, per use |
| shader access | raw fetch, or a pointer via BDA | through a sampler |
| attachment | no | yes |

> **Buffer = you decide what the bytes mean. Image = the hardware already knows.** Anything you sample with filtering or render into is an image

What only images get: **tiling** (2D neighbours share a cache line, which is why layouts exist), **sampler hardware** (filtering, mips, anisotropy, hardware depth compare), **free format conversion** (sRGB, BC/ASTC decode, 4–6× less memory), and **attachment** (only the ROPs write images).

## 9. Transfers and staging

A transfer command is **always GPU to GPU**:

| Command | src → dst |
| --- | --- |
| `vkCmdCopyBuffer` | buffer → buffer |
| `vkCmdCopyImage` | image → image |
| `vkCmdCopyBufferToImage` | buffer → image |
| `vkCmdCopyImageToBuffer` | image → buffer |

The CPU reaches GPU memory only by memcpy through a mapped pointer, so an upload is two steps:

```
memcpy into a HOST_VISIBLE staging buffer   <- a pointer write, no command, no usage flag
vkCmdCopyBuffer staging -> DEVICE_LOCAL     <- the transfer command
```

An image can never be mapped (its tiling is opaque), so a buffer is the only bridge to it. buffer → image is routine (every texture upload); image → buffer is a deliberate stall (screenshots, picking), never in a frame loop.

**Usage flags** are a promise at creation so the driver can place the memory, not a runtime check, and orthogonal to memory type. `TRANSFER_SRC/DST_BIT` only say a copy may name the resource. Copying to a resource without `TRANSFER_DST_BIT` often *appears* to work, then breaks on another GPU.

## 10. Buffers and BDA

`vmaCreateBuffer` creates buffer and allocation together. Usage: `VERTEX_BUFFER`, `INDEX_BUFFER`, `TRANSFER_SRC/DST`, `SHADER_DEVICE_ADDRESS`.

Staging exists because the fastest memory is usually the one the CPU cannot write:

```
create staging buffer (HOST_VISIBLE) + destination buffer (DEVICE_LOCAL)
memcpy data into staging's mapped pointer
one-time command buffer: vkCmdCopyBuffer(cb, staging, dst, 1, &region)
submit + wait fence, destroy staging
```

**Buffer device address.** `vkGetBufferDeviceAddress` returns a 64-bit GPU pointer. Pass it in a push constant and dereference it like C: no descriptors for buffers.

```slang
[shader("vertex")]
VSOutput main(VSInput input, uniform ShaderData *shaderData) {
    float4x4 m = shaderData->model[instanceIndex];
    ...
}
```

Four things must all be true, and each fails differently:

| Requirement | Where | If missing |
| --- | --- | --- |
| `bufferDeviceAddress` feature | `VkPhysicalDeviceVulkan12Features` | validation error |
| `VMA_ALLOCATOR_CREATE_BUFFER_DEVICE_ADDRESS_BIT` | `vmaCreateAllocator` | the address is undefined |
| `SHADER_DEVICE_ADDRESS_BIT` usage | `VkBufferCreateInfo` | invalid for that buffer only |
| `scalarBlockLayout` + `-fvk-use-scalar-layout` | feature + compile flag | compiles, runs, renders garbage |

## 11. Descriptors

Layout (the interface), pool (the memory), set (the instance). **BDA replaces buffer descriptors; it cannot replace image descriptors**: an image is retiled and compressed, with no meaningful address.

A `COMBINED_IMAGE_SAMPLER` descriptor is a triple from three places:

```
(image view, sampler, image layout)
```

| Part | Contributes | Decided when |
| --- | --- | --- |
| image view | which pixels, as what type | view creation |
| sampler | filter, address mode, anisotropy, LOD | sampler creation, no image involved |
| layout | the arrangement when read | a **promise**, kept by barriers elsewhere |

> **The layout in a descriptor write performs no transition.** Sampling while the image is still in an attachment layout reads undefined data

- The same image can sit in two descriptors with two samplers at no memory cost.
- Alternatives: separate `SAMPLED_IMAGE` + `SAMPLER` (one sampler, many images, two indices per read), or **immutable samplers** baked into the layout.
- **Pool sizing** takes two different numbers: `maxSets` (sets) and `pPoolSizes` (descriptors per type, summed). Bindless is typically one set with hundreds of descriptors.

**Bindless**: one big set, filled once, bound once per frame, indexed per draw:

```slang
Sampler2D textures[];  // unbounded array
float3 color = textures[NonUniformResourceIndex(materialIndex)].Sample(uv).rgb;
```

`NonUniformResourceIndex` is required when threads of a warp may use different indices.

## 12. Shaders: SPIR-V and Slang

Vulkan consumes **SPIR-V**, from GLSL (`glslc`), HLSL (DXC) or **Slang**: all stages in one file, first-class pointers (made for BDA), many targets.

```slang
// FULL MINIMAL MODULE
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

`uniform ShaderData *sd` is the BDA pointer, pushed by the app.

## 13. Pipelines

`vkCreateGraphicsPipelines` bakes vertex input, topology, shader stages, rasterisation, multisample, depth/stencil, blend, the pipeline layout and the attachment formats into one immutable object.

> A pipeline is **a compiled shader plus every piece of GPU state it was compiled to assume**. OpenGL's `glEnable(GL_BLEND)` could force a recompile mid-frame; Vulkan makes you name the combinations up front

- Only viewport and scissor are dynamic by default (more with extended dynamic state).
- The **pipeline layout** is separate because many pipelines share one resource interface.
- **Push constants** (`vkCmdPushConstants`) are the cheapest per-draw data. Only **128 bytes** are guaranteed, shared by all stages: room for pointers, not matrices.

## 14. Command buffers

> **CPU timeline vs GPU timeline:** every `vkCmd*` records; execution happens after submit

A command pool is a cheap allocator tied to one queue family, used by **one thread at a time**.

```
// LIFECYCLE
Initial → (begin) → Recording → (end) → Executable → (submit) → Pending
                                             ↑                      ↓
                                          (reset) ←──── (work complete, fence knows)
```

Never re-record a pending buffer: that is what the per-frame fence wait guarantees.

## 15. Textures and samplers

**KTX2 + Basis Universal** stores GPU-compressed formats (4–8× less VRAM) with mips baked in; libktx transcodes per device. Upload is staging + `vkCmdCopyBufferToImage` per mip + two barriers.

A sampler references no image, so a handful serves a whole renderer:

| Field | Decides |
| --- | --- |
| `magFilter` / `minFilter` | linear or nearest |
| `mipmapMode` | blending between mips |
| `addressModeU/V/W` | repeat, clamp-to-edge, clamp-to-border |
| `borderColor` | what clamp-to-border returns |
| `anisotropyEnable` / `maxAnisotropy` | extra samples along a stretched footprint |
| `minLod` / `maxLod` | reachable mips |
| `compareEnable` / `compareOp` | hardware depth comparison for shadow maps |

- **Anisotropy needs mips**: its job is picking a sharper mip; with one level it does almost nothing.
- **RGB formats** are often unsupported: use RGBA (OpenGL silently padded).

## 16. Frames in flight and the render loop

While the GPU renders N, the CPU records N+1 and the screen shows N-1. Two frames in flight is the sweet spot. **Duplicate** what CPU and GPU both touch (command buffers, uniform buffers, fences, acquire semaphores); **do not duplicate** GPU-only data (depth, textures, meshes, pipelines). The fence wait is the CPU throttle.

```c
while (!quit) {
    // (1) Throttle: wait for this slot's previous GPU work to complete.
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

    // (4a) Layout transition: UNDEFINED -> ATTACHMENT_OPTIMAL
    vkCmdPipelineBarrier2(cb, &preRenderBarriers);

    // (4b) Start dynamic rendering — no render pass object.
    vkCmdBeginRendering(cb, &renderingInfo);
        vkCmdSetViewport(cb, 0, 1, &vp);
        vkCmdSetScissor(cb, 0, 1, &scissor);
        vkCmdBindPipeline(cb, GRAPHICS, pipeline);
        vkCmdBindDescriptorSets(cb, ...);         // bindless textures
        vkCmdBindVertexBuffers(cb, ...);
        vkCmdBindIndexBuffer(cb, ...);
        vkCmdPushConstants(cb, ..., &bdaPointer); // address of per-frame shader data
        vkCmdDrawIndexed(cb, indexCount, instanceCount, 0, 0, 0);
    vkCmdEndRendering(cb);

    // (4c) Layout transition: ATTACHMENT_OPTIMAL -> PRESENT_SRC_KHR
    vkCmdPipelineBarrier2(cb, &presentBarrier);
    vkEndCommandBuffer(cb);

    // (5) Submit: wait on presentSem, signal renderSem[imageIndex], signal fence.
    vkQueueSubmit(queue, 1, &submitInfo, fences[frameIndex]);

    // (6) Hand image back to compositor once renderSem is signaled.
    vkQueuePresentKHR(queue, &presentInfo);

    frameIndex = (frameIndex + 1) % maxFramesInFlight;
    pollEvents();
    if (resized) recreateSwapchain();
}
```

- Without (1), frames pile up and (3) overwrites a buffer the GPU is reading.
- (4a) starts from `UNDEFINED` because the old contents are overwritten anyway.
- (5) is one GPU completion observed twice: the fence (CPU throttle) and the render semaphore (present gate).

**Cleanup**: `vkDeviceWaitIdle`, then destroy in reverse creation order. Swapchain-sized resources also die on every recreate.

## 17. Validation and debugging

Enable the layers with `vkconfig` or an environment variable: spec violations, wrong layouts, sync hazards, out-of-bounds shader access. `VK_EXT_debug_utils` routes messages to your log. Validation clean but render wrong means a logic bug: use **RenderDoc**.

## 18. Beginner mistakes

- Forgetting a layout transition
- `vkCmd*` outside begin/end: a segfault with no validation help
- Re-recording a pending command buffer, or writing per-frame uniforms before the fence wait
- Ignoring `VK_SUBOPTIMAL_KHR` / `VK_ERROR_OUT_OF_DATE_KHR`
- Mismatched CPU/GPU struct layout (vec3, arrays) → `scalarBlockLayout`
- Recreating the swapchain without `oldSwapchain`
- Not enabling the 1.3 feature structs
- Treating `imageIndex` and `frameIndex` as the same
- BDA enabled in only one of its three places
- Assuming a descriptor's layout performs the transition
- Pushing more than 128 bytes of push constants
- Enabling anisotropy on textures with no mips

## 19. Learning order and resources

1. Instance + device + queue: print the GPU, verify 1.3
2. Swapchain + clear colour (~500 lines, 60% of Vulkan)
3. Hardcoded triangle
4. Vertex + index buffers via VMA
5. Per-frame data via BDA + push constants: frames in flight
6. Depth buffer
7. Textures (staging, transitions, sampler)
8. Bindless texture array
9. Resize handling
10. Tighten barriers, run sync validation
11. A second pipeline and mesh, to stress the abstractions

Then pipeline caches, render graphs, GPU-driven rendering, mesh shaders, ray tracing.

**Resources**: the Vulkan Docs site, Sascha Willems' samples, vkguide.dev, vulkan.gpuinfo.org, RenderDoc, vkconfig, Arseny Kapoulkine's "Writing an Efficient Vulkan Renderer".
