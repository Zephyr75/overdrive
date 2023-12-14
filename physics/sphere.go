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

func (s *Sphere) Collide(c Collider) mgl32.Vec3 {
  switch c.(type) {
  case Sphere:
    return s.sphereCollide(c.(Sphere))
  case Plane:
    return s.planeCollide(c.(Plane))
  default:
  // case Box:
  //   return s.BoxCollide(c.(Box))
  }
  return mgl32.Vec3{0.0, 0.0, 0.0}
}

func (s *Sphere) sphereCollide(s2 Sphere) mgl32.Vec3 {
  colAxis := s.Pos.Sub(s2.Pos)
  colDist := colAxis.Len()
  if colDist < s.Radius * 2.0 {
    n := colAxis.Mul(1.0 / colDist)
    delta := (s.Radius * 2.0 - colDist) * 0.5
    s.Pos = s.Pos.Add(n.Mul(delta))
    return n.Mul(delta)
  }
  
  return mgl32.Vec3{0.0, 0.0, 0.0}
}

func (s *Sphere) planeCollide(p Plane) mgl32.Vec3 {
  distNormal := s.Pos.Sub(p.Pos).Dot(p.Normal)
  distMain := s.Pos.Sub(p.Pos).Dot(p.MainAxis)
  distCross := s.Pos.Sub(p.Pos).Dot(p.CrossAxis)

  if distNormal > -s.Radius && distNormal < s.Radius {
    if distMain > -p.MainHalf && distMain < p.MainHalf {
      if distCross > -p.CrossHalf && distCross < p.CrossHalf {
        s.Pos = s.Pos.Add(p.Normal.Mul(s.Radius - distNormal))
        return p.Normal.Mul(s.Radius - distNormal)
      }
    }
  }

  return mgl32.Vec3{0.0, 0.0, 0.0}
}

func (s Sphere) BoxCollide(b Box) bool {
  return false
}
