package main

import (
	"image/color"
	"github.com/Zephyr75/overdrive/core"
	"github.com/Zephyr75/overdrive/ecs"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"

	"github.com/Zephyr75/gutter/ui"
	"github.com/go-gl/mathgl/mgl32"
)

/////////////

type Sphere struct {
  name string
  *physics.Sphere
  *scene.Mesh
  ground *Plane
  cube *Box
}

func (s *Sphere) Init(world *ecs.World) { }

func (s *Sphere) Update(world *ecs.World) {
  s.Accelerate(mgl32.Vec3{0.0, -9.8, 0.0})
  s.Collide(*s.ground.Plane)
  // s.Collide(*s.cube.Box)
  s.UpdatePosition(1.0 / 60.0)
  s.Mesh.MoveTo(s.Pos)
  spheres := world.GetEntities("Sphere")
  for _, sphere := range spheres {
    // println(sphere.(*Sphere).name)
    sphere.(*Sphere).name = "Alice"
  }

}

func (s *Sphere) GetType() string { return "Sphere" }





type Plane struct {
  *physics.Plane
}
func (p *Plane) Init(world *ecs.World) { }
func (p *Plane) Update(world *ecs.World) { }


type Box struct {
  *physics.Box
  *scene.Mesh
}
func (b *Box) Init(world *ecs.World) { }
func (b *Box) Update(world *ecs.World) { }


var (
	// s          scene.Scene
  // w          ecs.World
  // inGame     bool = true
	// firstMouse bool    = true
	// lastX      float64 = float64(settings.WindowWidth) / 2.0
	// lastY      float64 = float64(settings.WindowHeight) / 2.0
)

func main() {

	app := core.NewApp("Gutter", 1920, 1080, nil, nil)

  scene := scene.NewScene("assets/sphere.xml")

  world := runWorld(&scene)

	app.Run(&scene, nil, world)
	// app.Run(nil, nil)
  
}

func runWorld(scene *scene.Scene) *ecs.World {
  ground := Plane{
    &physics.Plane{
      Verlet: physics.NewVerlet(mgl32.Vec3{0.0, 0.0, 0.0}),
      Normal: mgl32.Vec3{0.0, 1.0, 0.0},
      MainAxis: mgl32.Vec3{1.0, 0.0, 0.0},
      CrossAxis: mgl32.Vec3{0.0, 0.0, 1.0},
      MainHalf: 10.0,
      CrossHalf: 10.0,
    },
  }

  physicsBox := physics.NewBox(
    mgl32.Vec3{0.0, 5.0, 0.0},
    mgl32.Vec3{1.0, 0.0, 0.0},
    mgl32.Vec3{0.0, 0.0, 1.0},
    mgl32.Vec3{0.0, 1.0, 0.0},
    2.0,
    3.0,
    1.0,
  )
  cube := Box{
    &physicsBox,
    scene.GetMesh("Cube"),
  }

  s1 := Sphere{
    "Bob",
    &physics.Sphere{
      Verlet: physics.NewVerlet(mgl32.Vec3{1.0, 10.0, 0.0}),
      Radius: 1.5,
    }, 
    scene.GetMesh("Sphere"),
    &ground,
    &cube,
  }

	world := ecs.World{}
  world.AddEntities(&s1)
  world.Init()
  // world.Update(time.Second / 60)
  return &world
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
