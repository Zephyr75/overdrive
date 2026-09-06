package physics

import (
	"github.com/go-gl/mathgl/mgl32"
	// "math"
)

type Collider interface {
	// Resolves this collider against another, moving only itself
	Collide(collider Collider)
	// Returns the Verlet state the integrator steps, named Body because a method cannot share a name with the embedded field
	Body() *Verlet
}

type Verlet struct {
	Pos     mgl32.Vec3
	PrevPos mgl32.Vec3 // last step's Pos; the gap between the two stands in for velocity
	Accel   mgl32.Vec3 // accumulated this step by Accelerate, consumed and zeroed by UpdatePosition
	Fixed   bool       // true skips integration, an immovable collider
}

// Creates Verlet state at rest, fixed bodies never being integrated
func NewVerlet(pos mgl32.Vec3, fixed bool) Verlet { // TODO: review
	return Verlet{pos, pos, mgl32.Vec3{0.0, 0.0, 0.0}, fixed}
}

// Runs one Verlet integration step, velocity being implied by the previous position
func (verlet *Verlet) UpdatePosition(deltaTime float32) { // TODO: review
	if verlet.Fixed {
		return
	}
	velocity := verlet.Pos.Sub(verlet.PrevPos)
	verlet.PrevPos = verlet.Pos
	verlet.Pos = verlet.Pos.Add(velocity).Add(verlet.Accel.Mul(deltaTime * deltaTime))
	verlet.Accel = mgl32.Vec3{0.0, 0.0, 0.0}
}

// Accumulates acceleration for this step, cleared by UpdatePosition
func (verlet *Verlet) Accelerate(accel mgl32.Vec3) { // TODO: review
	verlet.Accel = verlet.Accel.Add(accel)
}
