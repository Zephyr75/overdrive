// Package gameobject runs a game's scripts: every object starts once, then
// updates once per physics step, in the order it was added
package gameobject

import (
	"slices"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
)

// One script, usually one per kind of object in the game
type GameObject interface {
	// Runs once, before the object's first Update: before the first frame for
	// an object added before App.Run, at the start of the next step otherwise
	Start(game *Game)
	// Runs once per physics step, before the bodies move, so the contacts it
	// reads are the last step's
	Update(game *Game)
}

// Every object in the game, and what their scripts reach the rest of it through
type Game struct {
	scene *scene.Scene
	// nil for a game without physics
	physics *physics.World
	window *glfw.Window
	// Seconds one Update covers: the physics step, fixed whatever the frame rate
	deltaTime float32
	objects []GameObject
	// Added since the last Start, not yet started
	pending []GameObject
	// Destroyed during the step in progress, dropped once it ends
	destroyed []GameObject
}

// Creates an empty game whose Dt is the physics step, or DefaultRate's without physics
func NewGame(level *scene.Scene, bodies *physics.World, window *glfw.Window) *Game {
	rate := physics.DefaultRate
	if bodies != nil {
		rate = bodies.StepRate()
	}
	return &Game{scene: level, physics: bodies, window: window, deltaTime: 1 / float32(rate)}
}

// Getters rather than fields, so no script can change what every other script sees
func (game *Game) Scene() *scene.Scene { return game.scene }

// nil for a game without physics
func (game *Game) Physics() *physics.World { return game.physics }

func (game *Game) Window() *glfw.Window { return game.window }

// Seconds one Update covers: the physics step, fixed whatever the frame rate
func (game *Game) DeltaTime() float32 { return game.deltaTime }

// Adds objects, which start before the next step: the update order is the order they were added
func (game *Game) Add(objects ...GameObject) {
	game.pending = append(game.pending, objects...)
}

// Removes an object once the step in progress ends: its body and mesh are its own to remove and hide
func (game *Game) Destroy(object GameObject) {
	game.destroyed = append(game.destroyed, object)
}

// Starts every object added since the last call, and any those add in turn
//
// A batch joins the list before any of it starts, so a Start can Find an
// object added beside it whatever the order
func (game *Game) Start() {
	for len(game.pending) > 0 {
		batch := game.pending
		game.pending = nil
		game.objects = append(game.objects, batch...)
		for _, object := range batch {
			object.Start(game)
		}
	}
}

// Runs one step: starts what was added, updates every object in order, then drops what was destroyed
//
// Adding or destroying during the loop never changes the list it walks
func (game *Game) Step() {
	game.Start()
	for _, object := range game.objects {
		object.Update(game)
	}
	if len(game.destroyed) == 0 {
		return
	}
	kept := game.objects[:0]
	for _, object := range game.objects {
		if !slices.Contains(game.destroyed, object) {
			kept = append(kept, object)
		}
	}
	clear(game.objects[len(kept):])
	game.objects = kept
	game.destroyed = game.destroyed[:0]
}

// Returns the first object of type T, or T's zero value (nil for a pointer)
func Find[T GameObject](game *Game) T {
	for _, list := range [][]GameObject{game.objects, game.pending} {
		for _, object := range list {
			if found, ok := object.(T); ok {
				return found
			}
		}
	}
	var none T
	return none
}

// Returns every object of type T, in update order
func FindAll[T GameObject](game *Game) []T {
	var found []T
	for _, list := range [][]GameObject{game.objects, game.pending} {
		for _, object := range list {
			if typed, ok := object.(T); ok {
				found = append(found, typed)
			}
		}
	}
	return found
}
