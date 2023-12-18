package main

import (
	"image/color"
	// "time"

	"github.com/Zephyr75/overdrive/core"
	"github.com/Zephyr75/overdrive/ecs"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
	"github.com/Zephyr75/overdrive/settings"

	"github.com/Zephyr75/gutter/ui"
	"github.com/go-gl/glfw/v3.3/glfw"
  // "math"
	"github.com/go-gl/mathgl/mgl32"
)

/////////////

type Player struct {
	*physics.Sphere
	*scene.Mesh
	ground *physics.Plane
	cam    *scene.Camera
}

func (p *Player) Init(world *ecs.World) {

}

func (p *Player) Update(world *ecs.World) {
	p.cam.Move(p.Sphere.Pos.Add(mgl32.Vec3{0.0, 2.0, 5.0}))
	p.cam.LookAt(p.Sphere.Pos)
	p.Mesh.MoveTo(p.Sphere.Pos)
}

func (p *Player) GetType() string { return "Player" }

type Enemy struct {
	*physics.Box
	*scene.Mesh
	health int
}

func (e *Enemy) Init(world *ecs.World) {}
func (e *Enemy) Update(world *ecs.World) {
}

var (
	s          scene.Scene
  w          ecs.World
  inGame     bool = true
	firstMouse bool    = true
	lastX      float64 = float64(settings.WindowWidth) / 2.0
	lastY      float64 = float64(settings.WindowHeight) / 2.0
)

func main() {

	app := core.NewApp("Gutter", 1920, 1080, processInput, mouseCallback)

	s = scene.NewScene("assets/planet.xml")

	// go runWorld(&s)
  runWorld(&s)

	app.Run(&s, nil, &w)
	// app.Run(nil, nil)

}

func runWorld(scene *scene.Scene) {
	ground := physics.Plane{
		Verlet:    physics.NewVerlet(mgl32.Vec3{0.0, 0.0, 0.0}),
		Normal:    mgl32.Vec3{0.0, 1.0, 0.0},
		MainAxis:  mgl32.Vec3{1.0, 0.0, 0.0},
		CrossAxis: mgl32.Vec3{0.0, 0.0, 1.0},
		MainHalf:  10.0,
		CrossHalf: 10.0,
	}

	player := Player{
		&physics.Sphere{
			Verlet: physics.NewVerlet(mgl32.Vec3{0.0, 10.0, 0.0}),
			Radius: 1.5,
		},
		scene.GetMesh("Ship"),
		&ground,
		scene.GetCamera(),
	}

	w = ecs.World{}
	w.AddEntities(&player)
	w.Init()
	// w.Update(time.Second / 60)
}

func processInput(window *glfw.Window, deltaTime float32) {
	if window.GetKey(glfw.KeyEscape) == glfw.Press {
		window.SetShouldClose(true)
	}
	var cameraSpeed float32 = 10 * deltaTime
	if window.GetKey(glfw.KeyLeftShift) == glfw.Press {
		cameraSpeed *= 4
	}


  player := w.GetEntity("Player").(*Player)



	if window.GetKey(glfw.KeyW) == glfw.Press {
		// s.Cam.Pos = s.Cam.Pos.Add(s.Cam.Front.Mul(cameraSpeed))
    // player.Accelerate(player.cam.Front.Mul(cameraSpeed))
    player.Sphere.Pos = player.Sphere.Pos.Add(player.cam.Front.Mul(cameraSpeed))

	}
	if window.GetKey(glfw.KeyS) == glfw.Press {
		// s.Cam.Pos = s.Cam.Pos.Sub(s.Cam.Front.Mul(cameraSpeed))
    player.Sphere.Pos = player.Sphere.Pos.Sub(player.cam.Front.Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyA) == glfw.Press {
		// s.Cam.Pos = s.Cam.Pos.Sub((s.Cam.Front.Cross(s.Cam.Up).Normalize()).Mul(cameraSpeed))
    player.Sphere.Pos = player.Sphere.Pos.Sub((player.cam.Front.Cross(player.cam.Up).Normalize()).Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyD) == glfw.Press {
		// s.Cam.Pos = s.Cam.Pos.Add((s.Cam.Front.Cross(s.Cam.Up).Normalize()).Mul(cameraSpeed))
    player.Sphere.Pos = player.Sphere.Pos.Add((player.cam.Front.Cross(player.cam.Up).Normalize()).Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyQ) == glfw.Press {
		// s.Cam.Pos = s.Cam.Pos.Add(s.Cam.Up.Mul(cameraSpeed))
    player.Sphere.Pos = player.Sphere.Pos.Add(player.cam.Up.Mul(cameraSpeed))
	}
	if window.GetKey(glfw.KeyE) == glfw.Press {
		// s.Cam.Pos = s.Cam.Pos.Sub(s.Cam.Up.Mul(cameraSpeed))
    player.Sphere.Pos = player.Sphere.Pos.Sub(player.cam.Up.Mul(cameraSpeed))
	}

	if window.GetKey(glfw.KeyTab) == glfw.Press {
		if inGame {
			window.SetCursorPosCallback(nil)
			window.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
			inGame = false
		} else {
			window.SetCursorPosCallback(mouseCallback)
			window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
			inGame = true
		}
	}

}

func mouseCallback(window *glfw.Window, xPos, yPos float64) {
	if firstMouse {
		lastX = xPos
		lastY = yPos
		firstMouse = false
	}
	// xOffset := xPos - lastX
	// yOffset := lastY - yPos
	// lastX = xPos
	// lastY = yPos
	// sensitivity := 0.1
	// xOffset *= sensitivity
	// yOffset *= sensitivity
	// s.Cam.Yaw -= float32(xOffset)
	// s.Cam.Pitch -= float32(yOffset)
	// if s.Cam.Pitch > 89.0 {
	// 	s.Cam.Pitch = 89.0
	// }
	// if s.Cam.Pitch < -89.0 {
	// 	s.Cam.Pitch = -89.0
	// }
	// var direction mgl32.Vec3
	// direction[2] = -float32(math.Cos(float64(mgl32.DegToRad(s.Cam.Pitch))) * math.Cos(float64(mgl32.DegToRad(s.Cam.Yaw))))
	// direction[1] = -float32(math.Sin(float64(mgl32.DegToRad(s.Cam.Pitch))))
	// direction[0] = -float32(math.Cos(float64(mgl32.DegToRad(s.Cam.Pitch))) * math.Sin(float64(mgl32.DegToRad(s.Cam.Yaw))))
	// s.Cam.Front = direction.Normalize()

}

var (
	counter int = 10
)

func MainWindow(app core.App) ui.UIElement {
	return ui.Row{
		Style: ui.Style{
			Color: color.Transparent,
		},
		Children: []ui.UIElement{
			ui.Button{
				Properties: ui.Properties{
					Alignment: ui.AlignmentTop,
					Size: ui.Size{
						Scale:  ui.ScalePixel,
						Width:  100,
						Height: 100,
					},
				},
				Function: func() {
					app.Quit()
				},
				Style: ui.Style{
					Color: green,
				},
			},
			ui.Column{
				Properties: ui.Properties{
					Padding: ui.PaddingSideBySide(ui.ScaleRelative, 0, 25, 25, 0),
				},
				Style: ui.Style{
					Color: color.White,
				},
				Children: []ui.UIElement{
					ui.Button{
						Properties: ui.Properties{
							Size: ui.Size{
								Scale:  ui.ScaleRelative,
								Width:  50,
								Height: 50,
							},
						},
						Function: func() {
							counter += 1
						},
						Style: ui.Style{
							Color: green,
						},
						// Image:      "white_on_black.png",
						// HoverImage: "black_on_white.png",
					},
					ui.Button{
						Properties: ui.Properties{
							Size: ui.Size{
								Scale:  ui.ScaleRelative,
								Width:  50,
								Height: 100,
							},
						},
						Function: func() {
							counter -= 1
						},
						Style: ui.Style{
							Color: red,
							// BorderColor: white,
							// BorderWidth: 10,
							// CornerRadius: 25,
						},
						Child: ui.Text{
							Properties: ui.Properties{
								Alignment: ui.AlignmentTopLeft,
								//Padding:   ui.PaddingEqual(ui.ScalePixel, 100),
								Size: ui.Size{
									Scale:  ui.ScalePixel,
									Width:  100,
									Height: 50,
								},
							},
							StyleText: ui.StyleText{
								Font:      "Comfortaa.ttf",
								FontSize:  counter,
								FontColor: black,
							},
						},
						// Image:      "white_on_black.png",
						// HoverImage: "black_on_white.png",
					},
					ui.Container{
						Properties: ui.Properties{
							Size: ui.Size{
								Scale:  ui.ScaleRelative,
								Width:  50,
								Height: 50,
							},
						},
						Style: ui.Style{
							// BorderWidth: 10,
							// BorderColor: white,
							// CornerRadius: 25,
							Color: color.Transparent,
							// ShadowWidth: 10,
							// ShadowAlignment: ui.AlignmentBottom,
						},
						// Image: "white_on_black.png",
					},
				},
			},
			ui.Container{
				Style: ui.Style{
					Color: red,
				},
				Child: ui.Text{
					Properties: ui.Properties{
						Alignment: ui.AlignmentTopLeft,
						//Padding:   ui.PaddingEqual(ui.ScalePixel, 100),
						Size: ui.Size{
							Scale:  ui.ScalePixel,
							Width:  100,
							Height: 50,
						},
					},
					StyleText: ui.StyleText{
						Font:      "Comfortaa.ttf",
						FontSize:  counter,
						FontColor: black,
					},
				},
			},
		},
	}
}

var (
	green       = color.RGBA{158, 206, 106, 255}
	white       = color.RGBA{192, 202, 245, 255}
	blue        = color.RGBA{122, 162, 247, 255}
	red         = color.RGBA{247, 118, 142, 255}
	black       = color.RGBA{26, 27, 38, 255}
	transparent = color.RGBA{0, 0, 0, 0}
)
