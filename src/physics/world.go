package physics

import (
	"slices"

	"github.com/go-gl/mathgl/mgl32"
)

// The step rate of a World that sets none
const DefaultRate = 120

// Every body the simulation steps, and the gravity that pulls the movable ones
type World struct {
	Gravity  mgl32.Vec3
	// Steps per simulated second, DefaultRate when zero; fixed, so a faster
	// display draws the world more often rather than running it faster
	Rate     int
	bodies   []Body
	collisions []Collision
}

// Two bodies that touched in the last step: A was pushed off B along Normal
type Collision struct {
	A, B   Body
	Normal mgl32.Vec3 // unit, pointing out of B into A
}

// Adds bodies to the simulation
func (world *World) Add(body ...Body) {
	world.bodies = append(world.bodies, body...)
}

// Takes bodies out of the simulation; their meshes stay where they were
func (world *World) Remove(bodies ...Body) {
	kept := world.bodies[:0]
	for _, body := range world.bodies {
		if !slices.Contains(bodies, body) {
			kept = append(kept, body)
		}
	}
	clear(world.bodies[len(kept):])
	world.bodies = kept
}

// Returns the steps per simulated second the world runs at
func (world *World) StepRate() int {
	if world.Rate == 0 {
		return DefaultRate
	}
	return world.Rate
}

// Returns the seconds one step covers
func (world *World) StepLength() float32 {
	return 1 / float32(world.StepRate())
}

// Returns every contact of the last step, each pair once
func (world *World) Contacts() []Collision {
	return world.collisions
}

// Returns the last step's contacts involving body, turned so that A is body
func (world *World) ContactsOf(body Body) []Collision {
	var found []Collision
	for _, contact := range world.collisions {
		switch body {
		case contact.A:
			found = append(found, contact)
		case contact.B:
			found = append(found, Collision{A: body, B: contact.A, Normal: contact.Normal.Mul(-1)})
		}
	}
	return found
}

// Advances the simulation by one fixed step, collisions after integrating so
// a step always ends on resolved positions
func (world *World) Step() {
	seconds := world.StepLength()
	world.collisions = world.collisions[:0]
	for _, body := range world.bodies {
		verlet := body.GetVerlet(); 
		if !verlet.Fixed {
			verlet.Accelerate(world.Gravity)
			verlet.Integrate(seconds)
		}
	}
	// Every ordered pair, each collider resolving its own half. A pair is
	// reported once: whichever side resolves it leaves the reverse call nothing
	for _, body := range world.bodies {
		for _, other := range world.bodies {
			if body == other {
				continue
			}
			normal, touched := body.Collide(other)
			if touched {
				world.collisions = append(world.collisions, Collision{A: body, B: other, Normal: normal})
			}
		}
	}
}

// Moves every movable body's mesh to where the last step left it
func (world *World) PlaceAndRotateMeshes() {
	for _, body := range world.bodies {
		body.GetVerlet().placeAndRotate()
	}
}
