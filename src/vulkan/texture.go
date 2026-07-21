package vulkan

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// --- loading -----------------------------------------------------------------

func (b *VKBackend) LoadTexture(path string) (renderer.TextureHandle, error) {
	rgba, err := loadRGBA(path)
	if err != nil {
		return 0, err
	}
	size := rgba.Rect.Size()
	return b.uploadTexture(rgba.Pix, size.X, size.Y, 1, false, b.samplerRepeat), nil
}

func (b *VKBackend) LoadCubemap(faces [6]string) (renderer.TextureHandle, error) {
	// All six faces go into one 6-layer image, so they are staged as one
	// contiguous block and copied in a single command.
	var pixels []byte
	var w, h int
	for i, path := range faces {
		rgba, err := loadRGBA(path)
		if err != nil {
			return 0, fmt.Errorf("cubemap face %s: %w", path, err)
		}
		size := rgba.Rect.Size()
		if i == 0 {
			w, h = size.X, size.Y
		} else if size.X != w || size.Y != h {
			return 0, fmt.Errorf("cubemap face %s: %dx%d, expected %dx%d", path, size.X, size.Y, w, h)
		}
		pixels = append(pixels, rgba.Pix...)
	}
	return b.uploadTexture(pixels, w, h, 6, true, b.samplerCubeLinear), nil
}

func (b *VKBackend) WhiteTexture() renderer.TextureHandle { return 0 }

func loadRGBA(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)
	return rgba, nil
}

// uploadTexture creates a sampled image, fills it through a staging buffer, and
// registers it in the bindless array of its kind.
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

// recordImageUpload records the two layout transitions around a full-image
// buffer copy. The old layout is always Undefined: every caller overwrites the
// whole image, so discarding the previous contents is free and correct.
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

// registerTexture records the image in the handle table and writes its
// descriptor into the bindless array, so shaders can reach it by slot index.
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

// writeDedicatedTexture writes one image into a non-bindless binding (the
// shadow maps, bindings 2 and 3). Binding 3 is an array, one cube per
// point-shadow caster, selected by arrayElement.
func (b *VKBackend) writeDedicatedTexture(binding, arrayElement uint32, view vk.ImageView, sampler vk.Sampler) {
	vk.UpdateDescriptorSets(b.device, []vk.WriteDescriptorSet{{
		DstSet: b.descriptorSet, DstBinding: binding, DstArrayElement: arrayElement,
		DescriptorType: vk.DescriptorTypeCombinedImageSampler,
		ImageInfo: []vk.DescriptorImageInfo{{
			Sampler: sampler, ImageView: view, ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
		}},
	}})
}

// slot2D / slotCube translate an engine texture handle into the bindless array
// index the shader indexes with. An unset or mismatched handle falls back to
// slot 0, which is the white pixel (2D) or the black dummy (cube).
func (b *VKBackend) slot2D(h renderer.TextureHandle) int32 {
	if int(h) < len(b.textures) && b.textures[h].valid && !b.textures[h].cube {
		return int32(b.textures[h].slot)
	}
	return 0
}

func (b *VKBackend) slotCube(h renderer.TextureHandle) int32 {
	if int(h) < len(b.textures) && b.textures[h].valid && b.textures[h].cube {
		return int32(b.textures[h].slot)
	}
	return 0
}

// --- the UI overlay texture --------------------------------------------------

// UpdateTexture2D (re)uploads the UI's CPU-rasterised pixels.
//
// The engine calls this from inside the main pass, where a copy cannot be
// recorded, so the pixels are staged here and the copy is recorded at the top
// of the next BeginFrame instead. That costs the overlay one frame of latency
// and avoids stalling the queue every frame, which an immediate submit here
// would do.
func (b *VKBackend) UpdateTexture2D(h renderer.TextureHandle, w, hgt int, pixels []byte) renderer.TextureHandle {
	needed := uint64(len(pixels))

	// Handle 0 means "allocate one" here, matching the OpenGL backend's
	// contract. It must not be looked up: handle 0 is the built-in white pixel.
	var e *texEntry
	if h != 0 {
		e = b.texture(h)
	}

	// First call, or the widget canvas resized: build a new image and staging
	// pair. The old one is retired rather than destroyed — this runs inside the
	// main pass, and the command buffer being recorded already references it
	// (BeginFrame flushed a copy into it).
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

	// Persistently mapped: the per-frame update is then a plain memcpy.
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

// flushPendingUploads records the staged UI copies into this frame's command
// buffer. Called from BeginFrame, before any pass has begun.
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

// retire queues a texture's GPU objects for destruction once every frame that
// could reference them has completed. Vulkan has no driver-side refcounting, so
// replacing a resource mid-frame needs this: destroying it immediately would
// invalidate the command buffer currently being recorded.
func (b *VKBackend) retire(e *texEntry) {
	b.retired = append(b.retired, retiredTexture{
		frame: b.frameCounter,
		view:  e.view, image: e.image, alloc: e.alloc,
		staging: e.staging, stagingAlloc: e.stagingAlloc,
	})
}

// drainRetired destroys everything retired long enough ago to be unreferenced.
// An item retired during frame F is referenced by F's command buffer at the
// latest; that buffer has certainly completed once framesInFlight further
// frames have begun, because BeginFrame waits on the fence of the slot it
// reuses.
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

func (b *VKBackend) texture(h renderer.TextureHandle) *texEntry {
	if int(h) >= len(b.textures) || !b.textures[h].valid {
		return nil
	}
	return &b.textures[h]
}

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

// --- shadow render targets ---------------------------------------------------

func (b *VKBackend) CreateShadowMap2D(w, h int) (renderer.FramebufferHandle, renderer.TextureHandle) {
	return b.createShadowTarget(w, h, false)
}

func (b *VKBackend) CreateShadowCubemap(w, h int) (renderer.FramebufferHandle, renderer.TextureHandle) {
	return b.createShadowTarget(w, h, true)
}

// createShadowTarget builds a depth image that is both rendered into and
// sampled. It needs two views of the same image: one to attach (a plain 2D
// view, or a 6-layer 2D-array view that the geometry shader routes faces into)
// and one to sample (2D, or cube).
func (b *VKBackend) createShadowTarget(w, h int, cube bool) (renderer.FramebufferHandle, renderer.TextureHandle) {
	layers := uint32(1)
	flags := vk.ImageCreateFlags(0)
	if cube {
		layers = 6
		flags = vk.ImageCreateCubeCompatible
	}

	img, alloc, err := b.allocator.VmaCreateImage(vk.ImageCreateInfo{
		Flags:       flags,
		ImageType:   vk.ImageType2D,
		Format:      depthFormat,
		Extent:      vk.Extent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		ArrayLayers: layers,
		Usage:       vk.ImageUsageDepthStencilAttachment | vk.ImageUsageSampled,
	}, vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto})
	fatal(err, "create shadow image")

	viewCI := vk.ImageViewCreateInfo{
		Image: img, ViewType: vk.ImageViewType2D, Format: depthFormat,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: vk.ImageAspectDepth, LevelCount: 1, LayerCount: layers,
		},
	}
	sampler := b.samplerShadow2D
	if cube {
		viewCI.ViewType = vk.ImageViewType2DArray
		sampler = b.samplerShadowCube
	}
	attachmentView, err := vk.CreateImageView(b.device, viewCI)
	fatal(err, "create shadow attachment view")

	if cube {
		viewCI.ViewType = vk.ImageViewTypeCube
	}
	sampleView, err := vk.CreateImageView(b.device, viewCI)
	fatal(err, "create shadow sample view")

	// ownsImage=false: the shadowEntry frees the image, not the texture entry.
	tex := b.registerTexture(cube, img, vk.VmaAllocation{}, sampleView, sampler, false)

	b.shadowTargets = append(b.shadowTargets, shadowEntry{
		cube: cube, image: img, alloc: alloc,
		attachmentView: attachmentView, tex: tex,
		layout: vk.ImageLayoutUndefined, valid: true,
	})
	return renderer.FramebufferHandle(len(b.shadowTargets) - 1), tex
}

func (b *VKBackend) DestroyFramebuffer(f renderer.FramebufferHandle) {
	if f == 0 || int(f) >= len(b.shadowTargets) || !b.shadowTargets[f].valid {
		return
	}
	b.waitAllFrames()
	e := &b.shadowTargets[f]
	vk.DestroyImageView(b.device, e.attachmentView)
	b.allocator.VmaDestroyImage(e.image, e.alloc)
	e.valid = false
}
