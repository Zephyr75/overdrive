package physics

import (
	"math"

	"github.com/Zephyr75/overdrive/scene"
	"github.com/go-gl/mathgl/mgl32"
)

type Sphere struct {
	Verlet
	Radius float32
	// Share of the speed into a surface kept on the rebound: 0 lands dead, 1 bounces forever
	Restitution float32
	// Coulomb's coefficient, capping the grip that turns sliding into rolling
	Friction float32
	// Moment of inertia over m r²: 2/5 solid, 2/3 a thin shell such as a football
	Inertia float32
	// Rolling speed lost per unit of speed into the surface, a few percent on hard ground
	RollingResistance float32
	// A rebound slower than this, in metres per step, is dropped, so a ball that
	// barely falls settles rather than hopping on; 0 for a ball that must keep
	// every bounce, such as one with no gravity grazing a wall
	RestingSpeed float32
}

// RestingSpeed's default, a few millimetres a step: what gravity alone adds in one
const restingSpeed = 0.005

// Returns the Verlet state the integrator steps
func (sphere *Sphere) GetVerlet() *Verlet { return &sphere.Verlet }

// Creates a movable sphere at a position; set Fixed for one that never moves
//
// Inertia starts solid rather than zero, which the spin update divides by
func NewSphere(pos mgl32.Vec3, radius float32) *Sphere {
	return &Sphere{Verlet: NewVerlet(pos), Radius: radius, Inertia: 2.0 / 5.0, RestingSpeed: restingSpeed}
}

// Fits a movable sphere to a mesh, which then follows it, its radius being the distance to the first vertex
func NewSphereFromMesh(mesh *scene.Mesh) *Sphere {
	sphere := NewSphere(mesh.Position, mesh.Vertices[0].Sub(mesh.Position).Len())
	sphere.Mesh = mesh
	return sphere
}

// Dispatches to the collision routine matching the other collider's shape
func (sphere *Sphere) Collide(collider Body) (mgl32.Vec3, bool) {
	switch collider := collider.(type) {
	case *Sphere:
		return sphere.sphereCollide(collider)
	case *Plane:
		return sphere.planeCollide(*collider)
	case *Box:
		return sphere.boxCollide(*collider)
	}
	return mgl32.Vec3{}, false
}

// Separates two spheres and exchanges momentum along the line between them,
// the lighter taking more of both
//
// Both bodies at once, not each its own half: the first half would bounce this
// sphere away before the other saw it coming, and the other would get nothing.
// Resolved here, the reverse call of the pair finds the two already apart
func (sphere *Sphere) sphereCollide(other *Sphere) (mgl32.Vec3, bool) {
	axis := sphere.Pos.Sub(other.Pos)
	distance := axis.Len()
	overlap := sphere.Radius + other.Radius - distance
	weight, otherWeight := sphere.inverseMass(), other.inverseMass()
	if overlap <= 0 || distance == 0 || weight+otherWeight == 0 {
		return mgl32.Vec3{}, false
	}
	normal := axis.Mul(1 / distance)

	// Velocities taken before the push, which Verlet would otherwise read as speed
	velocity := sphere.Pos.Sub(sphere.PrevPos)
	otherVelocity := other.Pos.Sub(other.PrevPos)
	sphere.Pos = sphere.Pos.Add(normal.Mul(overlap * weight / (weight + otherWeight)))
	other.Pos = other.Pos.Sub(normal.Mul(overlap * otherWeight / (weight + otherWeight)))

	if closing := velocity.Sub(otherVelocity).Dot(normal); closing < 0 {
		restitution := min(sphere.Restitution, other.Restitution)
		impulse := -(1 + restitution) * closing / (weight + otherWeight)
		velocity = velocity.Add(normal.Mul(impulse * weight))
		otherVelocity = otherVelocity.Sub(normal.Mul(impulse * otherWeight))
	}
	sphere.PrevPos = sphere.Pos.Sub(velocity)
	other.PrevPos = other.Pos.Sub(otherVelocity)
	return normal, true
}

// Lifts this sphere out of a plane, but only within the plane's finite extent
func (sphere *Sphere) planeCollide(plane Plane) (mgl32.Vec3, bool) {
	distNormal := sphere.Pos.Sub(plane.Pos).Dot(plane.Normal)
	distMain := sphere.Pos.Sub(plane.Pos).Dot(plane.MainAxis)
	distCross := sphere.Pos.Sub(plane.Pos).Dot(plane.CrossAxis)

	if distNormal > -sphere.Radius && distNormal < sphere.Radius {
		if distMain > -plane.MainHalf && distMain < plane.MainHalf {
			if distCross > -plane.CrossHalf && distCross < plane.CrossHalf {
				sphere.resolve(plane.Normal, sphere.Radius-distNormal)
				return plane.Normal, true
			}
		}
	}
	return mgl32.Vec3{}, false
}

// Pushes this sphere out of a fixed box, along the line from its closest point
//
// The closest point covers faces, edges and corners alike, which is what lets a
// ball roll off the end of a plank; a centre inside the box leaves by the
// nearest face instead, there being no closest point outside to push from
func (sphere *Sphere) boxCollide(box Box) (mgl32.Vec3, bool) {
	local := sphere.Pos.Sub(box.Pos)
	closest := box.Pos
	inside := true
	var depth [3]float32
	for i, axis := range box.Axes {
		d := local.Dot(axis)
		depth[i] = d
		if d > box.Half[i] {
			d, inside = box.Half[i], false
		} else if d < -box.Half[i] {
			d, inside = -box.Half[i], false
		}
		closest = closest.Add(axis.Mul(d))
	}

	if inside {
		face, least := 0, float32(math.Inf(1))
		for i := range box.Axes {
			if penetration := box.Half[i] - float32(math.Abs(float64(depth[i]))); penetration < least {
				face, least = i, penetration
			}
		}
		normal := box.Axes[face]
		if depth[face] < 0 {
			normal = normal.Mul(-1)
		}
		sphere.resolve(normal, least+sphere.Radius)
		return normal, true
	}

	offset := sphere.Pos.Sub(closest)
	distance := offset.Len()
	if distance >= sphere.Radius || distance == 0 {
		return mgl32.Vec3{}, false
	}
	normal := offset.Mul(1 / distance)
	sphere.resolve(normal, sphere.Radius-distance)
	return normal, true
}

// Pushes the sphere out along a contact normal, bounces it off the surface,
// and grips it so it rolls rather than slides:
// the push moves Pos without PrevPos, which Verlet would read as velocity and
// launch the ball by its penetration; so the velocity is taken before the push
// and PrevPos rebuilt behind the new Pos from the resolved velocity
func (sphere *Sphere) resolve(normal mgl32.Vec3, push float32) {
	velocity := sphere.Pos.Sub(sphere.PrevPos)
	sphere.Pos = sphere.Pos.Add(normal.Mul(push))

	into := velocity.Dot(normal)
	if into >= 0 {
		sphere.PrevPos = sphere.Pos.Sub(velocity)
		return
	}
	restitution := sphere.Restitution
	// Kill momentum if bounce speed is below a threshold
	if -into*restitution < sphere.RestingSpeed {
		restitution = 0
	}
	// The normal impulse, per unit mass: a whole impact on landing, one step of gravity while resting
	impulse := -(1 + restitution) * into
	velocity = velocity.Add(normal.Mul(impulse))

	// Friction cancels the contact point's slip, the inertia sharing the
	// correction between speed and spin; on a slope this alone gives the
	// rolling acceleration g sin(slope) / (1 + Inertia)
	arm := normal.Mul(-sphere.Radius)
	slip := velocity.Add(sphere.Spin.Cross(arm))
	slip = slip.Sub(normal.Mul(slip.Dot(normal)))
	grip := slip.Mul(-sphere.Inertia / (1 + sphere.Inertia))
	if limit := sphere.Friction * impulse; grip.Len() > limit {
		grip = grip.Mul(limit / grip.Len())
	}
	velocity = velocity.Add(grip)
	sphere.Spin = sphere.Spin.Add(arm.Cross(grip).Mul(1 / (sphere.Inertia * sphere.Radius * sphere.Radius)))

	// Speed and spin slowed in proportion, so a rolling ball stays rolling as it
	// stops; a constant deceleration that stops it rather than reversing it
	tangent := velocity.Sub(normal.Mul(velocity.Dot(normal)))
	slow := sphere.RollingResistance * -into
	if speed := tangent.Len(); speed <= slow {
		velocity = velocity.Sub(tangent)
		sphere.Spin = mgl32.Vec3{}
	} else {
		velocity = velocity.Sub(tangent.Mul(slow / speed))
		sphere.Spin = sphere.Spin.Mul(1 - slow/speed)
	}		
	sphere.PrevPos = sphere.Pos.Sub(velocity)
}
