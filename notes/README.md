# notes/ — what each file is, and whether it still describes this engine

These predate the current tree to varying degrees. Several were written against
the C++ engine that was deleted on 2026-07-22, and some describe designs that
were considered and *not* taken. That is fine — they are the record of why the
renderer is shaped the way it is — but it means none of them should be read as a
description of the code as it stands today.

**For the current engine, read the top-level docs instead:** `ENGINE_FLOW.md`
(what happens, in order), `GO_BACKEND.md` (why the abstraction is shaped this
way), `GO_PARITY.md` (known gaps), `ABSTRACTION_REVIEW.md` (what to change next).

## Still current

| File | What it is |
|---|---|
| `VULKAN.md` | The prescribed Vulkan techniques — 1.3, dynamic rendering, BDA + scalar layout, bindless descriptor indexing, synchronization2, VMA, 2 frames in flight. The Vulkan backend follows this. |
| `NEXT_LEVEL.md` | Design notes for features past the current forward+PBR renderer. Explicitly nothing-is-implemented; a menu, not a description. |
| `RAYTRACING_PLAN.md` | How hardware ray tracing would slot in. Design note only. Referenced from the README roadmap. |

## C++-era, but still the reference for *why*

Written against the deleted `cpp/` tree. The reasoning holds; the file paths and
function names do not.

| File | What it is |
|---|---|
| `FEATURES.md` | Feature report and roadmap. Part 2 is still the roadmap of record for texture-driven PBR, real IBL, HDR/bloom. |
| `OPTIMISATION.md` | Performance log: every problem hit, its cause, its fix, and how it was measured. The argument for GPU timestamp queries over FPS subtraction lives here. |
| `PIPELINE.md` | Frame walkthrough with both backends side by side, for someone who knew OpenGL and was new to Vulkan. Superseded operationally by `ENGINE_FLOW.md`, but its descriptor deep-dive (Part 4) has no equivalent there. |

## Personal reference — not engine documentation

Learning and revision notes. They describe graphics concepts, not this codebase,
so they do not go stale with the code.

| File | What it is |
|---|---|
| `ALGEBRA.md` | Vectors, matrices, transforms from first principles. |
| `PBR.md` | PBR cheat-sheet (FR), sourced from PBRT 4th ed., with real-time shortcuts flagged. |
| `OPENGL.md` | GLFW / OpenGL API notes. |
| `RAYTRACING.md` | Ray tracing vs path tracing vocabulary (FR). |
| `REV.md`, `REV2.md` | Interview revision notes (FR). |

## `archive/` — superseded, kept for history

Do not read these as documentation of the current engine. They are here because
they explain decisions, not because they describe the code.

| File | Why it moved |
|---|---|
| `ABSTRACTION.md` | An RHI design (GL + Vulkan + DX12) that was considered and not taken — `GO_BACKEND.md` §1.3 explains what was chosen instead and why. |
| `ARCHITECTURE.md` | Describes the engine as "Go with OpenGL 4.1" — predates the Vulkan backend and the `renderer.Backend` abstraction entirely. |
| `BACKEND.md` | The C++ renderer contract. Superseded by `renderer/backend.go` (the interface itself) and `ENGINE_FLOW.md` §4 (what each backend does with it). |
| `UNDERSTANDING.md` | Reading guide for the C++ engine. Superseded by `ENGINE_FLOW.md`. |
| `CHANGES.md` | Changelog that stops at 2026-04-07, C++ era. Git history covers everything after. |
