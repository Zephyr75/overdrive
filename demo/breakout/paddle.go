package main

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/gameobject"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
)

const (
	paddleSpeed = 3.5   // metres per second, on the keys
	mouseSpeed  = 0.003 // metres per cursor pixel
)

// The paddle, moved by the keys, the cursor or, in the demo, towards the ball
type Paddle struct {
	Mesh *scene.Mesh
	// The walls' inner faces, which its edges stop at
	MinX, MaxX float32

	body *physics.Box
	// Half its size, taken from the mesh so make_meshes.py is the one place it lives
	half mgl32.Vec3
	x    float32
	// Times the ball came off its top, which the demo varies its aim by
	hits int
	// Cursor motion since the last step, and the position it is measured from
	mouseDX float64
	lastX   float64
	haveX   bool

	rules *Rules
	ball  *Ball
}

func (paddle *Paddle) Start(game *gameobject.Game) {
	paddle.rules = gameobject.Find[*Rules](game)
	paddle.ball = gameobject.Find[*Ball](game)
	paddle.body = physics.NewBoxFromMesh(paddle.Mesh)
	lo, hi := paddle.Mesh.LocalBounds()
	paddle.half = hi.Sub(lo).Mul(0.5)
	game.Physics().Add(paddle.body)
	paddle.reset()
}

func (paddle *Paddle) Update(game *gameobject.Game) {
	window := game.Window()
	move := float32(paddle.mouseDX * mouseSpeed)
	paddle.mouseDX = 0
	if window.GetKey(glfw.KeyA) == glfw.Press || window.GetKey(glfw.KeyLeft) == glfw.Press {
		move -= paddleSpeed * game.DeltaTime()
	}
	if window.GetKey(glfw.KeyD) == glfw.Press || window.GetKey(glfw.KeyRight) == glfw.Press {
		move += paddleSpeed * game.DeltaTime()
	}
	if paddle.rules.Demo {
		// Meets the ball off-centre, a different side each hit, to aim it about
		target := paddle.ball.body.Pos[0] - 0.6*paddle.half[0]*sin(float32(paddle.hits)*1.7)
		reach := 1.5 * paddleSpeed * game.DeltaTime()
		move = mgl32.Clamp(target-paddle.x, -reach, reach)
	}
	paddle.moveTo(paddle.x + move)
}

// Back to the middle, for a new game
func (paddle *Paddle) reset() { paddle.moveTo(0) }

// Teleported rather than driven through Pos, so the ball takes none of its
// speed: the ball aims itself off the paddle. The world places the mesh
func (paddle *Paddle) moveTo(x float32) {
	paddle.x = mgl32.Clamp(x, paddle.MinX+paddle.half[0], paddle.MaxX-paddle.half[0])
	paddle.body.Teleport(mgl32.Vec3{paddle.x, paddle.half[1], paddleZ})
}

// Collects cursor motion while it is captured; a free cursor is the HUD's
func (paddle *Paddle) mouse(window *glfw.Window, x, y float64) {
	if window.GetInputMode(glfw.CursorMode) != glfw.CursorDisabled {
		paddle.haveX = false
		return
	}
	if paddle.haveX {
		paddle.mouseDX += x - paddle.lastX
	}
	paddle.lastX, paddle.haveX = x, true
}
