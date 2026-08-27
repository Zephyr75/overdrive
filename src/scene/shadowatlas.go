package scene

import (
	"math"
	"sort"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// One depth texture holds every shadow in the scene, each light owning a
// sub-rect of it: a sun or spot one tile, a point light six 90° tiles
//
// The layout below is rebuilt from settings on every shadowAtlas.reset, which is
// what makes it a quality tier rather than a constant. settings.checkShadowAtlas
// is where a layout that could not be carved is rejected — by the time these are
// read they are known good.
var (
	atlasSize = settings.ShadowAtlasSize
	// The extremes of the slot layout, named so the layout and the allocator can
	// size themselves off it
	sunTileSize = atlasSize / 2  // the largest slot, and the sun's ceiling
	minTileSize = atlasSize / 32 // the smallest, so the unit buildLayout counts in

	// The fixed slot layout: the atlas is carved once at load and never
	// repartitioned
	//
	// Every size is a division of atlasSize rather than a pixel count, so
	// changing the atlas rescales the whole layout instead of changing how many
	// lights fit. That splits one confusing knob into two orthogonal ones — atlas
	// size buys sharpness, the counts buy light budget.
	//
	// Slots are typeless; only size matters. A point light's six faces each carry
	// their own atlasRect and are never filtered across, so they need not be
	// adjacent — a point light takes six slots of one size from wherever they
	// happen to be. That is what keeps a fixed layout from being rigid.
	//
	// The default is LIGHTING_PLAN.md §4.1's partition, quadrant for quadrant:
	// the sun owns one, and the other three each hold one tier at a single size.
	// 337 slots for the plan's 1 sun + 52 point + 24 spot = 77 shadowed lights,
	// and 100% of the atlas — 4 + 4 + 4 + 4 of the atlas's 16 cells of 1024².
	slotLayout []struct{ size, count int }

	// Score thresholds and the tile size each earns, highest first
	//
	// score = radius / distance, which is the light's rough screen-space
	// footprint: the same light gets a bigger tile as the camera walks toward it.
	// Below the last threshold a light is not worth a tile at all.
	//
	// This is the *ceiling* on what a light may hold, not what it is handed. Rank
	// picks the slot and this caps it, so a lone light with a tiny footprint
	// cannot claim a big slot it would only spend bake time on.
	//
	// The sizes are the non-sun rows of slotLayout rather than a second list, so
	// a ceiling can never name a size no pool holds.
	shadowTiers []struct {
		minScore float32
		size     int
	}
)

// Rebuilds the layout tables from settings, before any slot is carved
func loadLayoutSettings() {
	atlasSize = settings.ShadowAtlasSize
	div, count := settings.ShadowSlotDivisors, settings.ShadowSlotCounts

	slotLayout = slotLayout[:0]
	for i := range div {
		slotLayout = append(slotLayout, struct{ size, count int }{atlasSize / div[i], count[i]})
	}
	sunTileSize = slotLayout[0].size
	minTileSize = slotLayout[len(slotLayout)-1].size

	shadowTiers = shadowTiers[:0]
	for i, sc := range settings.ShadowTierScores {
		shadowTiers = append(shadowTiers, struct {
			minScore float32
			size     int
		}{sc, slotLayout[i+1].size})
	}
}

// How far past a threshold a score must go before the tier actually changes
//
// A light sitting exactly on a boundary would otherwise reallocate every frame,
// and a reallocation is a forced full re-bake — the exact opposite of what the
// tile caching is for.
const nextTierThreshold = 1.2

// How much a challenger must outscore a slot's current holder to evict it
//
// The failure a fixed pool has and a splitting tree does not: once a pool is
// empty, two lights with near-equal scores trade the last slot every frame,
// which is both a forced re-bake and a visible flicker. nextTierThreshold
// cannot damp this — it is a margin against a fixed threshold, and this
// contention is between lights. So the margin goes on the ranking instead.
const slotStickiness = 1.2

// The atlas and who owns what of it
//
// The layout persists for the life of the scene and its rects never move, which
// is the precondition for Part E baking a tile once and leaving it alone: a
// light that keeps its slot keeps its exact pixels, so validity is one dirty
// flag per slot rather than a comparison of rects.
type shadowAtlas struct {
	// Two atlases carved by one slotLayout, so a tile has the same rect in both
	// and CopyDepthRegion is a straight blit with no remap. The static one holds
	// what cannot move and is baked on demand; the dynamic one is that copy plus
	// the movable casters, and is what a light near a moving object samples
	staticTarget  renderer.RenderTargetHandle
	staticTex     renderer.TextureHandle
	dynamicTarget renderer.RenderTargetHandle
	dynamicTex    renderer.TextureHandle

	slotsPool   []slotsPool           // one per size, largest first
	lightAllocs map[int32]*lightAlloc // by index into Scene.Lights
}

// One slot of the fixed layout: a top-left corner that never moves
type slot struct {
	x, y int
}

// Every slot of one size, plus which of them are free this frame
type slotsPool struct {
	size  int
	slots []slot
	used  []bool // scratch, rebuilt every frame from what the keepers hold
	free  []int  // indices into slots, rebuilt from used
}

// What one light currently holds
type lightAlloc struct {
	tier  int   // index into shadowTiers: -1 for a sun
	size  int   // the per-face size it actually got, its ceiling or smaller
	pool  int   // index into shadowAtlas.slotsPool
	slots []int // indices into that pool: 1 for a sun or a spot, 6 for a point light

	// Whether each atlas holds a current bake of these tiles. staticValid lasts
	// as long as the light keeps its slots — the point of a layout whose rects
	// never move — and dynamicValid until a caster in range moves
	staticValid, dynamicValid bool
	// Whether a movable caster is in range at all, and what this frame queued
	dynamicStudy                bool
	staticQueued, dynamicQueued bool
	// Where this light's tiles start in Scene.shadowTiles
	tilesIndex int
}

// Creates both atlas textures, once, at scene load
func (a *shadowAtlas) setup(b renderer.Backend) {
	a.reset()
	spec := renderer.RenderTargetSpec{
		Width:  atlasSize,
		Height: atlasSize,
		Format: renderer.TargetDepth,
	}
	a.staticTarget, a.staticTex = b.CreateRenderTarget(spec)
	a.dynamicTarget, a.dynamicTex = b.CreateRenderTarget(spec)
}

// Rebuilds the layout and forgets every allocation
func (a *shadowAtlas) reset() {
	loadLayoutSettings()
	a.slotsPool = buildLayout()
	a.lightAllocs = map[int32]*lightAlloc{}
}

// Carves the fixed layout out of the atlas, once
//
// Every slot is a power-of-two division and the sizes descend, so placement needs
// no search: walk the atlas in Z order, a cursor counting cells of the smallest
// slot. That is what a buddy tree filled largest-first emits — first-fit in
// quadrant order is Z order — and it cannot fragment either.
func buildLayout() []slotsPool {
	pools := make([]slotsPool, 0, len(slotLayout))
	cell, side := 0, atlasSize/minTileSize
	for _, spec := range slotLayout {
		p := slotsPool{
			size:  spec.size,
			slots: make([]slot, 0, spec.count),
			used:  make([]bool, spec.count),
			free:  make([]int, 0, spec.count),
		}
		// The cursor stays aligned without padding: each tier's cell count
		// divides the one above, which init() enforces by ordering the sizes
		cells := (spec.size / minTileSize) * (spec.size / minTileSize)
		for n := 0; n < spec.count; n++ {
			x, y := zOrder(cell)
			p.slots = append(p.slots, slot{x: x, y: y})
			cell += cells
		}
		pools = append(pools, p)
	}
	if cell > side*side {
		panic("the shadow slot layout does not pack into the atlas")
	}
	return pools
}

// De-interleaves a Z-order cell index into the atlas pixels of its top-left corner
func zOrder(cell int) (int, int) {
	x, y := 0, 0
	for b := 0; cell != 0; b, cell = b+1, cell>>2 {
		x |= (cell & 1) << b
		y |= (cell >> 1 & 1) << b
	}
	return x * minTileSize, y * minTileSize
}

// --- allocation policy -------------------------------------------------------

// The tier a raw score earns, len(shadowTiers) meaning no tile at all
func rawTier(score float32) int {
	for i, t := range shadowTiers {
		if score > t.minScore {
			return i
		}
	}
	return len(shadowTiers)
}

// The tier a light should hold, given what it holds now
//
// Promotion needs the score to clear the new tier's threshold by the hysteresis
// margin; demotion needs it to have fallen the same margin below the threshold
// of the tier currently held. Between the two the light keeps what it has.
func tierFor(score float32, cur int) int {
	switch want := rawTier(score); {
	case want < cur: // a smaller index is a bigger tile
		if score > shadowTiers[want].minScore*nextTierThreshold {
			return want
		}
	case want > cur:
		if score < shadowTiers[cur].minScore/nextTierThreshold {
			return want
		}
	}
	return cur
}

// How many tiles a light needs: one per cube face, or one
func tileCount(l *Light) int {
	if l.Type == renderer.LightPoint {
		return 6
	}
	return 1
}

// A light's screen-space importance: how much of the view its lit volume covers
//
// A sun has no radius and no position that means anything to this, and it is the
// one light every pixel sees, so it outranks everything scored.
func lightScore(l *Light, camPos mgl32.Vec3) float32 {
	if l.Type == renderer.LightSun {
		return math.MaxFloat32
	}
	dist := l.Pos.Sub(camPos).Len()
	if dist < 1e-3 {
		dist = 1e-3
	}
	return l.Radius / dist
}

// Gives back everything a light holds
//
// Only the map entry: the free lists are rebuilt wholesale from what survives,
// so there is nothing to hand back to a pool.
func (a *shadowAtlas) drop(light int32) {
	delete(a.lightAllocs, light)
}

// Rescores every light and hands out the fixed layout's slots
//
// Rank picks the slot, the tier caps it. Lights sort by score and take the best
// free slot no larger than their ceiling, so importance decides who gets the
// good slots and the ceiling stops an unimportant light wasting one. Running out
// of slots costs the least important light its resolution and never costs frame
// time — it walks down a pool at a time and finally holds nothing, which leaves
// ShadowIndex = -1 and lights it unshadowed.
func (a *shadowAtlas) allocate(lights []Light, camPos mgl32.Vec3) { // TODO: understand
	type request struct {
		idx        int32
		eff        float32
		tier, want int
		count      int
	}
	reqs := make([]request, 0, len(lights))

	for i := range lights {
		l := &lights[i]
		cur, held := len(shadowTiers), false
		if al, ok := a.lightAllocs[int32(i)]; ok {
			cur, held = al.tier, true
		}
		score := lightScore(l, camPos)
		// A sun is never scored, so it never enters shadowTiers: tier -1
		tier, want := -1, sunTileSize
		if l.Type != renderer.LightSun {
			tier = tierFor(score, cur)
			if tier == len(shadowTiers) {
				a.drop(int32(i))
				continue
			}
			want = shadowTiers[tier].size
		}
		// A light already holding slots ranks above an equal challenger, so the
		// two either side of the last free slot do not trade it every frame
		eff := score
		if held {
			eff *= slotStickiness
		}
		reqs = append(reqs, request{
			idx: int32(i), eff: eff,
			tier: tier, want: want, count: tileCount(l),
		})
	}

	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].eff > reqs[j].eff })

	// Phase 1: plan against slot counts alone, in rank order, before touching
	// what anyone holds. Pools run largest first, so the first one both small
	// enough and deep enough is the best slot this light is allowed.
	plan := make(map[int32]int, len(reqs))
	avail := make([]int, len(a.slotsPool))
	for i := range a.slotsPool {
		avail[i] = len(a.slotsPool[i].slots)
	}
	for _, r := range reqs {
		for p := range a.slotsPool {
			if a.slotsPool[p].size > r.want || avail[p] < r.count {
				continue
			}
			avail[p] -= r.count
			plan[r.idx] = p
			break
		}
	}

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
	for _, r := range reqs {
		p, planned := plan[r.idx]
		if planned && a.slotsPool[p].size >= r.want {
			continue
		}
		for q := range a.slotsPool {
			if planned && a.slotsPool[q].size <= a.slotsPool[p].size {
				break // nothing larger than what it already has is spare
			}
			if avail[q] < r.count {
				continue
			}
			if planned {
				avail[p] += r.count
			}
			avail[q] -= r.count
			plan[r.idx] = q
			break
		}
	}

	// Phase 2: a light whose plan lands in the pool it already holds keeps its
	// exact slots. That is the point of a layout that never moves — the same
	// rects, so Part E can leave the tile baked. Everyone else gives theirs back
	keep := make(map[int32]bool, len(reqs))
	for _, r := range reqs {
		al, ok := a.lightAllocs[r.idx]
		p, planned := plan[r.idx]
		if ok && planned && al.pool == p && len(al.slots) == r.count {
			al.tier = r.tier
			keep[r.idx] = true
		}
	}
	for idx := range a.lightAllocs {
		if !keep[idx] {
			a.drop(idx)
		}
	}

	// Rebuild each pool's free list from what the keepers hold, rather than
	// pushing and popping as lights come and go: one pass over ~140 slots a
	// frame, and it cannot leak a slot the way an incremental stack can
	for p := range a.slotsPool {
		pool := &a.slotsPool[p]
		for i := range pool.used {
			pool.used[i] = false
		}
	}
	for _, al := range a.lightAllocs {
		for _, s := range al.slots {
			a.slotsPool[al.pool].used[s] = true
		}
	}
	for p := range a.slotsPool {
		pool := &a.slotsPool[p]
		pool.free = pool.free[:0]
		for i, u := range pool.used {
			if !u {
				pool.free = append(pool.free, i)
			}
		}
	}

	// Phase 3: hand the planned slots to everyone not keeping theirs. The plan
	// was made against the same counts and the keepers hold exactly what it gave
	// them, so a pool coming up short is a bug in the phases above
	for _, r := range reqs {
		if keep[r.idx] {
			continue
		}
		p, planned := plan[r.idx]
		if !planned {
			continue
		}
		pool := &a.slotsPool[p]
		if len(pool.free) < r.count {
			panic("the shadow slot plan promised slots the pool does not hold")
		}
		idxs := make([]int, r.count)
		copy(idxs, pool.free[len(pool.free)-r.count:])
		pool.free = pool.free[:len(pool.free)-r.count]

		a.lightAllocs[r.idx] = &lightAlloc{
			tier: r.tier, size: pool.size,
			pool: p, slots: idxs,
		}
	}
}

// --- records -----------------------------------------------------------------

// The six cube-face view directions and up vectors, in the order forward.slang's
// cubeFace() indexes them: +X, -X, +Y, -Y, +Z, -Z
var cubeFaceDirs = [6][2]mgl32.Vec3{
	{{1, 0, 0}, {0, -1, 0}},
	{{-1, 0, 0}, {0, -1, 0}},
	{{0, 1, 0}, {0, 0, 1}},
	{{0, -1, 0}, {0, 0, -1}},
	{{0, 0, 1}, {0, -1, 0}},
	{{0, 0, -1}, {0, -1, 0}},
}

// Bake a bit more than 90°, so the outer one-texel band of every face tile
// holds real depth for geometry instead of sampling data outside the frustum
func cubeFaceFov(tile int) float32 {
	half := math.Tan(math.Pi/4) * float64(tile+2) / float64(tile)
	return float32(2 * math.Atan(half))
}

// Whether a mesh that can move is close enough to matter to this light
//
// A sun reaches everything; anything else is the caster's bounding sphere
// against the radius the shading already culls by, so the two agree.
func (s *Scene) casterInRange(l *Light, m *Mesh, center mgl32.Vec3) bool {
	if l.Type == renderer.LightSun {
		return true
	}
	return center.Sub(l.Pos).Len() <= l.Radius+m.boundsRadius
}

// Whether any movable caster is in range, which is what earns a dynamic tile
func (s *Scene) movableCasterInRange(l *Light) bool {
	for i := range s.Meshes {
		m := &s.Meshes[i]
		if m.Movable && m.CastsShadow && s.casterInRange(l, m, m.boundsCenter) {
			return true
		}
	}
	return false
}

// Whether a caster that moved since the last update is in range
//
// Both centres, because a caster leaving a light's range has to dirty the tile
// it is leaving: its new position alone would say it never mattered.
func (s *Scene) movedCasterInRange(l *Light) bool {
	for _, i := range s.movedMeshes {
		m := &s.Meshes[i]
		if !m.CastsShadow {
			continue
		}
		if s.casterInRange(l, m, m.boundsCenter) || s.casterInRange(l, m, m.prevCenter) {
			return true
		}
	}
	return false
}

// Allocates a tile per casting light, decides this frame's bake work and builds
// the shadow records
//
// Runs before FillFrameUniforms, which copies each light's ShadowIndex into the
// block, and before the bake, which walks the queues this leaves behind.
func (s *Scene) UpdateShadows(nearPlane, farPlane float32) {
	s.atlas.allocate(s.Lights, s.Cam.Pos)

	// Clear everything
	s.shadowTiles = s.shadowTiles[:0]
	s.staticQueue = s.staticQueue[:0]
	s.dynamicQueue = s.dynamicQueue[:0]

	// A light wanting its dynamic tile rebuilt, ranked so a budget that runs out
	// costs the least important light its update rather than costing frame time
	type dynamicUpdate struct {
		idx   int32
		score float32
	}
	dynamicUpdates := make([]dynamicUpdate, 0, len(s.Lights))
	runStatic := false

	for i := range s.Lights {
		allocs, ok := s.atlas.lightAllocs[int32(i)]
		if !ok {
			continue
		}
		l := &s.Lights[i]
		allocs.staticQueued, allocs.dynamicQueued = false, false
		// A scene built without the second atlas has no dynamic tiles at all:
		// every record falls back to the static one and movers cast nothing
		allocs.dynamicStudy = settings.ShadowDynamicAtlas && s.movableCasterInRange(l)
		if !allocs.staticValid {
			runStatic = true
		}
		// A dynamic tile is built from its static one, so a static re-bake forces
		// the copy; otherwise only a caster that moved does
		if allocs.dynamicStudy && (!allocs.staticValid || !allocs.dynamicValid || s.movedCasterInRange(l)) {
			dynamicUpdates = append(dynamicUpdates, dynamicUpdate{idx: int32(i), score: lightScore(l, s.Cam.Pos)})
		}
	}

	// Queue static bakes: all or nothing
	if runStatic {
		for i := range s.Lights {
			if alloc, ok := s.atlas.lightAllocs[int32(i)]; ok {
				alloc.staticQueued = true
				alloc.dynamicValid = false
				s.staticQueue = append(s.staticQueue, int32(i))
			}
		}
	}

	// Queue dynamic bakes
	sort.SliceStable(dynamicUpdates, func(i, j int) bool { return dynamicUpdates[i].score > dynamicUpdates[j].score })
	budget := settings.ShadowBakeBudget()
	for _, w := range dynamicUpdates {
		alloc := s.atlas.lightAllocs[w.idx]
		cost := len(alloc.slots) * alloc.size * alloc.size
		if cost > budget {
			continue
		}
		budget -= cost
		alloc.dynamicQueued = true
		s.dynamicQueue = append(s.dynamicQueue, w.idx)
	}

	for i := range s.Lights {
		light := &s.Lights[i]
		alloc, ok := s.atlas.lightAllocs[int32(i)]
		if !ok {
			light.shadowIndex, light.shadowCount = -1, 0
			continue
		}
		alloc.tilesIndex = len(s.shadowTiles)

		// A tile whose static bake has not run holds whatever its last owner left,
		// so the light goes unshadowed until it does rather than wearing another
		// light's shadow
		if alloc.staticValid || alloc.staticQueued {
			light.shadowIndex = int32(len(s.shadowTiles))
			light.shadowCount = int32(len(alloc.slots))
		} else {
			light.shadowIndex, light.shadowCount = -1, 0
		}
		// Sampling the dynamic atlas needs its copy to have happened. Until it
		// does, the static tile is the same shadow without the moving caster,
		// which is the right thing to fall back to
		dyn := alloc.dynamicStudy && (alloc.dynamicQueued || (alloc.dynamicValid && !alloc.staticQueued))

		// The rect a slot names, resolved here rather than stored: the layout is
		// carved once and never moves, so the pool is the one place it lives
		pool := &s.atlas.slotsPool[alloc.pool]
		for face, si := range alloc.slots {
			sl := pool.slots[si]
			s.shadowTiles = append(s.shadowTiles, light.shadowRecord(sl, alloc.size, face, len(alloc.slots),
				dyn, nearPlane, farPlane))
		}
	}

	// Queueing is what marks a tile current, not the bake: BakeShadows draws
	// exactly what these queues hold and cannot fail partway, so keeping the
	// bookkeeping here is what lets the decision be tested without a GPU
	for _, idx := range s.staticQueue {
		s.atlas.lightAllocs[idx].staticValid = true
	}
	for _, idx := range s.dynamicQueue {
		s.atlas.lightAllocs[idx].dynamicValid = true
	}

	// Every mover has been accounted for; remember where they were, so one that
	// leaves a light's range still dirties the tile it left
	for _, i := range s.movedMeshes {
		s.Meshes[i].prevCenter = s.Meshes[i].boundsCenter
	}
	s.movedMeshes = s.movedMeshes[:0]
}

// Builds one tile's record: its projection, its rect and its depth encoding
//
// Takes the rect rather than reaching for it, so this stays a Light method with
// no knowledge of the atlas
func (l *Light) shadowRecord(sl slot, size int, face, faces int, dynamic bool,
	nearPlane, farPlane float32) renderer.ShadowTile {

	// Bit 0 picks the atlas: set only once the dynamic tile actually holds this
	// frame's copy, so a light never samples a tile that was not built for it.
	// Bit 1 is the PCF quality, riding the same word rather than growing
	// ShadowRecord — a per-tile knob for free, should a tier ever want one
	var flags int32
	if dynamic {
		flags |= 1
	}
	if settings.ShadowPCF == settings.PCFCheap {
		flags |= 2
	}
	rec := renderer.ShadowTile{
		AtlasCoords: [4]float32{
			float32(sl.x) / float32(atlasSize), float32(sl.y) / float32(atlasSize),
			float32(size) / float32(atlasSize), float32(size) / float32(atlasSize),
		},
		PCFStep:   1.0 / float32(size),
		FarPlane:  farPlane,
		FaceIndex: -1,
		Flags:     flags,
	}

	if faces == 6 {
		dir, up := cubeFaceDirs[face][0], cubeFaceDirs[face][1]
		proj := mgl32.Perspective(cubeFaceFov(size), 1, nearPlane, farPlane)
		rec.WorldToTile = proj.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(dir), up))
		rec.FaceIndex = int32(face)
		return rec
	}

	if l.Type == renderer.LightSpot {
		// The cone's own frustum, widened by two texels for the same reason a
		// cube face is: the PCF kernel must stay inside the tile at its rim
		fov := 2 * float32(math.Acos(float64(mgl32.Clamp(l.OuterCutoff, -1, 1))))
		fov *= float32(size+2) / float32(size)
		proj := mgl32.Perspective(mgl32.Clamp(fov, 0.01, 3.0), 1, nearPlane, farPlane)
		rec.WorldToTile = proj.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), spotUp(l.Dir)))
		return rec
	}

	// A sun's ortho box, unchanged from the per-light target it replaces
	proj := mgl32.Ortho(-10, 10, -10, 10, nearPlane, farPlane)
	rec.WorldToTile = proj.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), mgl32.Vec3{0, 1, 0}))
	return rec
}

// An up vector that is not parallel to dir, LookAtV producing NaNs when it is
func spotUp(dir mgl32.Vec3) mgl32.Vec3 {
	if math.Abs(float64(dir[1])) > 0.99 {
		return mgl32.Vec3{0, 0, 1}
	}
	return mgl32.Vec3{0, 1, 0}
}

// The six clip planes of a tile's frustum, as (a, b, c, d) with abc normalised
//
// Gribb-Hartmann: each plane is a row of the matrix combined with the w row.
// These matrices are the OpenGL convention, so near is row3 + row2
func frustumPlanes(m mgl32.Mat4) [6]mgl32.Vec4 {
	r0, r1, r2, r3 := m.Row(0), m.Row(1), m.Row(2), m.Row(3)
	p := [6]mgl32.Vec4{
		r3.Add(r0), r3.Sub(r0),
		r3.Add(r1), r3.Sub(r1),
		r3.Add(r2), r3.Sub(r2),
	}
	for i := range p {
		if n := (mgl32.Vec3{p[i][0], p[i][1], p[i][2]}).Len(); n > 0 {
			p[i] = p[i].Mul(1 / n)
		}
	}
	return p
}

// Whether a bounding sphere is inside every plane
//
// Conservative at the corners by design: over-including costs a draw the
// rasteriser discards, under-including costs a shadow
func sphereInFrustum(p *[6]mgl32.Vec4, c mgl32.Vec3, r float32) bool {
	for _, pl := range p {
		if pl[0]*c[0]+pl[1]*c[1]+pl[2]*c[2]+pl[3] < -r {
			return false
		}
	}
	return true
}

// Draws one light's tiles into the pass in progress, returning how many it drew into
//
// movable picks the half of the caster set this pass owns: the static atlas
// holds everything that cannot move, the dynamic one only what can.
//
// Back-face culling, the scene default, so the surface facing the light is what
// lands in the map and a shadow stays welded to its caster's base. Front-face
// culling is the other classic choice and is wrong here: it bakes the far side
// of a closed mesh, so the depth stored is a whole diameter too far and a sphere
// floats above a lit disc of its own size. The normal offset in shadowLookup
// does that job instead, and CastsShadow does the rest.
func (s *Scene) bakeLight(light int32, al *lightAlloc, f *renderer.FrameUniforms,
	depthShader, depthPointShader renderer.ShaderHandle, movable bool) int {

	b := s.backend
	// Static mesh geometry is baked into the OBJ vertices, so the depth passes
	// draw everything with an identity model matrix and no material at all
	u := renderer.DrawUniforms{Model: mgl32.Ident4()}
	drawn := 0

	l := &s.Lights[light]
	pool := &s.atlas.slotsPool[al.pool]

	for k, si := range al.slots {
		rec := &s.shadowTiles[al.tilesIndex+k]
		sl := pool.slots[si]
		planes := frustumPlanes(rec.WorldToTile)

		// The tile's state goes out only once a caster has survived the cull, so
		// a face pointing at empty space costs six plane tests and nothing else
		started := false
		for m := range s.Meshes {
			mesh := &s.Meshes[m]
			if !mesh.CastsShadow || mesh.Movable != movable {
				continue
			}
			if !sphereInFrustum(&planes, mesh.boundsCenter, mesh.boundsRadius) {
				continue
			}
			if !started {
				// Focus on part of atlas corresponding to tile
				b.SetViewportScissor(sl.x, sl.y, al.size, al.size)
				f.CurWorldToTile = rec.WorldToTile
				f.CurLightPos = l.Pos
				f.CurFarPlane = rec.FarPlane
				b.BindFrameUniforms(f)
				// A face tile stores radial distance, which needs the fragment stage
				if rec.FaceIndex >= 0 {
					b.BindShader(depthPointShader)
				} else {
					b.BindShader(depthShader)
				}
				started = true
				drawn++
			}
			mesh.draw(&u)
		}
	}
	return drawn
}

// Bakes this frame's queued tiles: the static atlas when allocation moved, then
// the dynamic one from a copy of it plus whatever can move
//
// One BeginPass per atlas rather than one per light: the target bind was the
// expensive part, and SetViewportScissor is what makes the tiles separate. A
// settled scene queues nothing and this does no GPU work at all.
func (s *Scene) BakeShadows(depthShader, depthPointShader renderer.ShaderHandle,
	f *renderer.FrameUniforms) {

	if len(s.shadowTiles) == 0 {
		return
	}
	b := s.backend
	s.staticBakes, s.dynamicBakes = 0, 0

	// Clears, because the queue is every allocated light whenever it is not
	// empty — see UpdateShadows for why the static side is all-or-nothing
	if len(s.staticQueue) > 0 {
		b.BeginPass(s.atlas.staticTarget, nil, false)
		for _, idx := range s.staticQueue {
			al := s.atlas.lightAllocs[idx]
			s.staticBakes += s.bakeLight(idx, al, f, depthShader, depthPointShader, false)
		}
		b.EndPass()
	}

	// The dynamic tile starts as a copy of the static one, so the movable casters
	// draw on top of the baked scene with an ordinary depth test and the union
	// falls out. Outside any pass: a copy inside CmdBeginRendering is invalid
	for _, idx := range s.dynamicQueue {
		al := s.atlas.lightAllocs[idx]
		pool := &s.atlas.slotsPool[al.pool]
		for _, si := range al.slots {
			sl := pool.slots[si]
			b.CopyDepthRegion(s.atlas.staticTarget, s.atlas.dynamicTarget,
				sl.x, sl.y, sl.x, sl.y, al.size, al.size)
		}
	}

	if len(s.dynamicQueue) > 0 {
		// Loads rather than clears: every queued tile was just overwritten by its
		// copy, and every tile not queued is holding the frame it was built for.
		// That cache is the whole point of the split
		b.BeginPass(s.atlas.dynamicTarget, nil, true)
		for _, idx := range s.dynamicQueue {
			al := s.atlas.lightAllocs[idx]
			s.dynamicBakes += s.bakeLight(idx, al, f, depthShader, depthPointShader, true)
		}
		b.EndPass()
	}
}

// Returns how many tiles the last frame baked into each atlas
func (s *Scene) BakeCounts() (static, dynamic int) { return s.staticBakes, s.dynamicBakes }

// Returns this frame's records, for the backend to publish once per frame
func (s *Scene) ShadowRecords() []renderer.ShadowTile { return s.shadowTiles }
