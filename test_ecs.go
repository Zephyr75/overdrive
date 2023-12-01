package main

import (
	"overdrive/ecs"
	"time"
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
    &dylan,
  )

  loseHealthSystem := ecs.NewSystem(
    &world,
    func(entity ecs.Entity) ecs.Entity {
      healthBar := entity.Get("HealthBar").(HealthBar)
      println(healthBar.health)
      healthBar.health -= 1
      entity.Set("HealthBar", healthBar)
      return entity
    },
    &bob,
  )

	// World
	world.AddEntities(&bob)
  world.AddEntities(&dylan)
	world.AddUpdateSystems(loseHealthSystem)

  // println(bob.healthBar.health)
  renameSystem.RunOnEntities([]*ecs.Entity{&bob, &dylan})
	renameSystem.RunOnQuery([]string{"Name", "HealthBar"})
  renameSystem.RunOnTargets()

  world.Init()
  world.Update(time.Second / 60)

  time.Sleep(2 * time.Second)
}
