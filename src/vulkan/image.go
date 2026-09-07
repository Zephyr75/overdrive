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
type imageInfo struct {
	name          string
	image         vk.Image
	alloc         vk.VmaAllocation
	view          vk.ImageView // whole image, what a descriptor points at
	format        vk.Format
	aspect        vk.ImageAspectFlags
	kind          renderer.ImageKind
	width, height int
	depth         int
	layers        uint32
	samples       vk.SampleCountFlags
	usage         renderer.ImageUsage
	sampler       vk.Sampler
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
	// persistently mapped staging buffer plus the deferred copy it feeds
	staging       vk.Buffer
	stagingAlloc  vk.VmaAllocation
	stagingMapped unsafe.Pointer
	stagingSize   uint64
	pending       bool
	pendingCopy   vk.BufferImageCopy
}

// One view: a mip, a slice and an aspect of an image, which is what an
// attachment names
type viewInfo struct {
	image renderer.ImageHandle
	view  vk.ImageView
	// Extent of the mip this view covers, which is a pass's render area
	width, height int
	layers        uint32
	valid         bool
}

// Creates an image and the whole-image view descriptors sample it through
func (backend *VKBackend) CreateImage(spec renderer.ImageSpec) renderer.ImageHandle { // TODO: review
	layers := uint32(spec.Layers)
	if spec.Kind == renderer.ImageCube && layers < 6 {
		layers = 6
	}
	if layers == 0 {
		layers = 1
	}
	depth := spec.Depth
	if depth == 0 {
		depth = 1
	}

	format := backend.format(spec.Format)
	aspect := vk.ImageAspectFlags(vk.ImageAspectColor)
	if spec.Usage&renderer.ImageDepthAttachment != 0 {
		aspect = vk.ImageAspectDepth
	}

	flags := vk.ImageCreateFlags(0)
	if spec.Kind == renderer.ImageCube {
		flags = vk.ImageCreateCubeCompatible
	}
	imageType := vk.ImageType2D
	if spec.Kind == renderer.Image3D {
		imageType = vk.ImageType3D
	}

	img, alloc, err := backend.allocator.VmaCreateImage(vk.ImageCreateInfo{
		Flags:       flags,
		ImageType:   imageType,
		Format:      format,
		Extent:      vk.Extent3D{Width: uint32(spec.Width), Height: uint32(spec.Height), Depth: uint32(depth)},
		ArrayLayers: layers,
		Usage:       imageUsage(spec.Usage),
		Samples:     sampleCount(spec.Samples),
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create image "+spec.Name)

	info := &imageInfo{
		name: spec.Name, image: img, alloc: alloc, format: format, aspect: aspect,
		kind: spec.Kind, width: spec.Width, height: spec.Height, depth: depth,
		layers: layers, samples: sampleCount(spec.Samples),
		usage: spec.Usage, ownsImage: true, binding: -1,
		hot: spec.Hot, hotSlot: spec.HotSlot, use: useNone, valid: true,
	}
	info.sampler = backend.samplerOf(spec.Sampler)
	info.view = backend.makeView(info, renderer.ViewSpec{Kind: spec.Kind, Aspect: aspectOf(aspect)})

	backend.images = append(backend.images, info)
	handle := renderer.ImageHandle(len(backend.images) - 1)
	return handle
}

// Creates a view over one slice and one aspect
func (backend *VKBackend) CreateView(handle renderer.ImageHandle, spec renderer.ViewSpec) renderer.ViewHandle { // TODO: review
	info := backend.image(handle)
	if info == nil {
		return renderer.NoView
	}
	layers := uint32(spec.LayerCount)
	if layers == 0 {
		layers = info.layers - uint32(spec.BaseLayer)
	}
	backend.views = append(backend.views, &viewInfo{
		image: handle, view: backend.makeView(info, spec), width: info.width, height: info.height,
		layers: layers, valid: true,
	})
	return renderer.ViewHandle(len(backend.views) - 1 + firstUserView)
}

// Builds the Vulkan view a ViewSpec describes, without registering it
func (backend *VKBackend) makeView(info *imageInfo, spec renderer.ViewSpec) vk.ImageView { // TODO: review
	layers := uint32(spec.LayerCount)
	if layers == 0 {
		layers = info.layers - uint32(spec.BaseLayer)
	}
	aspect := info.aspect
	if spec.Aspect == renderer.AspectDepth {
		aspect = vk.ImageAspectDepth
	}
	imageView, err := vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: info.image, ViewType: viewType(spec.Kind, layers), Format: info.format,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: aspect, BaseMipLevel: 0, LevelCount: 1,
			BaseArrayLayer: uint32(spec.BaseLayer), LayerCount: layers,
		},
	})
	fatal(err, "create image view "+info.name)
	return imageView
}

// Uploads CPU pixels into an image, whole or a region
//
// Outside a frame the copy is submitted and waited on. Inside one it is staged
// and recorded at the start of the next frame, a copy being illegal inside a
// render pass and the UI overlay's update happening in the middle of one.
func (backend *VKBackend) UpdateImage(handle renderer.ImageHandle, data renderer.ImageData) { // TODO: review
	info := backend.image(handle)
	if info == nil || len(data.Pixels) == 0 {
		return
	}
	width, height := data.Width, data.Height
	if width == 0 {
		width = info.width
	}
	if height == 0 {
		height = info.height
	}
	layers := uint32(data.LayerCount)
	if layers == 0 {
		layers = info.layers
	}
	region := vk.BufferImageCopy{
		AspectMask:     info.aspect,
		BaseArrayLayer: uint32(data.BaseLayer), LayerCount: layers,
		ImageOffset: vk.Offset2D{X: int32(data.X), Y: int32(data.Y)},
		ImageExtent: vk.Extent3D{Width: uint32(width), Height: uint32(height), Depth: 1},
	}

	if !backend.recording {
		backend.uploadNow(info, data.Pixels, region)
		return
	}

	// Keep a persistently mapped staging buffer on the info, so the per-frame
	// update is a memcpy and the copy costs one frame of latency
	size := uint64(len(data.Pixels))
	if info.staging == 0 || info.stagingSize != size {
		if info.staging != 0 {
			backend.retire(retired{frame: backend.frameCounter, staging: info.staging, stagingAlloc: info.stagingAlloc})
		}
		buf, alloc, allocInfo, err := backend.allocator.VmaCreateBuffer(
			vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageTransferSrc},
			vk.VmaAllocationCreateInfo{
				Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
				Usage: vk.VmaMemoryUsageAuto,
			})
		fatal(err, "create image staging buffer")
		info.staging, info.stagingAlloc, info.stagingMapped, info.stagingSize = buf, alloc, allocInfo.MappedData, size
	}
	memcpy(info.stagingMapped, unsafe.Pointer(&data.Pixels[0]), size)
	info.pendingCopy = region
	if !info.pending {
		info.pending = true
		backend.pendingUploads = append(backend.pendingUploads, handle)
	}
}

// Stages pixels and submits the copy immediately, the load-time path
func (backend *VKBackend) uploadNow(info *imageInfo, pixels []byte, region vk.BufferImageCopy) { // TODO: review
	staging, alloc, createInfo, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: uint64(len(pixels)), Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create image staging buffer")
	memcpy(createInfo.MappedData, unsafe.Pointer(&pixels[0]), uint64(len(pixels)))

	backend.immediateSubmit(func(commandBuffer vk.CommandBuffer) {
		backend.recordImageCopy(commandBuffer, info, staging, region)
	})
	backend.allocator.VmaDestroyBuffer(staging, alloc)
}

// Records one staged copy into an image, between the two transitions it needs
func (backend *VKBackend) recordImageCopy(commandBuffer vk.CommandBuffer, info *imageInfo, staging vk.Buffer, region vk.BufferImageCopy) { // TODO: review
	backend.useImage(commandBuffer, info, useCopyDst)
	vk.CmdCopyBufferToImage(commandBuffer, staging, info.image, vk.ImageLayoutTransferDstOptimal, []vk.BufferImageCopy{region})
	backend.useImage(commandBuffer, info, useSampled)
}

// Records the copies staged during the previous frame, from the top of this one
func (backend *VKBackend) flushPendingUploads(commandBuffer vk.CommandBuffer) { // TODO: review
	for _, handle := range backend.pendingUploads {
		info := backend.image(handle)
		if info == nil {
			continue
		}
		backend.recordImageCopy(commandBuffer, info, info.staging, info.pendingCopy)
		info.pending = false
	}
	backend.pendingUploads = backend.pendingUploads[:0]
}

// Resolves an image handle, nil for out-of-range or destroyed entries
//
// The reserved backbuffer handle names whichever swapchain image this frame
// acquired, so a copy out of the screen is an ordinary copy
func (backend *VKBackend) image(handle renderer.ImageHandle) *imageInfo { // TODO: review
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
// The two reserved views are the backend's own: Backbuffer is this frame's
// swapchain image, or the multisampled image that resolves into it, and
// BackbufferDepth the depth buffer sized to the window
func (backend *VKBackend) view(handle renderer.ViewHandle) (vk.ImageView, *imageInfo, int, int, uint32) { // TODO: review
	switch handle {
	case renderer.NoView:
		return 0, nil, 0, 0, 0
	case renderer.Backbuffer:
		if backend.msaa.image != 0 {
			return backend.msaa.view, &backend.msaa, int(backend.swapExtent.Width), int(backend.swapExtent.Height), 1
		}
		info := &backend.swapchainImages[backend.imageIndex]
		return info.view, info, int(backend.swapExtent.Width), int(backend.swapExtent.Height), 1
	case renderer.BackbufferDepth:
		return backend.depth.view, &backend.depth, int(backend.swapExtent.Width), int(backend.swapExtent.Height), 1
	}
	i := int(handle) - firstUserView
	if i < 0 || i >= len(backend.views) || !backend.views[i].valid {
		fmt.Fprintf(os.Stderr, "vulkan: view %d is not live\n", handle)
		return 0, nil, 0, 0, 0
	}
	stored := backend.views[i]
	return stored.view, backend.image(stored.image), stored.width, stored.height, stored.layers
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
		frame: backend.frameCounter, view: info.view,
		image: info.image, alloc: info.alloc, owns: info.ownsImage,
		staging: info.staging, stagingAlloc: info.stagingAlloc,
		binding: info.binding, slot: info.slot,
	})
	info.valid = false
}
