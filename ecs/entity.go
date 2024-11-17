package ecs

import (
	"time"

	"github.com/Zephyr75/overdrive/physics"
)

type World struct {
	entities []Entity
}

func (w *World) AddEntities(entities ...Entity) {
	w.entities = append(w.entities, entities...)
}

func (w *World) Init() {
	for _, entity := range w.entities {
		entity.Init(w)
	}
}

func (w *World) Update(timeInterval time.Duration) {
	for _, entity := range w.entities {
		entity.Update(w)
	}

	// Handle collisions
	for i, entity := range w.entities {
		for j, otherEntity := range w.entities {
			if i != j {
				entity.GetCollider().Collide(otherEntity.GetCollider())
			}
		}
	}
	for _, entity := range w.entities {
		entity.GetCollider().GetVerlet().UpdatePosition(1.0 / 60.0)
	}

	// for {
	//   for _, entity := range w.entities {
	//     entity.Update(w)
	//   }
	//   time.Sleep(timeInterval)
	// }
}

func (w *World) GetEntities(entityType string) []Entity {
	var entities []Entity
	for _, entity := range w.entities {
		if entity.GetType() == entityType {
			entities = append(entities, entity)
		}
	}
	return entities
}

func (w *World) GetEntity(entityType string) Entity {
	for _, entity := range w.entities {
		if entity.GetType() == entityType {
			return entity
		}
	}
	return nil
}

type Entity interface {
	Init(world *World)
	Update(world *World)
	GetType() string
	GetCollider() physics.Collider
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
