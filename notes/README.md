# notes/

Two kinds of document, kept apart: what **this engine** does, and what the
**field** does. Nothing is duplicated between files — each one states at the top
what it does not cover and where that lives instead.

```
notes/
├── OVERVIEW.md         the whole engine in one read — start here
├── ENGINE_FLOW.md      one frame, then the Backend contract method by method
├── ARCHITECTURE.md     the code map: layout, packages, symbols, scene format
├── FEATURES.md         what is implemented and why, roadmap, performance history
├── TODO.md             the working list
├── tmp/                forward-looking documents, parked here rather than in notes/
│   ├── INTERFACE_PLAN.md    what replaces the Backend interface, method by method
│   ├── BACKEND_DECISION.md  why Vulkan only, and the strategy INTERFACE_PLAN executes
│   ├── CLUSTERED_FORWARD.md the one deferred part of the lighting plan
│   ├── LIGHTING_PLAN.md     the shadow-atlas design — built, and the rationale for it
│   │                        design sound, implementation constraints stale — see its banner
│   └── LIGHTING_IMPL.md     how it was built, Parts A-H; only G is still open
└── cheatsheets/        reference notes, engine-independent
    ├── GRAPHICS.md     real-time techniques, procedural, physics, AI, GPGPU, optimisation
    ├── PBR.md          radiometry, BRDF, Cook-Torrance, metallic-roughness, IBL
    ├── RAYTRACING.md   ray vs path tracing, acceleration structures, Vulkan RT
    ├── OPENGL.md       the OpenGL API, call by call — reference only, no longer a backend
    ├── VULKAN.md       the Vulkan 1.3 object model, for OpenGL developers
    └── ALGEBRA.md      linear algebra and quaternions, geometric reading
```

## Which one do I want?

| I want to… | Read |
|---|---|
| get the whole picture, or come back after a while away | `OVERVIEW.md` |
| understand how a frame is drawn, or touch the backend | `ENGINE_FLOW.md` — start at §0 |
| find where something lives in `src/` | `ARCHITECTURE.md` §5 |
| know whether a feature exists, or why it was built that way | `FEATURES.md` Part 1 |
| pick up the next piece of work | `tmp/INTERFACE_PLAN.md` §6, then `TODO.md` |
| know why there is one backend, or why a gap blocks what it blocks | `tmp/BACKEND_DECISION.md` |
| know what replaces the interface, or whether a technique is expressible | `tmp/INTERFACE_PLAN.md` §3, §4 |
| know where the lighting and shadow work is heading | `tmp/LIGHTING_PLAN.md` — read its staleness banner first |
| pick up the next part of that work | `tmp/LIGHTING_IMPL.md`, then `tmp/CLUSTERED_FORWARD.md` |
| debug a wrong image | `ENGINE_FLOW.md` §5 and §6 |
| understand a Vulkan object's lifetime | `ENGINE_FLOW.md` §7 |
| revise the theory behind the shaders | `cheatsheets/PBR.md` |
| revise an API call I have forgotten | `cheatsheets/OPENGL.md`, `cheatsheets/VULKAN.md` |
| revise real-time technique in general | `cheatsheets/GRAPHICS.md` |

## Conventions

The engine documents describe **what the code does today**. When they disagree
with `src/`, the code wins and the document is the bug.

Nothing is duplicated between files, with one deliberate exception:
`OVERVIEW.md` restates enough of the others to be readable start to finish. When
it disagrees with them, they win.

`tmp/` is the exception: those describe what the code should *become*, so they
disagree with `src/` on purpose. Each carries its own status line. A `tmp/`
document is deleted once the code has caught up with it and the reasoning worth
keeping has moved into an engine document — that is what happened to the
simplification plan and the Part D report.

The cheatsheets are engine-independent reference. Each opens with a **Scope /
Not here / Source** block, carries a numbered table of contents, and defines
terms in the same way — `` `Term` `` followed by an explanation, `>` blockquotes
for the insight or the gotcha, tables for comparisons, diagrams where a pipeline
or a hierarchy is easier seen than read.
