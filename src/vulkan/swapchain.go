package vulkan

import (
	"go-vulkan/vk"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/overdrive/settings"
)

// Builds the swapchain, its image views, the per-image render semaphores and the shared depth buffer
func (b *VKBackend) createSwapchain() error {
	caps, err := vk.GetPhysicalDeviceSurfaceCapabilitiesKHR(b.physicalDevice, b.surface)
	if err != nil {
		return err
	}
	extent := caps.CurrentExtent
	// A currentExtent of 0xFFFFFFFF means "surface size is defined by the
	// swapchain", so fall back to the window's own size.
	if extent.Width == 0xFFFFFFFF {
		w, h := b.window.GetSize()
		extent = vk.Extent2D{Width: uint32(w), Height: uint32(h)}
	}
	b.swapExtent = extent

	// Keep the create info on the backend, so recreation can reuse it with a
	// new extent
	b.swapchainCI = vk.SwapchainCreateInfo{
		Surface:         b.surface,
		MinImageCount:   caps.MinImageCount,
		ImageFormat:     b.swapFormat,
		ImageColorSpace: vk.ColorSpaceSrgbNonlinearKHR,
		ImageExtent:     extent,
		ImageUsage:      vk.ImageUsageColorAttachment,
		PreTransform:    vk.SurfaceTransformIdentityKHR,
		CompositeAlpha:  vk.CompositeAlphaOpaqueKHR,
		PresentMode:     vk.PresentModeFifoKHR, // vsync, always supported
	}
	sc, err := vk.CreateSwapchainKHR(b.device, b.swapchainCI)
	if err != nil {
		return err
	}
	b.swapchain = sc

	if b.swapImages, err = vk.GetSwapchainImagesKHR(b.device, sc); err != nil {
		return err
	}
	b.swapViews = make([]vk.ImageView, len(b.swapImages))
	for i := range b.swapImages {
		b.swapViews[i], err = vk.CreateImageView(b.device, vk.ImageViewCreateInfo{
			Image: b.swapImages[i], ViewType: vk.ImageViewType2D, Format: b.swapFormat,
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
	b.renderSems = make([]vk.Semaphore, len(b.swapImages))
	for i := range b.renderSems {
		if b.renderSems[i], err = vk.CreateSemaphore(b.device); err != nil {
			return err
		}
	}

	if err := b.createMSAABuffer(); err != nil { // TODO: check if not abstractable
		return err
	}
	return b.createDepthBuffer() // TODO: check if we need the for loop line 264 in howtovulkan
}

// Resolves settings.MSAASamples against the device's limits, returning the main pass's sample count
//
// Colour and depth limits are intersected, the pass attaching one of each. The
// spec guarantees 1 and 4 in both, so stepping down always terminates.
func (b *VKBackend) pickSampleCount() vk.SampleCountFlags {
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

	props := vk.GetPhysicalDeviceProperties2(b.physicalDevice)
	supported := props.FramebufferColorSampleCounts & props.FramebufferDepthSampleCounts
	for want > vk.SampleCount1Bit && supported&want == 0 {
		want >>= 1
	}
	return want
}

// Creates the multisampled colour image the main pass renders into, or nothing when MSAA is off
//
// Transient: nothing samples it, so a tiler can keep it on-chip.
func (b *VKBackend) createMSAABuffer() error {
	if b.samples == vk.SampleCount1Bit {
		return nil
	}
	img, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType: vk.ImageType2D,
		Format:    b.swapFormat,
		Extent:    vk.Extent3D{Width: b.swapExtent.Width, Height: b.swapExtent.Height, Depth: 1},
		Usage:     vk.ImageUsageColorAttachment | vk.ImageUsageTransientAttachment,
		Samples:   b.samples,
	}, vk.VmaAllocationCreateInfo{
		Flags: vk.VmaAllocationCreateDedicatedMemory,
		Usage: vk.VmaMemoryUsageAuto,
	})
	if err != nil {
		return err
	}
	b.msaaImage, b.msaaAlloc = img, alloc

	b.msaaView, err = vk.CreateImageView(b.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: b.swapFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
		},
	})
	return err
}

// Creates the one depth image and view every main pass renders into
func (b *VKBackend) createDepthBuffer() error {
	img, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType: vk.ImageType2D,
		Format:    depthFormat,
		Extent:    vk.Extent3D{Width: b.swapExtent.Width, Height: b.swapExtent.Height, Depth: 1},
		Usage:     vk.ImageUsageDepthStencilAttachment,
		// Match the colour attachment, which a pass's attachments must all do
		Samples: b.samples,
	}, vk.VmaAllocationCreateInfo{
		Flags: vk.VmaAllocationCreateDedicatedMemory,
		Usage: vk.VmaMemoryUsageAuto,
	})
	if err != nil {
		return err
	}
	b.depthImage, b.depthAlloc = img, alloc

	b.depthView, err = vk.CreateImageView(b.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: depthFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectDepth, LevelCount: 1, LayerCount: 1,
		},
	})
	return err
}

// Destroys the swapchain and everything sized to it
func (b *VKBackend) destroySwapchain() {
	for _, v := range b.swapViews {
		vk.DestroyImageView(b.device, v)
	}
	b.swapViews = nil
	for _, s := range b.renderSems {
		vk.DestroySemaphore(b.device, s)
	}
	b.renderSems = nil
	if b.depthView != 0 {
		vk.DestroyImageView(b.device, b.depthView)
		b.allocator.VmaDestroyImage(b.depthImage, b.depthAlloc)
		b.depthView = 0
	}
	if b.msaaView != 0 {
		vk.DestroyImageView(b.device, b.msaaView)
		b.allocator.VmaDestroyImage(b.msaaImage, b.msaaAlloc)
		b.msaaView, b.msaaImage = 0, 0
	}
	if b.swapchain != 0 {
		vk.DestroySwapchainKHR(b.device, b.swapchain)
		b.swapchain = 0
	}
}

// Rebuilds everything sized to the window, after acquire or present reports the surface out of date, which is how a resize reaches a Vulkan app
func (b *VKBackend) recreateSwapchain() {
	// Block while minimised, as a zero-sized surface is one no swapchain accepts
	w, h := b.window.GetSize()
	for w == 0 || h == 0 {
		glfw.WaitEvents()
		w, h = b.window.GetSize()
	}

	fatal(vk.DeviceWaitIdle(b.device), "wait idle before swapchain recreate")
	b.destroySwapchain()
	fatal(b.createSwapchain(), "recreate swapchain")
}
