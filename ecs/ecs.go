package ecs

import (
  "time"
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
  for {
    for _, entity := range w.entities {
      entity.Update(w)
    }
    time.Sleep(timeInterval)
  }
}



type Entity interface {
  Init(world *World) 
  Update(world *World) 
}


