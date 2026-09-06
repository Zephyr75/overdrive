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

// Fits a plane collider to a mesh's first quad
func NewPlaneFromMesh(mesh *scene.Mesh, fixed bool) *Plane {
	return NewPlane(mesh.Vertices[0], mesh.Vertices[1], mesh.Vertices[3], mesh.Vertices[2], fixed)
}

// Builds a finite plane from four corners, deriving its centre, normal and half-extents
func NewPlane(p1 mgl32.Vec3, p2 mgl32.Vec3, p3 mgl32.Vec3, p4 mgl32.Vec3, fixed bool) *Plane {
	mainAxis := p2.Sub(p1)
	crossAxis := p4.Sub(p1)
	center := p1.Add(mainAxis.Mul(0.5)).Add(crossAxis.Mul(0.5))
	normal := mainAxis.Cross(crossAxis).Normalize()
	verlet := NewVerlet(center, fixed)

	// fmt.Println("NewPlane",normal, mainAxis, crossAxis, mainAxis.Len() * 0.5, crossAxis.Len() * 0.5, center)

	return &Plane{verlet, normal, mainAxis.Normalize(), crossAxis.Normalize(), mainAxis.Len() * 0.5, crossAxis.Len() * 0.5}
}

// Does nothing, the sphere side of the pair resolving plane contacts
func (plane *Plane) Collide(collider Collider) {
	// TODO: Implement
}

// Returns the Verlet state the integrator steps
func (plane *Plane) Body() *Verlet { return &plane.Verlet }
