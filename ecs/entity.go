package ecs

import (
	"github.com/deckarep/golang-set"
	"reflect"
)

func sliceToSet(mySlice []string) mapset.Set {
	mySet := mapset.NewSet()
	for _, ele := range mySlice {
		mySet.Add(ele)
	}
	return mySet
}

func entitySliceToSet(mySlice []Entity) mapset.Set {
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

type Entity interface {
	Entity() string
}

func getTypes(e Entity) []string {
	types := []string{}
	v := reflect.ValueOf(e)
	for i := 0; i < v.NumField(); i++ {
		// types = append(types, v.Field(i).Interface().(Component).Component())
    types = append(types, v.Field(i).Type().Name())
	}
	return types
}

//////////////////////

type System struct {
	world  *World
	update func(Entity) Entity
}

func NewSystem(world *World, update func(Entity) Entity) System {
	return System{world: world, update: update}
}

func (s System) RunOnQuery(query []string) {
	for i, entity := range s.world.entities {
		if sliceToSet(query).IsSubset(sliceToSet(getTypes(entity))) {
			s.world.entities[i] = s.update(entity)
		}
	}
}

func (s System) RunOnTypes(list []string) {
  for i, entity := range s.world.entities {
    if sliceToSet(list).Contains(entity.Entity()) {
      s.world.entities[i] = s.update(entity)
    }
  }
}

func (s System) RunOnEntities(list []Entity) {
  for i, entity := range s.world.entities {
    println(entity)
    if entitySliceToSet(list).Contains(entity) {
      s.world.entities[i] = s.update(entity)
    }
  }
}

//////////////////////

type World struct {
	systems  []System
	entities []Entity
}

func (w *World) AddSystems(systems ...System) {
	w.systems = append(w.systems, systems...)
}

func (w *World) AddEntities(entities ...Entity) {
	w.entities = append(w.entities, entities...)
}

