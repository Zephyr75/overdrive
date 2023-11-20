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



func main() {
  bob := ecs.Entity{Name{"Bob"}, HealthBar{100}}
  dylan := ecs.Entity{Name{"Dylan"}}
	world := ecs.World{}

	// Systems
	renameSystem := ecs.NewSystem(
    &world, 
    func(entity ecs.Entity) ecs.Entity {
      name := entity.Get("Name").(Name)
      println(name.firstName)
      name.firstName = "Bobby"
      entity.Set("Name", name)
      return entity
    },
    &bob,
  )

	// World
	world.AddEntities(&bob)
  world.AddEntities(&dylan)
	world.AddInitSystems(renameSystem)

  // println(bob.healthBar.health)
  renameSystem.RunOnEntities([]*ecs.Entity{&bob, &dylan})
	renameSystem.RunOnQuery([]string{"Name", "HealthBar"})
  renameSystem.RunOnTargets()

  world.Init()
}
