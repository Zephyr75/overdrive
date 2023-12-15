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
type Mesh struct {
  *scene.Mesh
}
func (Mesh) Component() string { return "Mesh" }


type Light struct {
  *scene.Light
}
func (Light) Component() string { return "Light" }

type Camera struct {
  camera *scene.Camera
}
func (Camera) Component() string { return "Camera" }

func (camera Camera) Move(x float32, y float32, z float32) {
  camera.camera.Move(x, y, z)
}

type Sphere struct {
  *physics.Sphere
}
func (Sphere) Component() string { return "Sphere" }

type Plane struct {
  *physics.Plane
}
func (Plane) Component() string { return "Plane" }


func main() {

	app := core.NewApp("Gutter", 1920, 1080)

  scene := scene.NewScene("assets/sphere.xml")

  go test_ecs(app, &scene)

	app.Run(&scene, nil)
	// app.Run(nil, nil)
  
}

func test_ecs(app core.App, scene *scene.Scene) {
  s1 := ecs.Entity{
    Mesh{scene.GetMesh("Sphere")},
    Sphere{
      &physics.Sphere{
        Verlet: physics.NewVerlet(mgl32.Vec3{1.0, 10.0, 0.0}),
        Radius: 1.0,
      }, 
    },
  }

  s2 := ecs.Entity{
    Mesh{scene.GetMesh("Sphere.001")},
    Sphere{
      &physics.Sphere{
        Verlet: physics.NewVerlet(mgl32.Vec3{0.0, 0.0, 0.0}),
        Radius: 1.0,
      }, 
    },
  }

  ground := ecs.Entity{
    Mesh{scene.GetMesh("Plane")},
    Plane{
      &physics.Plane{
        Verlet: physics.NewVerlet(mgl32.Vec3{0.0, 0.0, 0.0}),
        Normal: mgl32.Vec3{0.0, 1.0, 0.0},
        MainAxis: mgl32.Vec3{1.0, 0.0, 0.0},
        CrossAxis: mgl32.Vec3{0.0, 0.0, 1.0},
        MainHalf: 10.0,
        CrossHalf: 10.0,
      },
    },
  }

  wall := ecs.Entity{
    Plane{
      &physics.Plane{
        Verlet: physics.NewVerlet(mgl32.Vec3{10.0, 0.0, 0.0}),
        Normal: mgl32.Vec3{-1.0, 0.0, 0.0},
        MainAxis: mgl32.Vec3{0.0, 0.0, 1.0},
        CrossAxis: mgl32.Vec3{0.0, 1.0, 0.0},
        MainHalf: 10.0,
        CrossHalf: 10.0,
      },
    },
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
    &s1,
    &s2,
  )
  

	moveSystem := ecs.NewSystem(
    &world,
    func(entity ecs.Entity) ecs.Entity {
      sphere := entity.Get("Sphere").(Sphere)
      sphere.Accelerate(gravity)

      sphere2 := s2.Get("Sphere").(Sphere)
      sphere.Collide(*sphere2.Sphere)

      groundPlane := ground.Get("Plane").(Plane)
      sphere.Collide(*groundPlane.Plane)

      wallPlane := wall.Get("Plane").(Plane)
      sphere.Collide(*wallPlane.Plane)


      print(groundPlane.Collide(*wallPlane.Plane))

      sphere.UpdatePosition(1.0 / 60.0)
      pos := sphere.Pos
      sphere.Pos = pos
      entity = entity.Set("Sphere", sphere)


      mesh := entity.Get("Mesh").(Mesh)
      mesh.MoveTo(pos[0], pos[1], pos[2])


      entity = entity.Set("Mesh", mesh)


      return entity
    },
    &s1,
  )

	// moveSystem2 := ecs.NewSystem(
 //    &world,
 //    func(entity ecs.Entity) ecs.Entity {
 //      sphere := entity.Get("Sphere").(Sphere)
 //      // println(sphere.verlet.Pos[0], sphere.verlet.Pos[1], sphere.verlet.Pos[2])
 //      sphere.verlet.Accelerate(gravity)
 //      // sphere.verlet.FloorConstraint(0)


 //      sphere.verlet.SphereConstraint(physics.Sphere{
 //        Verlet: physics.NewVerlet(mgl32.Vec3{3.0, 11.0, 0.0}),
 //        Radius: 10.0,
 //      })

 //      sphere2 := s1.Get("Sphere").(Sphere)
 //      sphere.verlet.CollisionConstraint(sphere2.sphere)

 //      sphere.verlet.UpdatePosition(1.0 / 60.0)
 //      pos := sphere.verlet.Pos
 //      sphere.sphere.Pos = pos
 //      entity = entity.Set("Sphere", sphere)


 //      mesh := entity.Get("Mesh").(Mesh)
 //      // mesh.mesh.Position = pos
 //      mesh.MoveTo(pos[0], pos[1], pos[2])
 //      

 //      entity = entity.Set("Mesh", mesh)

 //      return entity
 //    },
 //    &s2,
 //  )



	// World
  world.AddEntities(&s1)
  world.AddEntities(&s2)
  world.AddInitSystems(initSystem)
  world.AddUpdateSystems(moveSystem)
  // world.AddUpdateSystems(moveSystem2)

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
