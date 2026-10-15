// Breakout on Overdrive: the scene is scene/breakout.toml, the game logic one
// game object per kind of thing (rules.go, paddle.go, brick.go, ball.go)
// stepped at the physics rate, the bounces physics.World's, the HUD a gutter tree
//
// Run from demo/: go run ./breakout   (-demo plays itself)
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/core"
	"github.com/Zephyr75/overdrive/gameobject"
	"github.com/Zephyr75/overdrive/paths"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
	"github.com/Zephyr75/overdrive/settings"
	"github.com/Zephyr75/overdrive/utils"
)

func main() {
	configName := flag.String("config", "vulkan.toml", "settings file: a bare name resolves under configs/, a path is used as given")
	scenePath := flag.String("scene", filepath.Join(gameDir(), "scene", "breakout.toml"), "scene file")
	shot := flag.String("screenshot", "", "write one PNG of the rendered frame to this path, then quit")
	demo := flag.Bool("demo", false, "the paddle plays itself")
	flag.Parse()
	if err := settings.Load(paths.Config(*configName)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Before the app, which takes its cursor callback
	paddle := &Paddle{}
	// A no-op handler replaces the default fly camera; the objects read their keys in Update
	app := core.NewApp("Breakout", settings.Current.Window.Width, settings.Current.Window.Height,
		func(*glfw.Window, float32) {}, paddle.mouse)
	app.ScreenshotFile = *shot
	// A game starts at once, where the showcase waits to be looked at
	app.StartDelay = 0
	// NewApp skips the cursor callback under [debug] lockCamera; the paddle wants it either way
	app.Window.SetCursorPosCallback(paddle.mouse)
	// Or the wheel would zoom the fixed camera
	app.Window.SetScrollCallback(nil)

	level, err := scene.NewScene(*scenePath, app.Backend)
	utils.HandleError(err)

	// No gravity: the ball slides on the floor plane, so the floor is not a body
	bodies := physics.World{Rate: stepRate}
	objects := gameobject.NewGame(&level, &bodies, app.Window)
	rules := &Rules{Demo: *demo}
	utils.HandleError(load(&level, &bodies, objects, rules, paddle))
	app.Run(&level, (&hud{rules: rules}).build, &bodies, objects)
}

const (
	stepRate = 120 // physics steps per second
	columns  = 6
	paddleZ  = 1.25
	// Between brick rows, and from the back wall to the first one
	rowGap, backGap = 0.06, 0.3
)

// Finds the level's meshes, adds the walls to the physics, lays the bricks out
// between them and adds the game objects in update order: the rules read the
// keys, the paddle moves, the bricks break, and the ball, last, sees all of it
func load(level *scene.Scene, bodies *physics.World, objects *gameobject.Game, rules *Rules, paddle *Paddle) error {
	meshes := map[string]*scene.Mesh{}
	for _, name := range []string{"Ball", "Paddle", "WallLeft", "WallRight", "WallBack"} {
		if meshes[name] = level.FindMesh(name); meshes[name] == nil {
			return fmt.Errorf("the scene has no mesh %q", name)
		}
	}
	for _, name := range []string{"WallLeft", "WallRight", "WallBack"} {
		bodies.Add(physics.NewBoxFromMesh(meshes[name]))
	}
	// The walls' inner faces, which bound the paddle and space the bricks
	_, hi := meshes["WallLeft"].Bounds()
	minX := hi[0]
	lo, _ := meshes["WallRight"].Bounds()
	maxX := lo[0]
	_, hi = meshes["WallBack"].Bounds()
	minZ := hi[2]

	// In file order, six to a row from the back
	var bricks []*Brick
	for i := range level.Meshes {
		if strings.HasPrefix(level.Meshes[i].Name, "Brick.") {
			bricks = append(bricks, &Brick{Mesh: &level.Meshes[i]})
		}
	}
	if len(bricks) == 0 {
		return fmt.Errorf("the scene has no Brick.* meshes")
	}
	lo, hi = bricks[0].Mesh.Bounds()
	brickHalf := hi.Sub(lo).Mul(0.5)
	rows := (len(bricks) + columns - 1) / columns
	pitchX := (maxX - minX) / columns
	for i, brick := range bricks {
		row, col := i/columns, i%columns
		brick.Center = mgl32.Vec3{
			minX + pitchX*(float32(col)+0.5),
			brickHalf[1],
			minZ + backGap + brickHalf[2] + float32(row)*(2*brickHalf[2]+rowGap),
		}
		brick.Points = 10 * (rows - row)
	}

	paddle.Mesh, paddle.MinX, paddle.MaxX = meshes["Paddle"], minX, maxX
	objects.Add(rules, paddle)
	for _, brick := range bricks {
		objects.Add(brick)
	}
	objects.Add(&Ball{Mesh: meshes["Ball"], LoseZ: paddleZ + 0.5})
	return nil
}

// This folder, which holds the scene and the HUD's images
func gameDir() string {
	return filepath.Join(paths.Root(), "demo", "breakout")
}

func sin(a float32) float32 { return float32(math.Sin(float64(a))) }
func cos(a float32) float32 { return float32(math.Cos(float64(a))) }
func abs(a float32) float32 { return float32(math.Abs(float64(a))) }
func sign(a float32) float32 {
	if a < 0 {
		return -1
	}
	return 1
}
