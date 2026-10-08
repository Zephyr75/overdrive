package main

import (
	"flag"
	"fmt"
	"image/color"
	"os"
	"time"

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

func (collider *StaticCollider) Init(world *ecs.World)      {}                           
func (collider *StaticCollider) Update(world *ecs.World)    {}                           
func (collider *StaticCollider) Type() string               { return "StaticCollider" } 
func (collider *StaticCollider) Collider() physics.Collider { return collider.collider }

// A falling ball, its mesh following the collider each frame
type Sphere struct {
	*physics.Sphere
	*scene.Mesh
}

func (sphere *Sphere) Init(world *ecs.World) {} 

func (sphere *Sphere) Update(world *ecs.World) { 
	sphere.Accelerate(mgl32.Vec3{0.0, -9.8, 0.0})
	sphere.Mesh.MoveTo(sphere.Pos)
}

func (sphere *Sphere) Type() string { return "Sphere" } 

func (sphere *Sphere) Collider() physics.Collider { return sphere.Sphere } 

// A static ball the falling one collides against
type Sphere2 struct {
	name string
	*physics.Sphere
	*scene.Mesh
}

func (sphere *Sphere2) Init(world *ecs.World)      {}                       
func (sphere *Sphere2) Update(world *ecs.World)    {}                      
func (sphere *Sphere2) Type() string               { return "Sphere2" }     
func (sphere *Sphere2) Collider() physics.Collider { return sphere.Sphere } 

func main() { 
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

	app := core.NewApp("Gutter", settings.Current.Window.Width, settings.Current.Window.Height, nil, nil)
	app.ScreenshotFile = *shot

	scene, err := scene.NewScene(paths.Asset(*sceneName), app.Backend)
	utils.HandleError(err)

	world := createWorld(&scene)

	app.Run(&scene, HUD, world)

}

// Wires the physics bodies this demo needs, skipping any mesh the scene lacks so every scene still loads
func createWorld(scene *scene.Scene) *ecs.World { 
	world := ecs.World{}

	if mesh := scene.FindMesh("Ground"); mesh != nil {
		world.AddEntities(&StaticCollider{physics.NewPlaneFromMesh(mesh, true)})
	}
	if mesh := scene.FindMesh("Sphere2"); mesh != nil {
		world.AddEntities(&StaticCollider{physics.NewSphereFromMesh(mesh, true)})
	}
	if mesh := scene.FindMesh("Sphere"); mesh != nil {
		world.AddEntities(&Sphere{physics.NewSphereFromMesh(mesh, false), mesh})
	}

	world.Init()
	return &world
}

// The HUD's state. gutter keeps none, so a click lands here and the next
// frame's tree reads it back
var hud = struct {
	health, slot int
	frames, fps  int
	lastSecond   time.Time
}{health: 70}

var items = []struct{ name, icon, hover string }{
	{"Stone", "brick_color.png", "brick_normal.png"},
	{"Wood", "wood_color.png", "wood_normal.png"},
	{"Metal", "metal_color.png", "metal_normal.png"},
	{"Pavers", "ground_color.png", "ground_normal.png"},
	{"Crate", "container.jpg", ""}, // no hover image: darkened instead
}

// HUD is the demo's overlay: status top-left, stats top-right, a crosshair,
// and the objective, hotbar and actions along the bottom. The middle stays
// empty so the scene shows through. Clicks need [debug] lockCamera = true,
// since mouse-look captures the cursor.
func HUD(app core.App) ui.UIElement {
	// Counted per built frame, published once a second so the label's text,
	// and so its cached bitmap, changes once a second rather than every frame
	if hud.lastSecond.IsZero() {
		hud.lastSecond = time.Now()
	}
	hud.frames++
	if time.Since(hud.lastSecond) >= time.Second {
		hud.fps, hud.frames, hud.lastSecond = hud.frames, 0, time.Now()
	}

	return ui.Column{
		Properties: ui.Properties{Padding: ui.SpacingEqual(ui.ScalePixel, 24)},
		Style:      ui.Style{Color: clear},
		Children: []ui.UIElement{
			row(12, panel(25, ui.Column{Style: ui.Style{Color: clear}, Children: []ui.UIElement{
				text(fmt.Sprintf("PILOT 01   HP %d", hud.health), 20, white),
				healthBar(),
			}}), space(50), panel(25, text(fmt.Sprintf("FPS %d\nOVERDRIVE x gutter", hud.fps), 16, muted))),

			// A 2 px bar holding a 2 px tall child that overflows it: a plus
			ui.Container{Properties: rel(100, 74), Style: ui.Style{Color: clear},
				Child: ui.Container{Properties: ui.Properties{Size: px(24, 2)}, Style: ui.Style{Color: white},
					Child: ui.Container{Properties: ui.Properties{Size: px(2, 24)}, Style: ui.Style{Color: white}}}},

			row(14,
				panel(25, text("OBJECTIVE\nKnock the ball off the pillar\n\nHolding: "+items[hud.slot].name, 16, white)),
				ui.Container{Properties: rel(50, 100), Style: ui.Style{Color: clear}, Child: hotbar()},
				panel(25, ui.Column{Style: ui.Style{Color: clear}, Children: []ui.UIElement{
					button("heal +10", green, func() { hud.health = min(hud.health+10, 100) }),
					button("hit -15", red, func() { hud.health = max(hud.health-15, 0) }),
					button("quit", blue, app.Quit),
				}})),
		},
	}
}

// A track with padding, holding a fill whose width is the health percentage,
// pinned left
func healthBar() ui.UIElement {
	fill := green
	if hud.health < 30 {
		fill = red
	}
	return ui.Container{
		Properties: ui.Properties{Margin: ui.SpacingSymmetric(ui.ScalePixel, 6, 8), Padding: ui.SpacingEqual(ui.ScalePixel, 3)},
		Style:      ui.Style{Color: track},
		Child: ui.Container{
			Properties: ui.Properties{Size: ui.Size{Scale: ui.ScaleRelative, Width: hud.health, Height: 100}, Alignment: ui.AlignmentLeft},
			Style:      ui.Style{Color: fill},
		},
	}
}

// Five fixed-size slots, bottom-centred in their space. Each is a frame whose
// padding shows as a border, gold when selected, around a clickable icon
func hotbar() ui.UIElement {
	slots := make([]ui.UIElement, len(items))
	for i, item := range items {
		var frame color.Color = panelColor
		if i == hud.slot {
			frame = gold
		}
		slots[i] = ui.Container{
			Properties: ui.Properties{Size: px(68, 68), Margin: ui.SpacingEqual(ui.ScalePixel, 4), Padding: ui.SpacingEqual(ui.ScalePixel, 4)},
			Style:      ui.Style{Color: frame},
			Child: ui.Button{
				Image:      paths.Asset("textures/" + item.icon),
				HoverImage: hoverAsset(item.hover),
				Function:   func() { hud.slot = i },
			},
		}
	}
	return ui.Row{
		Properties: ui.Properties{Size: px(len(items)*76, 76), Alignment: ui.AlignmentBottom},
		Style:      ui.Style{Color: clear},
		Children:   slots,
	}
}

func hoverAsset(name string) string {
	if name == "" {
		return ""
	}
	return paths.Asset("textures/" + name)
}

// Small builders, so the tree above reads as layout

func rel(w, h int) ui.Properties {
	return ui.Properties{Size: ui.Size{Scale: ui.ScaleRelative, Width: w, Height: h}}
}

func px(w, h int) ui.Size {
	return ui.Size{Scale: ui.ScalePixel, Width: w, Height: h}
}

func row(height int, children ...ui.UIElement) ui.UIElement {
	return ui.Row{Properties: rel(100, height), Style: ui.Style{Color: clear}, Children: children}
}

func space(width int) ui.UIElement {
	return ui.Container{Properties: rel(width, 100), Style: ui.Style{Color: clear}}
}

func panel(width int, child ui.UIElement) ui.UIElement {
	return ui.Container{
		Properties: ui.Properties{
			Size:    ui.Size{Scale: ui.ScaleRelative, Width: width, Height: 100},
			Padding: ui.SpacingSymmetric(ui.ScalePixel, 10, 12),
		},
		Style: ui.Style{Color: panelColor},
		Child: child,
	}
}

func text(content string, size int, col color.Color) ui.UIElement {
	return ui.Text{
		Properties: ui.Properties{Padding: ui.SpacingSymmetric(ui.ScalePixel, 0, 6)},
		StyleText:  ui.StyleText{Font: paths.Asset("Comfortaa.ttf"), FontSize: size, FontColor: col},
		Content:    content,
	}
}

func button(label string, col color.Color, fn func()) ui.UIElement {
	return ui.Button{
		Properties: ui.Properties{Margin: ui.SpacingSymmetric(ui.ScalePixel, 3, 0)},
		Style:      ui.Style{Color: col},
		Function:   fn,
		Child:      text(label, 15, black),
	}
}

var (
	white      = color.RGBA{192, 202, 245, 255}
	muted      = color.RGBA{150, 160, 200, 255}
	green      = color.RGBA{158, 206, 106, 255}
	red        = color.RGBA{247, 118, 142, 255}
	blue       = color.RGBA{122, 162, 247, 255}
	gold       = color.RGBA{224, 175, 104, 255}
	black      = color.RGBA{26, 27, 38, 255}
	clear      = color.RGBA{}
	panelColor = color.NRGBA{26, 27, 38, 180} // straight alpha: translucent over the scene
	track      = color.NRGBA{0, 0, 0, 150}
)
