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

// Advances the simulation by one fixed step, collisions after integrating so
// a step always ends on resolved positions
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
		body.Body().turn()
	}
}

// Moves every movable body's mesh to where the last step left it
//
// Once a frame rather than once a step: only the last step is drawn, and
// each Place dirties the mesh's shadow tiles
func (world *World) PlaceMeshes() {
	for _, body := range world.bodies {
		body.Body().place()
	}
}
