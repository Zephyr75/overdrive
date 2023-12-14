package physics

import (
  "github.com/go-gl/mathgl/mgl32"
  "math"
)

type Sphere struct {
  Verlet
  Radius float32
}

func (s Sphere) GetVerlet() Verlet {
  return s.Verlet
}

func NewSphere(pos mgl32.Vec3, radius float32) Sphere {
  verlet := NewVerlet(pos)
  return Sphere{verlet, radius}
}

func (s Sphere) Collide(c Collider) mgl32.Vec3 {
  switch c.(type) {
  case Sphere:
    println("sphere")
    return s.SphereCollide(c.(Sphere))
  case Plane:
    println("plane")
    return s.PlaneCollide(c.(Plane))
  default:
  // case Box:
  //   return s.BoxCollide(c.(Box))
  }
  println("0")
  return mgl32.Vec3{0.0, 0.0, 0.0}
}

func (s Sphere) SphereCollide(s2 Sphere) mgl32.Vec3 {
  colAxis := s.Pos.Sub(s2.Pos)
  colDist := colAxis.Len()
  if colDist < s.Radius * 2.0 {
    n := colAxis.Mul(1.0 / colDist)
    delta := (s.Radius * 2.0 - colDist) * 0.5
    return n.Mul(delta)
  }
  
  return mgl32.Vec3{0.0, 0.0, 0.0}
}

func (s Sphere) PlaneCollide(p Plane) mgl32.Vec3 {
  distNormal := s.Pos.Sub(p.Pos).Dot(p.Normal)
  distMain := s.Pos.Sub(p.Pos).Dot(p.MainAxis)
  distCross := s.Pos.Sub(p.Pos).Dot(p.CrossAxis)
  projRadius := float32(math.Asin(float64(s.Radius / distNormal)))

  if distNormal > 0.0 && distNormal < projRadius {
    if distMain > -p.MainHalf && distMain < p.MainHalf {
      if distCross > -p.CrossHalf && distCross < p.CrossHalf {
        return p.Normal.Mul(projRadius - distNormal)
      }
    }
  }

  return mgl32.Vec3{0.0, 0.0, 0.0}
}

func (s Sphere) BoxCollide(b Box) bool {
  return false
}
