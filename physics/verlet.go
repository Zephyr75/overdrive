package physics

import (
  "github.com/go-gl/mathgl/mgl32"
)

type Sphere struct {
  Pos mgl32.Vec3
  Radius float32
}

type Plane struct {
  Pos mgl32.Vec3
  Normal mgl32.Vec3
}

func (s Sphere) OverlapSphere(s2 Sphere) float32 {
  return s.Radius + s2.Radius - s.Pos.Sub(s2.Pos).Len()
}

func (s Sphere) OverlapPlane(p Plane) float32 {
  return s.Radius - s.Pos.Sub(p.Pos).Dot(p.Normal)
}

//////////////////////


type Verlet struct {
  Pos mgl32.Vec3
  PrevPos mgl32.Vec3
  Accel mgl32.Vec3
}

func (v *Verlet) Accelerate(accel mgl32.Vec3) {
  v.Accel = v.Accel.Add(accel)
}

func (v *Verlet) UpdatePosition(dt float32) {
  velocity := v.Pos.Sub(v.PrevPos)
  v.PrevPos = v.Pos
  v.Pos = v.Pos.Add(velocity).Add(v.Accel.Mul(dt * dt))
  v.Accel = mgl32.Vec3{0.0, 0.0, 0.0}

  // println(v.Pos[0], v.Pos[1], v.Pos[2])
}

func (v *Verlet) FloorConstraint(y float32) {
  if v.Pos[1] < y {
    v.Pos[1] = y
  }
}

func (v *Verlet) SphereConstraint(s Sphere) {
  toObj := v.Pos.Sub(s.Pos)
  dist := toObj.Len()
  if dist > s.Radius && dist > 0.0 {
    n := toObj.Mul(1.0 / dist)
    v.Pos = s.Pos.Add(n.Mul(s.Radius))
    // println(v.Pos[0], v.Pos[1], v.Pos[2])
  }
}

func (v *Verlet) CollisionConstraint(s *Sphere) {
  colAxis := v.Pos.Sub(s.Pos)
  colDist := colAxis.Len()
  if colDist < s.Radius * 2.0 {
    n := colAxis.Mul(1.0 / colDist)
    delta := (s.Radius * 2.0 - colDist) * 0.5
    v.Pos = v.Pos.Add(n.Mul(delta))
    s.Pos = s.Pos.Sub(n.Mul(delta))
  }
}



//////////////////////

var (
  gravity = mgl32.Vec3{0.0, -9.8, 0.0}
)

type World struct {
  Verlets []Verlet
  Spheres []Sphere
  Planes []Plane
}

func (w *World) Update(dt float32) {
  for i := 0; i < len(w.Verlets); i++ {
    w.Verlets[i].Accelerate(gravity)
  }
  for i := 0; i < len(w.Verlets); i++ {
    w.Verlets[i].UpdatePosition(dt)
  }
}
