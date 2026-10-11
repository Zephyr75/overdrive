package ecs

import "time"

type World struct {
	entities []Entity
	// Seconds the step being run covers, for an entity's Update to read
	step float32
}

// Returns the length of the step being run, in seconds
func (world *World) Step() float32 { return world.step }

// Adds entities to the world
func (world *World) AddEntities(entities ...Entity) {
	world.entities = append(world.entities, entities...)
}

// Runs every entity's Init once, before the first frame
func (world *World) Init() { 
	for _, entity := range world.entities {
		entity.Init(world)
	}
}

// Steps every entity's game logic, once per physics step and before it
func (world *World) Update(timeInterval time.Duration) {
	world.step = float32(timeInterval.Seconds())
	for _, entity := range world.entities {
		entity.Update(world)
	}

	// for {
	//   for _, entity := range w.entities {
	//     entity.Update(w)
	//   }
	//   time.Sleep(timeInterval)
	// }
}

// Returns every entity reporting a type
func (world *World) Entities(entityType string) []Entity { 
	var entities []Entity
	for _, entity := range world.entities {
		if entity.Type() == entityType {
			entities = append(entities, entity)
		}
	}
	return entities
}

// Returns the first entity reporting a type, or nil
func (world *World) FirstEntity(entityType string) Entity { 
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
	// Runs once per physics step, before the bodies move
	Update(world *World)
	// Names this entity's kind, for lookups by type
	Type() string
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
