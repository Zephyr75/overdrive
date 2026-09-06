package physics

import (
	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

type Sphere struct {
	Verlet
	Radius float32
}

// func (Sphere) Collider() string { return "Sphere" }

// Returns the Verlet state the integrator steps
func (sphere *Sphere) Body() *Verlet { return &sphere.Verlet }

// Creates a sphere collider at a position
func NewSphere(pos mgl32.Vec3, radius float32, fixed bool) *Sphere {
	verlet := NewVerlet(pos, fixed)
	return &Sphere{verlet, radius}
}

// Fits a sphere collider to a mesh, its radius being the distance to the first vertex
func NewSphereFromMesh(mesh *scene.Mesh, fixed bool) *Sphere {
	radius := mesh.Vertices[0].Sub(mesh.Position).Len()
	return &Sphere{NewVerlet(mesh.Position, fixed), radius}
}

// Dispatches to the collision routine matching the other collider's shape
func (sphere *Sphere) Collide(collider Collider) {
	switch collider := collider.(type) {
	case *Sphere:
		sphere.sphereCollide(*collider)
	case *Plane:
		sphere.planeCollide(*collider)
	}

}

// Pushes this sphere half the overlap out along the axis between the two centres
func (sphere *Sphere) sphereCollide(other Sphere) {
	colAxis := sphere.Pos.Sub(other.Pos)
	colDist := colAxis.Len()
	dist := sphere.Radius + other.Radius
	if colDist < dist {
		normal := colAxis.Mul(1.0 / colDist)
		delta := (dist - colDist) * 0.5
		sphere.Pos = sphere.Pos.Add(normal.Mul(delta))
	}
}

// Lifts this sphere out of a plane, but only within the plane's finite extent
func (sphere *Sphere) planeCollide(plane Plane) {
	distNormal := sphere.Pos.Sub(plane.Pos).Dot(plane.Normal)
	distMain := sphere.Pos.Sub(plane.Pos).Dot(plane.MainAxis)
	distCross := sphere.Pos.Sub(plane.Pos).Dot(plane.CrossAxis)

	if distNormal > -sphere.Radius && distNormal < sphere.Radius {
		if distMain > -plane.MainHalf && distMain < plane.MainHalf {
			if distCross > -plane.CrossHalf && distCross < plane.CrossHalf {
				sphere.Pos = sphere.Pos.Add(plane.Normal.Mul(sphere.Radius - distNormal))
			}
		}
	}
}

// func (s *Sphere) boxCollide(b Box) {
//   for _, sphere := range b.Spheres {
//     println(sphere.Pos[0], sphere.Pos[1], sphere.Pos[2])
//     println(sphere.Radius)
//     println(s.Pos[0], s.Pos[1], s.Pos[2])
//     s.sphereCollide(sphere)
//     println(s.Pos[0], s.Pos[1], s.Pos[2])
//   }
//   println("box")
// }
