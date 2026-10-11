package core

import (
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
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

	// Ambient occlusion reads depth single-sampled: depth itself when the
	// backbuffer is, otherwise sample 0 resolved out of it by the prepass, in
	// which case sceneDepthView is the resolve target. Zero when AO is off
	sceneDepth     renderer.ImageHandle
	sceneDepthView renderer.ViewHandle
	// The noisy occlusion and the view distance forward.slang blurs it by
	ao     renderer.ImageHandle
	aoView renderer.ViewHandle
	// Point sampling, clamped: AO reads texel centres, and linear filtering of
	// a depth format is optional in Vulkan
	pointSampler renderer.SamplerHandle
}

// rebuildOnResize guarantees that the window‑sized depth and (if MSAA > 1) colour
// images match the current swapchain size. It is called once per frame
// before the backend records a new frame. If the surface size has changed
// the old targets are destroyed and new ones created.
func (targets *screenTargets) rebuildOnResize(backend renderer.Backend) {
	width, height := backend.BackbufferSize()
	if width == targets.width && height == targets.height {
		return
	}
	targets.destroy(backend)
	targets.width, targets.height = width, height
	targets.samples = backend.Capacities().BackbufferSamples

	ao := settings.Current.AmbientOcclusion.Enabled
	if ao && targets.pointSampler == 0 {
		targets.pointSampler = backend.CreateSampler(renderer.SamplerSpec{
			Name: "screenPoint", Mag: renderer.FilterNearest, Min: renderer.FilterNearest,
			Mipmap:   renderer.FilterNearest,
			OutsideU: renderer.OutsideClampToEdge, OutsideV: renderer.OutsideClampToEdge,
			OutsideW: renderer.OutsideClampToEdge,
		})
	}

	// Sampled only when AO reads it directly, which is the single-sampled case
	depthUsage := renderer.ImageDepthAttachment
	if ao && targets.samples <= 1 {
		depthUsage |= renderer.ImageSampled
	}
	targets.depth = backend.CreateImage(renderer.ImageSpec{
		Name: "backbufferDepth", Width: width, Height: height,
		Format: renderer.FormatDepth32F, Usage: depthUsage,
		Samples: targets.samples, Sampler: targets.pointSampler,
	})
	targets.depthView = backend.CreateView(targets.depth, renderer.ViewSpec{
		Name: "backbufferDepth", Aspect: renderer.AspectDepth,
	})

	if ao {
		targets.createOcclusion(backend)
	}

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

// The single-sample depth and the occlusion image, at window size
func (targets *screenTargets) createOcclusion(backend renderer.Backend) {
	width, height := targets.width, targets.height
	targets.sceneDepth, targets.sceneDepthView = targets.depth, renderer.NoView
	if targets.samples > 1 {
		targets.sceneDepth = backend.CreateImage(renderer.ImageSpec{
			Name: "sceneDepth", Width: width, Height: height, Format: renderer.FormatDepth32F,
			Usage: renderer.ImageDepthAttachment | renderer.ImageSampled, Sampler: targets.pointSampler,
		})
		targets.sceneDepthView = backend.CreateView(targets.sceneDepth, renderer.ViewSpec{
			Name: "sceneDepth", Aspect: renderer.AspectDepth,
		})
	}
	// Half floats: R8 would do for the occlusion, but not for the distance
	targets.ao = backend.CreateImage(renderer.ImageSpec{
		Name: "ao", Width: width, Height: height, Format: renderer.FormatRG16F,
		Usage: renderer.ImageColorAttachment | renderer.ImageSampled, Sampler: targets.pointSampler,
	})
	targets.aoView = backend.CreateView(targets.ao, renderer.ViewSpec{Name: "ao"})
}

// The prepass's depth attachment, resolving sample 0 out for AO when it multisamples
func (targets *screenTargets) prepassDepth(clear *[4]float32) *renderer.Attachment {
	return &renderer.Attachment{View: targets.depthView, Resolve: targets.sceneDepthView, Clear: clear, Store: true}
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
	handles := []renderer.Handle{
		targets.depthView, targets.depth, targets.colorView, targets.color,
		targets.aoView, targets.ao,
	}
	// sceneDepth is depth itself when nothing was resolved, destroyed above
	if targets.sceneDepthView != renderer.NoView {
		handles = append(handles, targets.sceneDepthView, targets.sceneDepth)
	}
	for _, handle := range handles {
		if renderer.Index(handle) != 0 {
			backend.Destroy(handle)
		}
	}
	targets.depth, targets.depthView = 0, renderer.NoView
	targets.color, targets.colorView = 0, renderer.NoView
	targets.sceneDepth, targets.sceneDepthView = 0, renderer.NoView
	targets.ao, targets.aoView = 0, renderer.NoView
}
