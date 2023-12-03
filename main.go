package main

import (
  "overdrive/core"
  "overdrive/ecs"
	"github.com/Zephyr75/gutter/ui"
  "image/color"
  "time"
)


/////////////

type HealthBar struct {
	health int
}
func (HealthBar) Component() string { return "HealthBar" }

type Name struct {
	firstName string
}
func (Name) Component() string { return "Name" }



func main() {

	app := core.App{
		Name:   "Gutter",
		Width:  1920,
		Height: 1080,
	}

	app.Run(MainWindow)

  bob := ecs.Entity{Name{"Bob"}, HealthBar{60}}
  dylan := ecs.Entity{Name{"Dylan"}}
	world := ecs.World{}

	// Systems
	renameSystem := ecs.NewSystem(
    &world, 
    func(entity ecs.Entity) ecs.Entity {
      name := entity.Get("Name").(Name)
      println(name.firstName)
      name.firstName = "Bobby"
      entity.Set("Name", name)
      return entity
    },
    &bob,
    &dylan,
  )

  loseHealthSystem := ecs.NewSystem(
    &world,
    func(entity ecs.Entity) ecs.Entity {
      healthBar := entity.Get("HealthBar").(HealthBar)
      println(healthBar.health)
      healthBar.health -= 1
      entity.Set("HealthBar", healthBar)
      return entity
    },
    &bob,
  )

	// World
	world.AddEntities(&bob)
  world.AddEntities(&dylan)
	world.AddUpdateSystems(loseHealthSystem)

  // println(bob.healthBar.health)
  renameSystem.RunOnEntities([]*ecs.Entity{&bob, &dylan})
	renameSystem.RunOnQuery([]string{"Name", "HealthBar"})
  renameSystem.RunOnTargets()

  world.Init()
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
