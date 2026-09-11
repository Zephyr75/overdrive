package vulkan

import (
	"go-vulkan/vk"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/Zephyr75/overdrive/settings"
)

// The swapchain and the two images sized to it. They are the backend's rather
// than the caller's because only the backend sees a resize, and the reserved
// Backbuffer / BackbufferDepth views are how a pass names them.

// Builds the swapchain, its image entries, the per-image render semaphores and
// the depth and multisample images
func (backend *VKBackend) createSwapchain() error { // TODO: review
	capabilities, err := vk.GetPhysicalDeviceSurfaceCapabilitiesKHR(backend.physicalDevice, backend.surface)
	if err != nil {
		return err
	}
	// CurrentExtent is the current width and height of the surface
	swapExtent := capabilities.CurrentExtent
	// If CurrentExtent is 0xFFFFFFFF, surface will take swapchain size so we need to initialize swapchain size to window size
	// If CurrentExtent is not 0xFFFFFFFF, swapchain gets surface size directly
	if capabilities.CurrentExtent.Width == 0xFFFFFFFF {
		width, height := backend.window.GetSize()
		swapExtent = vk.Extent2D{Width: uint32(width), Height: uint32(height)}
	}
	backend.swapExtent = swapExtent

	// Create swapchain with provided parameters
	backend.swapchainCI = vk.SwapchainCreateInfo{
		Surface:       backend.surface,
		MinImageCount: capabilities.MinImageCount,
		ImageFormat:   backend.swapFormat,
		// Use SRGB non-linear for correct color space
		ImageColorSpace: vk.ColorSpaceSrgbNonlinearKHR,
		ImageExtent:     swapExtent,
		// Add TransferSrc so a frame can be copied out for the screenshot debugging
		ImageUsage:   vk.ImageUsageColorAttachment | vk.ImageUsageTransferSrc,
		PreTransform: vk.SurfaceTransformIdentityKHR,
		// No blending with window system
		CompositeAlpha: vk.CompositeAlphaOpaqueKHR,
		// FIFO present mode is always supported and provides v-sync
		PresentMode: vk.PresentModeFifoKHR,
	}
	swapchain, err := vk.CreateSwapchainKHR(backend.device, backend.swapchainCI)
	if err != nil {
		return err
	}
	backend.swapchain = swapchain

	// Create image views
	images, err := vk.GetSwapchainImagesKHR(backend.device, swapchain)
	if err != nil {
		return err
	}
	// Initialize empty imageInfos for each swapchain image
	backend.swapchainImages = make([]imageInfo, len(images))
	// Fill imageInfos with views to each swapchain image
	for i, img := range images {
		view, err := vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
			Image: img, ViewType: vk.ImageViewType2D, Format: backend.swapFormat,
			SubresourceRange: vk.ImageSubresourceRange{
				AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
			},
		})
		if err != nil {
			return err
		}
		backend.swapchainImages[i] = imageInfo{
			name: "swapchain", image: img, view: view, format: backend.swapFormat,
			aspect: vk.ImageAspectColor, width: int(swapExtent.Width), height: int(swapExtent.Height),
			layers: 1, samples: vk.SampleCount1Bit, binding: -1,
			use: useNone, valid: true,
		}
	}

	// One render-complete semaphore per swapchain image for present to wait
	// Each semaphore belongs to the image it shows, not to the frame slot
	backend.renderSems = make([]vk.Semaphore, len(images))
	for i := range backend.renderSems {
		if backend.renderSems[i], err = vk.CreateSemaphore(backend.device); err != nil {
			return err
		}
	}

	if err := backend.createMSAABuffer(); err != nil {
		return err
	}
	return backend.createDepthBuffer()
}

// Resolves settings.MSAASamples against the device's limits
func (backend *VKBackend) pickSampleCount() vk.SampleCountFlags {
	if !settings.IsMSAAEnabled() {
		return vk.SampleCount1Bit
	}
	wantedSamples := vk.SampleCount2Bit
	switch {
	case settings.MSAASamples >= 8:
		wantedSamples = vk.SampleCount8Bit
	case settings.MSAASamples >= 4:
		wantedSamples = vk.SampleCount4Bit
	}
	// Example: ColorSampleCounts and DepthSampleCounts look like 00111111 if 2^5 and below are supported
	// `supported` computes the XOR to get the values supported by both sample counts
	supported := backend.physicalDeviceProperties.FramebufferColorSampleCounts & backend.physicalDeviceProperties.FramebufferDepthSampleCounts
	// Divide wantedSamples by 2 until it matches `supported`
	for wantedSamples > vk.SampleCount1Bit && supported&wantedSamples == 0 {
		wantedSamples >>= 1
	}
	return wantedSamples
}

// Creates the multisampled colour image the backbuffer view resolves out of, or
// nothing when MSAA is off
//
// Transient: nothing samples it, so a tiler can keep it on-chip
func (backend *VKBackend) createMSAABuffer() error { // TODO: review
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
	view, err := vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: backend.swapFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
		},
	})
	if err != nil {
		return err
	}
	backend.msaa = imageInfo{
		name: "backbufferMSAA", image: img, alloc: alloc, view: view, format: backend.swapFormat,
		aspect: vk.ImageAspectColor, width: int(backend.swapExtent.Width), height: int(backend.swapExtent.Height),
		layers: 1, samples: backend.samples, ownsImage: true, binding: -1,
		use: useNone, valid: true,
	}
	return nil
}

// Creates the depth image every pass on the screen shares
func (backend *VKBackend) createDepthBuffer() error { // TODO: review
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
	view, err := vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: depthFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectDepth, LevelCount: 1, LayerCount: 1,
		},
	})
	if err != nil {
		return err
	}
	backend.depth = imageInfo{
		name: "backbufferDepth", image: img, alloc: alloc, view: view, format: depthFormat,
		aspect: vk.ImageAspectDepth, width: int(backend.swapExtent.Width), height: int(backend.swapExtent.Height),
		layers: 1, samples: backend.samples, ownsImage: true, binding: -1,
		use: useNone, valid: true,
	}
	return nil
}

// Destroys the swapchain and everything sized to it
func (backend *VKBackend) destroySwapchain() { // TODO: review
	for i := range backend.swapchainImages {
		vk.DestroyImageView(backend.device, backend.swapchainImages[i].view)
	}
	backend.swapchainImages = nil
	for _, semaphore := range backend.renderSems {
		vk.DestroySemaphore(backend.device, semaphore)
	}
	backend.renderSems = nil
	if backend.depth.view != 0 {
		vk.DestroyImageView(backend.device, backend.depth.view)
		backend.allocator.VmaDestroyImage(backend.depth.image, backend.depth.alloc)
		backend.depth = imageInfo{binding: -1}
	}
	if backend.msaa.view != 0 {
		vk.DestroyImageView(backend.device, backend.msaa.view)
		backend.allocator.VmaDestroyImage(backend.msaa.image, backend.msaa.alloc)
		backend.msaa = imageInfo{binding: -1}
	}
	if backend.swapchain != 0 {
		vk.DestroySwapchainKHR(backend.device, backend.swapchain)
		backend.swapchain = 0
	}
}

// Rebuilds everything sized to the window, after acquire or present reports the
// surface out of date, which is how a resize reaches a Vulkan app
func (backend *VKBackend) recreateSwapchain() { // TODO: review
	// Block while minimised: a zero-sized surface is one no swapchain accepts
	width, height := backend.window.GetSize()
	for width == 0 || height == 0 {
		glfw.WaitEvents()
		width, height = backend.window.GetSize()
	}

	fatal(vk.DeviceWaitIdle(backend.device), "wait idle before swapchain recreate")
	backend.destroySwapchain()
	fatal(backend.createSwapchain(), "recreate swapchain")
}
