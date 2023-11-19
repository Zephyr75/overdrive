package ecs

import (
  "github.com/deckarep/golang-set"
  // "reflect"
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
  // IsComponent() 
  ComponentType() string
}

type Entity *[]Component

func GetTypes(e Entity) []string {
  types := []string{}
  for _, component := range *e {
    types = append(types, component.ComponentType())
  }
  return types
}

func GetComponent(e Entity, componentType string) *Component {
  for _, component := range *e {
    if component.ComponentType() == componentType {
      return &component
    }
  }
  return nil
}

// func GetTypes(e Entity) []string {
//   types := []string{}
//   for _, component := range *e {
//     types = append(types, reflect.TypeOf(component).Name())
//   }
//   return types
// }

func NewEntity(components ...Component) Entity {
  entity := make([]Component, len(components))
  for i, component := range components {
    entity[i] = component
  }
  return &entity
}

//////////////////////

type System struct {
  World *World
  Entities []Entity
  Update func(Entity)
}

func NewSystem(world *World, update func(Entity)) System {
  return System{World: world, Update: update}
}

func (s *System) AddEntities(entities ...Entity) {
  s.Entities = append(s.Entities, entities...)
}

func (s *System) RunOnQuery(query []string) {
  for _, entity := range s.World.Entities {
    if sliceToSet(query).IsSubset(sliceToSet(GetTypes(entity))) {
      s.Update(entity)
    }
  }
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

func (w *World) AddSystems(systems ...System) {
  w.Systems = append(w.Systems, systems...)
}

func (w *World) AddEntities(entities ...Entity) {
  w.Entities = append(w.Entities, entities...)
}



