package main

import (
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/overdrive/gameobject"
)

const (
	startLives = 3
	serveSpeed = 2.2  // metres per second
	speedUp    = 1.03 // per brick broken
	maxSpeed   = 4.5
)

type state int

const (
	serving state = iota // the ball rides the paddle until launched
	playing
	lost
	won
)

// The game's state, score, lives and the ball's speed, and the one launch
// press every other object reads; first in the update order, so they all see
// this step's press
type Rules struct {
	// The paddle and the launches play themselves
	Demo bool

	State        state
	Score, Lives int
	// The ball's, metres per second: breakout's speed is a rule, not physics
	Speed float32
	// Whether launch was pressed this step, not merely held
	Pressed bool

	window     *glfw.Window
	launchHeld bool
	stateSteps int // steps spent in the current state, for the demo's pauses
	paddle     *Paddle
	ball       *Ball
	bricks     []*Brick
}

func (rules *Rules) Start(game *gameobject.Game) {
	rules.window = game.Window()
	rules.paddle = gameobject.Find[*Paddle](game)
	rules.ball = gameobject.Find[*Ball](game)
	rules.bricks = gameobject.FindAll[*Brick](game)
	rules.Score, rules.Lives = 0, startLives
	rules.setState(serving)
}

func (rules *Rules) Update(game *gameobject.Game) {
	rules.stateSteps++
	if rules.window.GetKey(glfw.KeyEscape) == glfw.Press {
		rules.window.SetShouldClose(true)
	}

	launch := rules.window.GetKey(glfw.KeySpace) == glfw.Press ||
		(rules.State <= playing && rules.window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press)
	rules.Pressed = launch && !rules.launchHeld
	rules.launchHeld = launch
	if rules.Demo {
		rules.Pressed = rules.stateSteps > 60 && rules.State == serving || rules.stateSteps > 300
	}

	if (rules.State == lost || rules.State == won) && rules.Pressed {
		rules.Restart()
	}
}

// Puts every brick back and starts a new game; the HUD's button calls it too
func (rules *Rules) Restart() {
	rules.Score, rules.Lives = 0, startLives
	for _, brick := range rules.bricks {
		brick.reset()
	}
	rules.setState(serving)
	rules.paddle.reset()
}

// Switches state, freeing the cursor for the HUD's buttons only when there is no paddle to move
func (rules *Rules) setState(next state) {
	rules.State, rules.stateSteps = next, 0
	// A press that ended the last state must be released before it launches,
	// including for the objects after this one in the step it was pressed
	rules.launchHeld = true
	rules.Pressed = false
	rules.paddle.haveX = false
	mode := glfw.CursorDisabled
	if next == lost || next == won {
		mode = glfw.CursorNormal
	}
	rules.window.SetInputMode(glfw.CursorMode, mode)
}

// The ball was launched off the paddle at serve speed
func (rules *Rules) served() {
	rules.Speed = serveSpeed
	rules.setState(playing)
}

// Scores a broken brick, speeds the ball up, and ends the game on the last one
func (rules *Rules) brickBroken(points int) {
	rules.Score += points
	rules.Speed = min(rules.Speed*speedUp, maxSpeed)
	if rules.BricksLeft() == 0 {
		rules.setState(won)
	}
}

// The ball went past the paddle
func (rules *Rules) ballLost() {
	rules.Lives--
	if rules.Lives == 0 {
		rules.setState(lost)
	} else {
		rules.setState(serving)
	}
}

func (rules *Rules) BricksLeft() int {
	n := 0
	for _, brick := range rules.bricks {
		if brick.alive {
			n++
		}
	}
	return n
}

func (rules *Rules) BrickCount() int { return len(rules.bricks) }
