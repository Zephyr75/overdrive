# ABSTRACTION_REVIEW.md — is the renderer decomposed well, and what to do next

Review of all of `src/`, 2026-08-01. Written in answer to two questions: is there a
more intuitive way to decompose the abstraction, and what would make the Vulkan
backend easier to hold in your head. Read alongside `ENGINE_FLOW.md` (what the
code does) and `GO_BACKEND.md` (why it is shaped this way).

**Verdict: the decomposition is good — better than most hand-rolled RHIs. Don't
restructure it.** There are three specific extensibility limits worth fixing, and
one of them blocks something already on the roadmap. Documentation is the bigger
win, and the reason recall is failing has a concrete cause.

---

## 1. Why the Vulkan methods are hard to place

This is not a decomposition problem. `renderer.Backend`'s 28 methods are organised
by **resource type** — textures, buffers, meshes, shaders, targets, draws. What you
are trying to recall is organised by **time**: what happens when, and how often.
Two different axes, and nothing in the code or the docs presents the second one.

The missing axis is **update frequency**:

| Frequency | Engine | Vulkan | OpenGL |
|---|---|---|---|
| Once, ever | `Init` | instance → device → VMA → swapchain → descriptors | `MakeContextCurrent`, enable depth/cull/blend |
| Once per resource | `LoadTexture`, `CreateMesh`, `CreateShader` | `VmaCreateImage` + staging + `immediateSubmit` | `glTexImage2D`, VAO+EBO, compile+link |
| Once per frame | `BeginFrame` / `EndFrame` | fence wait, acquire, reset+begin CB, bind set / submit + present | nothing / `SwapBuffers` |
| Once per pass (×3) | `BeginPass` / `EndPass` | `imageBarrier` + `CmdBeginRendering` + viewport | `glBindFramebuffer` + `glViewport` + `glClear` |
| Once per draw (~15) | `DrawMesh` | `getPipeline`, `pushUniforms` (ring + BDA push constant) | `UseProgram`, marshal 1600 B std140, bind 8 texture units |

Once that table is internalised the obscure names stop mattering:
`vkCmdPipelineBarrier2` is "the per-pass thing", `vkAcquireNextImageKHR` is "the
per-frame thing".

**Do not rename the Vulkan calls in your own wrappers.** Obscure as they are, they
are the names in every spec page and tutorial you will read; a private vocabulary
costs more than it saves.

This table exists nowhere in the docs today. `ENGINE_FLOW.md` §3 gives the
timeline and §4 gives the contract, but as separate sections, so reading one
means cross-referencing the other.

---

## 2. Three real limits

### 2.1 The interface can create depth targets but not colour targets

This is the one that actually bites.

```go
CreateShadowMap2D(w, h int) (FramebufferHandle, TextureHandle)
CreateShadowCubemap(w, h int) (FramebufferHandle, TextureHandle)
```

Both named for a *use*, not a *thing*. `vulkan/backend.go:731` proves it — any
non-zero target is assumed to be `shadowTargets[target]`, depth-only.

HDR / tone mapping / bloom is in the README roadmap and in `FEATURES.md`,
and it needs an offscreen **colour** target. The current interface cannot express
one. Same for a G-buffer, reflection probes, or post-processing of any kind.

Fix: one `CreateRenderTarget(spec RenderTargetSpec) (FramebufferHandle,
TextureHandle)` where the spec carries format, layers, cube-ness, and colour vs
depth. The two shadow methods become callers of it. `passKind` gains entries
driven by the spec rather than by a hardcoded enum.

### 2.2 `Uniforms` is a 1312-byte god-struct pushed per draw

Every feature grows it, and both backends pay per draw: GL hand-marshals 1600
bytes into a UBO for every draw (`opengl/uniforms.go:marshalStd140`), Vulkan
memcpys 1312 into the ring.

Look at what is actually in it. `View`, `Projection`, `ShadowMatrices[6]`,
`Lights[8]`, `ViewPos`, the shadow handles — all **per-frame**, identical across
every draw. Only `Model` and the seven `Mat*` / `Tex*` fields change per draw.
That is ~1200 bytes re-uploaded ~15 times a frame to say the same thing.

Split by frequency: `FrameUniforms` (bound once per frame) plus `DrawUniforms`
(~100 bytes, pushed per draw). On Vulkan the frame block becomes one ring entry
and the draw block fits in push constants outright — no BDA dereference for
material data at all. On GL it is two UBOs, and the per-draw marshal drops from
1600 bytes to ~100.

This also happens to be the most plausible lead on the ~10% Vulkan performance
gap that GPU-timer-less measurement could not attribute, since it directly
attacks the per-fragment BDA loads at `shaders/slang/common.slang:98`.

Rank this first. It is the change that makes every future feature cheaper
instead of more expensive.

### 2.3 Draws are per-object-kind

`DrawMesh` / `DrawSkybox` / `DrawFullscreenQuad` — three methods differing only in
vertex layout and indexed-vs-not. Adding a drawable kind (particles, debug lines,
instanced meshes) means a new interface method, a new `vertexLayout`, a new
pipeline slot.

`vertexLayout` is already an intrinsic property of the mesh, not of the call site,
so it belongs on `meshEntry`. Collapse to one `Draw(shader, mesh, u)`.

Lowest priority — three kinds works fine — but this is the seam that gets in the
way soonest once anything new becomes drawable.

---

## 3. What is genuinely good, leave alone

- **Opaque handles + backend-owned tables.** Clean, and the reason nothing above
  `renderer/` imports a graphics API.
- **Clears and viewports confined to `BeginPass`.** This is what makes two
  backends possible at all.
- **Lazy `getPipeline(shader, pass, layout)`** keyed on a 2D array — pipeline
  explosion handled without a cache or hashing.
- **Pass characteristics as four small pure functions** (`vertexInputState`,
  `frontFace`, `colorBlendState`, `renderingInfo`). Adding a pass kind means
  adding a row to four tables. Very easy to extend.
- **`opengl/uniforms_test.go` re-deriving std140 offsets from the generated
  GLSL.** The single best thing in the repo — the only reason the two backends
  cannot silently drift.
- **One descriptor set for the whole frame**, bindless arrays, dedicated shadow
  bindings, with the reasoning written down at `vulkan/backend.go:454`.

---

## 4. Documentation plan

**Status: done, 2026-08-01.** All four items implemented, plus a further pass
that moved every document except `README.md` and `CLAUDE.md` into `notes/`.
Item 3 was executed more conservatively than written — see the note under it.

Where the time is actually best spent:

1. **`ENGINE_FLOW.md` §0 — the frequency table from §1 above**, extended to all 28
   methods, three columns: engine method / VK calls / GL calls. One page. Replaces
   most of what §4 currently spends ten subsections on.
2. **A Vulkan object-lifetime diagram** — what owns what, what dies when.
   `Shutdown` (`vulkan/backend.go:523`) already encodes this in reverse creation
   order; make it a picture.
3. **Fold the C++-era `notes/` files.** `ABSTRACTION.md`, `BACKEND.md` and
   `ARCHITECTURE.md` describe an engine that no longer exists and three different
   abstraction plans that were not taken. They actively mislead. Keep
   `VULKAN.md`, `OPTIMISATION.md`, `PBR.md`, `ALGEBRA.md`; archive the rest.

   *As executed:* "archive the rest" was written before reading the files and was
   wrong — it would have archived `FEATURES.md` (the roadmap of record, linked
   from the README) and `RAYTRACING_PLAN.md` (also README-linked). What was
   archived instead: the superseded engine docs, plus the personal
   graphics-revision notes, which are not documentation of this engine at all.
   `README.md` in this directory is the index.
4. **Fix `README.md`** — still says `go/`, still says the C++ tree exists.

---

## 5. Housekeeping

Dead files, by non-comment line count:

| File | Total lines | Real lines |
|---|---|---|
| `ecs/ecs.go` | 142 | 1 |
| `physics/box_old.go` | 119 | 1 |
| `algorithms/wfc.go` | 1 | 1 (empty stub) |
| `physics/box.go` | 1 | 1 (empty stub) |

Delete them. They are four of the ~30 files in `src/`, and when navigating by
grep they are pure noise.

---

## 6. Suggested order

1. Documentation items 1 and 4, and delete the dead files. Cheap, immediate, and
   fixes the actual complaint.
2. Uniform split by frequency (§2.2). Biggest structural payoff, plausibly a
   performance win, and the guard test already exists to catch mistakes.
3. `CreateRenderTarget` (§2.1). Do it when starting HDR/bloom, not before — the
   spec will be better designed with a real second use case in hand.
4. Unified `Draw` (§2.3). Do it when a fourth drawable kind shows up.
