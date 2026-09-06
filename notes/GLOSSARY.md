# GLOSSARY.md — the vocabulary, one line each

Terms that come up constantly in `notes/` and in `src/`, defined once. Where a
term has an engine-specific meaning as well as a general one, both are given.

For the reasoning behind any of these, follow the pointer: `ENGINE_FLOW.md` for
the backend contract, `cheatsheets/VULKAN.md` for the general object model,
`FEATURES.md` for why a choice was made.

---

## Devices and queues

| term | meaning |
|---|---|
| `instance` | the loader's handle on Vulkan itself — extensions and layers are chosen here, before any GPU is picked |
| `physical device` | a GPU as reported by the driver. Read-only: you query its limits and features, you do not use it to draw |
| `logical device` | your open connection to one physical device. Everything else is created from it |
| `queue` | where commands are submitted to run. This engine uses exactly one, graphics + present |
| `surface` | the window, as Vulkan sees it. Platform glue between GLFW and the swapchain |

## The screen

| term | meaning |
|---|---|
| `swapchain` | the rotating set of 2-3 images the compositor displays. You draw into whichever one is free |
| `backbuffer` | not a Vulkan object — this engine's name for *whichever swapchain image this frame acquired*, resolved per frame from `imageIndex` |
| `acquire` | asking the presentation engine which swapchain image is free. Returns an index, and the index rotates |
| `present` | handing a finished image back to be displayed |
| `frames in flight` | how many frames the CPU may run ahead. Each owns a command buffer, a fence and an upload arena, so frame N+1 records while N is still on the GPU |

> Frames in flight and swapchain images are **two separate rotations** of possibly different lengths. Indexing one by the other's index is a real bug — see `ENGINE_FLOW.md` §7

## Resources

| term | meaning |
|---|---|
| `buffer` | linear bytes, meaning is yours. Vertices, indices, uniforms, indirect args |
| `image` | a typed grid the hardware understands: format, opaque tiling, layouts, samplers, attachable |
| `image view` | a window onto an image — one mip, one slice, one aspect. You bind views, never images |
| `image layout` | how an image is *physically arranged right now*. Changes per use, because the GPU reshuffles texels for each one |
| `sampler` | the filtering rulebook: filter, mip mode, anisotropy, address mode, border colour, depth compare |
| `usage flags` | a promise made at creation so the driver can place the memory. Not a runtime permission check |
| `attachment` | an image a pass renders into. Only images can be attachments |

## Memory

| term | meaning |
|---|---|
| `host-visible` | GPU memory the CPU can map and memcpy into. `LocationHost` here |
| `device-local` | VRAM. Fastest for the GPU, no CPU pointer at all, so data moves in and out by copy. `LocationDevice` here |
| `staging buffer` | a host-visible buffer used purely as the bridge to device-local memory. Memcpy in, then copy across |
| `transfer command` | a GPU copy — `vkCmdCopyBuffer`, `vkCmdCopyImage`, and the two image↔buffer forms. **Both ends are always Vulkan resources; neither is ever the CPU** |
| `BDA` | buffer device address. A buffer's 64-bit GPU pointer, dereferenced in the shader like C. How every uniform reaches a shader here |
| `arena` | the per-frame scratch buffer `Frame.Upload` memcpys into, returning a BDA. Reset each frame, panics on overflow |
| `scalar layout` | the SPIR-V layout rule that makes Go struct packing and shader struct packing identical. Load-bearing — see `ENGINE_FLOW.md` §4.3 |

## Shaders and pipelines

| term | meaning |
|---|---|
| `SPIR-V` | the compiled shader bytecode Vulkan consumes. Authored here in Slang, built by `build_shaders.sh` |
| `pipeline` | shaders plus all fixed-function state baked into one immutable object: cull, winding, depth, blend, formats |
| `push constant` | a tiny block of bytes sent straight with a draw, no buffer needed. 32 bytes here, holding four BDAs |
| `descriptor` | a handle to a resource, as the shader sees it. A pointer plus the metadata to interpret it |
| `descriptor set` | a bound group of descriptors. This engine has exactly one, built at startup and rebound once per frame |
| `bindless` | descriptors as an array the shader indexes at runtime (`textures2D[DRAW.tex]`), instead of rebinding per draw |
| `hot slot` | this engine's opt-out from bindless: 4 dedicated descriptors indexed by a **literal**, because some drivers re-fetch a dynamically indexed descriptor on every tap |
| `slot` | the index a resource occupies in a bindless array. `Slot(Handle)` is the whole handle→shader translation |

## Passes and commands

| term | meaning |
|---|---|
| `command buffer` | a recorded list of GPU commands. Recording is not executing — nothing runs until submit |
| `submit` | handing a command buffer to a queue. This is when the GPU actually starts |
| `render pass` | a scoped block of drawing with fixed attachments. `Frame.Pass` here; a copy or dispatch inside one is a compile error |
| `draw call` | one command to render geometry through a pipeline |
| `dispatch` | the compute equivalent of a draw. `Groups` is **workgroups, not threads** — the local size lives in the shader |
| `indirect` | draw or dispatch arguments read by the GPU from a buffer, so the CPU never learns the count |
| `barrier` | an ordering and visibility rule between two uses of a resource. Tracked automatically here, in `vulkan/barrier.go` |
| `fence` | GPU signals CPU. Used to know a frame finished |
| `semaphore` | GPU signals GPU. Used to order acquire → render → present |

## Rendering

| term | meaning |
|---|---|
| `clip space` | where the vertex stage outputs. OpenGL convention here (`z` in `[-w, w]`), so every vertex stage calls `TO_VK_DEPTH` |
| `winding` | which triangle vertex order counts as front-facing. A negative-height viewport flips it, which is why screen and atlas pipelines disagree |
| `depth prepass` | draw depth only first, so the forward pass shades each visible fragment once. Compared with `EQUAL`, which is exact — the two passes must build their matrices identically |
| `forward rendering` | shade each fragment against every light as it is drawn. What this engine does |
| `MSAA resolve` | collapsing a multisampled image down to a single-sample one. Why `Backbuffer` is a *view*: the pass may render into the MSAA image and resolve into the swapchain |
| `PCF` | percentage-closer filtering — several shadow taps averaged to soften the edge |
| `shadow atlas` | one big depth texture carved into fixed rects, so every light's shadow shares one image and the pass count does not grow with the light count |
| `tile` | one light's rect within the atlas. A sun or spot takes one, a point light six |

## Engine-specific

| term | meaning |
|---|---|
| `handle` | an opaque integer the packages above `renderer/` hold instead of a GPU object. The backend interprets it in its own table |
| `backend` | the one package allowed to import `vk.*`. Everything above it is buildable without a GPU |
| `record` vs `submit` | the whole mental model: `Frame.Pass`, `Draw`, `Copy` only *write commands down*. The GPU runs them when the frame closes |
| `retire queue` | why a destroyed resource is not freed immediately — a frame still in flight may reference it. Freed after `framesInFlight` more frames |
| `movable` | a mesh the scene marked `<movable>` or moved by code. Decides static vs dynamic shadow atlas |
| `gutter` | the UI library the overlay is drawn with. A CPU-rendered image uploaded as a texture |
