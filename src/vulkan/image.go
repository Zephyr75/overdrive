package vulkan

import (
	"fmt"
	"os"
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// One image: its allocation, the view descriptors reach it through, and the use
// the barrier table transitions it out of
type image struct {
	name          string
	vkImage       vk.Image
	vmaAllocation vk.VmaAllocation
	vkView        vk.ImageView // whole image, what a descriptor points at
	vkFormat      vk.Format
	vkAspect      vk.ImageAspectFlags
	kind          renderer.ImageKind
	width, height int
	depth         int
	layerCount    uint32
	vkSamples     vk.SampleCountFlags
	usage         renderer.ImageUsage
	vkSampler     vk.Sampler
	// The backend owns the allocation; false for a swapchain image
	ownsImage bool

	// Bindless bookkeeping: which array and which index, -1 before Slot ran
	binding int
	slot    uint32
	hot     bool
	hotSlot int

	use   use
	valid bool

	// Only on images the CPU rewrites while a frame is recording: a
	// persistently mapped stagingBuffer buffer plus the deferred copy it feeds
	stagingBuffer       vk.Buffer
	stagingAlloc  vk.VmaAllocation
	stagingMapped unsafe.Pointer
	stagingSize   uint64
	pending       bool
	pendingCopy   vk.BufferImageCopy
}

// One view: a mip, a slice and an aspect of an image, which is what an
// attachment names
type view struct {
	image  renderer.ImageHandle
	vkView vk.ImageView
	// Extent of the mip this view covers, which is a pass's render area
	width, height int
	layers        uint32
	valid         bool
}

// Creates an image and the whole-image view descriptors sample it through
func (backend *VKBackend) CreateImage(imageSpec renderer.ImageSpec) renderer.ImageHandle {
	// Ensure coherent number of layers for cubes and flat images
	layers := uint32(imageSpec.Layers)
	if imageSpec.Kind == renderer.ImageCube && layers < 6 {
		layers = 6
	}
	if layers == 0 {
		layers = 1
	}

	// Depth allows 3D textures, defaults to 1 for a single plane
	depth := imageSpec.Depth
	if depth == 0 {
		depth = 1
	}

	format := backend.toVkFormat(imageSpec.Format)
	vkAspect := vk.ImageAspectFlags(vk.ImageAspectColor)
	// If spec.Usage contains the ImageDepthAttachment type, image stores depth
	if imageSpec.Usage&renderer.ImageDepthAttachment != 0 {
		vkAspect = vk.ImageAspectDepth
	}

	vkFlags := vk.ImageCreateFlags(0)
	if imageSpec.Kind == renderer.ImageCube {
		vkFlags = vk.ImageCreateCubeCompatible
	}
	vkImageType := vk.ImageType2D
	if imageSpec.Kind == renderer.Image3D {
		vkImageType = vk.ImageType3D
	}

	vkImageCI, vkAlloc, err := backend.vmaAllocator.VmaCreateImage(vk.ImageCreateInfo{
		Flags:       vkFlags,
		ImageType:   vkImageType,
		Format:      format,
		Extent:      vk.Extent3D{Width: uint32(imageSpec.Width), Height: uint32(imageSpec.Height), Depth: uint32(depth)},
		ArrayLayers: layers,
		Usage:       toVkImageUsageFlags(imageSpec.Usage),
		Samples:     toVkSampleCountFlags(imageSpec.Samples),
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatalVk(err, "create image "+imageSpec.Name)

	info := &image{
		name: imageSpec.Name, vkImage: vkImageCI, vmaAllocation: vkAlloc, vkFormat: format, vkAspect: vkAspect,
		kind: imageSpec.Kind, width: imageSpec.Width, height: imageSpec.Height, depth: depth,
		layerCount: layers, vkSamples: toVkSampleCountFlags(imageSpec.Samples),
		usage: imageSpec.Usage, ownsImage: true, binding: -1,
		hot: imageSpec.Hot, hotSlot: imageSpec.HotSlot, use: useNone, valid: true,
	}
	info.vkSampler = backend.samplerOf(imageSpec.Sampler)
	info.vkView = backend.makeView(info, renderer.ViewSpec{Kind: imageSpec.Kind, Aspect: aspectOf(vkAspect)})

	backend.images = append(backend.images, info)
	handle := renderer.ImageHandle(len(backend.images) - 1)
	return handle
}

// Creates a view over one slice and one aspect
func (backend *VKBackend) CreateView(imageHandle renderer.ImageHandle, spec renderer.ViewSpec) renderer.ViewHandle { // TODO: review
	info := backend.image(imageHandle)
	if info == nil {
		return renderer.NoView
	}
	layers := uint32(spec.LayerCount)
	if layers == 0 {
		layers = info.layerCount - uint32(spec.BaseLayer)
	}
	backend.views = append(backend.views, &view{
		image: imageHandle, vkView: backend.makeView(info, spec), width: info.width, height: info.height,
		layers: layers, valid: true,
	})
	return renderer.ViewHandle(len(backend.views) - 1 + firstUserView)
}

// Builds the Vulkan view a ViewSpec describes, without registering it
func (backend *VKBackend) makeView(imageInfo *image, spec renderer.ViewSpec) vk.ImageView {
	// LayerCount 0 means every layer from BaseLayer to the end
	layers := uint32(spec.LayerCount)
	if layers == 0 {
		layers = imageInfo.layerCount - uint32(spec.BaseLayer)
	}
	// A depth-stencil image is viewed depth-only when the spec asks, since a sampled view may carry one aspect
	vkAspect := imageInfo.vkAspect
	if spec.Aspect == renderer.AspectDepth {
		vkAspect = vk.ImageAspectDepth
	}
	// Images are single-mip, so the range is always level 0 alone
	vkView, err := vk.CreateImageView(backend.vkDevice, vk.ImageViewCreateInfo{
		Image: imageInfo.vkImage, ViewType: toVkViewType(spec.Kind, layers), Format: imageInfo.vkFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vkAspect, BaseMipLevel: 0, LevelCount: 1,
			BaseArrayLayer: uint32(spec.BaseLayer), LayerCount: layers,
		},
	})
	fatalVk(err, "create image view "+imageInfo.name)
	return vkView
}

// Uploads CPU pixels into an image, whole or a region of it
func (backend *VKBackend) UpdateImage(handle renderer.ImageHandle, data renderer.ImageData) { 
	// A dead handle or an empty slice means no operation
	image := backend.image(handle)
	if image == nil || len(data.Pixels) == 0 {
		return
	}

	// Set dimensions if not defined
	width, height := data.Width, data.Height
	if width == 0 {
		width = image.width
	}
	if height == 0 {
		height = image.height
	}

	// Set layerCount if not defined
	layerCount := uint32(data.LayerCount)
	if layerCount == 0 {
		layerCount = image.layerCount
	}

	// Define copy region
	vkCopyRegion := vk.BufferImageCopy{
		AspectMask:     image.vkAspect,
		BaseArrayLayer: uint32(data.BaseLayer), LayerCount: layerCount,
		ImageOffset: vk.Offset2D{X: int32(data.X), Y: int32(data.Y)},
		ImageExtent: vk.Extent3D{Width: uint32(width), Height: uint32(height), Depth: 1},
	}

	// If no frame is being recorded, the copy can be done immediately
	if !backend.recording {
		backend.uploadNow(image, data.Pixels, vkCopyRegion)
		return
	}

	// If we have no staging buffer or it's the wrong size, we need to allocate a new one
	size := uint64(len(data.Pixels))
	if image.stagingBuffer == 0 || image.stagingSize != size {
		// If there is one (that is the wrong size), retire it so the GPU can finish using it
		if image.stagingBuffer != 0 {
			backend.retire(retired{frame: backend.frameCounter, vkStagingBuffer: image.stagingBuffer, vmaStagingAlloc: image.stagingAlloc})
		}
		// Allocate a new correctly sized staging buffer
		vkBuffer, vmaAlloc, vmaAllocInfo, err := backend.vmaAllocator.VmaCreateBuffer(
			vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageTransferSrc},
			vk.VmaAllocationCreateInfo{
				Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
				Usage: vk.VmaMemoryUsageAuto,
			})
		fatalVk(err, "create image staging buffer")
		image.stagingBuffer, image.stagingAlloc, image.stagingMapped, image.stagingSize = vkBuffer, vmaAlloc, vmaAllocInfo.MappedData, size
	}
	
	// Write pixels to the staging buffer
	memoryCopy(image.stagingMapped, unsafe.Pointer(&data.Pixels[0]), size)

	// Record the copy to be performed later
	image.pendingCopy = vkCopyRegion
	if !image.pending {
		image.pending = true
		backend.pendingUploads = append(backend.pendingUploads, handle)
	}
}

// Stages pixels and submits the copy immediately, the load-time path
func (backend *VKBackend) uploadNow(info *image, pixels []byte, copyRegion vk.BufferImageCopy) { 
	vkStagingBuffer, vmaAlloc, vmaAllocInfo, err := backend.vmaAllocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: uint64(len(pixels)), Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatalVk(err, "create image staging buffer")
	memoryCopy(vmaAllocInfo.MappedData, unsafe.Pointer(&pixels[0]), uint64(len(pixels)))

	backend.immediateSubmit(func(commandBuffer vk.CommandBuffer) {
		backend.recordImageCopy(commandBuffer, info, vkStagingBuffer, copyRegion)
	})
	backend.vmaAllocator.VmaDestroyBuffer(vkStagingBuffer, vmaAlloc)
}

// Records one staged copy into an image, between the two transitions it needs
func (backend *VKBackend) recordImageCopy(commandBuffer vk.CommandBuffer, image *image, stagingBuffer vk.Buffer, copyRegion vk.BufferImageCopy) { 
	// Record barrier to move the image to a state it can be copied to
	backend.useImage(commandBuffer, image, useCopyDst)
	// The copy itself, naming the layout the barrier above just put the image in
	vk.CmdCopyBufferToImage(commandBuffer, stagingBuffer, image.vkImage, vk.ImageLayoutTransferDstOptimal, []vk.BufferImageCopy{copyRegion})
	// Record barrier to move image back to a layout a sampler can read
	backend.useImage(commandBuffer, image, useSampled)
}

// Records the copies staged during the previous frame, from the top of this one
func (backend *VKBackend) flushPendingUploads(commandBuffer vk.CommandBuffer) { // TODO: review
	for _, handle := range backend.pendingUploads {
		info := backend.image(handle)
		if info == nil {
			continue
		}
		backend.recordImageCopy(commandBuffer, info, info.stagingBuffer, info.pendingCopy)
		info.pending = false
	}
	backend.pendingUploads = backend.pendingUploads[:0]
}

// Resolves an image handle, nil for out-of-range or destroyed entries
func (backend *VKBackend) image(handle renderer.ImageHandle) *image {
	if handle == renderer.BackbufferImage {
		if len(backend.swapchainImages) == 0 {
			return nil
		}
		return &backend.swapchainImages[backend.imageIndex]
	}
	if int(handle) >= len(backend.images) || !backend.images[handle].valid {
		return nil
	}
	return backend.images[handle]
}

// Resolves a view handle to its view and the image behind it
//
// The one reserved view is the backend's own: Backbuffer is this frame's
// swapchain image, which a multisampled pass names as its resolve target
func (backend *VKBackend) view(handle renderer.ViewHandle) (vk.ImageView, *image, int, int, uint32) { // TODO: review
	switch handle {
	case renderer.NoView:
		return 0, nil, 0, 0, 0
	case renderer.Backbuffer:
		info := &backend.swapchainImages[backend.imageIndex]
		return info.vkView, info, int(backend.vkSwapExtent.Width), int(backend.vkSwapExtent.Height), 1
	}
	i := int(handle) - firstUserView
	if i < 0 || i >= len(backend.views) || !backend.views[i].valid {
		fmt.Fprintf(os.Stderr, "vulkan: view %d is not live\n", handle)
		return 0, nil, 0, 0, 0
	}
	stored := backend.views[i]
	return stored.vkView, backend.image(stored.image), stored.width, stored.height, stored.layers
}

// Destroys an image's view and allocation once the frames in flight have retired
func (backend *VKBackend) destroyImage(handle renderer.ImageHandle) { // TODO: review
	// The backbuffer is the swapchain's, not the caller's
	if handle == renderer.BackbufferImage {
		return
	}
	info := backend.image(handle)
	if info == nil {
		return
	}
	backend.retire(retired{
		frame: backend.frameCounter, vkView: info.vkView,
		vkImage: info.vkImage, vmaAlloc: info.vmaAllocation, owns: info.ownsImage,
		vkStagingBuffer: info.stagingBuffer, vmaStagingAlloc: info.stagingAlloc,
		binding: info.binding, slot: info.slot,
	})
	info.valid = false
}
