package physics

import (
	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

type Body interface {
	// Resolves this collider against another, reporting whether they touched
	// and the contact normal, pointing out of the other into this one
	Collide(body Body) (normal mgl32.Vec3, touched bool)
	// Returns the Verlet state the integrator steps, named GetVerlet because a method cannot share a name with the embedded field
	GetVerlet() *Verlet
}

// The state every body has, whatever its shape
type Verlet struct {
	Pos     mgl32.Vec3
	PrevPos mgl32.Vec3 // last step's Pos; the gap between the two stands in for velocity
	Accel   mgl32.Vec3 // accumulated this step by Accelerate, consumed and zeroed by Integrate
	// True skips integration: an immovable collider, or one the game drives (a
	// paddle, a door) by setting Pos before a step, the distance moved in that
	// step being the speed it hits with. Teleport moves one without that speed
	Fixed bool
	// Kilograms: only the ratio between two bodies in contact matters
	Mass float32
	// Angular velocity in radians per step, the axis scaled by the angle, as Pos - PrevPos is velocity per step
	Spin mgl32.Vec3
	// The whole orientation, the mesh's when attached; turned by Spin each step,
	// for the mesh only: no collision reads it
	Orientation mgl32.Quat
	// Placed where the body is after every step; nil for a body nothing draws
	Mesh *scene.Mesh

	// Mesh.Position less Pos at creation, in the body's own axes: a box's
	// centre is not its mesh's pivot
	meshOffset mgl32.Vec3
	// Where the mesh was last placed, so a body that has not moved never calls
	// Place, which would dirty its shadow tiles
	placedPos mgl32.Vec3
	placedRot mgl32.Quat
}

// Creates a movable body at rest
func NewVerlet(pos mgl32.Vec3) Verlet {
	return Verlet{Pos: pos, PrevPos: pos, Mass: 1, Orientation: mgl32.QuatIdent(),
		placedPos: pos, placedRot: mgl32.QuatIdent()}
}

// Attaches the mesh the body moves: the body takes the mesh's orientation, and
// keeps the offset between its centre and the mesh's pivot in its own axes
func (verlet *Verlet) attach(mesh *scene.Mesh) {
	verlet.Mesh = mesh
	verlet.Orientation, verlet.placedRot = mesh.Rotation, mesh.Rotation
	verlet.meshOffset = mesh.Rotation.Inverse().Rotate(mesh.Position.Sub(verlet.Pos))
}

// How much of a shared push or impulse this body takes: none when fixed
func (verlet *Verlet) inverseMass() float32 {
	if verlet.Fixed {
		return 0
	}
	return 1 / verlet.Mass
}

// Runs one Verlet integration step, velocity being implied by the previous position
func (verlet *Verlet) Integrate(deltaTime float32) {
	if verlet.Fixed {
		return
	}
	velocity := verlet.Pos.Sub(verlet.PrevPos)
	verlet.PrevPos = verlet.Pos
	verlet.Pos = verlet.Pos.Add(velocity).Add(verlet.Accel.Mul(deltaTime * deltaTime))
	verlet.Accel = mgl32.Vec3{0.0, 0.0, 0.0}
	angle := verlet.Spin.Len()
	if angle > 0 {
		verlet.Orientation = mgl32.QuatRotate(angle, verlet.Spin.Mul(1/angle)).Mul(verlet.Orientation).Normalize()
	}
}

// Returns the velocity in metres per second, given the length of the world's step
func (verlet *Verlet) Velocity(step float32) mgl32.Vec3 {
	return verlet.Pos.Sub(verlet.PrevPos).Mul(1 / step)
}

// Sets the velocity in metres per second, given the length of the world's step
func (verlet *Verlet) SetVelocity(velocity mgl32.Vec3, step float32) {
	verlet.PrevPos = verlet.Pos.Sub(velocity.Mul(step))
}

// Moves the body keeping its velocity: setting Pos alone would read as a
// leap and launch it by the distance moved
func (verlet *Verlet) Teleport(pos mgl32.Vec3) {
	verlet.PrevPos = pos.Sub(verlet.Pos.Sub(verlet.PrevPos))
	verlet.Pos = pos
}

// Accumulates acceleration for this step, cleared by Integrate
func (verlet *Verlet) Accelerate(accel mgl32.Vec3) {
	verlet.Accel = verlet.Accel.Add(accel)
}

// Moves the mesh to the body, unless the body has not moved since the last time
//
// Place dirties the mesh's dynamic shadow tiles, so a body at rest must not
func (verlet *Verlet) placeAndRotate() {
	if verlet.Mesh == nil || (verlet.Pos == verlet.placedPos && verlet.Orientation == verlet.placedRot) {
		return
	}
	verlet.Mesh.Place(verlet.Pos.Add(verlet.Orientation.Rotate(verlet.meshOffset)), verlet.Orientation)
	verlet.placedPos, verlet.placedRot = verlet.Pos, verlet.Orientation
}
