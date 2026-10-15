package main

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/gameobject"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
)

const (
	// Off straight ahead at the paddle's edge, 60°: where the ball lands is how it is aimed
	maxBounce = math.Pi / 3
	// The least of its speed the ball keeps towards or away from the paddle
	minForward = 0.35
)

// The ball: rides the paddle until launched, then lets the world bounce it
// and keeps it at the rules' speed. Last in the update order, so it sees the
// paddle where it moved and the speed the bricks set this step
type Ball struct {
	Mesh *scene.Mesh
	// The line past which the ball is lost
	LoseZ float32

	body *physics.Sphere
	// Launches so far, which alternate the serve's angle
	serves int
	rules  *Rules
	paddle *Paddle
}

func (ball *Ball) Start(game *gameobject.Game) {
	ball.rules = gameobject.Find[*Rules](game)
	ball.paddle = gameobject.Find[*Paddle](game)
	ball.body = physics.NewSphereFromMesh(ball.Mesh)
	// Elastic and frictionless, keeping every bounce however glancing: the
	// rules set the speed, the world only turns it
	ball.body.Restitution, ball.body.Friction, ball.body.RestingSpeed = 1, 0, 0
	game.Physics().Add(ball.body)
}

func (ball *Ball) Update(game *gameobject.Game) {
	switch ball.rules.State {
	case serving:
		ball.serve(game.DeltaTime())
	case playing:
		ball.play(game)
	}
}

// Rides the paddle, at rest until launched
func (ball *Ball) serve(dt float32) {
	paddle := ball.paddle
	radius := ball.body.Radius
	ball.body.Teleport(mgl32.Vec3{paddle.x, radius, paddleZ - paddle.half[2] - radius - 0.01})
	ball.body.SetVelocity(mgl32.Vec3{}, dt)
	if !ball.rules.Pressed {
		return
	}
	// Alternating a little off straight, so two serves do not trace one path
	angle := float32(0.25)
	if ball.serves%2 == 1 {
		angle = -angle
	}
	ball.serves++
	ball.rules.served()
	ball.body.SetVelocity(mgl32.Vec3{sin(angle), 0, -cos(angle)}.Mul(ball.rules.Speed), dt)
}

// Re-aims off the paddle's top, keeps the rules' speed, rolls, and is lost past the paddle
func (ball *Ball) play(game *gameobject.Game) {
	dt := game.DeltaTime()
	paddle := ball.paddle
	for _, contact := range game.Physics().ContactsOf(ball.body) {
		// Off the top face only; a ball clipping the side keeps the world's bounce
		if contact.B == paddle.body && contact.Normal[2] < -0.5 {
			offset := mgl32.Clamp((ball.body.Pos[0]-paddle.x)/(paddle.half[0]+ball.body.Radius), -1, 1)
			angle := offset * maxBounce
			ball.body.SetVelocity(mgl32.Vec3{sin(angle), 0, -cos(angle)}, dt)
			paddle.hits++
		}
	}

	// The world only turns the velocity: the speed is the rules', and a
	// corner must not send the ball almost sideways, crossing for ever
	speed := ball.rules.Speed
	velocity := ball.body.Velocity(dt)
	velocity[1] = 0
	if abs(velocity[2]) < minForward*velocity.Len() {
		velocity[2] = sign(velocity[2]) * minForward * velocity.Len()
	}
	ball.body.SetVelocity(velocity.Normalize().Mul(speed), dt)

	// Rolls rather than slides: turned about the axis across its travel by distance over radius
	axis := mgl32.Vec3{0, 1, 0}.Cross(velocity).Normalize()
	ball.body.Orientation = mgl32.QuatRotate(speed*dt/ball.body.Radius, axis).Mul(ball.body.Orientation).Normalize()

	if ball.body.Pos[2] > ball.LoseZ {
		ball.rules.ballLost()
	}
}
