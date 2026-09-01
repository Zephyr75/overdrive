package vulkan

import (
	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// --- uploading ---------------------------------------------------------------

// Uploads tightly packed RGBA8 pixels as a sampled 2D texture
func (backend *VKBackend) CreateTexture(pixels []byte, w, h int) renderer.TextureHandle {
	return backend.uploadTexture(pixels, w, h, 1, false, backend.samplerRepeat)
}

// Uploads six same-sized RGBA8 faces as one 6-layer cube image, concatenated so a single copy fills it
func (backend *VKBackend) CreateCubemap(faces [6][]byte, w, h int) renderer.TextureHandle {
	pixels := make([]byte, 0, len(faces[0])*6)
	for _, f := range faces {
		pixels = append(pixels, f...)
	}
	return backend.uploadTexture(pixels, w, h, 6, true, backend.samplerCubeLinear)
}

// Creates a sampled image, fills it through a staging buffer, and registers it in the bindless array of its kind
func (backend *VKBackend) uploadTexture(pixels []byte, w, h, layers int, cube bool, sampler vk.Sampler) renderer.TextureHandle {
	flags := vk.ImageCreateFlags(0)
	if cube {
		flags = vk.ImageCreateCubeCompatible
	}
	image, alloc, err := backend.allocator.VmaCreateImage(vk.ImageCreateInfo{
		Flags:       flags,
		ImageType:   vk.ImageType2D,
		Format:      vk.FormatR8G8B8A8Unorm,
		Extent:      vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		ArrayLayers: uint32(layers),
		Usage:       vk.ImageUsageSampled | vk.ImageUsageTransferDst,
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create texture image")

	staging, stagingAlloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: uint64(len(pixels)), Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create texture staging buffer")
	vk.MemCopy(info.MappedData, pixels)

	backend.immediateSubmit(func(cb vk.CommandBuffer) {
		backend.recordImageUpload(cb, image, staging, w, h, layers)
	})
	backend.allocator.VmaDestroyBuffer(staging, stagingAlloc)

	viewType := vk.ImageViewType2D
	if cube {
		viewType = vk.ImageViewTypeCube
	}
	view, err := vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: image, ViewType: viewType, Format: vk.FormatR8G8B8A8Unorm,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: uint32(layers),
		},
	})
	fatal(err, "create texture view")

	return backend.registerTexture(cube, image, alloc, view, sampler, true)
}

// Records a full-image buffer copy between its two layout transitions
//
// Old layout is always Undefined: every caller overwrites the whole image, so
// discarding the previous contents is free.
func (backend *VKBackend) recordImageUpload(cb vk.CommandBuffer, img vk.Image, staging vk.Buffer, w, h, layers int) {
	backend.imageBarrier(cb, img, vk.ImageAspectColor, uint32(layers),
		vk.ImageLayoutUndefined, vk.ImageLayoutTransferDstOptimal,
		vk.PipelineStage2None, vk.Access2None,
		vk.PipelineStage2Copy, vk.Access2TransferWrite)

	vk.CmdCopyBufferToImage(cb, staging, img, vk.ImageLayoutTransferDstOptimal,
		[]vk.BufferImageCopy{{
			AspectMask:  vk.ImageAspectColor,
			LayerCount:  uint32(layers),
			ImageExtent: vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		}})

	backend.barrierToShaderRead(cb, img, vk.ImageAspectColor, uint32(layers), vk.ImageLayoutTransferDstOptimal)
}

// Records the image in the handle table and writes its descriptor into the bindless array, so shaders can reach it by slot index
func (backend *VKBackend) registerTexture(cube bool, img vk.Image, alloc vk.VmaAllocation,
	view vk.ImageView, sampler vk.Sampler, ownsImage bool) renderer.TextureHandle {

	e := texEntry{cube: cube, image: img, alloc: alloc, view: view, ownsImage: ownsImage, valid: true}
	binding := uint32(0)
	if cube {
		e.slot = backend.nextCubeSlot
		backend.nextCubeSlot++
		binding = 1
	} else {
		e.slot = backend.next2DSlot
		backend.next2DSlot++
	}

	vk.UpdateDescriptorSets(backend.device, []vk.WriteDescriptorSet{{
		DstSet: backend.descriptorSet, DstBinding: binding, DstArrayElement: e.slot,
		DescriptorType: vk.DescriptorTypeCombinedImageSampler,
		ImageInfo: []vk.DescriptorImageInfo{{
			Sampler: sampler, ImageView: view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
		}},
	}})

	backend.textures = append(backend.textures, e)
	return renderer.TextureHandle(len(backend.textures) - 1)
}

// Writes one image into a non-bindless binding, bindings 2 and 3 being the shadow maps and 3 holding one cube per point-shadow caster
func (backend *VKBackend) writeDedicatedTexture(binding, arrayElement uint32, view vk.ImageView, sampler vk.Sampler) {
	vk.UpdateDescriptorSets(backend.device, []vk.WriteDescriptorSet{{
		DstSet: backend.descriptorSet, DstBinding: binding, DstArrayElement: arrayElement,
		DescriptorType: vk.DescriptorTypeCombinedImageSampler,
		ImageInfo: []vk.DescriptorImageInfo{{
			Sampler: sampler, ImageView: view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
		}},
	}})
}

// Translates a texture handle into its 2D bindless slot, an unset or mismatched handle falling back to the white pixel in slot 0
func (backend *VKBackend) slot2D(h renderer.TextureHandle) int32 {
	if int(h) < len(backend.textures) && backend.textures[h].valid && !backend.textures[h].cube {
		return int32(backend.textures[h].slot)
	}
	return 0
}

// Translates a texture handle into its cube bindless slot, an unset or mismatched handle falling back to the black dummy in slot 0
func (backend *VKBackend) slotCube(h renderer.TextureHandle) int32 {
	if int(h) < len(backend.textures) && backend.textures[h].valid && backend.textures[h].cube {
		return int32(backend.textures[h].slot)
	}
	return 0
}

// --- the UI overlay texture --------------------------------------------------

// Stages the UI's CPU-rasterised pixels for a copy at the next BeginFrame
//
// Called from inside the main pass, where a copy cannot be recorded. Deferring
// costs one frame of latency and avoids stalling the queue every frame.
func (backend *VKBackend) UpdateTexture2D(h renderer.TextureHandle, w, hgt int, pixels []byte) renderer.TextureHandle {
	needed := uint64(len(pixels))

	// Treat handle 0 as "allocate one", which is the interface's contract. It
	// must not be looked up, handle 0 being the built-in white pixel
	var e *texEntry
	if h != 0 {
		e = backend.texture(h)
	}

	// First call, or the canvas resized. The old pair is retired rather than
	// destroyed: the command buffer being recorded already references it
	if e == nil || e.stagingSize != needed {
		if e != nil {
			backend.retire(e)
			e.valid = false
		}
		h = backend.createUpdatableTexture(w, hgt, needed)
		e = backend.texture(h)
	}

	vk.MemCopy(e.stagingMapped, pixels)
	if !e.pending {
		e.pending = true
		backend.pendingUploads = append(backend.pendingUploads, h)
	}
	return h
}

// Creates the UI overlay's image, view and persistently mapped staging buffer
func (backend *VKBackend) createUpdatableTexture(w, h int, size uint64) renderer.TextureHandle {
	img, alloc, err := backend.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType:   vk.ImageType2D,
		Format:      vk.FormatR8G8B8A8Unorm,
		Extent:      vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		ArrayLayers: 1,
		Usage:       vk.ImageUsageSampled | vk.ImageUsageTransferDst,
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create UI image")

	view, err := vk.CreateImageView(backend.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: vk.FormatR8G8B8A8Unorm,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
		},
	})
	fatal(err, "create UI image view")

	// Keep it persistently mapped, making the per-frame update a plain memcpy
	staging, stagingAlloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create UI staging buffer")

	handle := backend.registerTexture(false, img, alloc, view, backend.samplerRepeat, true)
	e := &backend.textures[handle]
	e.staging, e.stagingAlloc, e.stagingMapped, e.stagingSize = staging, stagingAlloc, info.MappedData, size
	e.width, e.height = w, h
	return handle
}

// Records the staged UI copies into this frame's command buffer, from BeginFrame, before any pass has begun
func (backend *VKBackend) flushPendingUploads(cb vk.CommandBuffer) {
	for _, h := range backend.pendingUploads {
		e := backend.texture(h)
		if e == nil {
			continue
		}
		backend.recordImageUpload(cb, e.image, e.staging, e.width, e.height, 1)
		e.pending = false
	}
	backend.pendingUploads = backend.pendingUploads[:0]
}

// Queues a texture's GPU objects for destruction once every frame that could reference them has completed
//
// Destroying immediately would invalidate the command buffer being recorded.
func (backend *VKBackend) retire(e *texEntry) {
	backend.retired = append(backend.retired, retiredTexture{
		frame: backend.frameCounter,
		view:  e.view, image: e.image, alloc: e.alloc,
		staging: e.staging, stagingAlloc: e.stagingAlloc,
	})
}

// Destroys everything retired long enough ago to be unreferenced
//
// An item retired in frame F is referenced by F's command buffer at the latest,
// which has certainly completed once framesInFlight further frames have begun.
func (backend *VKBackend) drainRetired() {
	kept := backend.retired[:0]
	for _, r := range backend.retired {
		if backend.frameCounter-r.frame <= framesInFlight {
			kept = append(kept, r)
			continue
		}
		vk.DestroyImageView(backend.device, r.view)
		backend.allocator.VmaDestroyImage(r.image, r.alloc)
		if r.staging != 0 {
			backend.allocator.VmaDestroyBuffer(r.staging, r.stagingAlloc)
		}
	}
	backend.retired = kept
}

// Resolves a texture handle, returning nil for out-of-range or destroyed entries
func (backend *VKBackend) texture(h renderer.TextureHandle) *texEntry {
	if int(h) >= len(backend.textures) || !backend.textures[h].valid {
		return nil
	}
	return &backend.textures[h]
}

// Destroys a texture's view, image and staging buffer once the frames in flight have drained
func (backend *VKBackend) DestroyTexture(h renderer.TextureHandle) {
	e := backend.texture(h)
	if e == nil || h == 0 {
		return
	}
	backend.waitAllFrames()
	vk.DestroyImageView(backend.device, e.view)
	if e.ownsImage {
		backend.allocator.VmaDestroyImage(e.image, e.alloc)
	}
	if e.staging != 0 {
		backend.allocator.VmaDestroyBuffer(e.staging, e.stagingAlloc)
	}
	e.valid = false
}

// --- offscreen render targets ------------------------------------------------

// Builds an image that is both rendered into and sampled, plus the two views that needs
//
// One view is attached (2D, or a 6-layer array a geometry stage routes faces
// into), the other sampled (2D or cube). You cannot attach a cube view.
func (backend *VKBackend) CreateRenderTarget(spec renderer.RenderTargetSpec) (renderer.RenderTargetHandle, renderer.TextureHandle) {
	layers := uint32(1)
	flags := vk.ImageCreateFlags(0)
	if spec.Cube {
		layers = 6
		flags = vk.ImageCreateCubeCompatible
	}

	format := depthFormat
	// Transfer on both ends is what CopyDepthRegion needs, one shadow atlas being
	// both the source of a cached tile and the destination of another's copy
	usage := vk.ImageUsageDepthStencilAttachment | vk.ImageUsageSampled |
		vk.ImageUsageTransferSrc | vk.ImageUsageTransferDst
	aspect := vk.ImageAspectFlags(vk.ImageAspectDepth)
	sampler := backend.samplerShadow2D
	if spec.Cube {
		sampler = backend.samplerShadowCube
	}
	if spec.Format == renderer.TargetColor {
		format = offscreenColorFormat
		usage = vk.ImageUsageColorAttachment | vk.ImageUsageSampled
		aspect = vk.ImageAspectColor
		// Colour targets are read back by post-processing, which wants filtering
		sampler = backend.samplerCubeLinear
	}

	img, alloc, err := backend.allocator.VmaCreateImage(vk.ImageCreateInfo{
		Flags:       flags,
		ImageType:   vk.ImageType2D,
		Format:      format,
		Extent:      vk.Extent3D{Width: uint32(spec.Width), Height: uint32(spec.Height), Depth: 1},
		ArrayLayers: layers,
		Usage:       usage,
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create render target image")

	viewCI := vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: format,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: aspect, LevelCount: 1, LayerCount: layers,
		},
	}
	if spec.Cube {
		viewCI.ViewType = vk.ImageViewType2DArray
	}
	attachmentView, err := vk.CreateImageView(backend.device, viewCI)
	fatal(err, "create render target attachment view")

	if spec.Cube {
		viewCI.ViewType = vk.ImageViewTypeCube
	}
	sampleView, err := vk.CreateImageView(backend.device, viewCI)
	fatal(err, "create render target sample view")

	// Register with ownsImage=false, the targetEntry freeing the image rather
	// than the texture entry
	tex := backend.registerTexture(spec.Cube, img, vk.VmaAllocation{}, sampleView, sampler, false)

	backend.targets = append(backend.targets, targetEntry{
		width: spec.Width, height: spec.Height,
		format: spec.Format, cube: spec.Cube, image: img, alloc: alloc,
		attachmentView: attachmentView, tex: tex,
		layout: vk.ImageLayoutUndefined, valid: true,
	})
	return renderer.RenderTargetHandle(len(backend.targets) - 1), tex
}

// Destroys a target's attachment view and image once the frames in flight have drained
func (backend *VKBackend) DestroyRenderTarget(f renderer.RenderTargetHandle) {
	if f == 0 || int(f) >= len(backend.targets) || !backend.targets[f].valid {
		return
	}
	backend.waitAllFrames()
	e := &backend.targets[f]
	vk.DestroyImageView(backend.device, e.attachmentView)
	backend.allocator.VmaDestroyImage(e.image, e.alloc)
	e.valid = false
}
