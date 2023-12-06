package main

import (
  "overdrive/core"
  "overdrive/ecs"
  "overdrive/scene"
	"github.com/Zephyr75/gutter/ui"
  "image/color"
  "time"
)


/////////////
type Name struct {
	firstName string
}
func (Name) Component() string { return "Name" }

type Mesh struct {
  mesh *scene.Mesh
}
func (Mesh) Component() string { return "Mesh" }

func (mesh Mesh) Move(x float32, y float32, z float32) {
  mesh.mesh.Move(x, y, z)
}

type Light struct {
  light *scene.Light
}

func (Light) Component() string { return "Light" }
  
func (light Light) Move(x float32, y float32, z float32) {
  light.light.Move(x, y, z)
}

func main() {

	app := core.NewApp("Gutter", 1920, 1080)

  scene := scene.NewScene()


  go test_ecs(app, &scene)
	app.Run(&scene, MainWindow)

  
}

func test_ecs(app core.App, scene *scene.Scene) {
  suzanne := ecs.Entity{
    Name{"Suzanne"},
    Mesh{scene.GetMesh("Suzanne")},
    Light{scene.GetLight("Light.003")},
  }

  for i := 0; i < 1000000; i++ {
    time.Sleep(1 * time.Second / 60)
    // suzanne.Get("Mesh").(Mesh).Move(0.1, 0, 0)

    scene.GetMesh(("Suzanne")).Move(0.1, 0, 0)

    println("2", scene, scene.GetMesh("Suzanne"))

  }



	world := ecs.World{}

	// Systems
	moveSystem := ecs.NewSystem(
    &world,
    func(entity ecs.Entity) ecs.Entity {
      mesh := entity.Get("Mesh").(Mesh)
      mesh.Move(0.1, 0, 0)
      entity = entity.Set("Mesh", mesh)
      light := entity.Get("Light").(Light)
      light.Move(0.1, 0, 0)
      entity = entity.Set("Light", light)

      // app.Scene.GetMesh(("Suzanne")).Move(0.01, 0, 0)

      println(scene.GetMesh("Suzanne").Positions[0].X())
      return entity
    },
    &suzanne,
  )


	// World
  world.AddEntities(&suzanne)
  world.AddUpdateSystems(moveSystem)

  // println(bob.healthBar.health)
  // renameSystem.RunOnEntities([]*ecs.Entity{&bob, &dylan})
	// renameSystem.RunOnQuery([]string{"Name", "HealthBar"})
  // renameSystem.RunOnTargets()

  // world.Init()
  world.Update(time.Second / 60)

  // time.Sleep(1 * time.Second)


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
