package vulkan

import (
	"go-vulkan/vk"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// The swapchain alone. Everything else sized to the window — the depth buffer,
// the multisampled colour image — is the caller's, built from BackbufferSize
// and rebuilt when that changes.

// Builds the swapchain, its image entries and the per-image render semaphores
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

	return nil
}

// Resolves the requested sample count against the device's limits
//
// It stays here because it is a device-limit query: the caller learns the
// answer from Capacities().BackbufferSamples and sizes its own colour and
// depth images with it
func (backend *VKBackend) pickSampleCount(wanted int) vk.SampleCountFlags {
	if wanted <= 1 {
		return vk.SampleCount1Bit
	}
	wantedSamples := vk.SampleCount2Bit
	switch {
	case wanted >= 8:
		wantedSamples = vk.SampleCount8Bit
	case wanted >= 4:
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

// Destroys the swapchain and the objects that belong to it
func (backend *VKBackend) destroySwapchain() { // TODO: review
	for i := range backend.swapchainImages {
		vk.DestroyImageView(backend.device, backend.swapchainImages[i].view)
	}
	backend.swapchainImages = nil
	for _, semaphore := range backend.renderSems {
		vk.DestroySemaphore(backend.device, semaphore)
	}
	backend.renderSems = nil
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
