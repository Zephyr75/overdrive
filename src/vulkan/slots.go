package vulkan

import (
	"fmt"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// The one descriptor set the engine binds. Layouts, pools, sets and writes never
// leave this file: above it an image is reached by the index Slot returns.
const (
	// Bindless sampled 2D textures
	bind2D = 0
	// Bindless cubemaps
	bindCube = 1
	// Bindless storage images, what a compute pass writes into
	bindStorage = 2
	// A short array of dedicated descriptors for images tapped many times per
	// fragment. Indexed by a constant in the shader, because some drivers
	// re-fetch a dynamically indexed bindless descriptor on every tap — a
	// measured 1.7x on the shadow atlases, which is why this exists
	bindHot      = 3
	bindingKinds = 4
)

const (
	max2DTextures    = 256
	maxCubeTextures  = 64
	maxStorageImages = 64
	maxHotTextures   = 4
)

// How many descriptors each binding holds
var bindingCapacity = [bindingKinds]uint32{
	bind2D: max2DTextures, bindCube: maxCubeTextures,
	bindStorage: maxStorageImages, bindHot: maxHotTextures,
}

// A resource whose GPU objects are waiting for the frames that could reference
// them to complete
//
// Destroying immediately would invalidate the command buffer being recorded, and
// reusing a bindless slot too early is silent wrong pixels rather than an error
type retired struct {
	frame        uint64
	view         vk.ImageView
	image        vk.Image
	alloc        vk.VmaAllocation
	owns         bool
	staging      vk.Buffer
	stagingAlloc vk.VmaAllocation
	buffer       vk.Buffer
	bufferAlloc  vk.VmaAllocation
	// The slot to give back, -1 when the resource never had one
	binding int
	slot    uint32
}

// Creates the descriptor set layout, pool and set, once
func (backend *VKBackend) createDescriptors() { // TODO: review
	const flags = vk.DescriptorBindingPartiallyBound | vk.DescriptorBindingUpdateAfterBind
	bindings := []vk.DescriptorSetLayoutBinding{
		{Binding: bind2D, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: max2DTextures, StageFlags: descriptorStages, BindingFlags: flags},
		{Binding: bindCube, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: maxCubeTextures, StageFlags: descriptorStages, BindingFlags: flags},
		{Binding: bindStorage, DescriptorType: vk.DescriptorTypeStorageImage,
			DescriptorCount: maxStorageImages, StageFlags: descriptorStages, BindingFlags: flags},
		{Binding: bindHot, DescriptorType: vk.DescriptorTypeCombinedImageSampler,
			DescriptorCount: maxHotTextures, StageFlags: descriptorStages, BindingFlags: flags},
	}

	layout, err := vk.CreateDescriptorSetLayout(backend.device, vk.DescriptorSetLayoutCreateInfo{
		Flags:           vk.DescriptorSetLayoutCreateUpdateAfterBindPool,
		Bindings:        bindings,
		UseBindingFlags: true,
	})
	fatal(err, "create descriptor set layout")
	backend.setLayout = layout

	pool, err := vk.CreateDescriptorPool(backend.device, vk.DescriptorPoolCreateInfo{
		Flags:   vk.DescriptorPoolCreateUpdateAfterBind,
		MaxSets: 1,
		PoolSizes: []vk.DescriptorPoolSize{
			{Type: vk.DescriptorTypeCombinedImageSampler, DescriptorCount: max2DTextures + maxCubeTextures + maxHotTextures},
			{Type: vk.DescriptorTypeStorageImage, DescriptorCount: maxStorageImages},
		},
	})
	fatal(err, "create descriptor pool")
	backend.descriptorPool = pool

	sets, err := vk.AllocateDescriptorSets(backend.device, vk.DescriptorSetAllocateInfo{
		Pool: pool, Layouts: []vk.DescriptorSetLayout{layout},
	})
	fatal(err, "allocate descriptor set")
	backend.descriptorSet = sets[0]
}

// The shader-visible index of an image, allocated and written on first call
//
// The caller stores it in its own uniform block. This is the whole of the
// handle-to-shader translation: nothing else in the backend reads that block
func (backend *VKBackend) Slot(handle renderer.Handle) uint32 { // TODO: review
	if renderer.Kind(handle) != renderer.KindImage {
		return 0
	}
	if renderer.ImageHandle(renderer.Index(handle)) == renderer.BackbufferImage {
		// The swapchain image is an attachment and a copy source, never a
		// descriptor
		return 0
	}
	entry := backend.image(renderer.ImageHandle(renderer.Index(handle)))
	if entry == nil {
		return 0
	}
	if entry.binding >= 0 {
		return entry.slot
	}

	binding := bind2D
	switch {
	case entry.hot:
		binding = bindHot
	case entry.kind == renderer.ImageCube:
		binding = bindCube
	case entry.usage&renderer.ImageSampled == 0 && entry.usage&renderer.ImageStorage != 0:
		binding = bindStorage
	}

	if entry.hot {
		if entry.hotSlot < 0 || entry.hotSlot >= maxHotTextures {
			panic(fmt.Sprintf("vulkan: image %q asked for hot slot %d of %d", entry.name, entry.hotSlot, maxHotTextures))
		}
		entry.binding, entry.slot = bindHot, uint32(entry.hotSlot)
	} else {
		entry.binding, entry.slot = binding, backend.takeSlot(binding)
	}
	backend.writeSlot(entry)
	return entry.slot
}

// Pops a free slot of a binding, or bumps its high-water mark
func (backend *VKBackend) takeSlot(binding int) uint32 { // TODO: review
	if n := len(backend.slotFree[binding]); n > 0 {
		slot := backend.slotFree[binding][n-1]
		backend.slotFree[binding] = backend.slotFree[binding][:n-1]
		return slot
	}
	slot := backend.slotNext[binding]
	if slot >= bindingCapacity[binding] {
		panic(fmt.Sprintf("vulkan: descriptor binding %d is full at %d entries", binding, slot))
	}
	backend.slotNext[binding]++
	return slot
}

// Writes an image's descriptor into the array it was given a slot in
func (backend *VKBackend) writeSlot(entry *imageInfo) { // TODO: review
	info := vk.DescriptorImageInfo{
		Sampler: entry.sampler, ImageView: entry.view,
		ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
	}
	kind := vk.DescriptorTypeCombinedImageSampler
	if entry.binding == bindStorage {
		// A storage image is bound with no sampler, in the layout a compute
		// pass writes it in
		info.Sampler, info.ImageLayout = 0, vk.ImageLayoutGeneral
		kind = vk.DescriptorTypeStorageImage
	}
	vk.UpdateDescriptorSets(backend.device, []vk.WriteDescriptorSet{{
		DstSet: backend.descriptorSet, DstBinding: uint32(entry.binding), DstArrayElement: entry.slot,
		DescriptorType: kind, ImageInfo: []vk.DescriptorImageInfo{info},
	}})
}

// Destroys a resource once the frames that could reference it have retired
func (backend *VKBackend) Destroy(handle renderer.Handle) { // TODO: review
	switch renderer.Kind(handle) {
	case renderer.KindImage:
		backend.destroyImage(renderer.ImageHandle(renderer.Index(handle)))
	case renderer.KindView:
		i := int(renderer.Index(handle)) - firstUserView
		if i >= 0 && i < len(backend.views) && backend.views[i].valid {
			backend.retire(retired{frame: backend.frameCounter, view: backend.views[i].view, binding: -1})
			backend.views[i].valid = false
		}
	case renderer.KindBuffer:
		if entry := backend.buffer(renderer.BufferHandle(renderer.Index(handle))); entry != nil {
			backend.retire(retired{frame: backend.frameCounter, buffer: entry.buffer, bufferAlloc: entry.alloc, binding: -1})
			entry.valid = false
		}
	case renderer.KindMesh:
		// The vertex buffer is shared across a multi-material mesh's groups, so
		// only the index buffer belongs to this handle
		if entry := backend.mesh(renderer.MeshHandle(renderer.Index(handle))); entry != nil {
			backend.retire(retired{frame: backend.frameCounter, buffer: entry.indexBuffer, bufferAlloc: entry.indexAlloc, binding: -1})
			entry.valid = false
		}
	case renderer.KindPipeline:
		if entry := backend.pipeline(renderer.PipelineHandle(renderer.Index(handle))); entry != nil {
			vk.DestroyPipeline(backend.device, entry.pipeline)
			entry.valid = false
		}
	}
}

// Queues GPU objects for destruction once every frame that could reference them
// has completed
func (backend *VKBackend) retire(resource retired) { // TODO: review
	backend.retired = append(backend.retired, resource)
}

// Destroys everything retired long enough ago to be unreferenced, and only then
// gives its descriptor slot back
//
// An item retired in frame F is referenced by F's command buffer at the latest,
// which has certainly completed once framesInFlight further frames have begun
func (backend *VKBackend) drainRetired() { // TODO: review
	kept := backend.retired[:0]
	for _, resource := range backend.retired {
		if backend.frameCounter-resource.frame <= framesInFlight {
			kept = append(kept, resource)
			continue
		}
		if resource.view != 0 {
			vk.DestroyImageView(backend.device, resource.view)
		}
		if resource.image != 0 && resource.owns {
			backend.allocator.VmaDestroyImage(resource.image, resource.alloc)
		}
		if resource.staging != 0 {
			backend.allocator.VmaDestroyBuffer(resource.staging, resource.stagingAlloc)
		}
		if resource.buffer != 0 {
			backend.allocator.VmaDestroyBuffer(resource.buffer, resource.bufferAlloc)
		}
		if resource.binding >= 0 && resource.binding != bindHot {
			backend.slotFree[resource.binding] = append(backend.slotFree[resource.binding], resource.slot)
		}
	}
	backend.retired = kept
}
