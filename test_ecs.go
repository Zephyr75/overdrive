package main

import (
	"overdrive/ecs"
	// "strconv"
	// "fmt"
)

////////////////COMPONENTS////////////////
type HealthBar struct {
	health int
}
func (HealthBar) Component() string { return "HealthBar" }

type Name struct {
	firstName string
}
func (Name) Component() string { return "Name" }

////////////////ENTITIES////////////////
type Player struct {
	name      Name
	healthBar HealthBar
}
func (Player) Entity() string { return "Player" }



func main() {

	world := ecs.World{}

	// Entities
	bob := Player{
		name:      Name{firstName: "A"},
		healthBar: HealthBar{health: 100},
	}

	// Systems
	loseHPSystem := ecs.NewSystem(&world, func(entity ecs.Entity) ecs.Entity {
      player := entity.(Player)
      player.healthBar.health -= 10
      println(player.healthBar.health)
      
      return player
    },
	)

	// World
	world.AddEntities(bob)
	world.AddSystems(loseHPSystem)

  println(bob.healthBar.health)
	loseHPSystem.RunOnQuery([]string{"Name", "HealthBar"})
  loseHPSystem.RunOnTypes([]string{"Player"})
  loseHPSystem.RunOnEntities([]ecs.Entity{bob})
}
