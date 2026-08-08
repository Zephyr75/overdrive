package vulkan

import (
	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// --- uploading ---------------------------------------------------------------

// Uploads tightly packed RGBA8 pixels as a sampled 2D texture
func (b *VKBackend) CreateTexture(pixels []byte, w, h int) renderer.TextureHandle {
	return b.uploadTexture(pixels, w, h, 1, false, b.samplerRepeat)
}

// Uploads six same-sized RGBA8 faces as one 6-layer cube image, concatenated so a single copy fills it
func (b *VKBackend) CreateCubemap(faces [6][]byte, w, h int) renderer.TextureHandle {
	pixels := make([]byte, 0, len(faces[0])*6)
	for _, f := range faces {
		pixels = append(pixels, f...)
	}
	return b.uploadTexture(pixels, w, h, 6, true, b.samplerCubeLinear)
}

// Creates a sampled image, fills it through a staging buffer, and registers it in the bindless array of its kind
func (b *VKBackend) uploadTexture(pixels []byte, w, h, layers int, cube bool, sampler vk.Sampler) renderer.TextureHandle {
	flags := vk.ImageCreateFlags(0)
	if cube {
		flags = vk.ImageCreateCubeCompatible
	}
	image, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
		Flags:       flags,
		ImageType:   vk.ImageType2D,
		Format:      vk.FormatR8G8B8A8Unorm,
		Extent:      vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		ArrayLayers: uint32(layers),
		Usage:       vk.ImageUsageSampled | vk.ImageUsageTransferDst,
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create texture image")

	staging, stagingAlloc, info, err := b.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: uint64(len(pixels)), Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create texture staging buffer")
	vk.MemCopy(info.MappedData, pixels)

	b.immediateSubmit(func(cb vk.CommandBuffer) {
		b.recordImageUpload(cb, image, staging, w, h, layers)
	})
	b.allocator.VmaDestroyBuffer(staging, stagingAlloc)

	viewType := vk.ImageViewType2D
	if cube {
		viewType = vk.ImageViewTypeCube
	}
	view, err := vk.CreateImageView(b.device, vk.ImageViewCreateInfo{
		Image: image, ViewType: viewType, Format: vk.FormatR8G8B8A8Unorm,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: uint32(layers),
		},
	})
	fatal(err, "create texture view")

	return b.registerTexture(cube, image, alloc, view, sampler, true)
}

// Records a full-image buffer copy between its two layout transitions
//
// Old layout is always Undefined: every caller overwrites the whole image, so
// discarding the previous contents is free.
func (b *VKBackend) recordImageUpload(cb vk.CommandBuffer, img vk.Image, staging vk.Buffer, w, h, layers int) {
	b.imageBarrier(cb, img, vk.ImageAspectColor, uint32(layers),
		vk.ImageLayoutUndefined, vk.ImageLayoutTransferDstOptimal,
		vk.PipelineStage2None, vk.Access2None,
		vk.PipelineStage2Copy, vk.Access2TransferWrite)

	vk.CmdCopyBufferToImage(cb, staging, img, vk.ImageLayoutTransferDstOptimal,
		[]vk.BufferImageCopy{{
			AspectMask:  vk.ImageAspectColor,
			LayerCount:  uint32(layers),
			ImageExtent: vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		}})

	b.imageBarrier(cb, img, vk.ImageAspectColor, uint32(layers),
		vk.ImageLayoutTransferDstOptimal, vk.ImageLayoutShaderReadOnlyOptimal,
		vk.PipelineStage2Copy, vk.Access2TransferWrite,
		vk.PipelineStage2FragmentShader, vk.Access2ShaderSampledRead)
}

// Records the image in the handle table and writes its descriptor into the bindless array, so shaders can reach it by slot index
func (b *VKBackend) registerTexture(cube bool, img vk.Image, alloc vk.VmaAllocation,
	view vk.ImageView, sampler vk.Sampler, ownsImage bool) renderer.TextureHandle {

	e := texEntry{cube: cube, image: img, alloc: alloc, view: view, ownsImage: ownsImage, valid: true}
	binding := uint32(0)
	if cube {
		e.slot = b.nextCubeSlot
		b.nextCubeSlot++
		binding = 1
	} else {
		e.slot = b.next2DSlot
		b.next2DSlot++
	}

	vk.UpdateDescriptorSets(b.device, []vk.WriteDescriptorSet{{
		DstSet: b.descriptorSet, DstBinding: binding, DstArrayElement: e.slot,
		DescriptorType: vk.DescriptorTypeCombinedImageSampler,
		ImageInfo: []vk.DescriptorImageInfo{{
			Sampler: sampler, ImageView: view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
		}},
	}})

	b.textures = append(b.textures, e)
	return renderer.TextureHandle(len(b.textures) - 1)
}

// Writes one image into a non-bindless binding, bindings 2 and 3 being the shadow maps and 3 holding one cube per point-shadow caster
func (b *VKBackend) writeDedicatedTexture(binding, arrayElement uint32, view vk.ImageView, sampler vk.Sampler) {
	vk.UpdateDescriptorSets(b.device, []vk.WriteDescriptorSet{{
		DstSet: b.descriptorSet, DstBinding: binding, DstArrayElement: arrayElement,
		DescriptorType: vk.DescriptorTypeCombinedImageSampler,
		ImageInfo: []vk.DescriptorImageInfo{{
			Sampler: sampler, ImageView: view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
		}},
	}})
}

// Translates a texture handle into its 2D bindless slot, an unset or mismatched handle falling back to the white pixel in slot 0
func (b *VKBackend) slot2D(h renderer.TextureHandle) int32 {
	if int(h) < len(b.textures) && b.textures[h].valid && !b.textures[h].cube {
		return int32(b.textures[h].slot)
	}
	return 0
}

// Translates a texture handle into its cube bindless slot, an unset or mismatched handle falling back to the black dummy in slot 0
func (b *VKBackend) slotCube(h renderer.TextureHandle) int32 {
	if int(h) < len(b.textures) && b.textures[h].valid && b.textures[h].cube {
		return int32(b.textures[h].slot)
	}
	return 0
}

// --- the UI overlay texture --------------------------------------------------

// Stages the UI's CPU-rasterised pixels for a copy at the next BeginFrame
//
// Called from inside the main pass, where a copy cannot be recorded. Deferring
// costs one frame of latency and avoids stalling the queue every frame.
func (b *VKBackend) UpdateTexture2D(h renderer.TextureHandle, w, hgt int, pixels []byte) renderer.TextureHandle {
	needed := uint64(len(pixels))

	// Treat handle 0 as "allocate one", which is the interface's contract. It
	// must not be looked up, handle 0 being the built-in white pixel
	var e *texEntry
	if h != 0 {
		e = b.texture(h)
	}

	// First call, or the canvas resized. The old pair is retired rather than
	// destroyed: the command buffer being recorded already references it
	if e == nil || e.stagingSize != needed {
		if e != nil {
			b.retire(e)
			e.valid = false
		}
		h = b.createUpdatableTexture(w, hgt, needed)
		e = b.texture(h)
	}

	vk.MemCopy(e.stagingMapped, pixels)
	if !e.pending {
		e.pending = true
		b.pendingUploads = append(b.pendingUploads, h)
	}
	return h
}

// Creates the UI overlay's image, view and persistently mapped staging buffer
func (b *VKBackend) createUpdatableTexture(w, h int, size uint64) renderer.TextureHandle {
	img, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
		ImageType:   vk.ImageType2D,
		Format:      vk.FormatR8G8B8A8Unorm,
		Extent:      vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		ArrayLayers: 1,
		Usage:       vk.ImageUsageSampled | vk.ImageUsageTransferDst,
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create UI image")

	view, err := vk.CreateImageView(b.device, vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: vk.FormatR8G8B8A8Unorm,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectColor, LevelCount: 1, LayerCount: 1,
		},
	})
	fatal(err, "create UI image view")

	// Keep it persistently mapped, making the per-frame update a plain memcpy
	staging, stagingAlloc, info, err := b.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create UI staging buffer")

	handle := b.registerTexture(false, img, alloc, view, b.samplerRepeat, true)
	e := &b.textures[handle]
	e.staging, e.stagingAlloc, e.stagingMapped, e.stagingSize = staging, stagingAlloc, info.MappedData, size
	e.width, e.height = w, h
	return handle
}

// Records the staged UI copies into this frame's command buffer, from BeginFrame, before any pass has begun
func (b *VKBackend) flushPendingUploads(cb vk.CommandBuffer) {
	for _, h := range b.pendingUploads {
		e := b.texture(h)
		if e == nil {
			continue
		}
		b.recordImageUpload(cb, e.image, e.staging, e.width, e.height, 1)
		e.pending = false
	}
	b.pendingUploads = b.pendingUploads[:0]
}

// Queues a texture's GPU objects for destruction once every frame that could reference them has completed
//
// Destroying immediately would invalidate the command buffer being recorded.
func (b *VKBackend) retire(e *texEntry) {
	b.retired = append(b.retired, retiredTexture{
		frame: b.frameCounter,
		view:  e.view, image: e.image, alloc: e.alloc,
		staging: e.staging, stagingAlloc: e.stagingAlloc,
	})
}

// Destroys everything retired long enough ago to be unreferenced
//
// An item retired in frame F is referenced by F's command buffer at the latest,
// which has certainly completed once framesInFlight further frames have begun.
func (b *VKBackend) drainRetired() {
	kept := b.retired[:0]
	for _, r := range b.retired {
		if b.frameCounter-r.frame <= framesInFlight {
			kept = append(kept, r)
			continue
		}
		vk.DestroyImageView(b.device, r.view)
		b.allocator.VmaDestroyImage(r.image, r.alloc)
		if r.staging != 0 {
			b.allocator.VmaDestroyBuffer(r.staging, r.stagingAlloc)
		}
	}
	b.retired = kept
}

// Resolves a texture handle, returning nil for out-of-range or destroyed entries
func (b *VKBackend) texture(h renderer.TextureHandle) *texEntry {
	if int(h) >= len(b.textures) || !b.textures[h].valid {
		return nil
	}
	return &b.textures[h]
}

// Destroys a texture's view, image and staging buffer once the frames in flight have drained
func (b *VKBackend) DestroyTexture(h renderer.TextureHandle) {
	e := b.texture(h)
	if e == nil || h == 0 {
		return
	}
	b.waitAllFrames()
	vk.DestroyImageView(b.device, e.view)
	if e.ownsImage {
		b.allocator.VmaDestroyImage(e.image, e.alloc)
	}
	if e.staging != 0 {
		b.allocator.VmaDestroyBuffer(e.staging, e.stagingAlloc)
	}
	e.valid = false
}

// --- offscreen render targets ------------------------------------------------

// Builds an image that is both rendered into and sampled, plus the two views that needs
//
// One view is attached (2D, or a 6-layer array a geometry stage routes faces
// into), the other sampled (2D or cube). You cannot attach a cube view.
func (b *VKBackend) CreateRenderTarget(spec renderer.RenderTargetSpec) (renderer.RenderTargetHandle, renderer.TextureHandle) {
	layers := uint32(1)
	flags := vk.ImageCreateFlags(0)
	if spec.Cube {
		layers = 6
		flags = vk.ImageCreateCubeCompatible
	}

	format := depthFormat
	usage := vk.ImageUsageDepthStencilAttachment | vk.ImageUsageSampled
	aspect := vk.ImageAspectFlags(vk.ImageAspectDepth)
	sampler := b.samplerShadow2D
	if spec.Cube {
		sampler = b.samplerShadowCube
	}
	if spec.Format == renderer.TargetColor {
		format = offscreenColorFormat
		usage = vk.ImageUsageColorAttachment | vk.ImageUsageSampled
		aspect = vk.ImageAspectColor
		// Colour targets are read back by post-processing, which wants filtering
		sampler = b.samplerCubeLinear
	}

	img, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
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
	attachmentView, err := vk.CreateImageView(b.device, viewCI)
	fatal(err, "create render target attachment view")

	if spec.Cube {
		viewCI.ViewType = vk.ImageViewTypeCube
	}
	sampleView, err := vk.CreateImageView(b.device, viewCI)
	fatal(err, "create render target sample view")

	// Register with ownsImage=false, the targetEntry freeing the image rather
	// than the texture entry
	tex := b.registerTexture(spec.Cube, img, vk.VmaAllocation{}, sampleView, sampler, false)

	b.targets = append(b.targets, targetEntry{
		width: spec.Width, height: spec.Height,
		format: spec.Format, cube: spec.Cube, image: img, alloc: alloc,
		attachmentView: attachmentView, tex: tex,
		layout: vk.ImageLayoutUndefined, valid: true,
	})
	return renderer.RenderTargetHandle(len(b.targets) - 1), tex
}

// Destroys a target's attachment view and image once the frames in flight have drained
func (b *VKBackend) DestroyRenderTarget(f renderer.RenderTargetHandle) {
	if f == 0 || int(f) >= len(b.targets) || !b.targets[f].valid {
		return
	}
	b.waitAllFrames()
	e := &b.targets[f]
	vk.DestroyImageView(b.device, e.attachmentView)
	b.allocator.VmaDestroyImage(e.image, e.alloc)
	e.valid = false
}
