package scene

import (
	"math"
	"sort"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

// One depth texture holds every shadow in the scene, each light owning a
// sub-rect of it: a sun or spot one tile, a point light six 90° tiles.
const atlasSize = 4096

// The extremes of the slot layout below, as constants so the debug map can size
// its grid with them. init() checks they still match the table
const (
	sunTileSize = atlasSize / 2
	maxTileSize = atlasSize / 2
	minTileSize = atlasSize / 32
)

// The fixed slot layout: the atlas is carved once at load and never repartitioned
//
// Every size is a division of atlasSize rather than a pixel count, so changing
// the atlas rescales the whole layout instead of changing how many lights fit.
// That splits one confusing knob into two orthogonal ones — atlas size buys
// sharpness, the counts below buy light budget — which is what a quality
// setting wants: turning shadows down must not stop lights casting.
//
// Slots are typeless; only size matters. A point light's six faces each carry
// their own atlasRect and are never filtered across, so they need not be
// adjacent — a point light takes six slots of one size from wherever they
// happen to be. That is what keeps a fixed layout from being rigid.
//
// This is LIGHTING_PLAN.md §4.1's partition, quadrant for quadrant: the sun owns
// one, and the other three each hold one tier at a single size. 337 slots for
// the plan's 1 sun + 52 point + 24 spot = 77 shadowed lights, and 100% of the
// atlas — the four rows below are 4 + 4 + 4 + 4 of the atlas's 16 cells of 1024².
//
// That exact accounting is why there is no 1024 tier. A 1024 row would cost four
// cells, which is a whole quadrant, and the only quadrant it could come from is
// the 128 row — so a 4096 atlas can have §4.3's high tier or the 40 far point
// lights the drawing puts in the bottom right, never both. The drawing chose the
// light count; this follows it, and shadowTiers below drops to match.
//
// A scene with no sun should trade the first row for four 1024s: nothing else is
// ever capped that high, so the slot would otherwise sit idle.
var slotLayout = []struct {
	size, count int
}{
	{atlasSize / 2, 1},    // 2048: the sun, alone
	{atlasSize / 8, 16},   // 512:  near lights
	{atlasSize / 16, 64},  // 256:  mid distance
	{atlasSize / 32, 256}, // 128:  distant / small
}

// Score thresholds and the tile size each earns, highest first
//
// score = radius / distance, which is the light's rough screen-space footprint:
// the same light gets a bigger tile as the camera walks toward it. Below the
// last threshold a light is not worth a tile at all and lights unshadowed.
//
// This is the *ceiling* on what a light may hold, not what it is handed. Rank
// picks the slot and this caps it, so a lone light with a tiny footprint cannot
// claim a big slot it would only spend bake time on.
//
// One tier per non-sun row of slotLayout, and they have to stay in step: a
// ceiling with no pool at that size is not an error, it just reads as every
// light in that band being permanently "degraded from" a size the atlas does not
// hold. §4.3 of the plan writes these as 1024 / 512 / 256, which is one step
// coarser than the partition §4.1 draws; the layout comment says why the
// drawing wins.
var shadowTiers = []struct {
	minScore float32
	size     int
}{
	{0.50, atlasSize / 8},
	{0.20, atlasSize / 16},
	{0.08, atlasSize / 32},
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

// Checks the layout against the constants derived from it, at load rather than
// as a silently wrong partition
//
// A slot size the tree can never produce is not an error anywhere else: alloc
// simply refuses, every light ends up with ShadowIndex = -1, and the scene
// renders unshadowed with nothing logged.
func init() {
	if atlasSize&(atlasSize-1) != 0 {
		panic("atlasSize must be a power of two: the layout is carved by halving")
	}
	if slotLayout[0].size != maxTileSize || slotLayout[len(slotLayout)-1].size != minTileSize {
		panic("slotLayout no longer spans maxTileSize..minTileSize")
	}
	texels := 0
	for i, s := range slotLayout {
		if s.size&(s.size-1) != 0 || s.size > atlasSize {
			panic("every slot size must be a power-of-two division of the atlas")
		}
		if i > 0 && s.size >= slotLayout[i-1].size {
			panic("slotLayout must be ordered largest size first")
		}
		texels += s.count * s.size * s.size
	}
	if texels > atlasSize*atlasSize {
		panic("the shadow slot layout asks for more texels than the atlas has")
	}
}

// The atlas and who owns what of it
//
// The layout persists for the life of the scene and its rects never move, which
// is the precondition for Part E baking a tile once and leaving it alone: a
// light that keeps its slot keeps its exact pixels, so validity is one dirty
// flag per slot rather than a comparison of rects.
type shadowAtlas struct {
	target renderer.RenderTargetHandle // the depth target BakeShadows draws into
	tex    renderer.TextureHandle      // the sampled view of the same image
	pools  []slotPool                  // one per size, largest first
	allocs map[int32]*lightAlloc       // by index into Scene.Lights
}

// One slot of the fixed layout: a top-left corner that never moves
type slot struct {
	x, y int
}

// Every slot of one size, plus which of them are free this frame
type slotPool struct {
	size  int
	slots []slot
	used  []bool // scratch, rebuilt every frame from what the keepers hold
	free  []int  // indices into slots, rebuilt from used
}

// What one light currently holds
type lightAlloc struct {
	tier  int          // the scored tier ceiling, kept as the hysteresis state
	want  int          // the per-face size that ceiling allows
	size  int          // the per-face size it actually got, want or smaller
	pool  int          // index into shadowAtlas.pools
	slots []int        // indices into that pool, so a keeper can hold its rects
	tiles []shadowTile // 1 for a sun or a spot, 6 for a point light
}

// One allocated tile, in atlas pixels: what the bake sets its viewport to
type shadowTile struct {
	x, y, size int   // top-left corner and side, in atlas pixels
	light      int32 // index into Scene.Lights, so the bake can find its Pos/Dir/Type
}

// Creates the atlas texture, once, at scene load
func (a *shadowAtlas) setup(b renderer.Backend) {
	a.reset()
	a.target, a.tex = b.CreateRenderTarget(renderer.RenderTargetSpec{
		Width:  atlasSize,
		Height: atlasSize,
		Format: renderer.TargetDepth,
	})
}

// Rebuilds the layout and forgets every allocation
func (a *shadowAtlas) reset() {
	a.pools = buildLayout()
	a.allocs = map[int32]*lightAlloc{}
}

// Carves the fixed layout out of the atlas, once
//
// A buddy tree places the rects and is then thrown away: filling largest size
// first can never fragment, so this is the one job splitting is unambiguously
// right for. Keeping it out of the frame loop is what lets the merge half of
// the tree — the subtle half, and the one that failed silently — go entirely.
func buildLayout() []slotPool {
	root := &quadNode{size: atlasSize}
	pools := make([]slotPool, 0, len(slotLayout))
	for _, spec := range slotLayout {
		p := slotPool{
			size:  spec.size,
			slots: make([]slot, 0, spec.count),
			used:  make([]bool, spec.count),
			free:  make([]int, 0, spec.count),
		}
		for n := 0; n < spec.count; n++ {
			x, y, ok := root.alloc(spec.size)
			if !ok {
				panic("the shadow slot layout does not pack into the atlas")
			}
			p.slots = append(p.slots, slot{x: x, y: y})
		}
		pools = append(pools, p)
	}
	return pools
}

// --- the layout quadtree -----------------------------------------------------

// One node of the buddy tree: free, allocated whole, or split into quadrants
//
// Used only by buildLayout, which allocates in descending size order and never
// frees, so there is no release and no merge. A quadtree rather than a shelf
// because every slot is a power of two: splitting is the whole placement rule
// and there is no packing heuristic to get wrong.
type quadNode struct {
	x, y, size int
	used       bool
	kids       [4]*quadNode
}

// Splits a node into its four quadrants
func (n *quadNode) split() {
	h := n.size / 2
	n.kids = [4]*quadNode{
		{x: n.x, y: n.y, size: h},
		{x: n.x + h, y: n.y, size: h},
		{x: n.x, y: n.y + h, size: h},
		{x: n.x + h, y: n.y + h, size: h},
	}
}

// Takes the first free node of exactly size, splitting larger ones on the way down
//
// First-fit in quadrant order, not best-fit: every node of a given depth is the
// same size, so there is no better fit to find.
func (n *quadNode) alloc(size int) (int, int, bool) {
	if n.used || size > n.size {
		return 0, 0, false
	}
	if n.size == size {
		// A split node still holds live descendants; only a leaf is takeable
		if n.kids[0] != nil {
			return 0, 0, false
		}
		n.used = true
		return n.x, n.y, true
	}
	if n.kids[0] == nil {
		n.split()
	}
	for _, k := range n.kids {
		if x, y, ok := k.alloc(size); ok {
			return x, y, true
		}
	}
	return 0, 0, false
}

// --- allocation policy -------------------------------------------------------

// The tile size a raw score earns, 0 meaning no tile at all
func rawTier(score float32) int {
	for _, t := range shadowTiers {
		if score > t.minScore {
			return t.size
		}
	}
	return 0
}

// The threshold a tier is entered at, 0 for the unshadowed tier
func tierThreshold(size int) float32 {
	for _, t := range shadowTiers {
		if t.size == size {
			return t.minScore
		}
	}
	return 0
}

// The tier a light should hold, given what it holds now
//
// Promotion needs the score to clear the new tier's threshold by the hysteresis
// margin; demotion needs it to have fallen the same margin below the threshold
// of the tier currently held. Between the two the light keeps what it has.
func tierFor(score float32, cur int) int {
	want := rawTier(score)
	if want == cur {
		return cur
	}
	if want > cur {
		if score > tierThreshold(want)*nextTierThreshold {
			return want
		}
		return cur
	}
	if score < tierThreshold(cur)/nextTierThreshold {
		return want
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

// The tier a light is entitled to this frame, given the tier it holds now
//
// The tier is the hysteresis state and stays in the units of shadowTiers; the
// per-face size below is derived from it, never the other way round, or a point
// light's halved size would read back as a demotion and oscillate.
func tierOf(l *Light, score float32, cur int) int {
	if l.Type == renderer.LightSun {
		return sunTileSize
	}
	return tierFor(score, cur)
}

// The per-face tile size a tier buys: the ceiling on what a light may hold
//
// A point light used to drop one step below its tier here, so its six faces
// would not cost 6x a spot's texels at the same score. That was rationing
// against an allocator which could hand the whole atlas to whoever asked first;
// a fixed layout does not need it, because a pool holds only the slots it holds
// and the degrade path covers a light that cannot fit in one.
//
// What keeping it cost is invisible until an atlas dump shows it. The largest
// scored tier and the largest scored pool are the same size, so halving locked
// every point light out of that pool — it stood empty behind lights entitled to
// it while everything below shuffled a tier down. §4.1 budgets that quadrant as
// 2 point lights plus 4 spots, which is only reachable unhalved.
func tileSizeFor(tier int) int {
	if tier == 0 {
		return 0
	}
	size := tier
	// Max clamps last: a layout whose smallest slot exceeds its largest is a
	// broken layout, and clamping up afterwards would hide it behind a ceiling
	// no pool can serve
	if size < minTileSize {
		size = minTileSize
	}
	if size > maxTileSize {
		size = maxTileSize
	}
	return size
}

// Gives back everything a light holds
//
// Only the map entry: the free lists are rebuilt wholesale from what survives,
// so there is nothing to hand back to a pool.
func (a *shadowAtlas) drop(light int32) {
	delete(a.allocs, light)
}

// Rescores every light and hands out the fixed layout's slots
//
// Rank picks the slot, the tier caps it. Lights sort by score and take the best
// free slot no larger than their ceiling, so importance decides who gets the
// good slots and the ceiling stops an unimportant light wasting one. Running out
// of slots costs the least important light its resolution and never costs frame
// time — it walks down a pool at a time and finally holds nothing, which leaves
// ShadowIndex = -1 and lights it unshadowed.
func (a *shadowAtlas) allocate(lights []Light, camPos mgl32.Vec3) {
	type request struct {
		idx        int32
		score, eff float32
		tier, want int
		count      int
	}
	reqs := make([]request, 0, len(lights))

	for i := range lights {
		l := &lights[i]
		cur, held := 0, false
		if al, ok := a.allocs[int32(i)]; ok {
			cur, held = al.tier, true
		}
		score := lightScore(l, camPos)
		tier := tierOf(l, score, cur)
		if tier == 0 {
			a.drop(int32(i))
			continue
		}
		// A light already holding slots ranks above an equal challenger, so the
		// two either side of the last free slot do not trade it every frame
		eff := score
		if held {
			eff *= slotStickiness
		}
		reqs = append(reqs, request{
			idx: int32(i), score: score, eff: eff,
			tier: tier, want: tileSizeFor(tier), count: tileCount(l),
		})
	}

	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].eff > reqs[j].eff })

	// Phase 1: plan against slot counts alone, in rank order, before touching
	// what anyone holds. Pools run largest first, so the first one both small
	// enough and deep enough is the best slot this light is allowed.
	plan := make(map[int32]int, len(reqs))
	avail := make([]int, len(a.pools))
	for i := range a.pools {
		avail[i] = len(a.pools[i].slots)
	}
	for _, r := range reqs {
		for p := range a.pools {
			if a.pools[p].size > r.want || avail[p] < r.count {
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
		if planned && a.pools[p].size >= r.want {
			continue
		}
		for q := range a.pools {
			if planned && a.pools[q].size <= a.pools[p].size {
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
		al, ok := a.allocs[r.idx]
		p, planned := plan[r.idx]
		if ok && planned && al.pool == p && len(al.slots) == r.count {
			al.tier, al.want = r.tier, r.want
			keep[r.idx] = true
		}
	}
	for idx := range a.allocs {
		if !keep[idx] {
			a.drop(idx)
		}
	}

	// Rebuild each pool's free list from what the keepers hold, rather than
	// pushing and popping as lights come and go: one pass over ~140 slots a
	// frame, and it cannot leak a slot the way an incremental stack can
	for p := range a.pools {
		pool := &a.pools[p]
		for i := range pool.used {
			pool.used[i] = false
		}
	}
	for _, al := range a.allocs {
		for _, s := range al.slots {
			a.pools[al.pool].used[s] = true
		}
	}
	for p := range a.pools {
		pool := &a.pools[p]
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
		pool := &a.pools[p]
		if len(pool.free) < r.count {
			panic("the shadow slot plan promised slots the pool does not hold")
		}
		idxs := make([]int, r.count)
		copy(idxs, pool.free[len(pool.free)-r.count:])
		pool.free = pool.free[:len(pool.free)-r.count]

		tiles := make([]shadowTile, r.count)
		for k, si := range idxs {
			s := pool.slots[si]
			tiles[k] = shadowTile{x: s.x, y: s.y, size: pool.size, light: r.idx}
		}
		a.allocs[r.idx] = &lightAlloc{
			tier: r.tier, want: r.want, size: pool.size,
			pool: p, slots: idxs, tiles: tiles,
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

// Allocates a tile per casting light and builds this frame's shadow records
//
// Runs before FillFrameUniforms, which copies each light's ShadowIndex into the
// block, and before the bake, which walks the same tiles.
func (s *Scene) UpdateShadows(nearPlane, farPlane float32) {
	s.atlas.allocate(s.Lights, s.Cam.Pos)

	s.tiles = s.tiles[:0]
	s.shadowRecords = s.shadowRecords[:0]

	for i := range s.Lights {
		l := &s.Lights[i]
		al, ok := s.atlas.allocs[int32(i)]
		if !ok {
			l.shadowIndex, l.shadowCount = -1, 0
			continue
		}
		l.shadowIndex = int32(len(s.shadowRecords))
		l.shadowCount = int32(len(al.tiles))
		for face, tile := range al.tiles {
			s.tiles = append(s.tiles, tile)
			s.shadowRecords = append(s.shadowRecords, l.shadowRecord(tile, face, len(al.tiles),
				nearPlane, farPlane))
		}
	}
}

// Builds one tile's record: its projection, its rect and its depth encoding
func (l *Light) shadowRecord(tile shadowTile, face, faces int,
	nearPlane, farPlane float32) renderer.ShadowRecord {

	rec := renderer.ShadowRecord{
		AtlasCoords: [4]float32{
			float32(tile.x) / atlasSize, float32(tile.y) / atlasSize,
			float32(tile.size) / atlasSize, float32(tile.size) / atlasSize,
		},
		PCFStep:   1.0 / float32(tile.size),
		FarPlane:  farPlane,
		FaceIndex: -1,
		// Part E sets bit 0 on the tiles that live in the dynamic atlas
		Flags: 0,
	}

	if faces == 6 {
		dir, up := cubeFaceDirs[face][0], cubeFaceDirs[face][1]
		proj := mgl32.Perspective(cubeFaceFov(tile.size), 1, nearPlane, farPlane)
		rec.WorldToTile = proj.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(dir), up))
		rec.FaceIndex = int32(face)
		return rec
	}

	if l.Type == renderer.LightSpot {
		// The cone's own frustum, widened by two texels for the same reason a
		// cube face is: the PCF kernel must stay inside the tile at its rim
		fov := 2 * float32(math.Acos(float64(mgl32.Clamp(l.OuterCutoff, -1, 1))))
		fov *= float32(tile.size+2) / float32(tile.size)
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

// Bakes every allocated tile in one pass over the atlas
//
// One BeginPass for the whole atlas rather than one per light: the target bind
// was the expensive part, and SetViewportScissor is what makes the tiles
// separate. The depth clear covers the whole atlas once, so a tile that is not
// re-baked this frame is cleared too — Part E is what stops that from happening.
func (s *Scene) BakeShadows(depthShader, depthPointShader renderer.ShaderHandle,
	f *renderer.FrameUniforms) {

	if len(s.tiles) == 0 {
		return
	}
	b := s.backend

	// Static mesh geometry is baked into the OBJ vertices, so the depth passes
	// draw everything with an identity model matrix and no material at all
	u := renderer.DrawUniforms{Model: mgl32.Ident4()}

	b.BeginPass(s.atlas.target, nil)
	for i, tile := range s.tiles {
		rec := &s.shadowRecords[i]
		l := &s.Lights[tile.light]

		// Focus on part of atlas corresponding to tile
		b.SetViewportScissor(tile.x, tile.y, tile.size, tile.size)

		f.CurWorldToTile = rec.WorldToTile
		f.CurLightPos = l.Pos
		f.CurFarPlane = rec.FarPlane
		b.BindFrameUniforms(f)

		// Back-face culling, the scene default, so the surface facing the light is
		// what lands in the map and a shadow stays welded to its caster's base.
		//
		// Front-face culling is the other classic choice and it is wrong here: it
		// bakes the far side of a closed mesh, so the depth stored is a whole
		// diameter too far and a sphere floats above a lit disc of its own size.
		// It escapes acne by hiding the bias inside the geometry; the normal
		// offset in shadowLookup does that job instead, and CastsShadow does the
		// rest — see the field's comment for why a ground plane opts out.
		if rec.FaceIndex >= 0 {
			// A face tile stores radial distance, which needs the fragment stage
			b.BindShader(depthPointShader)
		} else {
			b.BindShader(depthShader)
		}
		for m := range s.Meshes {
			if !s.Meshes[m].CastsShadow {
				continue
			}
			s.Meshes[m].draw(&u)
		}
	}
	b.EndPass()
}

// Returns this frame's records, for the backend to publish once per frame
func (s *Scene) ShadowRecords() []renderer.ShadowRecord { return s.shadowRecords }
