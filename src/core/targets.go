package core

import (
	"github.com/Zephyr75/overdrive/renderer"
)

// The images sized to the window: the depth buffer every screen pass shares
// and, when the backbuffer multisamples, the colour image the main pass
// resolves out of.
//
// They are the caller's rather than the backend's, which leaves the swapchain
// the only thing at window size the backend still owns. The price is this
// struct: only the backend sees the surface go out of date, so the size has to
// be polled back out of it.
type screenTargets struct {
	samples       int
	width, height int

	depth     renderer.ImageHandle
	depthView renderer.ViewHandle
	// NoView when the backbuffer is single-sampled, where the main pass draws
	// straight into the swapchain image
	color     renderer.ImageHandle
	colorView renderer.ViewHandle
}

// Rebuilds the targets when the swapchain has resized under them, which is the
// only thing that invalidates them
//
// Called before Backend.Frame, never inside it: a frame that found the surface
// out of date rebuilds the swapchain and records nothing, so by the time this
// runs the size is already the new one and the old images are unreferenced
func (targets *screenTargets) ensure(backend renderer.Backend) {
	width, height := backend.BackbufferSize()
	if width == targets.width && height == targets.height {
		return
	}
	targets.destroy(backend)
	targets.width, targets.height = width, height
	targets.samples = backend.Capacities().BackbufferSamples

	targets.depth = backend.CreateImage(renderer.ImageSpec{
		Name: "backbufferDepth", Width: width, Height: height,
		Format: renderer.FormatDepth32F, Usage: renderer.ImageDepthAttachment,
		Samples: targets.samples,
	})
	targets.depthView = backend.CreateView(targets.depth, renderer.ViewSpec{
		Name: "backbufferDepth", Aspect: renderer.AspectDepth,
	})

	if targets.samples <= 1 {
		targets.color, targets.colorView = 0, renderer.NoView
		return
	}
	// Nothing samples it: it exists to be resolved into the swapchain image at
	// the end of the pass, so a tiler may keep it on-chip
	targets.color = backend.CreateImage(renderer.ImageSpec{
		Name: "backbufferColor", Width: width, Height: height,
		Format:  renderer.FormatBackbuffer,
		Usage:   renderer.ImageColorAttachment | renderer.ImageTransient,
		Samples: targets.samples,
	})
	targets.colorView = backend.CreateView(targets.color, renderer.ViewSpec{Name: "backbufferColor"})
}

// The main pass's colour attachment: the swapchain image itself, or the
// multisampled image resolving into it
func (targets *screenTargets) colorAttachment(clear *[4]float32) renderer.Attachment {
	if targets.colorView == renderer.NoView {
		return renderer.Attachment{View: renderer.Backbuffer, Clear: clear, Store: true}
	}
	// Store stays false: the samples themselves are never kept, only the resolve
	return renderer.Attachment{View: targets.colorView, Resolve: renderer.Backbuffer, Clear: clear}
}

func (targets *screenTargets) destroy(backend renderer.Backend) {
	for _, handle := range []renderer.Handle{
		targets.depthView, targets.depth, targets.colorView, targets.color,
	} {
		if renderer.Index(handle) != 0 {
			backend.Destroy(handle)
		}
	}
	targets.depth, targets.depthView = 0, renderer.NoView
	targets.color, targets.colorView = 0, renderer.NoView
}
