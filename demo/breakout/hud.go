package main

import (
	"fmt"
	"image/color"
	"path/filepath"
	"time"

	"github.com/Zephyr75/gutter/ui"
	"github.com/Zephyr75/overdrive/core"
	"github.com/Zephyr75/overdrive/paths"
)

// The HUD, rebuilt every frame from the rules; not a game object, since it
// draws at the frame rate rather than stepping with the game
type hud struct {
	rules *Rules
	// Frames built since lastSecond, and the count published for the last whole second
	frames, fps int
	lastSecond  time.Time
}

// The HUD: score and balls along the top, a hint along the bottom, and a
// panel stacked over the middle when the game is over
func (h *hud) build(app core.App) ui.UIElement {
	rules := h.rules
	// Published once a second, so the label's bitmap changes once a second rather than every frame
	if h.lastSecond.IsZero() {
		h.lastSecond = time.Now()
	}
	h.frames++
	if time.Since(h.lastSecond) >= time.Second {
		h.fps, h.frames, h.lastSecond = h.frames, 0, time.Now()
	}

	hint := "SPACE or click to launch      A / D, arrows or mouse to move"
	layers := []ui.UIElement{nil}
	switch rules.State {
	case playing:
		hint = "ESC to quit"
	case lost:
		layers = append(layers, h.endPanel(app, "GAME OVER"))
		hint = "SPACE to play again"
	case won:
		layers = append(layers, h.endPanel(app, "CLEARED!"))
		hint = "SPACE to play again"
	}
	layers[0] = ui.Column{
		Properties: ui.Properties{Padding: ui.SpacingEqual(ui.ScalePixel, 24)},
		Style:      ui.Style{Color: clear},
		Children: []ui.UIElement{
			ui.Row{Properties: rel(100, 12), Style: ui.Style{Color: clear}, Children: []ui.UIElement{
				panel("blue", rel(18, 100), text(fmt.Sprintf("SCORE  %d", rules.Score), 30, ink, ui.TextLeft)),
				clearBox(rel(26, 100)),
				panel("blue", rel(12, 100), text(fpsLabel(h.fps), 22, ink, ui.TextCenter)),
				clearBox(rel(26, 100)),
				panel("blue", rel(18, 100), text(fmt.Sprintf("BALLS  %d", rules.Lives), 30, ink, ui.TextRight)),
			}},
			clearBox(rel(100, 76)),
			ui.Container{Properties: rel(100, 12), Style: ui.Style{Color: clear},
				Child: panel("grey", ui.Properties{Size: px(900, 72)}, text(hint, 20, paper, ui.TextCenter))},
		},
	}
	return ui.Stack{Style: ui.Style{Color: clear}, Children: layers}
}

// Dashes until a whole second has been counted
func fpsLabel(fps int) string {
	if fps == 0 {
		return "FPS  --"
	}
	return fmt.Sprintf("FPS  %d", fps)
}

// The centre panel once the game is over: what happened, the score, and two buttons
func (h *hud) endPanel(app core.App, title string) ui.UIElement {
	rules := h.rules
	return panel("yellow", ui.Properties{Size: px(640, 340)}, ui.Column{
		Style: ui.Style{Color: clear},
		Children: []ui.UIElement{
			textSized(title, 56, amber, ui.TextCenter, rel(100, 40)),
			textSized(fmt.Sprintf("Score %d, %d of %d bricks", rules.Score, rules.BrickCount()-rules.BricksLeft(), rules.BrickCount()),
				24, ink, ui.TextCenter, rel(100, 25)),
			ui.Row{Properties: rel(100, 35), Style: ui.Style{Color: clear}, Children: []ui.UIElement{
				button("PLAY AGAIN", "green", rules.Restart),
				button("QUIT", "red", app.Quit),
			}},
		},
	})
}

// Small builders, so the trees above read as layout

func rel(w, h int) ui.Properties {
	return ui.Properties{Size: ui.Size{Scale: ui.ScaleRelative, Width: w, Height: h}}
}

func px(w, h int) ui.Size {
	return ui.Size{Scale: ui.ScalePixel, Width: w, Height: h}
}

func clearBox(props ui.Properties) ui.UIElement {
	return ui.Container{Properties: props, Style: ui.Style{Color: clear}}
}

// A box drawn with Kenney's ui/panel_<colour>.png, nine-sliced so its 16 px frame keeps its width at any size
func panel(colour string, props ui.Properties, child ui.UIElement) ui.UIElement {
	// Inside the frame, with room to spare
	props.Padding = ui.SpacingSymmetric(ui.ScalePixel, 12, 32)
	return ui.Container{
		Properties: props,
		Style:      ui.Style{Slice: panelSlice},
		Image:      filepath.Join(gameDir(), "ui", "panel_"+colour+".png"),
		Child:      child,
	}
}

// The panels' frame and rounded corners, in image pixels, which the 9-slice keeps unstretched
const panelSlice = 20

func text(content string, size int, col color.Color, align ui.TextAlign) ui.UIElement {
	return textSized(content, size, col, align, ui.Properties{})
}

func textSized(content string, size int, col color.Color, align ui.TextAlign, props ui.Properties) ui.UIElement {
	return ui.Text{
		Properties: props,
		StyleText:  ui.StyleText{Font: paths.Asset("Comfortaa.ttf"), FontSize: size, FontColor: col, Align: align},
		Content:    content,
	}
}

// A button drawn with Kenney's ui/button_<colour>.png, swapped for its flat, pressed-looking variant under the cursor
func button(label, colour string, fn func()) ui.UIElement {
	return ui.Button{
		Properties: ui.Properties{Margin: ui.SpacingSymmetric(ui.ScalePixel, 14, 10)},
		Image:      filepath.Join(gameDir(), "ui", "button_"+colour+".png"),
		HoverImage: filepath.Join(gameDir(), "ui", "button_"+colour+"_hover.png"),
		Function:   fn,
		Child:      text(label, 24, ink, ui.TextCenter),
	}
}

var (
	clear = color.RGBA{}
	ink   = color.RGBA{20, 24, 32, 255}
	// The grey panel's fill is mid-grey, so its text is light
	paper = color.RGBA{248, 248, 252, 255}
	amber = color.RGBA{196, 120, 10, 255}
)
