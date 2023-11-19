package main

import (
  "overdrive/ecs"
  // "strconv"
  "fmt"
)

type Player struct {
  health int
}
// func (Player) IsComponent() {}
func (Player) ComponentType() string { return "Player" }

type Name struct {
  firstName string
  lastName string
}
// func (Name) IsComponent() {}
func (Name) ComponentType() string { return "Name" }

func main() {

  world := ecs.World{}


  // Entities
  bob := ecs.NewEntity(
    Player{health: 100},
    Name{firstName: "Bob", lastName: "Smith"},
  )

  // Systems
  loseHPSystem := ecs.NewSystem(&world, func(entity ecs.Entity) {
      var name string
      var player Player
      var playerIndex int

      for i := range *entity {
        switch (*entity)[i].ComponentType() {
        case "Name":
          name = (*entity)[i].(Name).firstName
        case "Player":
          player = (*entity)[i].(Player)
          playerIndex = i
        }
      }
      player.health -= 10
      (*entity)[playerIndex] = player

      fmt.Printf("%s's health decreased to %d\n", name, player.health)
    },
  )
  loseHPSystem.AddEntities(bob)

  // World
  world.AddEntities(bob)
  world.AddSystems(loseHPSystem)

  loseHPSystem.RunOnQuery([]string{"Name", "Player"})
  loseHPSystem.RunOnQuery([]string{"Name", "Player"})
  loseHPSystem.RunOnQuery([]string{"Name", "Player"})
  loseHPSystem.RunOnQuery([]string{"Name", "Player"})
}


