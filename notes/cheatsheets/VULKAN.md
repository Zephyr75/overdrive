# Vulkan — the object model, for OpenGL developers

> **Scope** the Vulkan 1.3 API: object hierarchy, memory, swapchain, image layouts, synchronisation, buffers and BDA, descriptors, pipelines, command buffers, the render loop. Assumes OpenGL knowledge — only what differs is covered.
>
> **Not here** the OpenGL side of each concept → `OPENGL.md`. What Overdrive's own Vulkan backend does with all of this, method by method → `../ENGINE_FLOW.md`. Hardware ray tracing extensions → `RAYTRACING.md` §5.
>
> **Source** [How to Vulkan in 2026](https://howtovulkan.com) (Sascha Willems).

---

## Contents

1. [Baseline: Vulkan 1.3](#baseline-vulkan-13)
2. [Libraries](#libraries)
3. [Object hierarchy](#object-hierarchy)
4. [Instance and device](#instance-and-device)
5. [Memory: VMA](#memory--vma)
6. [Surface and swapchain](#surface-and-swapchain)
7. [Images and layouts](#images-and-layouts)
8. [Synchronization](#synchronization)
9. [Buffers](#buffers)
10. [Descriptors](#descriptors)
11. [Shaders: SPIR-V and Slang](#shaders--spir-v-and-slang)
12. [Pipelines](#pipelines)
13. [Command buffers](#command-buffers)
14. [Textures: KTX](#textures--ktx)
15. [Frames in flight](#frames-in-flight)
16. [Render loop](#render-loop)
17. [Cleanup](#cleanup)
18. [Validation layers](#validation-layers)
19. [Beginner mistakes](#beginner-mistakes)
20. [Learning order](#learning-order)
21. [Resources](#resources)

---

OpenGL = giant state machine + smart driver doing memory/sync/state management behind your back. Vulkan exposes all of it as explicit objects: predictable performance, multithreadable command generation, ~1000 lines for a triangle.

> Everywhere Vulkan feels verbose, it's exposing something OpenGL was secretly doing for you

## Baseline: Vulkan 1.3

Target **Vulkan 1.3**, enable these core features on the device (each kills a category of boilerplate):

- `dynamicRendering` no more render pass + framebuffer objects: describe attachments at draw time
- `bufferDeviceAddress` buffers become raw 64-bit pointers in shaders: no buffer descriptors
- `descriptorIndexing` one giant bindless texture array: no per-material descriptor sets
- `synchronization2` cleaner barrier API, harder to misuse

> "Core" still means opt-in: enable via `VkPhysicalDeviceVulkan1{2,3}Features` chained into device creation. Forgetting them causes confusing "extension not enabled" validation errors

## Libraries

- **Volk** loads Vulkan function pointers (the GLAD equivalent)
- **VMA** (Vulkan Memory Allocator) memory management, basically mandatory
- **SDL** window + surface creation (broadest platform support; GLFW also works)
- **GLM** math
- **Slang** shader language → SPIR-V
- **KTX-Software** GPU texture format loading
- **tinyobjloader** mesh loading

## Object hierarchy

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

## Instance and device

### Instance

`vkCreateInstance(&createInfo, nil, &instance)` create instance: app info + instance extensions + layers

> **Instance** knows about *Vulkan* (loader, surface extensions, debug utils). **Device** knows about *your GPU* (features, queues). Instance extensions are global; device extensions live on a GPU

### Physical device selection

`vkEnumeratePhysicalDevices(instance, &count, devices)` list GPUs

`vkGetPhysicalDeviceProperties(physicalDevice, &props)` name, type (discrete/integrated), limits, API version

`vkGetPhysicalDeviceFeatures2(physicalDevice, &features)` query supported features (chain 1.2/1.3 feature structs)

> PhysicalDevice = read-only capability handle. Device (logical) = created with the features + queues you want. Check `vulkan.gpuinfo.org` for real-world feature support

### Queues

GPU exposes queues grouped into **families**; each family advertises support: graphics, compute, transfer, present

`vkGetPhysicalDeviceQueueFamilyProperties(physicalDevice, &count, families)` list families

`vkGetDeviceQueue(device, familyIndex, 0, &queue)` get queue handle after device creation

> On most desktop GPUs family 0 supports everything: use it. Queues in the same family are equivalent. Command pools are tied to one family

### Logical device

`vkCreateDevice(physicalDevice, &createInfo, nil, &device)` create with queue create infos + device extensions (`VK_KHR_swapchain`) + enabled features

```c
// FULL FEATURE CHAIN
VkPhysicalDeviceVulkan13Features f13 { .sType = ..., .dynamicRendering = VK_TRUE, .synchronization2 = VK_TRUE };
VkPhysicalDeviceVulkan12Features f12 { .sType = ..., .pNext = &f13,
    .descriptorIndexing = VK_TRUE, .bufferDeviceAddress = VK_TRUE, .scalarBlockLayout = VK_TRUE };
VkDeviceCreateInfo ci { .sType = ..., .pNext = &f12, ... };
```

## Memory : VMA

GPU exposes **memory heaps** (physical pools: VRAM, system RAM) containing **memory types** (logical properties):

> **Heap = where the memory physically is. Type = what you are allowed to do with it.** One heap exposes several types, so "VRAM" may appear twice — once device-local only, once device-local *and* host-visible

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

## Surface and swapchain

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

## Images and layouts

### Four objects, four different questions

Reading a texture in Vulkan involves four things. OpenGL fused them into one `GLuint`, which is why the split feels like bureaucracy at first — but each answers a genuinely separate question:

| object | answers | analogy |
|---|---|---|
| `VkImage` | *where are the pixels* | the storage |
| `VkImageView` | *which* pixels, interpreted as *what type* | a window onto the storage |
| `VkSampler` | *how* to read them | the filtering rulebook |
| layout | how the pixels are *physically arranged right now* | no OpenGL equivalent |

> The one to internalise: **image ≠ view**. You never bind an image; you bind a view of one. The image is memory, the view is an interpretation of it

### Layouts

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

### Image views

`vkCreateImageView(device, &createInfo, nil, &view)` images are never used raw: a view selects

- **view type** 2D, 2D_ARRAY, CUBE, 3D — how the layers are addressed
- **format** can reinterpret the same bits (UNORM vs SRGB)
- **subresource range** which mip levels, which array layers, which **aspect** (color / depth / stencil)

> Two views of the *same image* is a normal, load-bearing pattern, not a hack. A cube render target needs exactly that: a **2D_ARRAY view to render into** (a geometry stage routes triangles to the six layers) and a **CUBE view to sample from**. You cannot attach a cube view, and cannot sample an array view with a direction vector — so you make both, over one allocation

### Attachments, targets, framebuffers

Vocabulary that trips people, partly because it predates dynamic rendering:

- `attachment` an image view a pass renders into or reads. **Color / depth / stencil attachments** by what they hold; **resolve attachments** receive the MSAA resolve; **input attachments** are read by a later subpass
- `render target` one attachable image. D3D's word; means a single surface you draw into, as opposed to one you sample
- `framebuffer` the *set* of attachments bound for a pass — a container, not storage

> **`VkFramebuffer` no longer exists under dynamic rendering.** It was an immutable object binding a `VkRenderPass` to specific image views; `VkRenderingInfo`, filled fresh at `vkCmdBeginRendering`, is what replaced it. So "framebuffer" in modern Vulkan is a *concept* (the attachment set) rather than an object you create

> **"Backbuffer" is a double-buffering word and doesn't survive contact with a swapchain.** There is no fixed "back" image: you acquire whichever of N images is free, render, present. Say "the swapchain image for this frame"

## Synchronization

Three primitives, three different jobs:

### Fences : GPU signals CPU

`vkQueueSubmit(queue, 1, &submit, fence)` GPU signals fence when this submission completes

`vkWaitForFences(device, 1, &fence, VK_TRUE, UINT64_MAX)` CPU blocks until signaled

`vkResetFences(device, 1, &fence)` back to unsignaled

> Use: "is the GPU done with frame N-2's resources so I can reuse them?" Create with `SIGNALED_BIT` so frame 0 doesn't deadlock

### Semaphores : GPU signals GPU

Binary semaphores order GPU work against GPU work; CPU cannot wait on them

Use: gate presentation. Submit waits on `presentSem` (image acquired) and signals `renderSem` (rendering done); present waits on `renderSem`

> **The two-semaphore indexing trap:** `presentSemaphores[frameIndex]` (acquire doesn't know the image index yet) but `renderSemaphores[imageIndex]` (present does). Using frameIndex for both = subtle race

> Timeline semaphores replace fences + binary semaphores with one counter object: cleaner, less universal

### Pipeline barriers : ordering within command buffers

`vkCmdPipelineBarrier2(cb, &dependencyInfo)` recorded command, not an object

**One barrier does three jobs at once**, and this is the part worth internalising:

1. **Execution dependency — ordering.** Everything in `srcStageMask` recorded *before* this point finishes before anything in `dstStageMask` recorded *after* it starts
2. **Memory dependency — cache flushing.** GPU caches are *not* coherent between stages. `srcAccessMask` makes writes **available** (flushed out of the writer's cache); `dstAccessMask` makes them **visible** (pulled into the reader's cache)
3. **Layout transition.** `oldLayout → newLayout`, the physical re-tiling

Jobs 2 and 3 happen *because* you expressed job 1 — the transition is scheduled inside the execution dependency. Which is why a barrier with the right layouts but sloppy stage masks still renders garbage.

> "The write finished" and "the reader can see it" are **different claims**. Ordering alone is not enough; you have to ask for both, which is what the two access masks are for

A worked example — handing a finished shadow map to the pass that samples it:

```
oldLayout DEPTH_ATTACHMENT_OPTIMAL  →  newLayout SHADER_READ_ONLY_OPTIMAL
src  LateFragmentTests / DepthStencilAttachmentWrite
dst  FragmentShader    / ShaderSampledRead
```

Reads as: *the depth writes from the late-fragment-test stage must complete and be flushed; then re-tile the image for sampling; then the fragment shader may read it.*

> Beginner shortcut: `ALL_COMMANDS_BIT` + `MEMORY_READ | MEMORY_WRITE` everywhere is correct but serializes the pipeline; tighten later

> Run with **synchronization validation** (vkconfig preset) at least once per feature: catches bugs that happen to work on your GPU

### Synchronization2 : what the `2` means

`VK_KHR_synchronization2`, core in **1.3**. A redesign of the same concepts, not new capability — but every `*2` name you see comes from it:

| | 1.0 | synchronization2 |
|---|---|---|
| mask width | 32-bit `VkPipelineStageFlags`, out of bits | **64-bit** `…Flags2` — which is how ray tracing / mesh shader / video stages could be added at all |
| stage masks | **one pair for the whole call**, covering every barrier in it | **per barrier** |
| "nothing" | `TOP_OF_PIPE` / `BOTTOM_OF_PIPE` + access `0`, easy to get backwards | explicit `STAGE_2_NONE` / `ACCESS_2_NONE` |
| barrier arguments | three separate arrays (memory, buffer, image) | one `VkDependencyInfo` holding all three |
| submit | `vkQueueSubmit` + a parallel array of wait stages | `vkQueueSubmit2`, stage mask attached to each semaphore |

> The per-barrier stage masks are the practical win. Under 1.0, batching two barriers into one call forced the **union** of their stage masks, dragging a cheap barrier up into an expensive one's scope

> Use it wholesale. Mixing `vkCmdPipelineBarrier2` with the old `vkQueueSubmit` works and is a common half-migration, but then `VkSemaphoreSubmitInfo`'s per-semaphore stage mask is not available to you

## Buffers

`vmaCreateBuffer(...)` create buffer + allocation in one call (see VMA pattern above)

Usage flags: `VERTEX_BUFFER_BIT`, `INDEX_BUFFER_BIT`, `TRANSFER_SRC/DST_BIT`, `SHADER_DEVICE_ADDRESS_BIT`

**Staging upload** (for DEVICE_LOCAL data):

> Staging exists because the fastest memory is usually the memory the CPU cannot write to. You allocate a second, host-visible buffer, memcpy into that, and ask the GPU to copy across — the copy runs at full bandwidth, and the staging buffer is freed straight after

```
create staging buffer (HOST_VISIBLE) + destination buffer (DEVICE_LOCAL)
memcpy data into staging's mapped pointer
one-time command buffer: vkCmdCopyBuffer(cb, staging, dst, 1, &region)
submit + wait fence, destroy staging
```

### Buffer device address (BDA)

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

**Four things must all be true, and missing any one fails differently:**

| requirement | where | symptom if missing |
|---|---|---|
| `bufferDeviceAddress` device feature | `VkPhysicalDeviceVulkan12Features` | `vkGetBufferDeviceAddress` is invalid — validation error |
| `VMA_ALLOCATOR_CREATE_BUFFER_DEVICE_ADDRESS_BIT` on the allocator | `vmaCreateAllocator` | VMA omits `VK_MEMORY_ALLOCATE_DEVICE_ADDRESS_BIT` from the allocation; the address is undefined |
| `VK_BUFFER_USAGE_SHADER_DEVICE_ADDRESS_BIT` on the buffer | `VkBufferCreateInfo` | per-buffer opt-in; invalid for *that* buffer only |
| `scalarBlockLayout` + `-fvk-use-scalar-layout` | device feature + shader compile flag | compiles and runs, renders garbage — the worst one |

> The middle two are the ones that bite: the feature is documented everywhere, the *allocator* and *per-buffer* opt-ins are easy to miss because nothing complains until you dereference

## Descriptors

Handles describing shader resources to a pipeline. Vanilla Vulkan trio:
- **DescriptorSetLayout** the interface ("slot 0 = uniform buffer, slot 1 = sampled image")
- **DescriptorPool** memory the sets are allocated from
- **DescriptorSet** the instance (actual handles), bound before drawing

> With BDA handling buffers, descriptors only remain necessary for **textures**. That is not an oversight waiting to be fixed: you can take the address of a buffer because it is plain memory, but a sampled image is an opaque, retiled, possibly-compressed object with no meaningful address. **BDA replaces buffer descriptors; it cannot replace image descriptors**

### What a descriptor actually holds

For `COMBINED_IMAGE_SAMPLER` — the type you will use most — one slot is a **triple**, and the parts come from three different places:

```
(image view, sampler, image layout)
```

| part | what it contributes | decided when |
|---|---|---|
| image view | which pixels, as what type | view creation — several views may overlay one image |
| sampler | filter, address mode, anisotropy, LOD | sampler creation, independent of any image |
| layout | the arrangement the image will be in *when read* | a **promise**, kept by barriers elsewhere |

That third one is the subtle one. **The layout field in a descriptor write does not perform a transition** — it is a declaration that "whenever a shader reads through this descriptor, the image will already be in this layout". Writing `SHADER_READ_ONLY_OPTIMAL` and then sampling while the image is still `COLOR_ATTACHMENT_OPTIMAL` reads undefined data. Keeping that promise is the job of `vkCmdPipelineBarrier2` at the end of the pass that wrote the image.

> Consequence worth knowing: because the sampler is baked into the *write*, not the layout, the same image can appear in two descriptors with two different samplers — one linear-repeat for normal use, one nearest-clamp for a special case — at no memory cost. The image is referenced, not copied

`VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE` + `VK_DESCRIPTOR_TYPE_SAMPLER` are the split alternative: one sampler then serves many images, at the cost of two bindings and two indices per read. **Immutable samplers** are the third option — bake the sampler into the `VkDescriptorSetLayoutBinding` itself, so writes carry only the view. Only workable when a binding uses exactly one sampler.

### Pool sizing

`VkDescriptorPoolCreateInfo` wants two independent numbers, and mixing them up is a common first-time error:

- `maxSets` how many **sets** can be allocated from this pool
- `pPoolSizes` how many **descriptors of each type**, summed across all those sets

> A bindless renderer typically has `maxSets = 1` and a pool size in the hundreds — one set, many descriptors in it. That looks wrong until you notice the two numbers count different things

### Descriptor indexing (bindless)

One big descriptor set with N texture slots, filled once, bound once per frame; per-draw you pass an index (push constant, instance attribute...)

```slang
Sampler2D textures[];  // unbounded array
float3 color = textures[NonUniformResourceIndex(materialIndex)].Sample(uv).rgb;
```

`NonUniformResourceIndex` required when threads in a warp may use different indices (e.g. index from per-fragment data)

## Shaders : SPIR-V and Slang

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

## Pipelines

`vkCreateGraphicsPipelines(device, cache, 1, &createInfo, nil, &pipeline)` bake everything into one immutable object:

- Vertex input layout, input assembly (topology)
- Shader stages
- Rasterization (cull, polygon mode), multisample, depth/stencil, blend state
- Pipeline layout (descriptor set layouts + push constant ranges)
- Attachment formats (replaces render pass with dynamic rendering)

> Frozen state = driver can fully specialize shaders. Consequence: different blend mode = different pipeline; real renderers have hundreds (hence pipeline caches/libraries)

> The intuition: a pipeline is **a compiled shader plus every piece of GPU state it was compiled to assume**. OpenGL's `glEnable(GL_BLEND)` could invalidate a driver's shader specialisation at any moment, so the driver re-checked and sometimes recompiled mid-frame — the notorious hitch. Vulkan makes you name the combinations up front, so nothing is decided during the frame

**Dynamic without a new pipeline:** viewport, scissor (always); more with `VK_EXT_extended_dynamic_state3` / `VK_EXT_shader_object`

`vkCreatePipelineLayout(...)` separate object because many pipelines share one resource interface

`vkCmdPushConstants(cb, layout, stages, offset, size, data)` inline a few bytes of per-draw data into the command buffer: cheapest parameter path, perfect for BDA pointers / instance indices / material IDs

> **The budget is small and shared.** `maxPushConstantsSize` is only guaranteed to be **128 bytes** — that is the whole range, across all stages that declare it, not per stage. Plenty for two 64-bit BDA pointers; nowhere near enough for a matrix set, which is exactly why the pointers are what you push

## Command buffers

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

## Textures : KTX

PNG decode + blit-generated mipmaps works but is slow and wastes VRAM. **KTX2 + Basis Universal**:

- Stores natively compressed GPU formats (BCn/ASTC/ETC): 4-8× less VRAM
- Mipmaps baked in, file memcpys straight into staging
- libktx transcodes to the best format per device

`ktxTexture2_CreateFromNamedFile(...)` + `ktxTexture2_TranscodeBasis(...)` load + pick GPU format

Upload = staging buffer + `vkCmdCopyBufferToImage` (one region per mip) + the two barriers (see image layouts)

`vkCreateSampler(device, &createInfo, nil, &sampler)` filtering, addressing, anisotropy, LOD clamps: **separate object**, one sampler serves many images

A sampler **references no image at all** — it is pure policy, which is why a handful of them covers a whole renderer:

| field | decides |
|---|---|
| `magFilter` / `minFilter` | linear or nearest when a texel is bigger / smaller than a pixel |
| `mipmapMode` | how to blend between mip levels |
| `addressModeU/V/W` | behaviour outside `[0,1]`: repeat, clamp-to-edge, clamp-to-border |
| `borderColor` | what clamp-to-border returns |
| `anisotropyEnable` / `maxAnisotropy` | extra samples along an elongated footprint |
| `minLod` / `maxLod` | which mip levels are reachable |
| `compareEnable` / `compareOp` | hardware depth comparison, for shadow maps |

> Anisotropy needs **mipmaps to matter**. Its real job is picking a sharper mip than isotropic LOD selection would; with a single mip level there is no LOD to pick and it only trims a little aliasing. Turning it on before generating mip chains buys almost nothing

> `borderColor` is a good illustration of the separation: "outside the shadow map means fully lit" is a *sampling* decision, encoded in the sampler, with nothing to do with the image or its view

> 3-channel (RGB) formats often unsupported: use RGBA. OpenGL silently padded; Vulkan just fails

## Frames in flight

While GPU renders frame N, CPU records frame N+1, monitor shows frame N-1. `maxFramesInFlight = 2` is the sweet spot (3 smooths spikes, more = input latency)

**Duplicate per frame in flight** (CPU and GPU both touch):
- Command buffers, uniform/shader-data buffers, fences, present semaphores

**Don't duplicate** (GPU-only):
- Depth buffer, textures, vertex/index buffers, pipelines

> The frame-start fence wait is the natural CPU throttle: zero wait if GPU keeps up, blocks if it doesn't

## Render loop

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

## Cleanup

`vkDeviceWaitIdle(device)` wait for all GPU work before destroying anything

Destroy in reverse creation order; every `vkCreate*`/`vmaCreate*` has a matching destroy. Swapchain-dependent resources (views, depth image) also die on every recreate

## Validation layers

Enable via `vkconfig` (SDK GUI) or env var; check spec violations, wrong layouts, sync hazards, shader OOB access

`VK_EXT_debug_utils` + callback route messages into your own log with severity filtering

> Validation clean but render wrong = logic bug (bad matrix, wrong attribute offset): reach for **RenderDoc** (per-draw GPU state inspection)

## Beginner mistakes

- Forgetting an image layout transition (validation screams)
- `vkCmd*` outside begin/end: segfault, no validation help
- Re-recording a Pending command buffer (fence not waited)
- Writing per-frame uniforms before the fence wait: flicker/corruption that vanishes under a debugger
- Ignoring `VK_SUBOPTIMAL_KHR` / `VK_ERROR_OUT_OF_DATE_KHR` return codes on acquire/present
- Mismatched CPU/GPU struct layout (garbage in shaders, especially vec3/arrays) → `scalarBlockLayout`
- Recreating the swapchain without `oldSwapchain`
- Not enabling the 1.3 feature structs at device creation
- Treating `imageIndex` and `frameIndex` as the same thing
- BDA set up in one place only: the device feature *and* the VMA allocator flag *and* the buffer usage flag are all required
- Assuming a descriptor's `imageLayout` performs the transition — it is a promise a barrier has to keep
- Pushing more than 128 bytes of push constants and only finding out on someone else's GPU
- Enabling anisotropy on textures that have no mip chain, then wondering why nothing looks different

## Learning order

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

## Resources

- **Vulkan Docs Site** combined spec + Khronos tutorial + samples index
- **Sascha Willems' samples repo** canonical reference implementations
- **vkguide.dev** complementary modern tutorial
- **vulkan.gpuinfo.org** real-hardware feature/format/limit database
- **RenderDoc** frame debugger
- **vkconfig** validation layer GUI
- **Arseny Kapoulkine, "Writing an Efficient Vulkan Renderer"** when performance time comes
