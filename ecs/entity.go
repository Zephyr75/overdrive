package ecs

import (
	"github.com/deckarep/golang-set"
  "time"
)

func sliceToSet(mySlice []string) mapset.Set {
	mySet := mapset.NewSet()
	for _, ele := range mySlice {
		mySet.Add(ele)
	}
	return mySet
}

//////////////////////

type Component interface {
	Component() string
}

type Entity []Component

func (e Entity) getTypes() []string {
  types := []string{}
  for _, component := range e {
    types = append(types, component.Component())
  }
  return types
}

// Get a component from an entity.
func (e Entity) Get(name string) Component {
  for _, component := range e {
    if component.Component() == name {
      return component
    }
  }
  return nil
}

// Set a component in an entity.
func (e Entity) Set(name string, component Component) Entity {
  for i, c := range e {
    if c.Component() == name {
      e[i] = component
    }
  }
  return e
}

//////////////////////

type System struct {
	world  *World
	update func(Entity) Entity
  targets []*Entity
}

// Create a new system.
func NewSystem(world *World, update func(Entity) Entity, targets ...*Entity) System {
  return System{world, update, targets}
}

// Add targets to the system.
func (s System) AddTargets(targets ...*Entity) {
  s.targets = append(s.targets, targets...)
}

// Run the system on all entities that have all components in the query.
func (s System) RunOnQuery(query []string) {
	for i, entity := range s.world.entities {
		if sliceToSet(query).IsSubset(sliceToSet(entity.getTypes())) {
			*(s.world.entities[i]) = s.update(*entity)
		}
	}
}

// Run the system on all entities in the list.
func (s System) RunOnEntities(list []*Entity) {
  for i, entity := range list {
    *(list[i]) = s.update(*entity)
  }
}

// Run the system on all system targets.
func (s System) RunOnTargets() {
  for i, entity := range s.targets {
    *(s.targets[i]) = s.update(*entity)
  }
}

//////////////////////

type World struct {
  init []System
  update []System
	entities []*Entity
}

// Add systems to Init() list. 
// Systems are not automatically added to the world's systems list.
func (w *World) AddInitSystems(systems ...System) {
  w.init = append(w.init, systems...)
}

// Run all systems in the Init() list.
func (w *World) Init() {
  for _, system := range w.init {
    system.RunOnTargets()
  }
}

// Add systems to Update() list.
// Systems are not automatically added to the world's systems list.
func (w *World) AddUpdateSystems(systems ...System) {
  w.update = append(w.update, systems...)
}

// Run all systems in the Update() list.
func (w *World) Update(timeInterval time.Duration) {
  go func() {
    for {
      for _, system := range w.update {
        system.RunOnTargets()
      }
      time.Sleep(timeInterval)
    }
  }()
}

// Add entities to the world.
func (w *World) AddEntities(entities ...*Entity) {
	w.entities = append(w.entities, entities...)
}

