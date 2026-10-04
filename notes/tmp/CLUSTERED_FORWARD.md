# Clustered forward — the deferred Part G

**Status: not started, deliberately.** Lifted out of `LIGHTING_IMPL.md` on
2026-08-22 so that file could be closed out with Part H. Nothing about the design
changed; only its position in the queue.

Every `§n` points into `LIGHTING_PLAN.md`. Why the design looks like this lives
there; what to edit lives here.

---

## Why it was deferred, and what that cost

Parts A–F and H landed. G is the only one left, and it is the one with no
prerequisite pushing it: the shadow budget (B–E) is complete without it, and the
depth prepass (F) was placed immediately before it precisely so it would already
be in place when this starts. **F is done, so G inherits it.**

Three things elsewhere in the tree are waiting on this part, and each is recorded
where it will bite:

- **The allocator's score ignores whether a light is on screen at all.**
  `lightScore` in `scene/shadowatlas.go` ranks by `radius / distance to camera`,
  so a bright light directly behind the camera outranks a dim one in front of it.
  §2.2's cluster gate is the fix — step 5 below. Logged in `FEATURES.md` Part 2
  §4 and as Part D's open risk.
- **The cube-face-vs-camera-frustum cull was not taken in Part E.** Every cheap
  version of it is a heuristic that silently drops a shadow. The honest form of
  the test is "can any visible fragment sample this face", which is this part.
- **`MaxLights` is still 64**, a fixed array in `FrameUniforms` and
  `common.slang`. Step 4 is what removes the ceiling.

---

## Steps

1. Froxel grid, default 16 × 9 × 24, exponential in Z. `ClusterGrid [4]int32` in
   `FrameUniforms` carries the dimensions and `maxPerCluster`.
2. CPU build per frame: every light's bounding sphere against every froxel,
   producing `clusterOffsets` (offset, count per cluster) and `clusterIndices`
   (flat light indices). Upload both into the storage buffer from Part C and
   push a fourth pointer for them.
3. `forward.slang`: derive the cluster from `gl_FragCoord.xy` and view depth,
   read offset and count, loop only those lights. The Part A early-out stays as
   the inner guard.
4. The scene light array outgrows `MaxLights` here — move `Lights[]` out of
   `FrameUniforms` into the same storage buffer. `MaxLights` stops being the
   scene cap and `maxPerCluster` (16) takes over as the per-fragment cap;
   `FrameUniforms` drops to roughly 236 bytes.
5. Feed the cluster result into Part D's allocator: a light intersecting zero
   clusters skips tile allocation entirely (§2.2). This is the synergy the
   ordering was chosen for.

**Follow-up, not required here.** A compute cluster build is a good first user
of `Dispatch` (`BACKEND_DECISION.md` §9 item 8). Build on the CPU first — it is
simpler and not obviously the bottleneck.

---

## The gate

The standard gate from `LIGHTING_IMPL.md`, run from `src/`:

```sh
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh
go build ./... && go vet ./...              # there are no tests; see CLAUDE.md
for f in shaders/vk/*.spv; do spirv-val --scalar-block-layout "$f"; done
go run .                                    # with [debug] validation = true
```

Plus, specific to this part: a stress scene with 200+ unshadowed lights holding
frame rate, and the froxel grid visualised as a debug overlay at least once.

**Steps 1 and 4 both move `FrameUniforms`**, so invariant 3 applies in full: the
`init()` size guard in `renderer/uniforms.go` catches a member added, removed or
resized, but **two members swapped leaves every size identical and renders silent
garbage**. Read the offsets the compiler actually emitted:

```sh
spirv-dis shaders/vk/forward.frag.spv | grep OpMemberDecorate
```

Step 4 is the largest layout change in the whole lighting plan — a 4608-byte
array leaving the struct. Do not batch it with step 1.

---

## Risks

- **Z-slice distribution interacts with the shadow far plane.** That is
  `[shadows] farPlane` since Part H, default 50. Fit the froxel Z range and the
  shadow projection to the same scene bounds in this part, or the two disagree at
  range.
- **The showcase cannot show any of this.** It has nine lights in a ~10–20 unit
  scene, and four of them reach every fragment. `stress.xml` (64 lights) is the
  floor; this part wants a scene with hundreds. Part A step 6 and Part F both
  wanted the same thing — a scene of many dim, localised lights with real depth
  complexity — and it is **still unbuilt**. Build it first, once, for all three.

---

## What Part H already left in place

`configs/low.toml` ships without cluster keys, since there are none yet. When
this part lands, its knobs join the existing `[shadows]` / `[renderer]` schema in
`settings/config.go` the same way — with a value check in `apply` and a case in
`settings/settings_test.go`, so a misspelt key rejects the file rather than being
ignored. `LIGHTING_PLAN.md` §9 sketched `8 × 5 × 12` clusters and
`maxPerCluster` 16 for the low tier.
