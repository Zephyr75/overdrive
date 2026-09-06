package ecs

import (
	"time"

	"github.com/Zephyr75/overdrive/physics"
)

type World struct {
	entities []Entity
}

// Adds entities to the world
func (world *World) AddEntities(entities ...Entity) { // TODO: review
	world.entities = append(world.entities, entities...)
}

// Runs every entity's Init once, before the first frame
func (world *World) Init() { // TODO: review
	for _, entity := range world.entities {
		entity.Init(world)
	}
}

// Steps every entity, resolves collisions pairwise, then integrates the Verlet positions
func (world *World) Update(timeInterval time.Duration) { // TODO: review
	for _, entity := range world.entities {
		entity.Update(world)
	}

	// Collide every ordered pair, each collider resolving its own half
	for i, entity := range world.entities {
		for j, otherEntity := range world.entities {
			if i != j {
				entity.Collider().Collide(otherEntity.Collider())
			}
		}
	}
	for _, entity := range world.entities {
		entity.Collider().Body().UpdatePosition(1.0 / 60.0)
	}

	// for {
	//   for _, entity := range w.entities {
	//     entity.Update(w)
	//   }
	//   time.Sleep(timeInterval)
	// }
}

// Returns every entity reporting a type
func (world *World) Entities(entityType string) []Entity { // TODO: review
	var entities []Entity
	for _, entity := range world.entities {
		if entity.Type() == entityType {
			entities = append(entities, entity)
		}
	}
	return entities
}

// Returns the first entity reporting a type, or nil
func (world *World) FirstEntity(entityType string) Entity { // TODO: review
	for _, entity := range world.entities {
		if entity.Type() == entityType {
			return entity
		}
	}
	return nil
}

type Entity interface {
	// Runs once, before the first frame
	Init(world *World)
	// Runs once per frame, before collisions are resolved
	Update(world *World)
	// Names this entity's kind, for lookups by type
	Type() string
	// Returns the collider the physics step drives
	Collider() physics.Collider
}

// type Sphere struct {
//   *physics.Sphere
//   *scene.Mesh
//   ground *Plane
//   cube *Box
// }

// func (s *Sphere) Init(world *ecs.World) { }

// func (s *Sphere) Update(world *ecs.World) {
//   s.Accelerate(mgl32.Vec3{0.0, -9.8, 0.0})
//   s.Collide(*s.ground.Plane)
//   // s.Collide(*s.cube.Box)
//   s.UpdatePosition(1.0 / 60.0)
//   s.Mesh.MoveTo(s.Pos)
// }
