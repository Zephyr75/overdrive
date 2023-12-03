package core

import (
	"image"

	"github.com/disintegration/imaging"
	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"overdrive/settings"
	"overdrive/utils"

	"github.com/Zephyr75/gutter/ui"
)


var (
  lastInstance = ""
	flippedImg *image.NRGBA
	lastMap = map[string]bool{}
	areas = []ui.Area{}
)

func renderUI(app App, window *glfw.Window, widget func(app App) ui.UIElement, uiProgram uint32) {
  // Create texture
  var texture uint32
  gl.GenTextures(1, &texture)
  gl.BindTexture(gl.TEXTURE_2D, texture)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
  gl.BindImageTexture(0, texture, 0, false, 0, gl.WRITE_ONLY, gl.RGBA8)

  // Initialize image
  var img = image.NewRGBA(image.Rect(0, 0, settings.WindowWidth, settings.WindowHeight))
  instance := widget(app)
  equal := true
  for _, area := range areas {
    if ui.MouseInBounds(window, area) != lastMap[area.ToString()] {
      equal = false
    }
    if ui.MouseInBounds(window, area) && window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
      area.Function()
    }
  }

  // Only redraw if the UI has changed
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
    flippedImg = imaging.FlipV(img)
  }
  for _, area := range areas {
    lastMap[area.ToString()] = ui.MouseInBounds(window, area)
  }

  // Bind image to OpenGL texture
  gl.BindTexture(gl.TEXTURE_2D, texture)
  gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(settings.WindowWidth), int32(settings.WindowHeight), 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(flippedImg.Pix))

  // Render texture to quad
  gl.UseProgram(uiProgram)
  gl.ActiveTexture(gl.TEXTURE0)
  gl.BindTexture(gl.TEXTURE_2D, texture)
  utils.RenderQuad()
}

