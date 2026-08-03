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
func (s *Sphere) Body() *Verlet { return &s.Verlet }

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
func (s *Sphere) Collide(c Collider) {
	switch collider := c.(type) {
	case *Sphere:
		s.sphereCollide(*collider)
	case *Plane:
		s.planeCollide(*collider)
	}

}

// Pushes this sphere half the overlap out along the axis between the two centres
func (s *Sphere) sphereCollide(s2 Sphere) {
	colAxis := s.Pos.Sub(s2.Pos)
	colDist := colAxis.Len()
	dist := s.Radius + s2.Radius
	if colDist < dist {
		n := colAxis.Mul(1.0 / colDist)
		delta := (dist - colDist) * 0.5
		s.Pos = s.Pos.Add(n.Mul(delta))
	}
}

// Lifts this sphere out of a plane, but only within the plane's finite extent
func (s *Sphere) planeCollide(p Plane) {
	distNormal := s.Pos.Sub(p.Pos).Dot(p.Normal)
	distMain := s.Pos.Sub(p.Pos).Dot(p.MainAxis)
	distCross := s.Pos.Sub(p.Pos).Dot(p.CrossAxis)

	if distNormal > -s.Radius && distNormal < s.Radius {
		if distMain > -p.MainHalf && distMain < p.MainHalf {
			if distCross > -p.CrossHalf && distCross < p.CrossHalf {
				s.Pos = s.Pos.Add(p.Normal.Mul(s.Radius - distNormal))
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
