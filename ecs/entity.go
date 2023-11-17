package ecs

type Component interface {
  IsComponent()
}

type Entity []Component

//////////////////////

type System struct {
  Entities []Entity
  Update func(Entity)
}

func (s *System) AddEntity(entity Entity) {
  s.Entities = append(s.Entities, entity)
}

func (s *System) RunOnQuery(query []Component) {
  // TODO: implement
}

func (s *System) RunOnEntities() {
  for _, entity := range s.Entities {
    s.Update(entity)
  }
}

//////////////////////

type World struct {
  Systems []System
  Entities []Entity
}

func (w *World) AddSystem(system System) {
  w.Systems = append(w.Systems, system)
}

func (w *World) AddEntity(entity Entity) {
  w.Entities = append(w.Entities, entity)
}

func (w *World) Update() {
  for _, system := range w.Systems {
    system.RunOnEntities()
  }
}

