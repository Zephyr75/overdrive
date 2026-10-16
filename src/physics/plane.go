package physics

import (
	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

type Plane struct {
	Verlet
	Normal    mgl32.Vec3 // unit, mainAxis × crossAxis
	MainAxis  mgl32.Vec3 // unit, p2 - p1
	CrossAxis mgl32.Vec3 // unit, p4 - p1
	MainHalf  float32    // half-extent along MainAxis, the plane's finite boundary
	CrossHalf float32    // half-extent along CrossAxis
}

// Fits a fixed plane collider to a mesh's first quad, where the mesh stands
func NewPlaneFromMesh(mesh *scene.Mesh) *Plane {
	corner := func(i int) mgl32.Vec3 { return mesh.ToWorld(mesh.Vertices[i]) }
	plane := NewPlane(corner(0), corner(1), corner(3), corner(2))
	plane.attach(mesh)
	return plane
}

// Builds a fixed finite plane from four corners, deriving its centre, normal and half-extents
func NewPlane(p1 mgl32.Vec3, p2 mgl32.Vec3, p3 mgl32.Vec3, p4 mgl32.Vec3) *Plane {
	mainAxis := p2.Sub(p1)
	crossAxis := p4.Sub(p1)
	center := p1.Add(mainAxis.Mul(0.5)).Add(crossAxis.Mul(0.5))
	normal := mainAxis.Cross(crossAxis).Normalize()
	verlet := NewVerlet(center)
	verlet.Fixed = true
	return &Plane{verlet, normal, mainAxis.Normalize(), crossAxis.Normalize(), mainAxis.Len() * 0.5, crossAxis.Len() * 0.5}
}

// Does nothing, the sphere side of the pair resolving and reporting plane contacts
func (plane *Plane) Collide(collider Body) (mgl32.Vec3, bool) { return mgl32.Vec3{}, false }

// Returns the Verlet state the integrator steps
func (plane *Plane) GetVerlet() *Verlet { return &plane.Verlet }
