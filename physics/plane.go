package physics

import (
	"fmt"

	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

type Plane struct {
  Verlet
  Normal mgl32.Vec3
  MainAxis mgl32.Vec3
  CrossAxis mgl32.Vec3
  MainHalf float32
  CrossHalf float32
}


func NewPlaneFromMesh(mesh *scene.Mesh) *Plane {
  fmt.Println(
    mesh.Vertices[0],
    mesh.Vertices[1],
    mesh.Vertices[2],
    mesh.Vertices[3],
    )
  return NewPlane(mesh.Vertices[0], mesh.Vertices[1], mesh.Vertices[3], mesh.Vertices[2])
}



func NewPlane(p1 mgl32.Vec3, p2 mgl32.Vec3, p3 mgl32.Vec3, p4 mgl32.Vec3) *Plane {
  mainAxis := p2.Sub(p1)
  crossAxis := p4.Sub(p1)
  center := p1.Add(mainAxis.Mul(0.5)).Add(crossAxis.Mul(0.5))
  normal := mainAxis.Cross(crossAxis).Normalize()
  verlet := NewVerlet(center)

  // fmt.Println("NewPlane",normal, mainAxis, crossAxis, mainAxis.Len() * 0.5, crossAxis.Len() * 0.5, center)

  return &Plane{verlet, normal, mainAxis.Normalize(), crossAxis.Normalize(), mainAxis.Len() * 0.5, crossAxis.Len() * 0.5}
}

func (p *Plane) Collide(c Collider) {
  // TODO: Implement
}

func (p *Plane) GetVerlet() *Verlet { return &p.Verlet }

