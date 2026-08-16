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

// The interim partition must hand out non-overlapping tiles, and must refuse
// rather than wrap once the atlas is full
func TestAtlasAllocationDoesNotOverlap(t *testing.T) {
	a := shadowAtlas{tile: 1024, perRow: atlasSize / 1024}
	seen := map[[2]int]bool{}
	for i := 0; i < a.perRow*a.perRow; i++ {
		tile, ok := a.alloc(0)
		if !ok {
			t.Fatalf("allocation %d failed while the atlas still has room", i)
		}
		if seen[[2]int{tile.x, tile.y}] {
			t.Errorf("tile %d reuses pixels at (%d, %d)", i, tile.x, tile.y)
		}
		seen[[2]int{tile.x, tile.y}] = true
		if tile.x+tile.size > atlasSize || tile.y+tile.size > atlasSize {
			t.Errorf("tile %d at (%d, %d) runs past the atlas edge", i, tile.x, tile.y)
		}
	}
	if _, ok := a.alloc(0); ok {
		t.Error("the atlas allocated a tile past its capacity")
	}
}
