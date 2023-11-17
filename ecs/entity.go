package ecs

import (
  "github.com/deckarep/golang-set"
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
  ComponentType() string
}

type Entity []Component

func (e Entity) Types() []string {
  types := make([]string, len(e))
  for i, component := range e {
    types[i] = component.ComponentType()
  }
  return types
}

//////////////////////

type System struct {
  Entities []Entity
  Update func(Entity)
}

func (s *System) AddEntity(entity Entity) {
  s.Entities = append(s.Entities, entity)
}

func (s *System) RunOnQuery(query []string) {
  for _, entity := range s.Entities {

    if sliceToSet(entity.Types()).IsSuperset(sliceToSet(query)) {
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

