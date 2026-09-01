package vulkan

import (
	"go-vulkan/vk"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/overdrive/settings"
)

// Builds the swapchain, its image views, the per-image render semaphores and the shared depth buffer
func (backend *VKBackend) createSwapchain() error {
	caps, err := vk.GetPhysicalDeviceSurfaceCapabilitiesKHR(backend.physicalDevice, backend.surface)
	if err != nil {
		return err
	}
	extent := caps.CurrentExtent
	// A currentExtent of 0xFFFFFFFF means "surface size is defined by the
	// swapchain", so fall back to the window's own size.
	if extent.Width == 0xFFFFFFFF {
		w, h := backend.window.GetSize()
		extent = vk.Extent2D{Width: uint32(w), Height: uint32(h)}
	}
	backend.swapExtent = extent

	// Keep the create info on the backend, so recreation can reuse it with a
	// new extent
	backend.swapchainCI = vk.SwapchainCreateInfo{
		Surface:         backend.surface,
		MinImageCount:   caps.MinImageCount,
		ImageFormat:     backend.swapFormat,
		ImageColorSpace: vk.ColorSpaceSrgbNonlinearKHR,
		ImageExtent:     extent,
		ImageUsage:      vk.ImageUsageColorAttachment,
		PreTransform:    vk.SurfaceTransformIdentityKHR,
		CompositeAlpha:  vk.CompositeAlphaOpaqueKHR,
		PresentMode:     vk.PresentModeFifoKHR, // vsync, always supported
	}
	sc, err := vk.CreateSwapchainKHR(backend.device, backend.swapchainCI)
	if err != nil {
		return err
	}
	backend.swapchain = sc

	if backend.swapImages, err = vk.GetSwapchainImagesKHR(backend.device, sc); err != nil {
		return err
	}
	backend.swapViews = make([]vk.ImageView, len(backend.swapImages))
	for i := range backend.swapImages {
		backend.swapViews[i], err = vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
			Image: backend.swapImages[i], ViewType: vk.ImageViewType2D, Format: backend.swapFormat,
			SubresourceRange: vk.ImageSubresourceRange{
				AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
			},
		})
		if err != nil {
			return err
		}
	}

	// Create one render-complete semaphore per swapchain image, present waiting
	// on the semaphore belonging to the image it shows rather than to the frame slot
	backend.renderSems = make([]vk.Semaphore, len(backend.swapImages))
	for i := range backend.renderSems {
		if backend.renderSems[i], err = vk.CreateSemaphore(backend.device); err != nil {
			return err
		}
	}

	if err := backend.createMSAABuffer(); err != nil { // TODO: check if not abstractable
		return err
	}
	return backend.createDepthBuffer() // TODO: check if we need the for loop line 264 in howtovulkan
}

// Resolves settings.MSAASamples against the device's limits, returning the main pass's sample count
//
// Colour and depth limits are intersected, the pass attaching one of each. The
// spec guarantees 1 and 4 in both, so stepping down always terminates.
func (backend *VKBackend) pickSampleCount() vk.SampleCountFlags {
	if !settings.MSAAEnabled() {
		return vk.SampleCount1Bit
	}
	want := vk.SampleCount2Bit
	switch {
	case settings.MSAASamples >= 8:
		want = vk.SampleCount8Bit
	case settings.MSAASamples >= 4:
		want = vk.SampleCount4Bit
	}

	props := vk.GetPhysicalDeviceProperties2(backend.physicalDevice)
	supported := props.FramebufferColorSampleCounts & props.FramebufferDepthSampleCounts
	for want > vk.SampleCount1Bit && supported&want == 0 {
		want >>= 1
	}
	return want
}

// Creates the multisampled colour image the main pass renders into, or nothing when MSAA is off
//
// Transient: nothing samples it, so a tiler can keep it on-chip.
func (backend *VKBackend) createMSAABuffer() error {
	if backend.samples == vk.SampleCount1Bit {
		return nil
	}
	img, alloc, err := backend.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType: vk.ImageType2D,
		Format:    backend.swapFormat,
		Extent:    vk.Extent3D{Width: backend.swapExtent.Width, Height: backend.swapExtent.Height, Depth: 1},
		Usage:     vk.ImageUsageColorAttachment | vk.ImageUsageTransientAttachment,
		Samples:   backend.samples,
	}, vk.VmaAllocationCreateInfo{
		Flags: vk.VmaAllocationCreateDedicatedMemory,
		Usage: vk.VmaMemoryUsageAuto,
	})
	if err != nil {
		return err
	}
	backend.msaaImage, backend.msaaAlloc = img, alloc

	backend.msaaView, err = vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: backend.swapFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
		},
	})
	return err
}

// Creates the one depth image and view every main pass renders into
func (backend *VKBackend) createDepthBuffer() error {
	img, alloc, err := backend.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType: vk.ImageType2D,
		Format:    depthFormat,
		Extent:    vk.Extent3D{Width: backend.swapExtent.Width, Height: backend.swapExtent.Height, Depth: 1},
		Usage:     vk.ImageUsageDepthStencilAttachment,
		// Match the colour attachment, which a pass's attachments must all do
		Samples: backend.samples,
	}, vk.VmaAllocationCreateInfo{
		Flags: vk.VmaAllocationCreateDedicatedMemory,
		Usage: vk.VmaMemoryUsageAuto,
	})
	if err != nil {
		return err
	}
	backend.depthImage, backend.depthAlloc = img, alloc

	backend.depthView, err = vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: depthFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectDepth, LevelCount: 1, LayerCount: 1,
		},
	})
	return err
}

// Destroys the swapchain and everything sized to it
func (backend *VKBackend) destroySwapchain() {
	for _, v := range backend.swapViews {
		vk.DestroyImageView(backend.device, v)
	}
	backend.swapViews = nil
	for _, s := range backend.renderSems {
		vk.DestroySemaphore(backend.device, s)
	}
	backend.renderSems = nil
	if backend.depthView != 0 {
		vk.DestroyImageView(backend.device, backend.depthView)
		backend.allocator.VmaDestroyImage(backend.depthImage, backend.depthAlloc)
		backend.depthView = 0
	}
	if backend.msaaView != 0 {
		vk.DestroyImageView(backend.device, backend.msaaView)
		backend.allocator.VmaDestroyImage(backend.msaaImage, backend.msaaAlloc)
		backend.msaaView, backend.msaaImage = 0, 0
	}
	if backend.swapchain != 0 {
		vk.DestroySwapchainKHR(backend.device, backend.swapchain)
		backend.swapchain = 0
	}
}

// Rebuilds everything sized to the window, after acquire or present reports the surface out of date, which is how a resize reaches a Vulkan app
func (backend *VKBackend) recreateSwapchain() {
	// Block while minimised, as a zero-sized surface is one no swapchain accepts
	w, h := backend.window.GetSize()
	for w == 0 || h == 0 {
		glfw.WaitEvents()
		w, h = backend.window.GetSize()
	}

	fatal(vk.DeviceWaitIdle(backend.device), "wait idle before swapchain recreate")
	backend.destroySwapchain()
	fatal(backend.createSwapchain(), "recreate swapchain")
}
