package physics

import (
  "github.com/go-gl/mathgl/mgl32"
)

type Box struct {
  Pos mgl32.Vec3
  MainAxis mgl32.Vec3
  CrossAxis mgl32.Vec3
  MainHalf float32
  CrossHalf float32
  Height float32
}

func NewBox(p1 mgl32.Vec3, p2 mgl32.Vec3, p3 mgl32.Vec3, p4 mgl32.Vec3, height float32) Box {
  mainAxis := p2.Sub(p1)
  crossAxis := p4.Sub(p1)
  center := p1.Add(mainAxis.Mul(0.5)).Add(crossAxis.Mul(0.5))

  return Box{center, mainAxis, crossAxis, mainAxis.Len() * 0.5, crossAxis.Len() * 0.5, height}
}

func (b Box) Collide(c Collider) bool {
  return false
}

func (b Box) GetVerlet() Verlet {
  return Verlet{b.Pos, b.Pos, mgl32.Vec3{0.0, 0.0, 0.0}}
}
