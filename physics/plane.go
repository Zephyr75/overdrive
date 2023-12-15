package physics

import (
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

func (p Plane) getVerlet() Verlet {
  return p.Verlet
}


func NewPlane(p1 mgl32.Vec3, p2 mgl32.Vec3, p3 mgl32.Vec3, p4 mgl32.Vec3) Plane {
  mainAxis := p2.Sub(p1)
  crossAxis := p4.Sub(p1)
  center := p1.Add(mainAxis.Mul(0.5)).Add(crossAxis.Mul(0.5))
  normal := mainAxis.Cross(crossAxis).Normalize()
  verlet := NewVerlet(center)

  return Plane{verlet, normal, mainAxis, crossAxis, mainAxis.Len() * 0.5, crossAxis.Len() * 0.5}
}

func (p Plane) Collide(c Collider) bool {
  switch c.(type) {
  case Sphere:
    return p.sphereCollide(c.(Sphere))
  case Plane:
    return p.planeCollide(c.(Plane), 0.1)
  // case Box:
  //   return p.BoxCollide(c.(Box))
  }
  return false
}

func (p *Plane) sphereCollide(s Sphere) bool {
  distNormal := s.Pos.Sub(p.Pos).Dot(p.Normal)
  distMain := s.Pos.Sub(p.Pos).Dot(p.MainAxis)
  distCross := s.Pos.Sub(p.Pos).Dot(p.CrossAxis)

  if distNormal > -s.Radius && distNormal < s.Radius {
    if distMain > -p.MainHalf && distMain < p.MainHalf {
      if distCross > -p.CrossHalf && distCross < p.CrossHalf {
        p.Pos = p.Pos.Add(p.Normal.Mul(s.Radius - distNormal))
        return true
      }
    }
  }

  return false
}

func (p *Plane) planeCollide(p2 Plane, delta float32) bool {
  if delta > 10.0 {
    return false
  }


  // compute intersection of two planes
  p3Normal := p.Normal.Cross(p2.Normal)
  det := p3Normal.Dot(p3Normal)
  if det == 0.0 {
    return false
  }


  p1Dist := p.Pos.Len()
  p2Dist := p2.Pos.Len()

  // println(p2.Pos[0], p2.Pos[1], p2.Pos[2])

  p3Point := p3Normal.Cross(p2.Normal.Mul(p1Dist)).Add(p.Normal.Mul(p2Dist)).Mul(1.0 / det)

  // compute projections of all points onto the intersection line
  pA := p.Pos.Add(p.MainAxis.Mul(p.MainHalf)).Add(p.CrossAxis.Mul(p.CrossHalf))
  pB := p.Pos.Add(p.MainAxis.Mul(p.MainHalf)).Add(p.CrossAxis.Mul(-p.CrossHalf))
  pC := p.Pos.Add(p.MainAxis.Mul(-p.MainHalf)).Add(p.CrossAxis.Mul(p.CrossHalf))
  pD := p.Pos.Add(p.MainAxis.Mul(-p.MainHalf)).Add(p.CrossAxis.Mul(-p.CrossHalf))

  p2A := p2.Pos.Add(p2.MainAxis.Mul(p2.MainHalf)).Add(p2.CrossAxis.Mul(p2.CrossHalf))
  p2B := p2.Pos.Add(p2.MainAxis.Mul(p2.MainHalf)).Add(p2.CrossAxis.Mul(-p2.CrossHalf))
  p2C := p2.Pos.Add(p2.MainAxis.Mul(-p2.MainHalf)).Add(p2.CrossAxis.Mul(p2.CrossHalf))
  p2D := p2.Pos.Add(p2.MainAxis.Mul(-p2.MainHalf)).Add(p2.CrossAxis.Mul(-p2.CrossHalf))

  pProj := make([]float32, 4)
  p2Proj := make([]float32, 4)

  pProj[0] = pA.Sub(p3Point).Dot(p3Normal)
  pProj[1] = pB.Sub(p3Point).Dot(p3Normal)
  pProj[2] = pC.Sub(p3Point).Dot(p3Normal)
  pProj[3] = pD.Sub(p3Point).Dot(p3Normal)

  p2Proj[0] = p2A.Sub(p3Point).Dot(p3Normal)
  p2Proj[1] = p2B.Sub(p3Point).Dot(p3Normal)
  p2Proj[2] = p2C.Sub(p3Point).Dot(p3Normal)
  p2Proj[3] = p2D.Sub(p3Point).Dot(p3Normal)

  // find min and max of all projections
  pMin := pProj[0]
  pMax := pProj[0]

  for i := 1; i < 4; i++ {
    if pProj[i] < pMin {
      pMin = pProj[i]
    }
    if pProj[i] > pMax {
      pMax = pProj[i]
    }
  }

  p2Min := p2Proj[0]
  p2Max := p2Proj[0]

  for i := 1; i < 4; i++ {
    if p2Proj[i] < p2Min {
      p2Min = p2Proj[i]
    }
    if p2Proj[i] > p2Max {
      p2Max = p2Proj[i]
    }
  }

  println(pMin, pMax, p2Min, p2Max)

  // check if projections overlap
  if pMin > p2Max || p2Min > pMax {
    return false
  }


  // compute intersection line

  // iterate exponentially to find distance such that planes do not intersect
  p.Pos = p.Pos.Add(p2.Normal.Mul(delta))
  p2.Pos = p2.Pos.Add(p.Normal.Mul(delta))

  p.planeCollide(p2, delta * 2.0)
  return true

}
