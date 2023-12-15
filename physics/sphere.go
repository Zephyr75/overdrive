package physics

import (
  "github.com/go-gl/mathgl/mgl32"
)

type Sphere struct {
  Verlet
  Radius float32
}

func (s Sphere) getVerlet() Verlet {
  return s.Verlet
}

func NewSphere(pos mgl32.Vec3, radius float32) Sphere {
  verlet := NewVerlet(pos)
  return Sphere{verlet, radius}
}

func (s *Sphere) Collide(c Collider) bool {
  switch c.(type) {
  case Sphere:
    return s.sphereCollide(c.(Sphere))
  case Plane:
    return s.planeCollide(c.(Plane))
  // case Box:
  //   return s.BoxCollide(c.(Box))
  }
  return false
}

func (s *Sphere) sphereCollide(s2 Sphere) bool {
  colAxis := s.Pos.Sub(s2.Pos)
  colDist := colAxis.Len()
  if colDist < s.Radius * 2.0 {
    n := colAxis.Mul(1.0 / colDist)
    delta := (s.Radius * 2.0 - colDist) * 0.5
    s.Pos = s.Pos.Add(n.Mul(delta))
    return true
  }
  
  return false
}

func (s *Sphere) planeCollide(p Plane) bool {
  distNormal := s.Pos.Sub(p.Pos).Dot(p.Normal)
  distMain := s.Pos.Sub(p.Pos).Dot(p.MainAxis)
  distCross := s.Pos.Sub(p.Pos).Dot(p.CrossAxis)

  if distNormal > -s.Radius && distNormal < s.Radius {
    if distMain > -p.MainHalf && distMain < p.MainHalf {
      if distCross > -p.CrossHalf && distCross < p.CrossHalf {
        s.Pos = s.Pos.Add(p.Normal.Mul(s.Radius - distNormal))
        return true
      }
    }
  }

  return false
}

func (s Sphere) BoxCollide(b Box) bool {
  return false
}
