package main

import (
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/gameobject"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
)

// One brick, which breaks the first time the ball touches it
type Brick struct {
	Mesh *scene.Mesh
	// Where load laid it out, and what breaking it scores
	Center mgl32.Vec3
	Points int

	body    *physics.Box
	alive   bool
	physics *physics.World
	rules   *Rules
}

func (brick *Brick) Start(game *gameobject.Game) {
	brick.rules = gameobject.Find[*Rules](game)
	brick.physics = game.Physics()
	brick.body = physics.NewBoxFromMesh(brick.Mesh)
	brick.reset()
}

// Before the ball in the update order, so a brick broken this step speeds the ball up this step
func (brick *Brick) Update(game *gameobject.Game) {
	if !brick.alive || brick.rules.State != playing {
		return
	}
	for _, contact := range game.Physics().ContactsOf(brick.body) {
		if _, ok := contact.B.(*physics.Sphere); ok {
			brick.breakOff()
			return
		}
	}
}

// Back in place and in the world, for a new game
func (brick *Brick) reset() {
	// Removed first so a brick still standing is not added twice
	brick.physics.Remove(brick.body)
	brick.physics.Add(brick.body)
	brick.alive = true
	// The world places the mesh wherever the body moved
	brick.body.Teleport(brick.Center)
	brick.Mesh.SetVisible(true)
}

// Takes the brick out of the world and the view, and scores it
func (brick *Brick) breakOff() {
	brick.alive = false
	brick.physics.Remove(brick.body)
	brick.Mesh.SetVisible(false)
	brick.rules.brickBroken(brick.Points)
}
