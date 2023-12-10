package main

import (
	"image/color"
	"overdrive/core"
	"overdrive/ecs"
	"overdrive/physics"
	"overdrive/scene"
	"time"

	"github.com/Zephyr75/gutter/ui"
  "github.com/go-gl/mathgl/mgl32"
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


type Light struct {
  light *scene.Light
}

func (Light) Component() string { return "Light" }
  
func (light Light) Move(x float32, y float32, z float32) {
  light.light.Move(x, y, z)
}

type Camera struct {
  camera *scene.Camera
}

func (Camera) Component() string { return "Camera" }

func (camera Camera) Move(x float32, y float32, z float32) {
  camera.camera.Move(x, y, z)
}

type Sphere struct {
  sphere *physics.Sphere
  verlet *physics.Verlet
}

func (Sphere) Component() string { return "Sphere" }



func main() {

	app := core.NewApp("Gutter", 1920, 1080)

  scene := scene.NewScene("assets/sphere.xml")

  go test_ecs(app, &scene)

	app.Run(&scene, nil)
	// app.Run(nil, nil)
  
}

func test_ecs(app core.App, scene *scene.Scene) {
  // suzanne := ecs.Entity{
  //   Name{"Suzanne"},
  //   Mesh{scene.GetMesh("Suzanne")},
  //   Light{scene.GetLight("Light.003")},
  //   Camera{scene.GetCamera()},
  // }
  suzanne := ecs.Entity{
    Name{"Sphere"},
    Mesh{scene.GetMesh("Sphere")},
    Light{scene.GetLight("Light")},
    Camera{scene.GetCamera()},
    Sphere{&physics.Sphere{}, &physics.Verlet{
      Pos: mgl32.Vec3{0.0, 5.0, 0.0},
      PrevPos: mgl32.Vec3{0.0, 5.0, 0.0},
    }},
  }

  
  gravity := mgl32.Vec3{0.0, -9.8, 0.0}

	world := ecs.World{}

	// Systems
  initSystem := ecs.NewSystem(
    &world,
    func(entity ecs.Entity) ecs.Entity {
      mesh := entity.Get("Mesh").(Mesh)
      // mesh.mesh.SetPosition(0.0, 4.0, 0.0)
      entity = entity.Set("Mesh", mesh)
      return entity
    },
    &suzanne,
  )
  

	moveSystem := ecs.NewSystem(
    &world,
    func(entity ecs.Entity) ecs.Entity {
      sphere := entity.Get("Sphere").(Sphere)
      println(sphere.verlet.Pos[0], sphere.verlet.Pos[1], sphere.verlet.Pos[2])
      sphere.verlet.Accelerate(gravity)
      sphere.verlet.FloorConstraint(0)
      // sphere.verlet.SphereConstraint(physics.Sphere{mgl32.Vec3{0.0, 0.0, 0.0}, 10.0})
      sphere.verlet.UpdatePosition(1.0 / 60.0)
      pos := sphere.verlet.Pos
      entity = entity.Set("Sphere", sphere)


      mesh := entity.Get("Mesh").(Mesh)
      // mesh.mesh.Position = pos
      mesh.mesh.MoveTo(pos[0], pos[1], pos[2])
      

      entity = entity.Set("Mesh", mesh)

      return entity
    },
    &suzanne,
  )


	// World
  world.AddEntities(&suzanne)
  world.AddInitSystems(initSystem)
  world.AddUpdateSystems(moveSystem)

  // println(bob.healthBar.health)
  // renameSystem.RunOnEntities([]*ecs.Entity{&bob, &dylan})
	// renameSystem.RunOnQuery([]string{"Name", "HealthBar"})
  // renameSystem.RunOnTargets()

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
