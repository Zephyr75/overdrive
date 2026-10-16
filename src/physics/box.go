package physics

import (
	"math"

	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

// An oriented box: a centre, three orthonormal axes and the half-extent along each
type Box struct {
	Verlet
	Axes [3]mgl32.Vec3
	Half [3]float32
}

// Fits a fixed box to a cuboid mesh where it stands, any orientation: only a
// sphere responds to contacts
//
// The axes come from the face normals rather than from edges: a box exported
// from Blender has exactly three normal directions, while picking edges from a
// corner breaks on a thin plank, whose face diagonal is shorter than its length
func NewBoxFromMesh(mesh *scene.Mesh) *Box {
	var axes [3]mgl32.Vec3
	count := 0
	for _, normal := range mesh.NormalCoords {
		if count == 3 {
			break
		}
		normal = mesh.NormalToWorld(normal)
		parallel := false
		for _, axis := range axes[:count] {
			if math.Abs(float64(normal.Dot(axis))) > 0.99 {
				parallel = true
				break
			}
		}
		if !parallel {
			axes[count] = normal
			count++
		}
	}
	// A mesh with fewer than three directions is a plane: its thickness axis is the cross product
	if count == 2 {
		axes[2] = axes[0].Cross(axes[1]).Normalize()
	}

	// Extents and centre from the vertices projected on each axis
	vertices := mesh.WorldVertices()
	var centre mgl32.Vec3
	var half [3]float32
	for i, axis := range axes {
		lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
		for _, vertex := range vertices {
			d := vertex.Dot(axis)
			lo, hi = min(lo, d), max(hi, d)
		}
		half[i] = (hi - lo) * 0.5
		centre = centre.Add(axis.Mul((hi + lo) * 0.5))
	}
	box := &Box{NewVerlet(centre), axes, half}
	box.Fixed = true
	box.attach(mesh)
	return box
}

// Does nothing, the sphere side of the pair resolving and reporting box contacts
func (box *Box) Collide(collider Body) (mgl32.Vec3, bool) { return mgl32.Vec3{}, false }

// Returns the Verlet state the integrator steps
func (box *Box) GetVerlet() *Verlet { return &box.Verlet }
