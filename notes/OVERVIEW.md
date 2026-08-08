# Overdrive, end to end

The whole engine in one read: what the layers are, what happens in what order,
and how data actually gets from a Go struct into a shader.

> **Scope** the mental model. Enough to know where any given thing lives and why
> it happens when it does.
>
> **Not here** the details. Every section ends with a pointer to the document
> that goes deeper.
>
> **On duplication** the rest of `notes/` deliberately never repeats itself.
> This file is the exception — it is the on-ramp, and it restates things so you
> can read it start to finish. When it disagrees with the others, they win.

---

## 1. The shape of it

Overdrive is a Go game engine rendering on Vulkan 1.3. The layers only ever
depend downwards:

```mermaid
graph TD
    M["main.go<br/><i>builds the app, loads a scene</i>"] --> C["core/<br/><i>window, the frame loop</i>"]
    C --> S["scene/ · ecs/ · input/ · physics/<br/><i>meshes, lights, camera, gameplay</i>"]
    S --> R["renderer/<br/><i>the Backend interface + opaque handles</i>"]
    C --> R
    R -.->|"implemented by"| V["vulkan/<br/><i>the only package that says vk.*</i>"]

    style R fill:#553c9a,color:#e2e8f0
    style V fill:#2b6cb0,color:#e2e8f0
```

**One rule holds the whole thing together:**

> Nothing above `renderer/` may import a graphics API.

`scene/` owns `renderer.MeshHandle`, `renderer.TextureHandle` — opaque integers
that only `vulkan/` knows how to interpret. That is why `go test ./...` runs
with no GPU, and why the Vulkan object graph stays in one package.

There is exactly one backend. The abstraction is kept for that rule, not for
portability.

→ `ARCHITECTURE.md` for the package-by-package map.

---

## 2. Startup, in order

Order matters here more than anywhere else — each step needs the one before it.

```mermaid
graph LR
    A["settings.Load"] --> B["vulkan.New()"]
    B --> C["glfw.Init<br/>ConfigureWindow<br/>CreateWindow"]
    C --> D["Backend.Init"]
    D --> E["CreateShader ×5"]
    E --> F["scene.NewScene"]
```

`Backend.Init` is the big one. Inside it, again strictly ordered:

```
instance → surface → physical device → queue family → logical device
  → VMA allocator          (needs the device)
  → swapchain              (needs the surface, and MSAA sample count)
  → command pool
  → per-frame data         (needs the pool AND the allocator)
  → samplers
  → descriptors            (layout, pool, one set)
  → pipeline layout        (needs the descriptor layout)
  → default textures       (needs samplers AND descriptors)
```

Two things to notice:

- **Per-frame data** is one function creating four things — command buffer,
  fence, semaphore, uniform ring — because they share a lifetime, not a
  subsystem. Everything that exists *once per frame in flight* is built there.
- **Samplers and descriptors are independent.** Neither uses the other. They
  only meet later, when a texture is written into a descriptor (§4).

Then `App.Run` loads five shader sets — `forward`, `depth`, `depth_cube`, `ui`,
`skybox` — and `scene.NewScene` parses XML, uploads meshes and textures, and
allocates the shadow maps.

→ `ENGINE_FLOW.md` §2.

---

## 3. One frame

```mermaid
graph TD
    P["physics · mesh re-upload · input"] --> BF["BeginFrame"]
    BF --> S1["shadow pass — sun<br/><i>depth only</i>"]
    S1 --> S2["shadow pass — point light<br/><i>depth cube, one layered draw</i>"]
    S2 --> MP["main pass"]
    MP --> SK["skybox"] --> SC["scene meshes"] --> UI["UI overlay"]
    UI --> EF["EndFrame<br/><i>submit + present</i>"]
    EF --> P

    style BF fill:#276749,color:#e2e8f0
    style EF fill:#9b2c2c,color:#e2e8f0
```

Each pass is the same three beats: `BeginPass` → bind uniforms → draw → `EndPass`.

What the green and red boxes actually do:

| | |
|---|---|
| **`BeginFrame`** | wait on this slot's fence *(the CPU throttle)* · acquire a swapchain image · reset the ring · flush any staged texture uploads · begin the command buffer · bind the one descriptor set |
| **`EndFrame`** | barrier the image to present layout · end the command buffer · **submit** · present · advance the frame slot |

The shadow budget is fixed at load: the first directional light gets a 2D map,
the first point light gets a cube. Other lights still light the scene, they just
cast nothing.

→ `ENGINE_FLOW.md` §3.

---

## 4. How data reaches a shader

Two completely separate paths, and the split is forced rather than chosen.

```mermaid
graph LR
    subgraph BDA["buffer device address"]
        U["FrameUniforms 1184 B<br/>DrawUniforms 128 B"] --> RG["per-frame ring<br/>1 MiB, mapped"]
        RG --> PC["push constant<br/>2 × 64-bit address"]
    end
    subgraph DESC["descriptors"]
        T["textures"] --> DS["one descriptor set<br/>4 bindings"]
    end
    PC --> SH["shader"]
    DS --> SH
```

### Uniforms — by pointer

The blocks are memcpy'd into a mapped ring buffer, and their **GPU addresses**
go out as a 16-byte push constant. The shader dereferences them like C pointers.
No descriptors, no dynamic offsets, no binding.

They are split by **update frequency**, which is the whole reason it is cheap:

- `FrameUniforms` — camera, lights, shadow maps — published **once per pass**
- `DrawUniforms` — model matrix, material — sent **once per draw**

So a draw costs one 128-byte memcpy plus a 16-byte push. Before the split, all
1.3 KB went out on every draw.

This works because Go packs `float32`/`int32` structs exactly the way Vulkan's
*scalar layout* does — so both sides agree with no marshalling code. The only
rule left: **keep the field order in `renderer/uniforms.go` and `common.slang`
identical.**

Four things must all be true for BDA, and they fail differently: the device
feature, the VMA allocator flag, the per-buffer usage flag, and scalar layout.
Miss the last one and it compiles, runs, and renders garbage.

### Textures — by descriptor

You cannot take the address of an image. Images are opaque, retiled, possibly
compressed — so they need descriptors, and always will.

One set, four bindings, bound **once per frame**:

| binding | what | how many |
|---|---|---|
| 0 | material textures — bindless | 256 |
| 1 | cubemaps — bindless | 64 |
| 2 | the sun's shadow map — dedicated | 1 |
| 3 | point-light shadow cubes — dedicated | 4 |

"Bindless" means the shader indexes an array: `textures2D[DRAW.texOurTexture]`.
The engine translates a `TextureHandle` into a slot index on the CPU and writes
that integer into the uniform block. **The shader never receives a descriptor —
it receives an int.**

Bindings 2 and 3 are deliberately *not* bindless: PCF taps them 9–20× per
fragment, and some drivers re-fetch a dynamically-indexed descriptor on every
tap. That cost ~1.7× the frame time.

### What a descriptor actually holds

For the type used here, one slot is a **triple**:

```
(image view, sampler, image layout)
```

| part | answers | comes from |
|---|---|---|
| **image view** | *which* pixels, read as what type | view creation — several views can overlay one image |
| **sampler** | *how* to filter them | sampler creation, referencing no image at all |
| **layout** | how the pixels are arranged *when read* | a **promise**, kept by a barrier elsewhere |

That last one is the subtle bit. **The layout in a descriptor performs no
transition.** It declares "when a shader reads through this, the image will be
in this layout". A barrier has to make that true.

→ `cheatsheets/VULKAN.md` §7, §10 · `ENGINE_FLOW.md` §4.5.

---

## 5. The Vulkan machinery underneath

### Recording is not executing

This is the mental model that makes everything else click:

```mermaid
graph LR
    subgraph CPU["CPU — during the frame"]
        R1["vkCmd… ×~21 sites"] --> R2["command buffer<br/><i>inert bytes</i>"]
    end
    R2 --> SUB["QueueSubmit2<br/><i>in EndFrame</i>"]
    subgraph GPU["GPU — after the submit"]
        SUB --> EX["shadow passes, draws,<br/>barriers all execute"]
    end
```

Every `vkCmd*` call **records**. Nothing in the frame's command buffer runs
until `EndFrame` submits. The queue is touched in only three places:

| where | what |
|---|---|
| `EndFrame` | `QueueSubmit2` — **one submit carries the entire frame** |
| `EndFrame` | `QueuePresentKHR` |
| `immediateSubmit` | load-time texture uploads, which block the CPU until done |

But *host-side* work is immediate, not deferred: memcpy into mapped memory,
`vkUpdateDescriptorSets`, and lazy pipeline compilation all take effect the
moment they are called. That distinction is why the fence throttle exists — it
protects the *memory the commands point at*, not the commands.

The submit also does not wait for everything at once. Its semaphore wait is
scoped to `ColorAttachmentOutput`, so the shadow passes can start immediately
while only the swapchain colour writes wait for the image to arrive.

### Barriers

Nine of the ~21 recording sites are barriers — the work OpenGL did invisibly.
One `vkCmdPipelineBarrier2` does **three jobs at once**:

1. **ordering** — these stages finish before those stages start
2. **cache flushing** — make writes *available* (out of the writer's cache),
   then *visible* (into the reader's)
3. **layout transition** — the physical re-tiling

Jobs 2 and 3 happen *because* you expressed job 1. Right layouts with sloppy
stage masks still renders garbage.

> "The write finished" and "the reader can see it" are different claims.

The `2` suffix is **synchronization2** — a redesign of the same concepts giving
64-bit stage masks and per-barrier stage scoping. `QueueSubmit2` is its submit
counterpart; the engine uses the pair consistently.

### Frames in flight

Two of everything the CPU and GPU both touch: command buffer, fence, acquire
semaphore, uniform ring. While the GPU renders frame N, the CPU records N+1.

Two index spaces that are **not** interchangeable:

- `frameIndex` cycles 0..1 — selects command buffer, fence, ring
- `imageIndex` comes back from acquire — selects the swapchain image and its
  render semaphore

Present waits on the *image's* semaphore; the submit waits on the *frame's*.
Mixing them is a subtle race.

→ `cheatsheets/VULKAN.md` §8 · `ENGINE_FLOW.md` §4.11, §7.

---

## 6. Things that would silently break the image

Not crashes — wrong pictures. Each is a convention that must hold everywhere.

| convention | why |
|---|---|
| Main pass uses a **negative-height viewport** | Vulkan's clip space is y-down; the projections in `scene/` assume y-up. Flipping the viewport also flips winding, which is why CCW front faces stay correct |
| Shadow passes use a **positive** viewport and declare **CW** front faces | a shadow map is sampled, not presented, so it wants the other memory layout — and pays for it in winding |
| Every vertex stage calls `TO_VK_DEPTH` | the projections give clip z in `[-w, w]`; Vulkan clips to `[0, w]` |
| Uniform **field order** matches between Go and Slang | scalar layout means no marshalling — and no protection if the order drifts |
| Offscreen targets stay single-sampled | a later pass samples them, and these shaders cannot read a multisampled texture |

The first three are all leftovers of the OpenGL era, and all four workarounds
disappear together if the projections are rebuilt Vulkan-native — which is the
next planned change.

→ `ENGINE_FLOW.md` §5, and §6 for a symptom→file table.

---

## 7. Where to go next

| you want | read |
|---|---|
| how a frame is drawn, method by method | `ENGINE_FLOW.md` — start at §0 |
| where a symbol lives | `ARCHITECTURE.md` §5 |
| whether a feature exists, and why it is built that way | `FEATURES.md` |
| Vulkan concepts, engine-independent | `cheatsheets/VULKAN.md` |
| the theory behind the shading | `cheatsheets/PBR.md` |
| what is planned, in order | `tmp/BACKEND_DECISION.md` §9 |
| the next small task | `TODO.md` |
