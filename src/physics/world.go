package physics

import "github.com/go-gl/mathgl/mgl32"

// Every body the simulation steps, and the gravity that pulls the movable ones
type World struct {
	Gravity mgl32.Vec3
	bodies  []Collider
}

// Adds bodies to the simulation
func (world *World) Add(bodies ...Collider) {
	world.bodies = append(world.bodies, bodies...)
}

// Advances the simulation by one fixed step
//
// Collisions after integrating and meshes placed last, so a mesh is always
// drawn where its body was resolved to, never sunk a step's travel into what
// it rests on
func (world *World) Step(seconds float32) {
	for _, body := range world.bodies {
		if verlet := body.Body(); !verlet.Fixed {
			verlet.Accelerate(world.Gravity)
			verlet.UpdatePosition(seconds)
		}
	}
	// Every ordered pair, each collider resolving its own half
	for _, body := range world.bodies {
		for _, other := range world.bodies {
			if body != other {
				body.Collide(other)
			}
		}
	}
	for _, body := range world.bodies {
		body.Body().follow()
	}
}
