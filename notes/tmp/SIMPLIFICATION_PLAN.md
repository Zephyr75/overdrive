# Simplification plan

**Scope.** Making the existing code shorter and easier to read. No behaviour
changes, no new features.

**Not here.** The roadmap (`BACKEND_DECISION.md`), the shadow design
(`LIGHTING_PLAN.md`), performance work (`../FEATURES.md` Part 2).

**Method.** Every item below was measured against the tree, not guessed. Line
numbers are from the `atlas` branch on 2026-08-28 and drift as items land.

---

## 0. The two rules

**One function, one case.** A function that branches on *what kind of thing it
got* should be a dispatcher: a short parent that picks, and one named method per
case. The reader then follows one path instead of holding three in their head.
Length alone is not the problem — a flat 100-line list of struct literals is
fine. Branching is the problem.

**Comments are one line.** State the *why* or the invariant and stop. If the
reason needs a paragraph, the paragraph belongs in `notes/`, and the comment
becomes one line pointing at it. Section 6 has the policy and the exceptions.

Target: **~6060 lines of Go → ~5100**, with the largest function under 60 lines.

---

## 1. Delete dead files — 380 lines, zero risk

Already inventoried in `../ARCHITECTURE.md` §8. All five are fully commented out
or empty:

| file | lines | live code |
| --- | --- | --- |
| `physics/box_old.go` | 119 | 1 (`package physics`) |
| `ecs/ecs.go` | 142 | 1 |
| `physics/link.go` | 24 | 1 |
| `physics/box.go` | — | empty |
| `algorithms/wfc.go` | — | empty |

`ecs/entity.go` is the live ECS. `ecs/ecs.go` is also the only file `gofmt -l`
reports, so deleting it makes the tree format-clean.

Do this first — it is free, and it stops the next reader wondering which ECS is
real.

**Then delete `../ARCHITECTURE.md` §8 itself**, which exists only to list them.

---

## 2. Split the four branching functions

These are the ones that make the reader hold several cases at once. Ranked by
how much they hurt.

### 2.1 `shadowAtlas.allocate` — `scene/shadowatlas.go:283`, 162 lines

The worst offender, and it already carries `// TODO: understand` in its
signature — written by the person who wrote it.

It is **five sequential phases**, each already labelled by a comment. Each
becomes a method, and the comment becomes the name:

```go
func (a *shadowAtlas) allocate(lights []Light, camPos mgl32.Vec3) {
	reqs := a.rankRequests(lights, camPos)   // score, tier, stickiness, sort
	plan := a.planByTier(reqs)               // phase 1:  ceiling-capped
	a.offerSpareSlots(reqs, plan)            // phase 1b: degraded lights retry
	keep := a.keepMatchingAllocs(reqs, plan) // phase 2:  who keeps their rect
	a.rebuildFreeLists()                     //           one pass over the pools
	a.assignPlanned(reqs, plan, keep)        // phase 3:  hand out the rest
}
```

Six lines that read as the algorithm. `avail []int` threads through phases 1 and
1b, so those two either share a small struct or 1b takes it as a parameter.

`rankRequests` returns the `request` slice, which should move out of the function
body to a package-level type — it is the value the other five pass around.

### 2.2 `Scene.UpdateShadows` — `scene/shadowatlas.go:510`, 112 lines

Same shape: six phases, each already comment-labelled.

```go
func (s *Scene) UpdateShadows(nearPlane, farPlane float32) {
	s.atlas.allocate(s.Lights, s.Cam.Pos)
	s.resetQueues()
	dirty := s.markDirtyTiles()          // per light: staticValid, dynamicStudy
	s.queueStaticBakes()                 // all or nothing
	s.queueDynamicBakes(dirty)           // score order, within bakeTexelBudget
	s.buildRecords(nearPlane, farPlane)  // one ShadowRecord per tile
	s.commitBakes()                      // queued ⇒ valid, clear movedMeshes
}
```

The `dynamicUpdate` struct declared inside the body (`:520`) moves to package
level alongside `request`.

### 2.3 `VKBackend.BeginPass` — `vulkan/backend.go:795`, 129 lines

Three attachment shapes in one body, and they share almost nothing:

| path | lines | call sites today |
| --- | --- | --- |
| backbuffer (`target == 0`) | `:822-868` | 1 — `core/app.go:164` |
| offscreen **colour** | `:878-903`, early `return` | **0** |
| offscreen **depth** | `:905-913` | 2 — `shadowatlas.go:795,820` |

Split the body, keep the one interface method:

```go
func (backend *VKBackend) BeginPass(target renderer.RenderTargetHandle, clear *[4]float32, keepDepth bool) {
	if !backend.frameActive {
		return
	}
	backend.passActive = true
	if target == 0 {
		backend.beginBackbufferPass(clear, keepDepth)
		return
	}
	t := backend.target(target)
	if t == nil {
		fmt.Fprintln(os.Stderr, "vulkan: BeginPass on an invalid target, ignored")
		return
	}
	if t.format == renderer.TargetColor {
		backend.beginColorTargetPass(target, t, clear)
		return
	}
	backend.beginDepthTargetPass(target, t, keepDepth)
}
```

Do **not** split the `Backend` interface method itself. Three call sites, one
branch dead — it would buy a 28th interface method to serve one caller. Instead
name the sentinel in `renderer/`:

```go
// The swapchain image acquired for this frame, as a render target
const Backbuffer RenderTargetHandle = 0
```

so `core/app.go:164` reads `b.BeginPass(renderer.Backbuffer, &clear, prepass)`
instead of `b.BeginPass(0, ...)`. Revisit the interface split when the colour
path gets a real caller.

Two defects the split exposes and should fix on the way:

- **`keepDepth` is silently ignored on the colour path.** `depthAtt` is built at
  `:810-818` consuming `keepDepth`, then the colour branch returns at `:903`
  before `info.DepthAttachment` is set at `:917`. A colour pass renders with no
  depth attachment at all. Probably right for post-processing quads — say so in
  a comment, or reject a non-default `keepDepth` there.
- **No bounds check.** `:800` and `:870` index `backend.targets[target]` raw
  while the `target()` helper at `:1055` checks range and validity. An
  out-of-range handle panics; an in-range invalid one renders against a zeroed
  `targetEntry`. The sketch above uses the helper.

### 2.4 `MeshXml.toMesh` — `scene/mesh.go:89`, 144 lines

Two unrelated parsers plus struct assembly in one function. Split by file:

```go
func (mXml MeshXml) toMesh() Mesh {
	obj, err := parseOBJ(paths.Mesh(mXml.Obj))
	if err != nil { ... }
	mtl, err := parseMTL(paths.Mesh(mXml.mtlPath()))
	if err != nil { ... }
	return mXml.assemble(obj, mtl)
}
```

`mtlPath()` absorbs the `<mtl>`-omitted fallback at `:163-167`.

The parsers are also where the copy-paste lives. This block appears **three
times** verbatim for `Ka`/`Kd`/`Ks` (`:190-205`):

```go
first, _ := strconv.ParseFloat(split_line[1], 32)
second, _ := strconv.ParseFloat(split_line[2], 32)
third, _ := strconv.ParseFloat(split_line[3], 32)
material.X = mgl32.Vec3{float32(first), float32(second), float32(third)}
```

and the one-float form appears **six** times (`Ns`, `d`, `Pm`, `Pr`, and twice in
the OBJ loop). Two helpers kill roughly 40 lines:

```go
func f32(fields []string, i int) float32
func vec3(fields []string) mgl32.Vec3
```

The MTL switch then reads as a table, one line per key.

Both parsers currently `strings.Split(line, " ")` and index blindly — a
malformed file panics rather than erroring. Worth fixing while the code is open,
and `strings.Fields` handles runs of spaces that `Split` does not.

---

## 3. Two more that are long but not branchy

Lower priority — they read fine top to bottom, they are just big.

- **`core/App.Run` — `core/app.go:86`, 109 lines.** Six shader loads, timing
  state and the frame loop. Extract `loadShaders() shaderSet` (a struct of the
  six handles) and `printFPS(...)`. Leave the loop body inline: it is the frame
  shape, and `CLAUDE.md:130` documents it as a flat sequence. Keeping it
  readable in one screen is the point.
- **`main.go:109 MainWindow`, 127 lines.** A declarative widget literal, flat, no
  branches. Fine as it is. Extract the two buttons only if it grows again.

---

## 4. Argument-heavy call sites

`imageBarrier` is called **15 times** (13 in `backend.go`, 2 in `texture.go`)
with 8–9 positional arguments each, e.g. `backend.go:826`:

```go
backend.imageBarrier(cb, backend.swapImages[backend.imageIndex], vk.ImageAspectColor, 1,
	vk.ImageLayoutUndefined, vk.ImageLayoutColorAttachmentOptimal,
	vk.PipelineStage2ColorAttachmentOutput, vk.Access2None,
	vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite)
```

Nothing here tells the reader *what transition this is*. The call sites collapse
to a handful of recurring transitions — name them:

```go
backend.barrierToColorAttachment(cb, img, layers)
backend.barrierToDepthAttachment(cb, img, layers, from)
backend.barrierToShaderRead(cb, img, aspect, layers, from)
backend.barrierToTransferSrc(cb, img)   // CopyDepthRegion
backend.barrierToTransferDst(cb, img)
backend.barrierToPresent(cb, img)       // EndFrame
```

Keep the general `imageBarrier` underneath. This is the single largest
readability win in `vulkan/` per line changed, and it shrinks `BeginPass`,
`EndPass`, `CopyDepthRegion` and `BeginDepthPrepass` at once.

---

## 5. One error style

Three styles are in use today:

| style | sites | where |
| --- | --- | --- |
| `utils.HandleError(err)` → panic | 9 | `core/`, `main.go` |
| `fmt.Println("Error opening file:", err)` + zero value | 9 | `scene/` |
| `fmt.Fprintln(os.Stderr, "vulkan: ...")` + carry on | 6 | `vulkan/` |

The `vulkan/` one is deliberate — a backend that shouts and continues is what
lets a bad frame still present. Leave it.

The `scene/` one is not: `toMesh` returns `Mesh{}` on a missing file, so a typo
in a scene XML silently loads an empty mesh. Loading is startup work with no
frame to protect, so make the loaders return `(T, error)` and let the caller
`utils.HandleError`. Same failure, visible instead of silent. Roughly 9 sites,
all in `scene/`.

`fatal(err, what)` in `vulkan/backend.go:294` is a fourth, but it is scoped to
device-creation calls that genuinely cannot be recovered from. Fine.

---

## 6. Comments — cut the essays

Current density: `scene/shadowatlas.go` is 228 comment lines in 833 (27%),
`vulkan/backend.go` 180 in 1171 (15%). There are **30 comment blocks of five or
more lines** in live files — 12 of them in `shadowatlas.go` alone.

**The rule.** One line, above the declaration, saying *why* or naming the
invariant. Never restating what the code does.

The prose is not wrong — it is in the wrong place. Move it to `notes/` and leave
a pointer. `../ENGINE_FLOW.md` and `LIGHTING_PLAN.md` already hold most of it,
so most of these blocks are duplicating a document that is one link away.

Worked example, `scene/shadowatlas.go:342-356` (15 lines above phase 1b):

```go
// Phase 1b: whoever ended up under their ceiling — or with nothing — retries
// into what is still spare, largest first and still in rank order
//
// The ceiling is there to stop a light *competing* for a slot it would waste,
// not to leave one idle: a slot nobody claimed costs the same whether it is
// baked into or not, so a degraded light may as well have the texels. This
// keeps a layout tuned for one light mix from wasting a whole size on a scene
// with a different one.
//
// Strictly degraded lights, never lights already at their ceiling. Letting
// the surplus go to anyone reads as free quality and is not: with an atlas
// sized for §4.1's 77 lights, a scene of 41 leaves enough spare that every
// light climbs to the largest pool there is, and the score stops selecting a
// resolution at all. TestTileSizeTracksCameraDistance is the guard — under
// that version a lone spot held the sun's 2048 slot at every distance.
```

becomes, on the extracted method from §2.1:

```go
// Re-offers spare slots to lights under their ceiling, never to lights at it
// (LIGHTING_PLAN.md §4.1: offering to everyone stops score selecting a size)
func (a *shadowAtlas) offerSpareSlots(reqs []request, plan map[int32]int) {
```

The method name carries what the first paragraph said. The rest moves to
`LIGHTING_PLAN.md`.

The same treatment applies to the other 29. The longest, in order:
`shadowatlas.go:27-43` (17 lines), `:46-57` (12), `:716-726` (11),
`renderer/uniforms.go:36-44` (9), `scene/scene.go:194-202` (9),
`scene/light.go:120-127` (8), `shadowatlas.go:275-282` (8). Full sweep:

```sh
cd src
for f in $(find . -name '*.go'); do
  awk -v F="$f" '/^[[:space:]]*\/\//{n++; if(n==1)s=FNR; next}
                 {if(n>=5) print F":"s"-"FNR-1" ("n")"; n=0}' "$f"
done
```

### Keep these — they prevent silent corruption

Five comments buy their length. Shorten the wording if you like, but the fact
must survive, because in each case the failure mode is a wrong image with no
error anywhere:

1. `scene/scene.go:194-202` — prepass and forward must combine the matrices in
   the *same arithmetic*; an EQUAL test rejects a last-bit difference and shows
   it as speckle.
2. `renderer/uniforms.go:90` `init()` — the size guard catches a member added or
   resized, **not two swapped**, which renders garbage at an identical size.
3. `shaders/slang/forward.slang:35-40` — clamp every PCF tap inside the tile; the
   neighbour is another light's shadow.
4. `vulkan/backend.go:957-966` — the negative-height viewport, and why the
   shadow passes are the exception.
5. `vulkan/backend.go:22-33` — why the uniform arena is 4 MiB, and that an
   overflow is silent wrong depth. Trim the history, keep the number's reason.

Everything in `../ENGINE_FLOW.md` §5 is in this class.

---

## 6b. Two `TODO`s in the code, answered

- `renderer/uniforms.go:90` — `func init() { // TODO: where is it called }`.
  Nothing calls it: Go runs every `init()` automatically when the package is
  first loaded, before `main`. Any import of `renderer` fires the size guard, so
  it protects every build. Delete the TODO and say that in one line.
- `scene/shadowatlas.go:283` — `allocate(...) { // TODO: understand }`. §2.1 is
  the answer: the five phases are already there, unnamed. Naming them is what
  makes it understandable, so this TODO closes when that split lands.

---

## 7. Order to do it in

1. **§1 delete dead files** — free, and shrinks the tree 6%
2. **§6 comment sweep** — mechanical, touches everything, do it before the splits
   so the splits are not moving 15-line blocks around
3. **§4 named barriers** — biggest readability win per line in `vulkan/`
4. **§2.1 + §2.2 shadowatlas splits** — the two worst functions, and `allocate`
   is the one marked `TODO: understand`
5. **§2.3 BeginPass** — plus the two defects it exposes
6. **§2.4 toMesh** — plus §5's error style, same files
7. **§3** — only if still bothersome

### Verifying, given there are no tests

`go test ./...` reports `[no test files]` for every package. `CLAUDE.md` claims a
uniform-layout check and `TestShowcaseLoads`; neither exists in the tree. So
every item above is verified by `go build ./...`, `gofmt -l`, and **looking at
the scene**.

Two of these changes are the kind that fail silently rather than loudly — §2.1
(allocation order decides which light gets which rect) and §2.3 (attachment
setup). For those, capture a RenderDoc frame of `showcase.xml` with
`lockCamera = true` before and after, and diff the swapchain image and the two
atlases.

Writing the missing tests first would be better than any of this. `allocate` is
pure CPU logic over `[]Light` — it needs no GPU, and `TestTileSizeTracksCameraDistance`
is named in a comment at `shadowatlas.go:355` as though it once existed.
