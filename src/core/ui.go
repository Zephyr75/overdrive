package core

import (
	"image"
	"image/color"
	"math"

	"github.com/disintegration/imaging"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

var (
	lastInstance string
	lastMap      = map[string]bool{}
	areas        = []ui.Area{}
	uiTexture    renderer.TextureHandle
)

// Rasterises the widget tree into an RGBA image, uploads it through the backend and draws it as a fullscreen quad, inside the main pass
func renderUI(app App, widget func(app App) ui.UIElement, uiShader renderer.ShaderHandle) {
	window := app.Window

	// Allocate the canvas the widgets rasterise into
	img := image.NewRGBA(image.Rect(0, 0, settings.WindowWidth, settings.WindowHeight))
	var instance ui.UIElement = nil
	if widget != nil {
		instance = widget(app)
	}
	equal := true
	for _, area := range areas {
		if ui.MouseInBounds(window, area) != lastMap[area.ToString()] {
			equal = false
		}
		if ui.MouseInBounds(window, area) && window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
			area.Function()
		}
	}

	// Draw the debug crosshair
	if app.Debug {
		radius := 50
		for i := 0; i < 360; i++ {
			x := int(float64(radius) * math.Cos(float64(i)))
			y := int(float64(radius) * math.Sin(float64(i)))
			img.SetRGBA(settings.WindowWidth/2+x, settings.WindowHeight/2+y, color.RGBA{255, 255, 255, 255})
		}
	}

	if instance != nil {
		// Redraw only when the widget tree or the hover state changed
		if lastInstance != instance.ToString() || !equal {
			lastInstance = instance.ToString()
			areas = instance.Draw(img, window)

			newAreas := []ui.Area{}
			for _, area := range areas {
				if area.Left != 0 || area.Right != 0 || area.Top != 0 || area.Bottom != 0 {
					newAreas = append(newAreas, area)
				}
			}
			areas = newAreas
		}
		for _, area := range areas {
			lastMap[area.ToString()] = ui.MouseInBounds(window, area)
		}
	}

	flippedImg := imaging.FlipV(img)

	uiTexture = app.Backend.UpdateTexture2D(uiTexture,
		settings.WindowWidth, settings.WindowHeight, flippedImg.Pix)
	app.Backend.DrawFullscreenQuad(uiShader, uiTexture)
}
