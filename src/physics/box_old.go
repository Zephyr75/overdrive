package physics

// import (
//   "github.com/go-gl/mathgl/mgl32"
// )

// type Box struct {
//   Pos mgl32.Vec3
//   MainAxis mgl32.Vec3
//   CrossAxis mgl32.Vec3
//   UpAxis mgl32.Vec3
//   MainHalf float32
//   CrossHalf float32
//   UpHalf float32
//   Spheres []Sphere
//   Links []Link
// }

// func (Box) Collider() string { return "Box" }

// func NewBox(center mgl32.Vec3, mainAxis mgl32.Vec3, crossAxis mgl32.Vec3, upAxis mgl32.Vec3, mainLength float32, crossLength float32, upLength float32) Box {

//   longAxis1 := mainAxis
//   longLength1 := mainLength
//   longAxis2 := crossAxis
//   longLength2 := crossLength

//   minWidth := upLength
//   if mainLength < minWidth {
//     minWidth = mainLength
//     longAxis1 = upAxis
//     longLength1 = upLength
//     longAxis2 = crossAxis
//     longLength2 = crossLength
//   }
//   if crossLength < minWidth {
//     minWidth = crossLength
//     longAxis1 = upAxis
//     longLength1 = upLength
//     longAxis2 = mainAxis
//     longLength2 = mainLength
//   }

//   radius := minWidth * 0.5

//   spheres := make([]Sphere, 0)
//   links := make([]Link, 0)

//   corner := center.Sub(longAxis1.Mul(longLength1 * 0.5)).Sub(longAxis2.Mul(longLength2 * 0.5))

//   var a float32 = radius
//   for a < longLength1 {
//     var b float32 = radius
//     for b < longLength2 {
//       spheres = append(spheres, NewSphere(corner.Add(longAxis1.Mul(a)).Add(longAxis2.Mul(b)), radius))
//       b += radius * 2.0
//     }
//     a += radius * 2.0
//   }

//   for i := 0; i < len(spheres); i++ {
//     for j := i + 1; j < len(spheres); j++ {
//       links = append(links, NewLink(&spheres[i].Verlet, &spheres[j].Verlet))
//     }
//   }

//   return Box{center, mainAxis, crossAxis, upAxis, mainLength * 0.5, crossLength * 0.5, upLength * 0.5, spheres, links}
// }

// func (b *Box) Collide(c Collider) {
//   switch c.(type) {
//   case Sphere:
//     b.sphereCollide(c.(Sphere))
//   case Plane:
//     b.planeCollide(c.(Plane))
//   case Box:
//     b.boxCollide(c.(Box))
//   }
//   for i, _ := range b.Links {
//     b.Links[i].Update()
//   }

// }

// func (b *Box) sphereCollide(s Sphere) {
//   for i, _ := range b.Spheres {
//     b.Spheres[i].Collide(s)
//   }
// }

// func (b *Box) planeCollide(p Plane) {
//   for i, _ := range b.Spheres {
//     b.Spheres[i].Collide(p)
//   }
// }

// func (b *Box) boxCollide(b2 Box) {
//   for i, _ := range b.Spheres {
//     b.Spheres[i].Collide(b2)
//   }
// }

// func (b *Box) UpdatePosition(dt float32) {
//   for i, _ := range b.Spheres {
//     b.Spheres[i].UpdatePosition(dt)
//   }
//   avgPos := mgl32.Vec3{0.0, 0.0, 0.0}
//   for i, _ := range b.Spheres {
//     avgPos = avgPos.Add(b.Spheres[i].Pos)
//   }
//   avgPos = avgPos.Mul(1.0 / float32(len(b.Spheres)))
//   b.Pos = avgPos
// }

// func (b *Box) Accelerate(a mgl32.Vec3) {
//   for i, _ := range b.Spheres {
//     b.Spheres[i].Accelerate(a)
//   }
// }
