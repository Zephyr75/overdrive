package physics

// import (
//   // "github.com/go-gl/mathgl/mgl32"
// )

// type Link struct {
//   Start *Verlet
//   End *Verlet
//   Length float32
// }

// func NewLink(start *Verlet, end *Verlet) Link {
//   return Link{start, end, start.Pos.Sub(end.Pos).Len()}
// }

// func (l *Link) Update() {
//   axis := l.Start.Pos.Sub(l.End.Pos)
//   dist := axis.Len()
//   n := axis.Mul(1.0 / dist)
//   delta := (l.Length - dist) * 0.5
//   l.Start.Pos = l.Start.Pos.Add(n.Mul(delta))
//   l.End.Pos = l.End.Pos.Sub(n.Mul(delta))
// }
