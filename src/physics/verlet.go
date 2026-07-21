package physics

import (
	"github.com/go-gl/mathgl/mgl32"
	// "math"
)

type Collider interface {
	Collide(c Collider)
	GetVerlet() *Verlet

	// Collider() string
}

type Verlet struct {
	Pos     mgl32.Vec3
	PrevPos mgl32.Vec3
	Accel   mgl32.Vec3
	Fixed   bool
}

func NewVerlet(pos mgl32.Vec3, fixed bool) Verlet {
	return Verlet{pos, pos, mgl32.Vec3{0.0, 0.0, 0.0}, fixed}
}

// Run Verlet integration
func (v *Verlet) UpdatePosition(dt float32) {
	if v.Fixed {
		return
	}
	velocity := v.Pos.Sub(v.PrevPos)
	v.PrevPos = v.Pos
	v.Pos = v.Pos.Add(velocity).Add(v.Accel.Mul(dt * dt))
	v.Accel = mgl32.Vec3{0.0, 0.0, 0.0}
}

func (v *Verlet) Accelerate(accel mgl32.Vec3) {
	v.Accel = v.Accel.Add(accel)
}
