package physics

import (
  "github.com/go-gl/mathgl/mgl32"
  "github.com/Zephyr75/overdrive/scene"
)

type Sphere struct {
  Verlet
  Radius float32
}

// func (Sphere) Collider() string { return "Sphere" }

func (s *Sphere) GetVerlet() *Verlet { return &s.Verlet }



func NewSphere(pos mgl32.Vec3, radius float32, fixed bool) *Sphere {
  verlet := NewVerlet(pos, fixed)
  return &Sphere{verlet, radius}
}

func NewSphereFromMesh(mesh *scene.Mesh, fixed bool) *Sphere {
  radius := mesh.Vertices[0].Sub(mesh.Position).Len()
  println("radius", radius)
  println("pos", mesh.Position[0], mesh.Position[1], mesh.Position[2])
  return &Sphere{NewVerlet(mesh.Position, fixed), radius}
}

func (s *Sphere) Collide(c Collider) {
  switch collider := c.(type) {
  case *Sphere:
    s.sphereCollide(*collider)
  case *Plane:
    s.planeCollide(*collider)
  }
  
}

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
