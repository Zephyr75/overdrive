package scene

import (
	"math"
	"sort"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// The quality tier the atlas is carved to, rebuilt from settings on every
// shadowAtlas.reset and known good by then (settings.checkShadowAtlas rejects a
// layout that cannot be carved). LIGHTING_PLAN.md §4.1 is the default partition
var (
	atlasSize = settings.ShadowAtlasSize
	// The extremes of the slot layout, named so the layout and the allocator can
	// size themselves off it
	sunTileSize = atlasSize / 2  // the largest slot, and the sun's ceiling
	minTileSize = atlasSize / 32 // the smallest, so the unit buildLayout counts in

	// The fixed slot layout, carved once at load and never repartitioned
	//
	// Sizes are divisions of atlasSize, so the atlas buys sharpness and the counts
	// buy light budget; slots are typeless and need not be adjacent
	slotLayout []struct{ size, count int }

	// Score thresholds and the tile size each earns, highest first, sized off the
	// non-sun rows of slotLayout so a ceiling can never name a size no pool holds
	//
	// A *ceiling*, not an allocation: rank picks the slot and this caps it
	shadowTiers []struct {
		minScore float32
		size     int
	}
)

// Rebuilds the layout tables from settings, before any slot is carved
func loadLayoutSettings() { // TODO: review
	atlasSize = settings.ShadowAtlasSize
	div, count := settings.ShadowSlotDivisors, settings.ShadowSlotCounts

	slotLayout = slotLayout[:0]
	for i := range div {
		slotLayout = append(slotLayout, struct{ size, count int }{atlasSize / div[i], count[i]})
	}
	sunTileSize = slotLayout[0].size
	minTileSize = slotLayout[len(slotLayout)-1].size

	shadowTiers = shadowTiers[:0]
	for i, score := range settings.ShadowTierScores {
		shadowTiers = append(shadowTiers, struct {
			minScore float32
			size     int
		}{score, slotLayout[i+1].size})
	}
}

// How far past a threshold a score must go before the tier actually changes,
// a boundary score otherwise forcing a full re-bake every frame
const nextTierThreshold = 1.2

// How much a challenger must outscore a slot's current holder to evict it
//
// The other axis: nextTierThreshold guards a fixed threshold, this guards two
// lights trading an empty pool's last slot (LIGHTING_PLAN.md §4.3)
const slotStickiness = 1.2

// The atlas and who owns what of it
//
// Its rects never move, which is what lets a light that keeps its slot keep its
// exact pixels: validity is one dirty flag, not a comparison of rects
type shadowAtlas struct {
	// Two atlases carved by one slotLayout, so a tile has the same rect in both
	// and the copy between them is a straight blit with no remap
	staticImage  renderer.ImageHandle
	dynamicImage renderer.ImageHandle
	staticView   renderer.ViewHandle
	dynamicView  renderer.ViewHandle

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

	// Whether each atlas holds a current bake: staticValid while the light keeps
	// its slots, dynamicValid until a caster in range moves
	staticValid, dynamicValid bool
	// Whether a movable caster is in range at all, and what this frame queued
	dynamicStudy                bool
	staticQueued, dynamicQueued bool
	// Where this light's tiles start in Scene.shadowTiles
	tilesIndex int
}

// Creates both atlas images, once, at scene load
//
// They take the two dedicated descriptor slots forward.slang indexes by a
// literal: the static atlas is hot slot 0 and the dynamic one hot slot 1, which
// is what ShadowRecord.Flags bit 0 selects between
func (atlas *shadowAtlas) setup(backend renderer.Backend) { // TODO: review
	atlas.reset()

	// Nearest, because PCF does its own filtering and a comparison here would
	// average depths rather than occlusions. The white border is what says
	// "fully lit" outside a sun's frustum
	sampler := backend.CreateSampler(renderer.SamplerInfo{
		Name: "shadowAtlas", Mag: renderer.FilterNearest, Min: renderer.FilterNearest,
		Mipmap:   renderer.FilterNearest,
		OutsideU: renderer.OutsideClampToBorder,
		OutsideV: renderer.OutsideClampToBorder,
		OutsideW: renderer.OutsideClampToBorder,
		Border:   renderer.BorderWhite,
		MaxLod:   1,
	})
	// Transfer on both ends: one atlas is the source of a cached tile and the
	// destination of another's copy
	spec := renderer.ImageInfo{
		Width: atlasSize, Height: atlasSize, Format: renderer.FormatDepth32F,
		Usage: renderer.ImageSampled | renderer.ImageDepthAttachment |
			renderer.ImageCopySrc | renderer.ImageCopyDst,
		Sampler: sampler, Hot: true,
	}
	spec.Name, spec.HotSlot = "shadowAtlasStatic", 0
	atlas.staticImage = backend.CreateImage(spec)
	spec.Name, spec.HotSlot = "shadowAtlasDynamic", 1
	atlas.dynamicImage = backend.CreateImage(spec)

	atlas.staticView = backend.CreateView(atlas.staticImage, renderer.ViewInfo{Name: "shadowAtlasStatic", Aspect: renderer.AspectDepth})
	atlas.dynamicView = backend.CreateView(atlas.dynamicImage, renderer.ViewInfo{Name: "shadowAtlasDynamic", Aspect: renderer.AspectDepth})

	// Writes the dedicated descriptors, which is what Slot does for a hot image
	backend.Slot(atlas.staticImage)
	backend.Slot(atlas.dynamicImage)
}

// Rebuilds the layout and forgets every allocation
func (atlas *shadowAtlas) reset() { // TODO: review
	loadLayoutSettings()
	atlas.slotsPool = buildLayout()
	atlas.lightAllocs = map[int32]*lightAlloc{}
}

// Carves the fixed layout out of the atlas, once
//
// Power-of-two divisions in descending size, so placement needs no search: Z
// order is what a buddy tree filled largest-first emits, and cannot fragment
func buildLayout() []slotsPool { // TODO: review
	pools := make([]slotsPool, 0, len(slotLayout))
	cell, side := 0, atlasSize/minTileSize
	for _, spec := range slotLayout {
		pool := slotsPool{
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
			pool.slots = append(pool.slots, slot{x: x, y: y})
			cell += cells
		}
		pools = append(pools, pool)
	}
	if cell > side*side {
		panic("the shadow slot layout does not pack into the atlas")
	}
	return pools
}

// De-interleaves a Z-order cell index into the atlas pixels of its top-left corner
func zOrder(cell int) (int, int) { // TODO: review
	x, y := 0, 0
	for bit := 0; cell != 0; bit, cell = bit+1, cell>>2 {
		x |= (cell & 1) << bit
		y |= (cell >> 1 & 1) << bit
	}
	return x * minTileSize, y * minTileSize
}

// --- allocation policy -------------------------------------------------------

// The tier a raw score earns, len(shadowTiers) meaning no tile at all
func rawTier(score float32) int { // TODO: review
	for i, tier := range shadowTiers {
		if score > tier.minScore {
			return i
		}
	}
	return len(shadowTiers)
}

// The tier a light should hold, given what it holds now
//
// Both directions need the score to clear the threshold by nextTierThreshold;
// between the two the light keeps what it has
func tierFor(score float32, cur int) int { // TODO: review
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
func tileCount(light *Light) int { // TODO: review
	if light.Type == renderer.LightPoint {
		return 6
	}
	return 1
}

// A light's screen-space importance: how much of the view its lit volume covers
//
// A sun has no radius and no position that means anything to this, and it is the
// one light every pixel sees, so it outranks everything scored.
func lightScore(light *Light, camPos mgl32.Vec3) float32 { // TODO: review
	if light.Type == renderer.LightSun {
		return math.MaxFloat32
	}
	dist := light.Pos.Sub(camPos).Len()
	if dist < 1e-3 {
		dist = 1e-3
	}
	return light.Radius / dist
}

// Gives back everything a light holds
//
// Only the map entry: the free lists are rebuilt wholesale from what survives
func (atlas *shadowAtlas) drop(light int32) { // TODO: review
	delete(atlas.lightAllocs, light)
}

// One light's claim on the atlas this frame, as the ranked phases see it
type request struct {
	idx        int32
	eff        float32 // score, times slotStickiness when the light already holds slots
	tier, want int     // index into shadowTiers, -1 for a sun, and the size it caps at
	count      int     // tiles needed: 6 for a point light, 1 otherwise
}

// Rescores every light and hands out the fixed layout's slots
//
// Rank picks the slot, the tier caps it. Running out costs the least important
// light its resolution, a pool at a time, and never costs frame time
func (atlas *shadowAtlas) allocate(lights []Light, camPos mgl32.Vec3) { // TODO: review
	reqs := atlas.rankRequests(lights, camPos)
	plan, avail := atlas.planByTier(reqs)
	atlas.offerSpareSlots(reqs, plan, avail)
	keep := atlas.keepMatchingAllocs(reqs, plan)
	atlas.rebuildFreeLists()
	atlas.assignPlanned(reqs, plan, keep)
}

// Scores every light, applies the tier hysteresis and sorts by rank
//
// A light already holding slots ranks above an equal challenger, so the two
// either side of the last free slot do not trade it every frame
func (atlas *shadowAtlas) rankRequests(lights []Light, camPos mgl32.Vec3) []request { // TODO: review
	reqs := make([]request, 0, len(lights))
	for i := range lights {
		light := &lights[i]
		cur, held := len(shadowTiers), false
		if alloc, ok := atlas.lightAllocs[int32(i)]; ok {
			cur, held = alloc.tier, true
		}
		score := lightScore(light, camPos)
		// A sun is never scored, so it never enters shadowTiers: tier -1
		tier, want := -1, sunTileSize
		if light.Type != renderer.LightSun {
			tier = tierFor(score, cur)
			if tier == len(shadowTiers) {
				atlas.drop(int32(i))
				continue
			}
			want = shadowTiers[tier].size
		}
		eff := score
		if held {
			eff *= slotStickiness
		}
		reqs = append(reqs, request{
			idx: int32(i), eff: eff,
			tier: tier, want: want, count: tileCount(light),
		})
	}
	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].eff > reqs[j].eff })
	return reqs
}

// Phase 1: plans against slot counts alone, in rank order, before touching what
// anyone holds — pools run largest first, so the first one both small enough and
// deep enough is the best slot this light is allowed
func (atlas *shadowAtlas) planByTier(reqs []request) (map[int32]int, []int) { // TODO: review
	plan := make(map[int32]int, len(reqs))
	avail := make([]int, len(atlas.slotsPool))
	for i := range atlas.slotsPool {
		avail[i] = len(atlas.slotsPool[i].slots)
	}
	for _, req := range reqs {
		for poolIdx := range atlas.slotsPool {
			if atlas.slotsPool[poolIdx].size > req.want || avail[poolIdx] < req.count {
				continue
			}
			avail[poolIdx] -= req.count
			plan[req.idx] = poolIdx
			break
		}
	}
	return plan, avail
}

// Phase 1b: re-offers spare slots to lights under their ceiling, never to lights
// at it (LIGHTING_PLAN.md §4.3: offering to everyone stops the score selecting a
// size at all)
func (atlas *shadowAtlas) offerSpareSlots(reqs []request, plan map[int32]int, avail []int) { // TODO: review
	for _, req := range reqs {
		poolIdx, planned := plan[req.idx]
		if planned && atlas.slotsPool[poolIdx].size >= req.want {
			continue
		}
		for otherIdx := range atlas.slotsPool {
			if planned && atlas.slotsPool[otherIdx].size <= atlas.slotsPool[poolIdx].size {
				break // nothing larger than what it already has is spare
			}
			if avail[otherIdx] < req.count {
				continue
			}
			if planned {
				avail[poolIdx] += req.count
			}
			avail[otherIdx] -= req.count
			plan[req.idx] = otherIdx
			break
		}
	}
}

// Phase 2: a light whose plan lands in the pool it already holds keeps its exact
// slots, so Part E can leave the tile baked. Everyone else gives theirs back
func (atlas *shadowAtlas) keepMatchingAllocs(reqs []request, plan map[int32]int) map[int32]bool { // TODO: review
	keep := make(map[int32]bool, len(reqs))
	for _, req := range reqs {
		alloc, ok := atlas.lightAllocs[req.idx]
		poolIdx, planned := plan[req.idx]
		if ok && planned && alloc.pool == poolIdx && len(alloc.slots) == req.count {
			alloc.tier = req.tier
			keep[req.idx] = true
		}
	}
	for idx := range atlas.lightAllocs {
		if !keep[idx] {
			atlas.drop(idx)
		}
	}
	return keep
}

// Rebuilds every pool's free list from what the keepers hold, wholesale rather
// than pushing and popping: it cannot leak a slot the way an incremental stack can
func (atlas *shadowAtlas) rebuildFreeLists() { // TODO: review
	for poolIdx := range atlas.slotsPool {
		pool := &atlas.slotsPool[poolIdx]
		for i := range pool.used {
			pool.used[i] = false
		}
	}
	for _, alloc := range atlas.lightAllocs {
		for _, slotIdx := range alloc.slots {
			atlas.slotsPool[alloc.pool].used[slotIdx] = true
		}
	}
	for poolIdx := range atlas.slotsPool {
		pool := &atlas.slotsPool[poolIdx]
		pool.free = pool.free[:0]
		for i, used := range pool.used {
			if !used {
				pool.free = append(pool.free, i)
			}
		}
	}
}

// Phase 3: hands the planned slots to everyone not keeping theirs
//
// The plan was made against the same counts and the keepers hold exactly what it
// gave them, so a pool coming up short is a bug in the phases above
func (atlas *shadowAtlas) assignPlanned(reqs []request, plan map[int32]int, keep map[int32]bool) { // TODO: review
	for _, req := range reqs {
		if keep[req.idx] {
			continue
		}
		poolIdx, planned := plan[req.idx]
		if !planned {
			continue
		}
		pool := &atlas.slotsPool[poolIdx]
		if len(pool.free) < req.count {
			panic("the shadow slot plan promised slots the pool does not hold")
		}
		idxs := make([]int, req.count)
		copy(idxs, pool.free[len(pool.free)-req.count:])
		pool.free = pool.free[:len(pool.free)-req.count]

		atlas.lightAllocs[req.idx] = &lightAlloc{
			tier: req.tier, size: pool.size,
			pool: poolIdx, slots: idxs,
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
func cubeFaceFov(tile int) float32 { // TODO: review
	half := math.Tan(math.Pi/4) * float64(tile+2) / float64(tile)
	return float32(2 * math.Atan(half))
}

// Whether a mesh that can move is close enough to matter to this light
//
// A sun reaches everything; anything else is the caster's bounding sphere
// against the radius the shading already culls by, so the two agree.
func (scene *Scene) casterInRange(light *Light, mesh *Mesh, center mgl32.Vec3) bool { // TODO: review
	if light.Type == renderer.LightSun {
		return true
	}
	return center.Sub(light.Pos).Len() <= light.Radius+mesh.boundsRadius
}

// Whether any movable caster is in range, which is what earns a dynamic tile
func (scene *Scene) movableCasterInRange(light *Light) bool { // TODO: review
	for i := range scene.Meshes {
		mesh := &scene.Meshes[i]
		if mesh.Movable && mesh.CastsShadow && scene.casterInRange(light, mesh, mesh.boundsCenter) {
			return true
		}
	}
	return false
}

// Whether a caster that moved since the last update is in range
//
// Both centres, because a caster leaving a light's range has to dirty the tile
// it is leaving: its new position alone would say it never mattered.
func (scene *Scene) movedCasterInRange(light *Light) bool { // TODO: review
	for _, i := range scene.movedMeshes {
		mesh := &scene.Meshes[i]
		if !mesh.CastsShadow {
			continue
		}
		if scene.casterInRange(light, mesh, mesh.boundsCenter) || scene.casterInRange(light, mesh, mesh.prevCenter) {
			return true
		}
	}
	return false
}

// A light wanting its dynamic tile rebuilt, ranked so a budget that runs out
// costs the least important light its update rather than costing frame time
type dynamicUpdate struct {
	idx   int32
	score float32
}

// Allocates a tile per casting light, decides this frame's bake work and builds
// the shadow records
//
// Runs before FillFrameUniforms and before the bake, both of which read what it
// leaves behind
func (scene *Scene) UpdateShadows(nearPlane, farPlane float32) { // TODO: review
	scene.atlas.allocate(scene.Lights, scene.Cam.Pos)
	scene.resetQueues()
	dirty, runStatic := scene.markDirtyTiles()
	scene.queueStaticBakes(runStatic)
	scene.queueDynamicBakes(dirty)
	scene.buildRecords(nearPlane, farPlane)
	scene.commitBakes()
}

// Empties this frame's tile and bake lists
func (scene *Scene) resetQueues() { // TODO: review
	scene.shadowTiles = scene.shadowTiles[:0]
	scene.staticQueue = scene.staticQueue[:0]
	scene.dynamicQueue = scene.dynamicQueue[:0]
}

// Decides per light whether its dynamic tile wants rebuilding, and reports
// whether any static tile is stale
func (scene *Scene) markDirtyTiles() ([]dynamicUpdate, bool) { // TODO: review
	updates := make([]dynamicUpdate, 0, len(scene.Lights))
	runStatic := false

	for i := range scene.Lights {
		alloc, ok := scene.atlas.lightAllocs[int32(i)]
		if !ok {
			continue
		}
		light := &scene.Lights[i]
		alloc.staticQueued, alloc.dynamicQueued = false, false
		// Without the second atlas every record falls back to the static one and
		// movers cast nothing
		alloc.dynamicStudy = settings.ShadowDynamicAtlas && scene.movableCasterInRange(light)
		if !alloc.staticValid {
			runStatic = true
		}
		// A dynamic tile is built from its static one, so a static re-bake forces
		// the copy; otherwise only a caster that moved does
		if alloc.dynamicStudy && (!alloc.staticValid || !alloc.dynamicValid || scene.movedCasterInRange(light)) {
			updates = append(updates, dynamicUpdate{idx: int32(i), score: lightScore(light, scene.Cam.Pos)})
		}
	}
	return updates, runStatic
}

// Queues the static atlas, all or nothing: a tile whose frustum holds no caster
// writes nothing, so baking only the changed slots would leave one wearing its
// last owner's depth
func (scene *Scene) queueStaticBakes(runStatic bool) { // TODO: review
	if !runStatic {
		return
	}
	for i := range scene.Lights {
		if alloc, ok := scene.atlas.lightAllocs[int32(i)]; ok {
			alloc.staticQueued = true
			alloc.dynamicValid = false
			scene.staticQueue = append(scene.staticQueue, int32(i))
		}
	}
}

// Queues dynamic tiles in score order, within the frame's texel budget: a light
// that misses out keeps the tile it has
func (scene *Scene) queueDynamicBakes(updates []dynamicUpdate) { // TODO: review
	sort.SliceStable(updates, func(i, j int) bool { return updates[i].score > updates[j].score })
	budget := settings.ShadowBakeBudget()
	for _, update := range updates {
		alloc := scene.atlas.lightAllocs[update.idx]
		cost := len(alloc.slots) * alloc.size * alloc.size
		if cost > budget {
			continue
		}
		budget -= cost
		alloc.dynamicQueued = true
		scene.dynamicQueue = append(scene.dynamicQueue, update.idx)
	}
}

// Builds one ShadowRecord per tile and points each light at its first
func (scene *Scene) buildRecords(nearPlane, farPlane float32) { // TODO: review
	for i := range scene.Lights {
		light := &scene.Lights[i]
		alloc, ok := scene.atlas.lightAllocs[int32(i)]
		if !ok {
			light.shadowIndex, light.shadowCount = -1, 0
			continue
		}
		alloc.tilesIndex = len(scene.shadowTiles)

		// A tile whose static bake has not run still holds its last owner's depth,
		// so the light goes unshadowed rather than wearing another light's shadow
		if alloc.staticValid || alloc.staticQueued {
			light.shadowIndex = int32(len(scene.shadowTiles))
			light.shadowCount = int32(len(alloc.slots))
		} else {
			light.shadowIndex, light.shadowCount = -1, 0
		}
		// Until the copy has happened the static tile is the same shadow without
		// the moving caster, which is the right thing to fall back to
		dyn := alloc.dynamicStudy && (alloc.dynamicQueued || (alloc.dynamicValid && !alloc.staticQueued))

		// The rect a slot names, resolved here rather than stored: the layout is
		// carved once and never moves, so the pool is the one place it lives
		pool := &scene.atlas.slotsPool[alloc.pool]
		for face, slotIdx := range alloc.slots {
			tile := pool.slots[slotIdx]
			scene.shadowTiles = append(scene.shadowTiles, light.shadowRecord(tile, alloc.size, face, len(alloc.slots),
				dyn, nearPlane, farPlane))
		}
	}
}

// Marks the queued tiles current and forgets this frame's movers
//
// Queueing marks a tile current, not the bake: BakeShadows draws exactly what
// these queues hold and cannot fail partway
func (scene *Scene) commitBakes() { // TODO: review
	for _, idx := range scene.staticQueue {
		scene.atlas.lightAllocs[idx].staticValid = true
	}
	for _, idx := range scene.dynamicQueue {
		scene.atlas.lightAllocs[idx].dynamicValid = true
	}
	// Where the movers were, so one that leaves a light's range still dirties the
	// tile it left
	for _, i := range scene.movedMeshes {
		scene.Meshes[i].prevCenter = scene.Meshes[i].boundsCenter
	}
	scene.movedMeshes = scene.movedMeshes[:0]
}

// Builds one tile's record: its projection, its rect and its depth encoding
//
// Takes the rect rather than reaching for it, so this stays a Light method with
// no knowledge of the atlas
func (light *Light) shadowRecord(tile slot, size int, face, faces int, dynamic bool,
	nearPlane, farPlane float32) renderer.ShadowTile { // TODO: review

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
			float32(tile.x) / float32(atlasSize), float32(tile.y) / float32(atlasSize),
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
		rec.WorldToTile = proj.Mul4(mgl32.LookAtV(light.Pos, light.Pos.Add(dir), up))
		rec.FaceIndex = int32(face)
		return rec
	}

	if light.Type == renderer.LightSpot {
		// The cone's own frustum, widened by two texels for the same reason a
		// cube face is: the PCF kernel must stay inside the tile at its rim
		fov := 2 * float32(math.Acos(float64(mgl32.Clamp(light.OuterCutoff, -1, 1))))
		fov *= float32(size+2) / float32(size)
		proj := mgl32.Perspective(mgl32.Clamp(fov, 0.01, 3.0), 1, nearPlane, farPlane)
		rec.WorldToTile = proj.Mul4(mgl32.LookAtV(light.Pos, light.Pos.Sub(light.Dir), spotUp(light.Dir)))
		return rec
	}

	// A sun's ortho box, unchanged from the per-light target it replaces
	proj := mgl32.Ortho(-10, 10, -10, 10, nearPlane, farPlane)
	rec.WorldToTile = proj.Mul4(mgl32.LookAtV(light.Pos, light.Pos.Sub(light.Dir), mgl32.Vec3{0, 1, 0}))
	return rec
}

// An up vector that is not parallel to dir, LookAtV producing NaNs when it is
func spotUp(dir mgl32.Vec3) mgl32.Vec3 { // TODO: review
	if math.Abs(float64(dir[1])) > 0.99 {
		return mgl32.Vec3{0, 0, 1}
	}
	return mgl32.Vec3{0, 1, 0}
}

// The six clip planes of a tile's frustum, as (a, b, c, d) with abc normalised
//
// Gribb-Hartmann: each plane is a row of the matrix combined with the w row.
// These matrices are the OpenGL convention, so near is row3 + row2
func frustumPlanes(matrix mgl32.Mat4) [6]mgl32.Vec4 { // TODO: review
	r0, r1, r2, r3 := matrix.Row(0), matrix.Row(1), matrix.Row(2), matrix.Row(3)
	planes := [6]mgl32.Vec4{
		r3.Add(r0), r3.Sub(r0),
		r3.Add(r1), r3.Sub(r1),
		r3.Add(r2), r3.Sub(r2),
	}
	for i := range planes {
		if n := (mgl32.Vec3{planes[i][0], planes[i][1], planes[i][2]}).Len(); n > 0 {
			planes[i] = planes[i].Mul(1 / n)
		}
	}
	return planes
}

// Whether a bounding sphere is inside every plane
//
// Conservative at the corners by design: over-including costs a draw the
// rasteriser discards, under-including costs a shadow
func sphereInFrustum(planes *[6]mgl32.Vec4, center mgl32.Vec3, radius float32) bool { // TODO: review
	for _, plane := range planes {
		if plane[0]*center[0]+plane[1]*center[1]+plane[2]*center[2]+plane[3] < -radius {
			return false
		}
	}
	return true
}

// Draws one light's tiles into the pass in progress, returning how many it drew into
//
// movable picks the half of the caster set this pass owns. Each tile uploads its
// own ~80-byte bake block rather than republishing the whole frame block, which
// is what keeps a 337-tile atlas inside the arena
func (scene *Scene) bakeLight(frame renderer.Frame, pass renderer.Pass, pipes Pipelines,
	lightIdx int32, alloc *lightAlloc, movable bool) int { // TODO: review

	// Static mesh geometry is baked into the OBJ vertices, so the depth passes
	// draw everything with an identity model matrix and no material at all
	uniforms := renderer.DrawUniforms{Model: mgl32.Ident4()}
	drawn := 0

	light := &scene.Lights[lightIdx]
	pool := &scene.atlas.slotsPool[alloc.pool]

	for k, slotIdx := range alloc.slots {
		rec := &scene.shadowTiles[alloc.tilesIndex+k]
		tile := pool.slots[slotIdx]
		planes := frustumPlanes(rec.WorldToTile)

		// The tile's state goes out only once a caster has survived the cull, so
		// a face pointing at empty space costs six plane tests and nothing else
		var ctx *drawContext
		for meshIdx := range scene.Meshes {
			mesh := &scene.Meshes[meshIdx]
			if !mesh.CastsShadow || mesh.Movable != movable {
				continue
			}
			if !sphereInFrustum(&planes, mesh.boundsCenter, mesh.boundsRadius) {
				continue
			}
			if ctx == nil {
				// Focus on the part of the atlas this tile occupies
				pass.Viewport(tile.x, tile.y, alloc.size, alloc.size)
				bake := renderer.BakeUniforms{
					WorldToTile: rec.WorldToTile,
					LightPos:    light.Pos,
					FarPlane:    rec.FarPlane,
				}
				// A face tile stores radial distance, which needs the fragment stage
				pipeline := pipes.Depth
				if rec.FaceIndex >= 0 {
					pipeline = pipes.DepthPoint
				}
				ctx = &drawContext{frame: frame, pass: pass, pipeline: pipeline}
				ctx.push[PushBake] = frame.Upload(&bake)
				drawn++
			}
			mesh.draw(ctx, &uniforms)
		}
	}
	return drawn
}

// Bakes this frame's queued tiles: the static atlas when allocation moved, then
// the dynamic one from a copy of it plus whatever can move
//
// One pass per atlas rather than one per light, the target bind being the
// expensive part; a settled scene queues nothing and does no GPU work at all
func (scene *Scene) BakeShadows(frame renderer.Frame, pipes Pipelines) { // TODO: review
	if len(scene.shadowTiles) == 0 {
		return
	}
	scene.staticBakes, scene.dynamicBakes = 0, 0

	// Clears, because the queue is every allocated light whenever it is not
	// empty — see UpdateShadows for why the static side is all-or-nothing
	if len(scene.staticQueue) > 0 {
		clear := [4]float32{1, 0, 0, 0}
		frame.Pass(renderer.PassInfo{
			Name:  "shadowStatic",
			Depth: &renderer.Attachment{View: scene.atlas.staticView, Clear: &clear, Store: true},
		}, func(pass renderer.Pass) {
			for _, idx := range scene.staticQueue {
				alloc := scene.atlas.lightAllocs[idx]
				scene.staticBakes += scene.bakeLight(frame, pass, pipes, idx, alloc, false)
			}
		})
	}

	// The dynamic tile starts as a copy of the static one, so the movable casters
	// draw on top of the baked scene with an ordinary depth test and the union
	// falls out. Outside any pass: a copy inside a render pass is invalid, which
	// is now a compile error rather than a line on stderr
	for _, idx := range scene.dynamicQueue {
		alloc := scene.atlas.lightAllocs[idx]
		pool := &scene.atlas.slotsPool[alloc.pool]
		for _, slotIdx := range alloc.slots {
			tile := pool.slots[slotIdx]
			frame.Copy(renderer.CopySpec{
				SrcImage: scene.atlas.staticImage, DstImage: scene.atlas.dynamicImage,
				SrcOffset: [3]int{tile.x, tile.y}, DstOffset: [3]int{tile.x, tile.y},
				Extent: [3]int{alloc.size, alloc.size, 1}, Aspect: renderer.AspectDepth,
			})
		}
	}

	if len(scene.dynamicQueue) > 0 {
		// Loads rather than clears: every queued tile was just overwritten by its
		// copy, and every tile not queued is holding the frame it was built for.
		// That cache is the whole point of the split
		frame.Pass(renderer.PassInfo{
			Name:  "shadowDynamic",
			Depth: &renderer.Attachment{View: scene.atlas.dynamicView, Store: true},
		}, func(pass renderer.Pass) {
			for _, idx := range scene.dynamicQueue {
				alloc := scene.atlas.lightAllocs[idx]
				scene.dynamicBakes += scene.bakeLight(frame, pass, pipes, idx, alloc, true)
			}
		})
	}
}

// Returns how many tiles the last frame baked into each atlas
func (scene *Scene) BakeCounts() (static, dynamic int) { return scene.staticBakes, scene.dynamicBakes } // TODO: review

// Returns this frame's shadow tiles, which the caller uploads once per frame
func (scene *Scene) ShadowRecords() []renderer.ShadowTile { return scene.shadowTiles } // TODO: review
