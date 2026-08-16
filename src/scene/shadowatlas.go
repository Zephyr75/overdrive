package scene

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// One depth texture holds every shadow in the scene, each light owning a
// sub-rect of it: a sun or spot one tile, a point light six 90° tiles.
//
// That is what replaced one render target per casting light. N tile bakes are
// one pass with N viewport changes, where N target binds were N render-pass
// boundaries, and the tile size stops being a compile-time constant shared by
// every light.
const atlasSize = 4096

// Interim fixed partition: a row-major grid of same-sized tiles, handed out in
// order. Part D replaces this with a quadtree keyed on screen-space importance,
// which is the whole reason the record carries a rect rather than a tile index.
//
// The tile size is settings.ShadowWidth so a tile matches what a whole shadow
// map used to be, and the atlas holds (4096/1024)² = 16 of them.
type shadowAtlas struct {
	target renderer.RenderTargetHandle // the depth target BakeShadows draws into
	tex    renderer.TextureHandle      // the sampled view of the same image
	tile   int                         // settings.ShadowWidth, one grid cell's side in pixels
	perRow int                         // atlasSize / tile, so perRow² is the tile capacity
	used   int                         // tiles handed out so far this frame, reset by UpdateShadows
}

// One allocated tile, in atlas pixels: what the bake sets its viewport to
type shadowTile struct {
	x, y, size int   // top-left corner and side, in atlas pixels
	light      int32 // index into Scene.Lights, so the bake can find its Pos/Dir/Type
}

// Creates the atlas texture, once, at scene load
func (a *shadowAtlas) setup(b renderer.Backend) {
	a.tile = settings.ShadowWidth
	a.perRow = atlasSize / a.tile
	a.target, a.tex = b.CreateRenderTarget(renderer.RenderTargetSpec{
		Width:  atlasSize,
		Height: atlasSize,
		Format: renderer.TargetDepth,
	})
}

// Hands out the next free tile, reporting false once the atlas is full
//
// Running out costs shadow quality and never frame time: the caller leaves the
// light's ShadowIndex at -1 and it lights unshadowed.
func (a *shadowAtlas) alloc(light int32) (shadowTile, bool) {
	if a.used >= a.perRow*a.perRow {
		return shadowTile{}, false
	}
	t := shadowTile{
		x:     (a.used % a.perRow) * a.tile,
		y:     (a.used / a.perRow) * a.tile,
		size:  a.tile,
		light: light,
	}
	a.used++
	return t, true
}

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
	s.atlas.used = 0
	s.tiles = s.tiles[:0]
	s.shadowRecords = s.shadowRecords[:0]

	for i := range s.Lights {
		l := &s.Lights[i]
		l.shadowIndex, l.shadowCount = -1, 0
		if !s.casts(int32(i)) {
			continue
		}

		faces := 1
		if l.Type == renderer.LightPoint {
			faces = 6
		}
		// All or nothing: five faces of a point light would leave a lit wedge
		if s.atlas.used+faces > s.atlas.perRow*s.atlas.perRow {
			continue
		}

		l.shadowIndex = int32(len(s.shadowRecords))
		l.shadowCount = int32(faces)
		for face := 0; face < faces; face++ {
			tile, _ := s.atlas.alloc(int32(i))
			s.tiles = append(s.tiles, tile)
			s.shadowRecords = append(s.shadowRecords, l.shadowRecord(tile, face, faces,
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

	// A sun's ortho box, unchanged from the per-light target it replaces
	proj := mgl32.Ortho(-10, 10, -10, 10, nearPlane, farPlane)
	rec.WorldToTile = proj.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), mgl32.Vec3{0, 1, 0}))
	return rec
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

		if rec.FaceIndex >= 0 {
			// A face tile stores radial distance, which needs the fragment stage
			b.BindShader(depthPointShader)
		} else {
			// Cull front faces, which avoids peter-panning on the shadow's near edge
			b.SetCullMode(renderer.CullFront)
			b.BindShader(depthShader)
		}
		for m := range s.Meshes {
			s.Meshes[m].draw(&u)
		}
		if rec.FaceIndex < 0 {
			b.SetCullMode(renderer.CullBack)
		}
	}
	b.EndPass()
}

// Returns this frame's records, for the backend to publish once per frame
func (s *Scene) ShadowRecords() []renderer.ShadowRecord { return s.shadowRecords }
