# Vulkan (for OpenGL devs)

Based on [How to Vulkan in 2026](https://howtovulkan.com) (Sascha Willems). Assumes OpenGL knowledge: only what is different is covered.

OpenGL = giant state machine + smart driver doing memory/sync/state management behind your back. Vulkan exposes all of it as explicit objects: predictable performance, multithreadable command generation, ~1000 lines for a triangle.

> Everywhere Vulkan feels verbose, it's exposing something OpenGL was secretly doing for you

## Baseline: Vulkan 1.3

Target **Vulkan 1.3**, enable these core features on the device (each kills a category of boilerplate):

- `dynamicRendering` no more render pass + framebuffer objects: describe attachments at draw time
- `bufferDeviceAddress` buffers become raw 64-bit pointers in shaders: no buffer descriptors
- `descriptorIndexing` one giant bindless texture array: no per-material descriptor sets
- `synchronization2` cleaner barrier API, harder to misuse

> "Core" still means opt-in: enable via `VkPhysicalDeviceVulkan1{2,3}Features` chained into device creation. Forgetting them causes confusing "extension not enabled" validation errors

# Libraries

- **Volk** loads Vulkan function pointers (the GLAD equivalent)
- **VMA** (Vulkan Memory Allocator) memory management, basically mandatory
- **SDL** window + surface creation (broadest platform support; GLFW also works)
- **GLM** math
- **Slang** shader language → SPIR-V
- **KTX-Software** GPU texture format loading
- **tinyobjloader** mesh loading

# Object hierarchy

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

# Instance and device

## Instance

`vkCreateInstance(&createInfo, nil, &instance)` create instance: app info + instance extensions + layers

> **Instance** knows about *Vulkan* (loader, surface extensions, debug utils). **Device** knows about *your GPU* (features, queues). Instance extensions are global; device extensions live on a GPU

## Physical device selection

`vkEnumeratePhysicalDevices(instance, &count, devices)` list GPUs

`vkGetPhysicalDeviceProperties(physicalDevice, &props)` name, type (discrete/integrated), limits, API version

`vkGetPhysicalDeviceFeatures2(physicalDevice, &features)` query supported features (chain 1.2/1.3 feature structs)

> PhysicalDevice = read-only capability handle. Device (logical) = created with the features + queues you want. Check `vulkan.gpuinfo.org` for real-world feature support

## Queues

GPU exposes queues grouped into **families**; each family advertises support: graphics, compute, transfer, present

`vkGetPhysicalDeviceQueueFamilyProperties(physicalDevice, &count, families)` list families

`vkGetDeviceQueue(device, familyIndex, 0, &queue)` get queue handle after device creation

> On most desktop GPUs family 0 supports everything: use it. Queues in the same family are equivalent. Command pools are tied to one family

## Logical device

`vkCreateDevice(physicalDevice, &createInfo, nil, &device)` create with queue create infos + device extensions (`VK_KHR_swapchain`) + enabled features

```c
// FULL FEATURE CHAIN
VkPhysicalDeviceVulkan13Features f13 { .sType = ..., .dynamicRendering = VK_TRUE, .synchronization2 = VK_TRUE };
VkPhysicalDeviceVulkan12Features f12 { .sType = ..., .pNext = &f13,
    .descriptorIndexing = VK_TRUE, .bufferDeviceAddress = VK_TRUE, .scalarBlockLayout = VK_TRUE };
VkDeviceCreateInfo ci { .sType = ..., .pNext = &f12, ... };
```

# Memory : VMA

GPU exposes **memory heaps** (physical pools: VRAM, system RAM) containing **memory types** (logical properties):

- `DEVICE_LOCAL` in VRAM, fast for GPU, possibly CPU-inaccessible
- `HOST_VISIBLE` CPU can map and memcpy into it
- `HOST_COHERENT` CPU writes visible to GPU without explicit flush
- `HOST_CACHED` fast CPU readback

Classic rule: meshes/textures/depth in `DEVICE_LOCAL` (upload via staging buffer), per-frame uniforms in `HOST_VISIBLE | HOST_COHERENT`

> ReBAR/SAM systems expose `DEVICE_LOCAL + HOST_VISIBLE` (mappable VRAM); VMA picks it automatically

Why VMA: picks the right memory type from usage flags, sub-allocates from big chunks (allocation count limited, sometimes 4096 per device), persistent mapping, BDA support

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

# Surface and swapchain

`SDL_Vulkan_CreateSurface(window, instance, &surface)` platform-specific window connection (SDL handles per-OS differences)

`vkCreateSwapchainKHR(device, &createInfo, nil, &swapchain)` create ring of presentable images

`vkGetSwapchainImagesKHR(device, swapchain, &count, images)` retrieve the images (driver decides the count)

**Present modes:**
- `FIFO_KHR` v-sync, guaranteed available, start here
- `MAILBOX_KHR` uncapped, tear-free, latest frame wins
- `IMMEDIATE_KHR` tears, fastest

`VK_ERROR_OUT_OF_DATE_KHR` (from acquire/present) surface resized: recreate swapchain, skip this frame

> Always pass the previous swapchain as `oldSwapchain` in the create info when recreating: avoids hitches and wasted memory

> **imageIndex ≠ frameIndex.** Swapchain image count (2-4, driver's choice) and frames in flight (your choice, usually 2) are different numbers. The compositor returns image indices in any order (0, 2, 1, 0...). Index per-image resources by `imageIndex`, per-frame resources by `frameIndex`

# Images and layouts

Every `VkImage` has a **layout**: abstract state describing how the image is arranged in memory and what operations are legal. GPUs physically reorder texels (tiling, compression) per use case; layout transitions tell the driver to reshuffle

- `UNDEFINED` contents garbage; valid transition source when previous data doesn't matter; always the state after creation
- `ATTACHMENT_OPTIMAL` written as color/depth attachment (1.3 unified color + depth)
- `SHADER_READ_ONLY_OPTIMAL` sampled in shader
- `TRANSFER_SRC/DST_OPTIMAL` copy source / destination
- `PRESENT_SRC_KHR` ready for the presentation engine

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

> Forgotten layout transition = #1 cause of "works on my GPU, breaks on yours". Validation catches it

`vkCreateImageView(device, &createInfo, nil, &view)` images are never used raw: views select format, mip range, layers

# Synchronization

Three primitives, three different jobs:

## Fences : GPU signals CPU

`vkQueueSubmit(queue, 1, &submit, fence)` GPU signals fence when this submission completes

`vkWaitForFences(device, 1, &fence, VK_TRUE, UINT64_MAX)` CPU blocks until signaled

`vkResetFences(device, 1, &fence)` back to unsignaled

> Use: "is the GPU done with frame N-2's resources so I can reuse them?" Create with `SIGNALED_BIT` so frame 0 doesn't deadlock

## Semaphores : GPU signals GPU

Binary semaphores order GPU work against GPU work; CPU cannot wait on them

Use: gate presentation. Submit waits on `presentSem` (image acquired) and signals `renderSem` (rendering done); present waits on `renderSem`

> **The two-semaphore indexing trap:** `presentSemaphores[frameIndex]` (acquire doesn't know the image index yet) but `renderSemaphores[imageIndex]` (present does). Using frameIndex for both = subtle race

> Timeline semaphores replace fences + binary semaphores with one counter object: cleaner, less universal

## Pipeline barriers : ordering within command buffers

`vkCmdPipelineBarrier2(cb, &dependencyInfo)` recorded command, not an object. Also performs image layout transitions

Four critical fields per barrier:
- `srcStageMask` which stages must finish first
- `srcAccessMask` which writes must become **available** (drained from writer's cache)
- `dstStageMask` which stages must wait
- `dstAccessMask` which reads need writes **visible** (loaded into reader's cache)

> Beginner shortcut: `ALL_COMMANDS_BIT` + `MEMORY_READ | MEMORY_WRITE` everywhere is correct but serializes the pipeline; tighten later

> Run with **synchronization validation** (vkconfig preset) at least once per feature: catches bugs that happen to work on your GPU

# Buffers

`vmaCreateBuffer(...)` create buffer + allocation in one call (see VMA pattern above)

Usage flags: `VERTEX_BUFFER_BIT`, `INDEX_BUFFER_BIT`, `TRANSFER_SRC/DST_BIT`, `SHADER_DEVICE_ADDRESS_BIT`

**Staging upload** (for DEVICE_LOCAL data):

```
create staging buffer (HOST_VISIBLE) + destination buffer (DEVICE_LOCAL)
memcpy data into staging's mapped pointer
one-time command buffer: vkCmdCopyBuffer(cb, staging, dst, 1, &region)
submit + wait fence, destroy staging
```

## Buffer device address (BDA)

`vkGetBufferDeviceAddress(device, &info)` get buffer's 64-bit GPU address

Pass the address via push constant, dereference in the shader like a C pointer: no descriptor sets, no bindings for buffers

```slang
[shader("vertex")]
VSOutput main(VSInput input, uniform ShaderData *shaderData) {
    float4x4 m = shaderData->model[instanceIndex];
    ...
}
```

> Gotcha: CPU and GPU struct layouts must match. Enable `scalarBlockLayout` (1.2 core) and write identical structs on both sides; otherwise std140-ish padding rules bite (especially vec3 and arrays)

# Descriptors

Handles describing shader resources to a pipeline. Vanilla Vulkan trio:
- **DescriptorSetLayout** the interface ("slot 0 = uniform buffer, slot 1 = sampled image")
- **DescriptorPool** memory the sets are allocated from
- **DescriptorSet** the instance (actual handles), bound before drawing

> With BDA handling buffers, descriptors only remain necessary for **textures** (no "image device address" yet)

## Descriptor indexing (bindless)

One big descriptor set with N texture slots, filled once, bound once per frame; per-draw you pass an index (push constant, instance attribute...)

```slang
Sampler2D textures[];  // unbounded array
float3 color = textures[NonUniformResourceIndex(materialIndex)].Sample(uv).rgb;
```

`NonUniformResourceIndex` required when threads in a warp may use different indices (e.g. index from per-fragment data)

# Shaders : SPIR-V and Slang

Vulkan consumes **SPIR-V** (binary IR), generated from GLSL (`glslc`), HLSL (DXC), or **Slang**

Why Slang:
- All stages in one file: `[shader("vertex")]` / `[shader("fragment")]` attributes, shared struct definitions
- First-class pointers → perfect fit for BDA
- Emits SPIR-V/HLSL/GLSL/Metal/CUDA; embeddable as a library for hot reload

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

> `uniform ShaderData *sd` = the BDA pointer, passed from the app as a push constant

`vkCreateShaderModule(device, &createInfo, nil, &module)` wrap SPIR-V blob for pipeline creation

# Pipelines

`vkCreateGraphicsPipelines(device, cache, 1, &createInfo, nil, &pipeline)` bake everything into one immutable object:

- Vertex input layout, input assembly (topology)
- Shader stages
- Rasterization (cull, polygon mode), multisample, depth/stencil, blend state
- Pipeline layout (descriptor set layouts + push constant ranges)
- Attachment formats (replaces render pass with dynamic rendering)

> Frozen state = driver can fully specialize shaders. Consequence: different blend mode = different pipeline; real renderers have hundreds (hence pipeline caches/libraries)

**Dynamic without a new pipeline:** viewport, scissor (always); more with `VK_EXT_extended_dynamic_state3` / `VK_EXT_shader_object`

`vkCreatePipelineLayout(...)` separate object because many pipelines share one resource interface

`vkCmdPushConstants(cb, layout, stages, offset, size, data)` inline ≥128 bytes of per-draw data into the command buffer: cheapest parameter path, perfect for BDA pointers / instance indices / material IDs

# Command buffers

> **CPU timeline vs GPU timeline:** every `vkCmd*` call *records* work, it doesn't execute it. Execution happens after submit, when the GPU gets to it

`vkCreateCommandPool(device, &createInfo, nil, &pool)` pool = cheap block allocator, tied to one queue family, **one thread at a time** (one pool per recording thread)

`vkAllocateCommandBuffers(device, &allocInfo, &cb)` get command buffer from pool

`vkBeginCommandBuffer(cb, &beginInfo)` start recording (implicitly resets with the right pool flag)

`vkEndCommandBuffer(cb)` finish recording

`vkQueueSubmit(queue, 1, &submitInfo, fence)` submit for execution

```
// LIFECYCLE
Initial → (begin) → Recording → (end) → Executable → (submit) → Pending
                                             ↑                      ↓
                                          (reset) ←──── (work complete, fence knows)
```

> Never re-record a Pending command buffer (GPU still reading it): that's what the per-frame fence wait guarantees

# Textures : KTX

PNG decode + blit-generated mipmaps works but is slow and wastes VRAM. **KTX2 + Basis Universal**:

- Stores natively compressed GPU formats (BCn/ASTC/ETC): 4-8× less VRAM
- Mipmaps baked in, file memcpys straight into staging
- libktx transcodes to the best format per device

`ktxTexture2_CreateFromNamedFile(...)` + `ktxTexture2_TranscodeBasis(...)` load + pick GPU format

Upload = staging buffer + `vkCmdCopyBufferToImage` (one region per mip) + the two barriers (see image layouts)

`vkCreateSampler(device, &createInfo, nil, &sampler)` filtering, addressing, anisotropy, LOD clamps: **separate object**, one sampler serves many images

> 3-channel (RGB) formats often unsupported: use RGBA. OpenGL silently padded; Vulkan just fails

# Frames in flight

While GPU renders frame N, CPU records frame N+1, monitor shows frame N-1. `maxFramesInFlight = 2` is the sweet spot (3 smooths spikes, more = input latency)

**Duplicate per frame in flight** (CPU and GPU both touch):
- Command buffers, uniform/shader-data buffers, fences, present semaphores

**Don't duplicate** (GPU-only):
- Depth buffer, textures, vertex/index buffers, pipelines

> The frame-start fence wait is the natural CPU throttle: zero wait if GPU keeps up, blocks if it doesn't

# Render loop

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

Worth staring at:
- (1) without the fence wait, frames pile up unbounded and (3) would overwrite a buffer the GPU is reading
- (4a) transitions *from* `UNDEFINED` because the old swapchain contents are about to be overwritten anyway
- (5) one GPU completion event observed twice: fence (CPU throttle) + renderSem (presentation gate)

# Cleanup

`vkDeviceWaitIdle(device)` wait for all GPU work before destroying anything

Destroy in reverse creation order; every `vkCreate*`/`vmaCreate*` has a matching destroy. Swapchain-dependent resources (views, depth image) also die on every recreate

# Validation layers

Enable via `vkconfig` (SDK GUI) or env var; check spec violations, wrong layouts, sync hazards, shader OOB access

`VK_EXT_debug_utils` + callback route messages into your own log with severity filtering

> Validation clean but render wrong = logic bug (bad matrix, wrong attribute offset): reach for **RenderDoc** (per-draw GPU state inspection)

# Beginner mistakes

- Forgetting an image layout transition (validation screams)
- `vkCmd*` outside begin/end: segfault, no validation help
- Re-recording a Pending command buffer (fence not waited)
- Writing per-frame uniforms before the fence wait: flicker/corruption that vanishes under a debugger
- Ignoring `VK_SUBOPTIMAL_KHR` / `VK_ERROR_OUT_OF_DATE_KHR` return codes on acquire/present
- Mismatched CPU/GPU struct layout (garbage in shaders, especially vec3/arrays) → `scalarBlockLayout`
- Recreating the swapchain without `oldSwapchain`
- Not enabling the 1.3 feature structs at device creation
- Treating `imageIndex` and `frameIndex` as the same thing

# Learning order

1. Instance + device + queue: print the GPU name, verify 1.3
2. Swapchain + clear color (no shaders): ~500 lines, teaches 60% of Vulkan
3. Hardcoded triangle (positions in shader, no vertex buffer)
4. Vertex + index buffer via VMA, mesh from tinyobjloader
5. Per-frame shader data via BDA + push constants: first contact with frames in flight
6. Depth buffer
7. Textures via KTX (staging, transitions, sampler)
8. Descriptor indexing: bindless texture array
9. Resize handling (swapchain + depth recreation)
10. Tighten barriers from "everything everywhere" to minimal; run sync validation
11. Second pipeline + second mesh: stress-test your abstractions

Past that: pipeline caching, render graphs, GPU-driven rendering, mesh shaders, raytracing

# Resources

- **Vulkan Docs Site** combined spec + Khronos tutorial + samples index
- **Sascha Willems' samples repo** canonical reference implementations
- **vkguide.dev** complementary modern tutorial
- **vulkan.gpuinfo.org** real-hardware feature/format/limit database
- **RenderDoc** frame debugger
- **vkconfig** validation layer GUI
- **Arseny Kapoulkine, "Writing an Efficient Vulkan Renderer"** when performance time comes
