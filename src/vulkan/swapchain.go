package vulkan

import (
	"go-vulkan/vk"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// The swapchain alone. Everything else sized to the window — the depth buffer,
// the multisampled colour image — is the caller's, built from BackbufferSize
// and rebuilt when that changes.

// Builds the swapchain, its image entries and the per-image render semaphores
func (backend *VKBackend) createSwapchain() error { 
	capabilities, err := vk.GetPhysicalDeviceSurfaceCapabilitiesKHR(backend.vkPhysDevice, backend.vkSurface)
	if err != nil {
		return err
	}
	// CurrentExtent is the current width and height of the surface
	vkSwapExtent := capabilities.CurrentExtent
	// If CurrentExtent is 0xFFFFFFFF, surface will take swapchain size so we need to initialize swapchain size to window size
	// If CurrentExtent is not 0xFFFFFFFF, swapchain gets surface size directly
	if capabilities.CurrentExtent.Width == 0xFFFFFFFF {
		width, height := backend.window.GetSize()
		vkSwapExtent = vk.Extent2D{Width: uint32(width), Height: uint32(height)}
	}
	backend.vkSwapExtent = vkSwapExtent

	// Create swapchain with provided parameters
	backend.vkSwapchainCI = vk.SwapchainCreateInfo{
		Surface:       backend.vkSurface,
		MinImageCount: capabilities.MinImageCount,
		ImageFormat:   backend.vkSwapFormat,
		// Use SRGB non-linear for correct color space
		ImageColorSpace: vk.ColorSpaceSrgbNonlinearKHR,
		ImageExtent:     vkSwapExtent,
		// Add TransferSrc so a frame can be copied out for the screenshot debugging
		ImageUsage:   vk.ImageUsageColorAttachment | vk.ImageUsageTransferSrc,
		PreTransform: vk.SurfaceTransformIdentityKHR,
		// No blending with window system
		CompositeAlpha: vk.CompositeAlphaOpaqueKHR,
		// FIFO present mode is always supported and provides v-sync
		PresentMode: vk.PresentModeFifoKHR,
	}
	swapchain, err := vk.CreateSwapchainKHR(backend.vkDevice, backend.vkSwapchainCI)
	if err != nil {
		return err
	}
	backend.vkSwapchain = swapchain

	// Create image views
	images, err := vk.GetSwapchainImagesKHR(backend.vkDevice, swapchain)
	if err != nil {
		return err
	}
	// Initialize empty imageInfos for each swapchain image
	backend.swapchainImages = make([]image, len(images))
	// Fill imageInfos with views to each swapchain image
	for i, img := range images {
		view, err := vk.CreateImageView(backend.vkDevice, vk.ImageViewCreateInfo{
			Image: img, ViewType: vk.ImageViewType2D, Format: backend.vkSwapFormat,
			SubresourceRange: vk.ImageSubresourceRange{
				AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
			},
		})
		if err != nil {
			return err
		}
		backend.swapchainImages[i] = image{
			name: "swapchain", vkImage: img, vkView: view, vkFormat: backend.vkSwapFormat,
			vkAspect: vk.ImageAspectColor, width: int(vkSwapExtent.Width), height: int(vkSwapExtent.Height),
			layerCount: 1, vkSamples: vk.SampleCount1Bit, binding: -1,
			use: useNone, valid: true,
		}
	}

	// One render-complete semaphore per swapchain image for present to wait
	// Each semaphore belongs to the image it shows, not to the frame slot
	backend.vkRenderSemaphores = make([]vk.Semaphore, len(images))
	for i := range backend.vkRenderSemaphores {
		backend.vkRenderSemaphores[i], err = vk.CreateSemaphore(backend.vkDevice)
		if err != nil {
			return err
		}
	}

	return nil
}

// Resolves the requested sample count against the device's limits
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
	supported := backend.vkPhysDeviceProps.FramebufferColorSampleCounts & backend.vkPhysDeviceProps.FramebufferDepthSampleCounts
	// Divide wantedSamples by 2 until it matches `supported`
	for wantedSamples > vk.SampleCount1Bit && supported&wantedSamples == 0 {
		wantedSamples >>= 1
	}
	return wantedSamples
}

// Destroys the swapchain and the objects that belong to it
func (backend *VKBackend) destroySwapchain() { 
	for i := range backend.swapchainImages {
		vk.DestroyImageView(backend.vkDevice, backend.swapchainImages[i].vkView)
	}
	backend.swapchainImages = nil
	for _, semaphore := range backend.vkRenderSemaphores {
		vk.DestroySemaphore(backend.vkDevice, semaphore)
	}
	backend.vkRenderSemaphores = nil
	if backend.vkSwapchain != 0 {
		vk.DestroySwapchainKHR(backend.vkDevice, backend.vkSwapchain)
		backend.vkSwapchain = 0
	}
}

// Rebuilds everything sized to the window, after acquire or present reports the
// surface out of date, which is how a resize reaches a Vulkan app
func (backend *VKBackend) recreateSwapchain() { 
	// Wait for the window to have a non‑zero size (minimised windows have 0×0)
	// and keep polling the event queue so the user can restore the window
	width, height := backend.window.GetSize()
	for width == 0 || height == 0 {
		glfw.WaitEvents()
		width, height = backend.window.GetSize()
	}
	
	// Ensure the GPU is idle before destroying the swapchain: 
	// all queued work must finish, otherwise destroying the swapchain
	// would leave dangling references and cause validation errors
	fatalVk(vk.DeviceWaitIdle(backend.vkDevice), "wait idle before swapchain recreate")
	
	// Destroy old swapchain and create a new one that matches the new size
	backend.destroySwapchain()
	fatalVk(backend.createSwapchain(), "recreate swapchain")
}
