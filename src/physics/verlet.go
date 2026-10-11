package physics

import (
	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

type Collider interface {
	// Resolves this collider against another, moving only itself
	Collide(collider Collider)
	// Returns the Verlet state the integrator steps, named Body because a method cannot share a name with the embedded field
	Body() *Verlet
}

// The state every body has, whatever its shape
type Verlet struct {
	Pos     mgl32.Vec3
	PrevPos mgl32.Vec3 // last step's Pos; the gap between the two stands in for velocity
	Accel   mgl32.Vec3 // accumulated this step by Accelerate, consumed and zeroed by UpdatePosition
	Fixed   bool       // true skips integration, an immovable collider
	// Angular velocity in radians per step, the axis scaled by the angle, as Pos - PrevPos is velocity per step
	Spin mgl32.Vec3
	// Turned by Spin each step, for the mesh only: no collision reads it
	Orientation mgl32.Quat
	// Placed where the body is after every step; nil for a body nothing draws
	Mesh *scene.Mesh
}

// Creates a movable body at rest
func NewVerlet(pos mgl32.Vec3) Verlet {
	return Verlet{Pos: pos, PrevPos: pos, Orientation: mgl32.QuatIdent()}
}

// Runs one Verlet integration step, velocity being implied by the previous position
func (verlet *Verlet) UpdatePosition(deltaTime float32) {
	if verlet.Fixed {
		return
	}
	velocity := verlet.Pos.Sub(verlet.PrevPos)
	verlet.PrevPos = verlet.Pos
	verlet.Pos = verlet.Pos.Add(velocity).Add(verlet.Accel.Mul(deltaTime * deltaTime))
	verlet.Accel = mgl32.Vec3{0.0, 0.0, 0.0}
}

// Accumulates acceleration for this step, cleared by UpdatePosition
func (verlet *Verlet) Accelerate(accel mgl32.Vec3) {
	verlet.Accel = verlet.Accel.Add(accel)
}

// Turns the body by its spin and moves its mesh to match
func (verlet *Verlet) follow() {
	if verlet.Fixed || verlet.Mesh == nil {
		return
	}
	if angle := verlet.Spin.Len(); angle > 0 {
		verlet.Orientation = mgl32.QuatRotate(angle, verlet.Spin.Mul(1/angle)).Mul(verlet.Orientation).Normalize()
	}
	verlet.Mesh.Place(verlet.Pos, verlet.Orientation)
}
