package scene

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

// Projects a world point through a record exactly as forward.slang's
// shadowLookup does, returning tile-local uv and depth
func projectIntoTile(rec renderer.ShadowRecord, p mgl32.Vec3) (u, v, z float32, ok bool) {
	clip := rec.WorldToTile.Mul4x1(p.Vec4(1))
	if clip[3] <= 0 {
		return 0, 0, 0, false
	}
	ndc := mgl32.Vec3{clip[0] / clip[3], clip[1] / clip[3], clip[2] / clip[3]}
	return ndc[0]*0.5 + 0.5, ndc[1]*0.5 + 0.5, ndc[2]*0.5 + 0.5, true
}

// Picks a cube face the way forward.slang's cubeFace() does, so a divergence
// between the two orders fails here rather than as a shadow from the wrong face
func cubeFaceOf(d mgl32.Vec3) int {
	ax, ay, az := abs32(d[0]), abs32(d[1]), abs32(d[2])
	switch {
	case ax >= ay && ax >= az:
		if d[0] > 0 {
			return 0
		}
		return 1
	case ay >= az:
		if d[1] > 0 {
			return 2
		}
		return 3
	default:
		if d[2] > 0 {
			return 4
		}
		return 5
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// The face a direction selects must be the face whose record projects that
// direction inside its own tile
//
// This is the failure the atlas made possible and the cubemap did not: the six
// matrices, the face order in cubeFaceDirs and the face order in cubeFace() are
// three separate lists that have to agree, and disagreeing renders a plausible
// shadow in the wrong place rather than an error.
func TestCubeFaceRecordsAgreeWithFaceSelection(t *testing.T) {
	l := Light{Type: renderer.LightPoint, Pos: mgl32.Vec3{1, 2, -3}}
	tile := shadowTile{x: 1024, y: 2048, size: 1024}

	// One direction per face, plus an off-axis one that still belongs to a face
	dirs := []mgl32.Vec3{
		{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1},
		{0.9, 0.3, -0.2}, {-0.2, -0.8, 0.4},
	}
	for _, d := range dirs {
		face := cubeFaceOf(d)
		rec := l.shadowRecord(tile, face, 6, 1, 50)
		if rec.FaceIndex != int32(face) {
			t.Fatalf("record for face %d reports face %d", face, rec.FaceIndex)
		}

		// A point 5 units from the light along d, well inside the far plane
		p := l.Pos.Add(d.Normalize().Mul(5))
		u, v, z, ok := projectIntoTile(rec, p)
		if !ok {
			t.Errorf("direction %v: face %d puts the point behind its frustum", d, face)
			continue
		}
		if u < 0 || u > 1 || v < 0 || v > 1 || z < 0 || z > 1 {
			t.Errorf("direction %v: face %d projects to uv (%.3f, %.3f) depth %.3f, outside its own tile",
				d, face, u, v, z)
		}
	}
}

// A tile's rect must map tile-local uv onto the pixels the bake's viewport wrote
func TestShadowRecordRectMatchesTile(t *testing.T) {
	l := Light{Type: renderer.LightSun, Dir: mgl32.Vec3{0, -1, 0}, Pos: mgl32.Vec3{0, 8, 0}}
	tile := shadowTile{x: 2048, y: 1024, size: 1024}
	rec := l.shadowRecord(tile, 0, 1, 1, 50)

	if rec.FaceIndex != -1 {
		t.Errorf("a sun tile reports face %d, want -1", rec.FaceIndex)
	}
	want := [4]float32{2048.0 / atlasSize, 1024.0 / atlasSize, 1024.0 / atlasSize, 1024.0 / atlasSize}
	if rec.AtlasCoords != want {
		t.Errorf("atlas rect %v, want %v", rec.AtlasCoords, want)
	}
	// The PCF step is one tile texel, which is what keeps the kernel inside the
	// rect after the clamp in tileSample
	if rec.PCFStep != 1.0/1024 {
		t.Errorf("texel size %v, want %v", rec.PCFStep, 1.0/1024)
	}

	// The scene origin sits under the sun, so it lands near the middle of the tile
	u, v, _, ok := projectIntoTile(rec, mgl32.Vec3{0, 0, 0})
	if !ok || u < 0.4 || u > 0.6 || v < 0.4 || v > 0.6 {
		t.Errorf("the origin projects to uv (%.3f, %.3f), want the tile centre", u, v)
	}
}

// The fixed layout must carve every slot it declares, inside the atlas and
// without two slots sharing a texel
//
// The layout is built once and trusted forever after, so a slot that overlaps
// its neighbour is not a transient allocator bug — it is two lights writing the
// same pixels for the whole session.
func TestSlotLayoutPacksTheAtlas(t *testing.T) {
	var a shadowAtlas
	a.reset()

	if len(a.pools) != len(slotLayout) {
		t.Fatalf("the layout built %d pools, want %d", len(a.pools), len(slotLayout))
	}

	type rect struct{ x, y, size int }
	var all []rect
	for p, pool := range a.pools {
		if pool.size != slotLayout[p].size || len(pool.slots) != slotLayout[p].count {
			t.Errorf("pool %d holds %d slots of %d, want %d of %d",
				p, len(pool.slots), pool.size, slotLayout[p].count, slotLayout[p].size)
		}
		for _, s := range pool.slots {
			if s.x < 0 || s.y < 0 || s.x+pool.size > atlasSize || s.y+pool.size > atlasSize {
				t.Errorf("slot (%d, %d) size %d runs past the atlas edge", s.x, s.y, pool.size)
			}
			all = append(all, rect{s.x, s.y, pool.size})
		}
	}

	for i, u := range all {
		for j, v := range all {
			if i >= j {
				continue
			}
			if u.x < v.x+v.size && v.x < u.x+u.size && u.y < v.y+v.size && v.y < u.y+u.size {
				t.Errorf("slots (%d,%d,%d) and (%d,%d,%d) overlap",
					u.x, u.y, u.size, v.x, v.y, v.size)
			}
		}
	}
}

// A light that holds a slot must keep the very same rect while its plan does not
// change, or Part E has nothing to cache
//
// This is what the fixed layout buys over the quadtree it replaces: there, a
// tier change relocated a light even when its size was unchanged.
func TestSlotsDoNotMoveWhileHeld(t *testing.T) {
	lights := []Light{
		{Type: renderer.LightSpot, Pos: mgl32.Vec3{0, 0, 0}, Radius: 10},
		{Type: renderer.LightPoint, Pos: mgl32.Vec3{5, 0, 0}, Radius: 10},
	}

	var a shadowAtlas
	a.reset()

	a.allocate(lights, mgl32.Vec3{0, 0, 12})
	first := map[int32][]shadowTile{}
	for idx, al := range a.allocs {
		first[idx] = append([]shadowTile(nil), al.tiles...)
	}
	if len(first) != 2 {
		t.Fatalf("%d of 2 lights got slots on the first frame", len(first))
	}

	// The camera creeps, but not far enough to change anyone's tier
	for i := 0; i < 8; i++ {
		a.allocate(lights, mgl32.Vec3{0, 0, 12 + float32(i)*0.01})
		for idx, want := range first {
			al, ok := a.allocs[idx]
			if !ok {
				t.Fatalf("frame %d: light %d lost its slots without its tier changing", i, idx)
			}
			for k := range want {
				if al.tiles[k] != want[k] {
					t.Fatalf("frame %d: light %d tile %d moved from (%d,%d,%d) to (%d,%d,%d)",
						i, idx, k, want[k].x, want[k].y, want[k].size,
						al.tiles[k].x, al.tiles[k].y, al.tiles[k].size)
				}
			}
		}
	}
}

// A score hovering on a tier boundary must not flip the tier every frame: a
// reallocation is a forced full re-bake, which is what Part E exists to avoid
func TestTierHysteresis(t *testing.T) {
	// The boundary between the two lowest tiers, read out of the table so
	// retuning slotLayout does not need this test edited
	n := len(shadowTiers)
	boundary := shadowTiers[n-2].minScore
	upper, lower := n-2, n-1

	if got := tierFor(boundary*1.01, lower); got != lower {
		t.Errorf("a score 1%% over the boundary promoted to tier %d, want to stay at %d", got, lower)
	}
	if got := tierFor(boundary*1.25, lower); got != upper {
		t.Errorf("a score 25%% over the boundary stayed at tier %d, want %d", got, upper)
	}
	if got := tierFor(boundary*0.99, upper); got != upper {
		t.Errorf("a score 1%% under the boundary demoted to tier %d, want to stay at %d", got, upper)
	}
	if got := tierFor(boundary*0.8, upper); got != lower {
		t.Errorf("a score 20%% under the boundary stayed at tier %d, want %d", got, lower)
	}
}

// Walking the camera toward a light must sharpen its shadow and walking away
// must coarsen it — the visible behaviour the whole part is for
func TestTileSizeTracksCameraDistance(t *testing.T) {
	lights := []Light{{Type: renderer.LightSpot, Pos: mgl32.Vec3{0, 0, 0}, Radius: 10}}

	var a shadowAtlas
	a.reset()

	sizeAt := func(dist float32) int {
		// Several frames, because hysteresis deliberately takes more than one
		for i := 0; i < 4; i++ {
			a.allocate(lights, mgl32.Vec3{0, 0, dist})
		}
		al, ok := a.allocs[0]
		if !ok {
			return 0
		}
		return al.size
	}

	near, mid, far, gone := sizeAt(10), sizeAt(30), sizeAt(80), sizeAt(200)
	if !(near > mid && mid > far) {
		t.Errorf("tile sizes %d, %d, %d at distances 10, 30, 80 are not monotonically coarser", near, mid, far)
	}
	if gone != 0 {
		t.Errorf("a light 200 units away still holds a %d tile, want none", gone)
	}
}

// Every point light the allocator serves must get all six faces, five leaving a
// lit wedge no other test would catch
func TestShowcasePointLightsGetSixFaces(t *testing.T) {
	s := loadShowcase(t)

	s.atlas.reset()
	s.UpdateShadows(1, 50)

	shadowed := 0
	for i := range s.Lights {
		l := &s.Lights[i]
		if l.shadowIndex < 0 {
			continue
		}
		shadowed++
		if l.Type == renderer.LightPoint && l.shadowCount != 6 {
			t.Errorf("point light %q holds %d tiles, want 6", l.Name, l.shadowCount)
		}
	}
	if shadowed < 2 {
		t.Errorf("%d of %d showcase lights cast a shadow, want several", shadowed, len(s.Lights))
	}
}
