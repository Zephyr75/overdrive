package vulkan

import (
	"go-vulkan/vk"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// createSwapchain builds the swapchain, its image views, the per-image render
// semaphores and the shared depth buffer. The create info is kept on the
// backend so recreateSwapchain can reuse it with a new extent.
func (b *VKBackend) createSwapchain() error {
	caps, err := vk.GetPhysicalDeviceSurfaceCapabilitiesKHR(b.physicalDevice, b.surface)
	if err != nil {
		return err
	}
	extent := caps.CurrentExtent
	if extent.Width == 0xFFFFFFFF {
		// "Surface size is defined by the swapchain" — use the window's size.
		w, h := b.window.GetSize()
		extent = vk.Extent2D{Width: uint32(w), Height: uint32(h)}
	}
	b.swapExtent = extent

	b.swapchainCI = vk.SwapchainCreateInfo{
		Surface:         b.surface,
		MinImageCount:   caps.MinImageCount,
		ImageFormat:     b.swapFormat,
		ImageColorSpace: vk.ColorSpaceSrgbNonlinearKHR,
		ImageExtent:     extent,
		ImageUsage:      vk.ImageUsageColorAttachment,
		PreTransform:    vk.SurfaceTransformIdentityKHR,
		CompositeAlpha:  vk.CompositeAlphaOpaqueKHR,
		PresentMode:     vk.PresentModeFifoKHR, // vsync; always supported
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

	// One render-complete semaphore per swapchain image: present waits on the
	// semaphore belonging to the image it is showing, not to the frame slot.
	b.renderSems = make([]vk.Semaphore, len(b.swapImages))
	for i := range b.renderSems {
		if b.renderSems[i], err = vk.CreateSemaphore(b.device); err != nil {
			return err
		}
	}

	return b.createDepthBuffer()
}

func (b *VKBackend) createDepthBuffer() error {
	img, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType: vk.ImageType2D,
		Format:    depthFormat,
		Extent:    vk.Extent3D{Width: b.swapExtent.Width, Height: b.swapExtent.Height, Depth: 1},
		Usage:     vk.ImageUsageDepthStencilAttachment,
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
	if b.swapchain != 0 {
		vk.DestroySwapchainKHR(b.device, b.swapchain)
		b.swapchain = 0
	}
}

// recreateSwapchain rebuilds everything that depends on the window size. Called
// when acquire or present reports the surface is out of date, which is how a
// resize surfaces in Vulkan (the OpenGL backend just gets a new viewport).
func (b *VKBackend) recreateSwapchain() {
	// A minimised window has a zero-sized surface, which no swapchain accepts.
	w, h := b.window.GetSize()
	for w == 0 || h == 0 {
		glfw.WaitEvents()
		w, h = b.window.GetSize()
	}

	fatal(vk.DeviceWaitIdle(b.device), "wait idle before swapchain recreate")
	b.destroySwapchain()
	fatal(b.createSwapchain(), "recreate swapchain")
}
