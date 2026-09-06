package main

import (
	"flag"
	"fmt"
	"image/color"
	"os"

	"github.com/Zephyr75/overdrive/core"
	"github.com/Zephyr75/overdrive/ecs"
	"github.com/Zephyr75/overdrive/paths"
	"github.com/Zephyr75/overdrive/physics"
	"github.com/Zephyr75/overdrive/scene"
	"github.com/Zephyr75/overdrive/settings"
	"github.com/Zephyr75/overdrive/utils"

	"github.com/Zephyr75/gutter/ui"
	"github.com/go-gl/mathgl/mgl32"
	// "fmt"
)

/////////////

// An immovable body, its collider a named field rather than embedded so the Collider() method has a name to occupy
type StaticCollider struct {
	collider physics.Collider
}

func (collider *StaticCollider) Init(world *ecs.World)      {}                           // TODO: review
func (collider *StaticCollider) Update(world *ecs.World)    {}                           // TODO: review
func (collider *StaticCollider) Type() string               { return "StaticCollider" }  // TODO: review
func (collider *StaticCollider) Collider() physics.Collider { return collider.collider } // TODO: review

// A falling ball, its mesh following the collider each frame
type Sphere struct {
	*physics.Sphere
	*scene.Mesh
}

func (sphere *Sphere) Init(world *ecs.World) {} // TODO: review

func (sphere *Sphere) Update(world *ecs.World) { // TODO: review
	sphere.Accelerate(mgl32.Vec3{0.0, -9.8, 0.0})
	sphere.Mesh.MoveTo(sphere.Pos)
}

func (sphere *Sphere) Type() string { return "Sphere" } // TODO: review

func (sphere *Sphere) Collider() physics.Collider { return sphere.Sphere } // TODO: review

// A static ball the falling one collides against
type Sphere2 struct {
	name string
	*physics.Sphere
	*scene.Mesh
}

func (sphere *Sphere2) Init(world *ecs.World)      {}                       // TODO: review
func (sphere *Sphere2) Update(world *ecs.World)    {}                       // TODO: review
func (sphere *Sphere2) Type() string               { return "Sphere2" }     // TODO: review
func (sphere *Sphere2) Collider() physics.Collider { return sphere.Sphere } // TODO: review

func main() { // TODO: review
	// Must load before NewApp, which is where the window and backend read them
	configName := flag.String("config", "vulkan.toml", "settings file: a bare name resolves under configs/, a path is used as given")
	sceneName := flag.String("scene", "showcase.xml", "scene file, resolved under assets/")
	shot := flag.String("screenshot", "", "write one PNG of the rendered frame to this path, then quit")
	flag.Parse()
	// A bad settings file is the user's mistake, not a crash, so it gets a line
	// on stderr rather than utils.HandleError's stack
	if err := settings.Load(paths.Config(*configName)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	app := core.NewApp("Gutter", settings.WindowWidth, settings.WindowHeight, nil, nil)
	app.ScreenshotFile = *shot

	scene, err := scene.NewScene(paths.Asset(*sceneName), app.Backend)
	utils.HandleError(err)

	world := createWorld(&scene)

	app.Run(&scene, nil, world)
	// app.Run(&scene, MainWindow, world)
	// app.Run(nil, nil)

}

// Wires the physics bodies this demo needs, skipping any mesh the scene lacks so every scene still loads
func createWorld(scene *scene.Scene) *ecs.World { // TODO: review
	world := ecs.World{}

	if mesh := scene.Mesh("Ground"); mesh != nil {
		world.AddEntities(&StaticCollider{physics.NewPlaneFromMesh(mesh, true)})
	}
	if mesh := scene.Mesh("Sphere2"); mesh != nil {
		world.AddEntities(&StaticCollider{physics.NewSphereFromMesh(mesh, true)})
	}
	if mesh := scene.Mesh("Sphere"); mesh != nil {
		world.AddEntities(&Sphere{physics.NewSphereFromMesh(mesh, false), mesh})
	}

	world.Init()
	return &world
}

var (
	counter int = 10
)

// Builds the demo's widget tree, one frame's worth of UI
func MainWindow(app core.App) ui.UIElement { // TODO: review
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
								Font:      paths.Asset("Comfortaa.ttf"),
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
						Font:      paths.Asset("Comfortaa.ttf"),
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
