package physics

import (
  "github.com/go-gl/mathgl/mgl32"
)

type Sphere struct {
  Verlet
  Radius float32
}

func (Sphere) Collider() string { return "Sphere" }


func NewSphere(pos mgl32.Vec3, radius float32) Sphere {
  verlet := NewVerlet(pos)
  return Sphere{verlet, radius}
}

func (s *Sphere) Collide(c Collider) {
  switch c.(type) {
  case Sphere:
    s.sphereCollide(c.(Sphere))
  case Plane:
    s.planeCollide(c.(Plane))
  case Box:
    s.boxCollide(c.(Box))
  }
}

func (s *Sphere) sphereCollide(s2 Sphere) {
  colAxis := s.Pos.Sub(s2.Pos)
  colDist := colAxis.Len()
  if colDist < s.Radius * 2.0 {
    n := colAxis.Mul(1.0 / colDist)
    delta := (s.Radius * 2.0 - colDist) * 0.5
    s.Pos = s.Pos.Add(n.Mul(delta))
    return 
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
        return
      }
    }
  }
}

func (s *Sphere) boxCollide(b Box) {
  for _, sphere := range b.Spheres {
    s.sphereCollide(sphere)
  }
}
