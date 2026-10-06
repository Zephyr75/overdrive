# Glossary — the vocabulary, one line each

> **Scope** terms that come up constantly in `notes/` and `src/`. Where a term has an engine-specific meaning as well as a general one, both are given.
>
> **Not here** the reasoning behind any of them → `../RENDERER.md`, `../SYNCHRONIZATION.md`, `VULKAN.md`.

---

## 1. Devices and queues

| Term | Meaning |
| --- | --- |
| `instance` | the loader's handle on Vulkan; extensions and layers are chosen here, before any GPU |
| `physical device` | a GPU as the driver reports it. Read-only: query limits and features |
| `logical device` | your open connection to one physical device; everything else is created from it |
| `queue` | where commands are submitted. This engine uses one, graphics + present |
| `surface` | the window as Vulkan sees it |

## 2. The screen

| Term | Meaning |
| --- | --- |
| `swapchain` | the rotating 2–3 images the compositor displays; you draw into whichever is free |
| `backbuffer` | not a Vulkan object: this engine's name for *the swapchain image this frame acquired* (`renderer.Backbuffer`) |
| `acquire` | asking which swapchain image is free. Returns an index that rotates |
| `present` | handing a finished image back to be displayed |
| `frames in flight` | how far the CPU may run ahead (2 here). Each owns a command buffer, fence and arena. A separate rotation from the swapchain's, never indexed by its index |

## 3. Resources and memory

| Term | Meaning |
| --- | --- |
| `buffer` | linear bytes whose meaning is yours: vertices, indices, uniforms, indirect args |
| `image` | a typed grid the hardware understands: format, opaque tiling, layouts, attachable |
| `image view` | one mip, slice and aspect of an image. You bind views, never images |
| `image layout` | how an image is physically arranged right now; changes per use |
| `sampler` | the filtering rulebook: filter, mips, anisotropy, address mode, border, compare |
| `usage flags` | a promise made at creation so the driver can place the memory, not a runtime check |
| `attachment` | an image a pass renders into |
| `host-visible` / `device-local` | memory the CPU can map / VRAM with no CPU pointer (`LocationHost` / `LocationDevice`) |
| `staging buffer` | a host-visible buffer used only as the bridge to device-local memory |
| `transfer command` | a GPU copy. Both ends are Vulkan resources, never the CPU |
| `BDA` | buffer device address: a buffer's 64-bit GPU pointer, how every uniform reaches a shader here |
| `arena` | the per-frame buffer `Frame.Upload` memcpys into. Reset each frame, panics on overflow |
| `scalar layout` | the layout rule that makes Go and shader struct packing identical |

## 4. Shaders and pipelines

| Term | Meaning |
| --- | --- |
| `SPIR-V` | the compiled shader bytecode Vulkan consumes; authored here in Slang |
| `pipeline` | shaders plus all fixed-function state in one immutable object |
| `push constant` | a few bytes recorded with a draw. 32 here, holding four BDAs |
| `descriptor` | a resource as the shader sees it: a pointer plus the metadata to read it |
| `descriptor set` | a bound group of descriptors. This engine has one, bound once per frame |
| `bindless` | descriptors as an array the shader indexes at runtime, instead of rebinding per draw |
| `slot` | the index a resource holds in a bindless array; `Slot(Handle)` hands it out |
| `hot slot` | one of 4 dedicated descriptors indexed by a literal, for the shadow atlases |

## 5. Commands and sync

| Term | Meaning |
| --- | --- |
| `command buffer` | a recorded list of GPU commands. Recording is not executing |
| `submit` | handing a command buffer to a queue; when the GPU starts |
| `render pass` | a scoped block of drawing with fixed attachments. `Frame.Pass` here |
| `draw call` / `dispatch` | one geometry draw / one compute launch (`Groups` counts workgroups, not threads) |
| `indirect` | draw or dispatch arguments read by the GPU from a buffer |
| `barrier` | an ordering and visibility rule between two uses of a resource. Automatic here (`vulkan/barrier.go`) |
| `fence` / `semaphore` | GPU signals CPU / GPU signals GPU |
| `retire queue` | why `Destroy` does not free at once: a frame in flight may still use the resource |

## 6. Rendering

| Term | Meaning |
| --- | --- |
| `clip space` | vertex-stage output. OpenGL convention here (`z` in `[-w, w]`), hence `TO_VK_DEPTH` |
| `winding` | which vertex order is front-facing. A negative-height viewport flips it |
| `forward rendering` | shade each fragment against every light as it is drawn. What this engine does |
| `depth prepass` | depth first, so the forward pass shades each visible pixel once, compared with `EQUAL` |
| `MSAA` | N coverage/depth samples per pixel, shaded once per triangle; smooths geometric edges |
| `resolve` | averaging a multisampled image into a single-sample one (`ResolveModeAverage`) |
| `PCF` | percentage-closer filtering: several shadow comparisons averaged to soften the edge |
| `shadow atlas` | one depth texture carved into fixed rects shared by every light |
| `tile` | one light's rect in the atlas: one for a sun or spot, six for a point light |
| `movable` | a mesh marked `<movable>` or moved by code; decides static vs dynamic atlas |

## 7. Engine-specific

| Term | Meaning |
| --- | --- |
| `handle` | an opaque integer held above `renderer/` instead of a GPU object |
| `backend` | the one package allowed to import `vk.*` |
| `gutter` | the UI library the overlay is drawn with, CPU-rendered into a texture |
