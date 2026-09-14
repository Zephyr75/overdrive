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
	frame           uint64
	vkView          vk.ImageView
	vkImage         vk.Image
	vmaAlloc        vk.VmaAllocation
	owns            bool
	vkStagingBuffer vk.Buffer
	vmaStagingAlloc vk.VmaAllocation
	vkBuffer        vk.Buffer
	vmaBufferAlloc  vk.VmaAllocation
	// The slot to give back, -1 when the resource never had one
	binding int
	slot    uint32
}

// Creates the descriptor set layout, pool and set, once
func (backend *VKBackend) createDescriptors() { 
	// PartiallyBound allows empty slots, UpdateAfterBind lets Slot fill one mid-frame
	const flags = vk.DescriptorBindingPartiallyBound | vk.DescriptorBindingUpdateAfterBind
	// The four arrays a shader sees, in binding order: sampled 2D, cubemaps, storage images, hot
	// Each count sizes the shader's unsized array, except bindHot's literal [4] it must match
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

	// The shape the pipeline layout and every shader agree on
	layout, err := vk.CreateDescriptorSetLayout(backend.vkDevice, vk.DescriptorSetLayoutCreateInfo{
		Flags:           vk.DescriptorSetLayoutCreateUpdateAfterBindPool,
		Bindings:        bindings,
		UseBindingFlags: true,
	})
	fatalVk(err, "create descriptor set layout")
	backend.vkSetLayout = layout

	// Pool sizes are per descriptor type, not per binding, so the three sampler arrays add into one entry
	pool, err := vk.CreateDescriptorPool(backend.vkDevice, vk.DescriptorPoolCreateInfo{
		Flags:   vk.DescriptorPoolCreateUpdateAfterBind,
		MaxSets: 1,
		PoolSizes: []vk.DescriptorPoolSize{
			{Type: vk.DescriptorTypeCombinedImageSampler, DescriptorCount: max2DTextures + maxCubeTextures + maxHotTextures},
			{Type: vk.DescriptorTypeStorageImage, DescriptorCount: maxStorageImages},
		},
	})
	fatalVk(err, "create descriptor pool")
	backend.vkDescriptorPool = pool

	// One set, bound for the engine's lifetime: nothing ever rebinds a set mid-frame
	sets, err := vk.AllocateDescriptorSets(backend.vkDevice, vk.DescriptorSetAllocateInfo{
		Pool: pool, Layouts: []vk.DescriptorSetLayout{layout},
	})
	fatalVk(err, "allocate descriptor set")
	backend.vkDescriptorSet = sets[0]
}

// The shader-visible index of an image, allocated and written on first call
func (backend *VKBackend) Slot(handle renderer.Handle) uint32 { 
	// Slots are only defined for images
	if renderer.Kind(handle) != renderer.KindImage {
		return 0
	}
	
	// The swapchain image is not a regular texture, it's a render-target that can't be bound to a descriptor set 
	if renderer.ImageHandle(renderer.Index(handle)) == renderer.BackbufferImage {
		return 0
	}

	// Get image from handle
	image := backend.image(renderer.ImageHandle(renderer.Index(handle)))
	if image == nil {
		return 0
	}
	// binding stays -1 until an image is slotted, so >=0 means it already has a slot
	if image.binding >= 0 {
		return image.slot
	}

	// Decode which descriptor array the image belongs to
	binding := bind2D
	switch {
	case image.hot:
		binding = bindHot
	case image.kind == renderer.ImageCube:
		binding = bindCube
	case image.usage&renderer.ImageSampled == 0 && image.usage&renderer.ImageStorage != 0:
		binding = bindStorage
	}

	if image.hot {
		// If image is hot, check that index is in range then use it
		if image.hotSlot < 0 || image.hotSlot >= maxHotTextures {
			panic(fmt.Sprintf("vulkan: image %q asked for hot slot %d of %d", image.name, image.hotSlot, maxHotTextures))
		}
		image.binding, image.slot = bindHot, uint32(image.hotSlot)
	} else {
		// Otherwise, find a free descriptor in the proper array and get the index
		image.binding, image.slot = binding, backend.findSlot(binding)
	}

	// Write descriptor to descriptor set
	backend.writeSlot(image)
	return image.slot
}

// Gets a freed slot of a binding or gets a new unused one
func (backend *VKBackend) findSlot(binding int) uint32 { 
	// Look for a freed slot that the engine has already returned by drainRetired
	// Re-using slots keeps the descriptor array size bounded and avoids a memory leak
	freeCount := len(backend.slotFree[binding]) 
	if freeCount > 0 {
		slot := backend.slotFree[binding][freeCount-1]
		// Remove slot from the list of free slots
		backend.slotFree[binding] = backend.slotFree[binding][:freeCount-1]
		return slot
	}
	// Otherwise take a new slot, up to the capacity the set layout was created with
	slot := backend.slotNext[binding]
	if slot >= bindingCapacity[binding] {
		panic(fmt.Sprintf("vulkan: descriptor binding %d is full at %d entries", binding, slot))
	}
	backend.slotNext[binding]++
	return slot
}

// Writes an image's descriptor into the array it was given a slot in
func (backend *VKBackend) writeSlot(info *image) { 
	descriptorInfo := vk.DescriptorImageInfo{
		Sampler: info.vkSampler, ImageView: info.vkView,
		ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
	}
	kind := vk.DescriptorTypeCombinedImageSampler
	if info.binding == bindStorage {
		// A storage image is written by a compute shader so it needs a general layout and no sampler
		descriptorInfo.Sampler, descriptorInfo.ImageLayout = 0, vk.ImageLayoutGeneral
		kind = vk.DescriptorTypeStorageImage
	}

	// Push the descriptor to the single descriptor set
	vk.UpdateDescriptorSets(backend.vkDevice, []vk.WriteDescriptorSet{{
		DstSet: backend.vkDescriptorSet, DstBinding: uint32(info.binding), DstArrayElement: info.slot,
		DescriptorType: kind, ImageInfo: []vk.DescriptorImageInfo{descriptorInfo},
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
			backend.retire(retired{frame: backend.frameCounter, vkView: backend.views[i].vkView, binding: -1})
			backend.views[i].valid = false
		}
	case renderer.KindBuffer:
		if info := backend.buffer(renderer.BufferHandle(renderer.Index(handle))); info != nil {
			backend.retire(retired{frame: backend.frameCounter, vkBuffer: info.buffer, vmaBufferAlloc: info.alloc, binding: -1})
			info.valid = false
		}
	case renderer.KindMesh:
		// The vertex buffer is shared across a multi-material mesh's groups, so
		// only the index buffer belongs to this handle
		if info := backend.mesh(renderer.MeshHandle(renderer.Index(handle))); info != nil {
			backend.retire(retired{frame: backend.frameCounter, vkBuffer: info.indexBuffer, vmaBufferAlloc: info.indexAlloc, binding: -1})
			info.valid = false
		}
	case renderer.KindPipeline:
		if info := backend.pipeline(renderer.PipelineHandle(renderer.Index(handle))); info != nil {
			vk.DestroyPipeline(backend.vkDevice, info.pipeline)
			info.valid = false
		}
	}
}

// Queues GPU objects for destruction once every frame that could reference them has completed
func (backend *VKBackend) retire(resource retired) { 
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
		if resource.vkView != 0 {
			vk.DestroyImageView(backend.vkDevice, resource.vkView)
		}
		if resource.vkImage != 0 && resource.owns {
			backend.vmaAllocator.VmaDestroyImage(resource.vkImage, resource.vmaAlloc)
		}
		if resource.vkStagingBuffer != 0 {
			backend.vmaAllocator.VmaDestroyBuffer(resource.vkStagingBuffer, resource.vmaStagingAlloc)
		}
		if resource.vkBuffer != 0 {
			backend.vmaAllocator.VmaDestroyBuffer(resource.vkBuffer, resource.vmaBufferAlloc)
		}
		// Hot slots are owned by the caller's literal index, never pooled
		if resource.binding >= 0 && resource.binding != bindHot {
			backend.slotFree[resource.binding] = append(backend.slotFree[resource.binding], resource.slot)
		}
	}
	backend.retired = kept
}
