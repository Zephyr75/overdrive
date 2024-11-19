package main

import (
	"github.com/Zephyr75/overdrive/core"
	"github.com/Zephyr75/overdrive/ecs"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
	"image/color"

	"github.com/Zephyr75/gutter/ui"
	"github.com/go-gl/mathgl/mgl32"
	// "fmt"
)

/////////////

type StaticCollider struct {
	physics.Collider
}

func (s *StaticCollider) Init(world *ecs.World)         {}
func (s *StaticCollider) Update(world *ecs.World)       {}
func (s *StaticCollider) GetType() string               { return "StaticCollider" }
func (s *StaticCollider) GetCollider() physics.Collider { return s.Collider }

type Sphere struct {
	*physics.Sphere
	*scene.Mesh
}

func (s *Sphere) Init(world *ecs.World) {}

func (s *Sphere) Update(world *ecs.World) {
	s.Accelerate(mgl32.Vec3{0.0, -9.8, 0.0})
	s.Mesh.MoveTo(s.Pos)
}

func (s *Sphere) GetType() string { return "Sphere" }

func (s *Sphere) GetCollider() physics.Collider { return s.Sphere }

type Sphere2 struct {
	name string
	*physics.Sphere
	*scene.Mesh
}

func (s *Sphere2) Init(world *ecs.World)         {}
func (s *Sphere2) Update(world *ecs.World)       {}
func (s *Sphere2) GetType() string               { return "Sphere2" }
func (s *Sphere2) GetCollider() physics.Collider { return s.Sphere }

func main() {

	app := core.NewApp("Gutter", 1920, 1080, true, nil, nil)

	scene := scene.NewScene("assets/sphere.xml")

	world := createWorld(&scene)

	app.Run(&scene, MainWindow, world)
	// app.Run(nil, nil)

}

func createWorld(scene *scene.Scene) *ecs.World {
	ground := StaticCollider{
		physics.NewPlaneFromMesh(scene.GetMesh("Ground"), true),
	}
	ball := StaticCollider{
		physics.NewSphereFromMesh(scene.GetMesh("Sphere2"), true),
	}

	sphereMesh := scene.GetMesh("Sphere")
	s1 := Sphere{
		physics.NewSphereFromMesh(sphereMesh, false),
		sphereMesh,
	}

	world := ecs.World{}
	world.AddEntities(&s1)
	world.AddEntities(&ball)
	world.AddEntities(&ground)
	world.Init()
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
