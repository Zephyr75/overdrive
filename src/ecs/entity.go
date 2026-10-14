package ecs

type World struct {
	entities []Entity
}

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

// Steps every entity's game logic, once per physics step and before it; an
// entity reads the step's length from the physics world it holds
func (world *World) Update() {
	for _, entity := range world.entities {
		entity.Update(world)
	}
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
