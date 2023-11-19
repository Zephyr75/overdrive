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

//////////////////////

type Component interface {
	Component() string
}

type Entity interface {
	Entity() string
}

func GetTypes(e Entity) []string {
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
	World  *World
	Update func(Entity) Entity
}

func NewSystem(world *World, update func(Entity) Entity) System {
	return System{World: world, Update: update}
}

func (s *System) RunOnQuery(query []string) {
	for i, entity := range s.World.Entities {
		if sliceToSet(query).IsSubset(sliceToSet(GetTypes(entity))) {
			s.World.Entities[i] = s.Update(entity)
		}
	}
}

// func (s *System) RunOnEntities(list []string) {
//   for _, entity := range s.World.Entities {
//     for _, name := range list {
//       if name == entity.GetTypes()[0] {
//         s.Update(entity)
//       }
//     }
//   }
// }

//////////////////////

type World struct {
	Systems  []System
	Entities []Entity
}

func (w *World) AddSystems(systems ...System) {
	w.Systems = append(w.Systems, systems...)
}

func (w *World) AddEntities(entities ...Entity) {
	w.Entities = append(w.Entities, entities...)
}

