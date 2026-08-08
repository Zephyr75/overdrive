package physics

import (
	"github.com/go-gl/mathgl/mgl32"
	// "math"
)

type Collider interface {
	// Resolves this collider against another, moving only itself
	Collide(c Collider)
	// Returns the Verlet state the integrator steps, named Body because a method cannot share a name with the embedded field
	Body() *Verlet
}

type Verlet struct {
	Pos     mgl32.Vec3
	PrevPos mgl32.Vec3
	Accel   mgl32.Vec3
	Fixed   bool
}

// Creates Verlet state at rest, fixed bodies never being integrated
func NewVerlet(pos mgl32.Vec3, fixed bool) Verlet {
	return Verlet{pos, pos, mgl32.Vec3{0.0, 0.0, 0.0}, fixed}
}

// Runs one Verlet integration step, velocity being implied by the previous position
func (v *Verlet) UpdatePosition(dt float32) {
	if v.Fixed {
		return
	}
	velocity := v.Pos.Sub(v.PrevPos)
	v.PrevPos = v.Pos
	v.Pos = v.Pos.Add(velocity).Add(v.Accel.Mul(dt * dt))
	v.Accel = mgl32.Vec3{0.0, 0.0, 0.0}
}

// Accumulates acceleration for this step, cleared by UpdatePosition
func (v *Verlet) Accelerate(accel mgl32.Vec3) {
	v.Accel = v.Accel.Add(accel)
}
