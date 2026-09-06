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
	caps, err := vk.GetPhysicalDeviceSurfaceCapabilitiesKHR(backend.physicalDevice, backend.surface)
	if err != nil {
		return err
	}
	extent := caps.CurrentExtent
	// A currentExtent of 0xFFFFFFFF means "surface size is defined by the
	// swapchain", so fall back to the window's own size
	if extent.Width == 0xFFFFFFFF {
		width, height := backend.window.GetSize()
		extent = vk.Extent2D{Width: uint32(width), Height: uint32(height)}
	}
	backend.swapExtent = extent

	backend.swapchainCI = vk.SwapchainCreateInfo{
		Surface:         backend.surface,
		MinImageCount:   caps.MinImageCount,
		ImageFormat:     backend.swapFormat,
		ImageColorSpace: vk.ColorSpaceSrgbNonlinearKHR,
		ImageExtent:     extent,
		// TransferSrc so a frame can be copied out: the screenshot and image-test path
		ImageUsage:     vk.ImageUsageColorAttachment | vk.ImageUsageTransferSrc,
		PreTransform:   vk.SurfaceTransformIdentityKHR,
		CompositeAlpha: vk.CompositeAlphaOpaqueKHR,
		PresentMode:    vk.PresentModeFifoKHR, // vsync, always supported
	}
	swapchain, err := vk.CreateSwapchainKHR(backend.device, backend.swapchainCI)
	if err != nil {
		return err
	}
	backend.swapchain = swapchain

	images, err := vk.GetSwapchainImagesKHR(backend.device, swapchain)
	if err != nil {
		return err
	}
	backend.swapchainImages = make([]imageEntry, len(images))
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
		backend.swapchainImages[i] = imageEntry{
			name: "swapchain", image: img, view: view, format: backend.swapFormat,
			aspect: vk.ImageAspectColor, width: int(extent.Width), height: int(extent.Height),
			layers: 1, samples: vk.SampleCount1Bit, binding: -1,
			use: useNone, valid: true,
		}
	}

	// One render-complete semaphore per swapchain image: present waits on the
	// semaphore belonging to the image it shows, not to the frame slot
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
//
// Colour and depth limits are intersected, the pass attaching one of each. The
// spec guarantees 1 and 4 in both, so stepping down always terminates
func (backend *VKBackend) pickSampleCount() vk.SampleCountFlags { // TODO: review
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
	supported := backend.props.FramebufferColorSampleCounts & backend.props.FramebufferDepthSampleCounts
	for want > vk.SampleCount1Bit && supported&want == 0 {
		want >>= 1
	}
	return want
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
	backend.msaa = imageEntry{
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
	backend.depth = imageEntry{
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
		backend.depth = imageEntry{binding: -1}
	}
	if backend.msaa.view != 0 {
		vk.DestroyImageView(backend.device, backend.msaa.view)
		backend.allocator.VmaDestroyImage(backend.msaa.image, backend.msaa.alloc)
		backend.msaa = imageEntry{binding: -1}
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
