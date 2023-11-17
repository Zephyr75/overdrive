package main

import (
  "overdrive/ecs"
)


type Name struct {
  firstName string
  lastName string
}
func (n Name) ComponentType() string {
  return "Name"
}

func main() {
  // Entities
  bob := ecs.Entity{
    Name{
      firstName: "Bob",
      lastName: "Dylan",
    },
  }
  zeph := ecs.Entity{
    Name{
      firstName: "Zeph",
      lastName: "Carter",
    },
  }

  // Systems
  helloSystem := ecs.System{
    Entities: []ecs.Entity{bob, zeph},
    Update: func (entity ecs.Entity) {
      name := entity[0].(Name)
      println("Hello, " + name.firstName + " " + name.lastName + "!")
    },
  }

  // World
  world := ecs.World{}
  world.AddEntity(bob)
  world.AddSystem(helloSystem)
  // world.Update()

  helloSystem.RunOnQuery([]string{"Name"})
}
