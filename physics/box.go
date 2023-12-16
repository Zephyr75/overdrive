package physics

import (
  "github.com/go-gl/mathgl/mgl32"
)

type Box struct {
  Pos mgl32.Vec3
  MainAxis mgl32.Vec3
  CrossAxis mgl32.Vec3
  UpAxis mgl32.Vec3
  MainHalf float32
  CrossHalf float32
  UpHalf float32
  Spheres []Sphere
  Links []Link
}


func (Box) Collider() string { return "Box" }

func NewBox(center mgl32.Vec3, mainAxis mgl32.Vec3, crossAxis mgl32.Vec3, upAxis mgl32.Vec3, mainLength float32, crossLength float32, upLength float32) Box {

  longAxis1 := mainAxis
  longLength1 := mainLength
  longAxis2 := crossAxis
  longLength2 := crossLength

  minWidth := upLength
  if mainLength < minWidth {
    minWidth = mainLength
    longAxis1 = upAxis
    longLength1 = upLength
    longAxis2 = crossAxis
    longLength2 = crossLength
  }
  if crossLength < minWidth {
    minWidth = crossLength
    longAxis1 = upAxis
    longLength1 = upLength
    longAxis2 = mainAxis
    longLength2 = mainLength
  }

  radius := minWidth * 0.5

  spheres := make([]Sphere, 0)
  links := make([]Link, 0)

  var a float32 = radius
  for a < longLength1 {
    var b float32 = radius
    for b < longLength2 {
      spheres = append(spheres, NewSphere(center.Add(longAxis1.Mul(float32(a))).Add(longAxis2.Mul(float32(b))), radius))
      b += radius * 2.0
    }
    a += radius * 2.0
  }

  for i := 0; i < len(spheres); i++ {
    for j := i + 1; j < len(spheres); j++ {
      links = append(links, NewLink(&spheres[i].Verlet, &spheres[j].Verlet))
    }
  }

  return Box{center, mainAxis, crossAxis, upAxis, mainLength * 0.5, crossLength * 0.5, upLength * 0.5, spheres, links}
}


func (b Box) Collide(c Collider) {
  switch c.(type) {
  case Sphere:
    b.sphereCollide(c.(Sphere))
  case Plane:
    b.planeCollide(c.(Plane))
  case Box:
    b.boxCollide(c.(Box))
  }
  for i, _ := range b.Links {
    b.Links[i].Update()
  }

}

func (b *Box) sphereCollide(s Sphere) {
  for i, _ := range b.Spheres {
    b.Spheres[i].Collide(s)
  }
}

func (b *Box) planeCollide(p Plane) {
  for i, _ := range b.Spheres {
    b.Spheres[i].Collide(p)
  }
}

func (b *Box) boxCollide(b2 Box) {
  for i, _ := range b.Spheres {
    b.Spheres[i].Collide(b2)
  }
}

