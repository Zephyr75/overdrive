package vulkan

import (
	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Creates a host-visible, persistently mapped buffer and fills it
func (backend *VKBackend) createBuffer(data []float32, usage vk.BufferUsageFlags) renderer.BufferHandle {
	size := uint64(len(data) * 4)
	if size == 0 {
		size = 4 // zero-sized buffers are not allowed
	}
	buf, alloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: usage},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create buffer")
	if len(data) > 0 {
		vk.MemCopy(info.MappedData, data)
	}

	backend.buffers = append(backend.buffers, bufEntry{
		buffer: buf, alloc: alloc, mapped: info.MappedData, size: size, valid: true,
	})
	return renderer.BufferHandle(len(backend.buffers) - 1)
}

// Creates a vertex buffer, always host-visible so an update is a memcpy
func (backend *VKBackend) CreateBuffer(data []float32) renderer.BufferHandle {
	return backend.createBuffer(data, vk.BufferUsageVertexBuffer)
}

// Memcpys new contents into a buffer's mapping, after draining the frames that might still read it
func (backend *VKBackend) UpdateBuffer(h renderer.BufferHandle, data []float32) {
	e := backend.buffer(h)
	if e == nil || len(data) == 0 {
		return
	}
	if uint64(len(data)*4) > e.size {
		return // a grown mesh would need a new allocation, which the engine never does
	}
	// No driver-side ghosting, and the GPU may still be reading. Rare by design:
	// per-frame motion belongs in the Model matrix, not a vertex rewrite
	backend.waitAllFrames()
	vk.MemCopy(e.mapped, data)
}

// Destroys a buffer once the frames in flight have drained
func (backend *VKBackend) DestroyBuffer(h renderer.BufferHandle) {
	e := backend.buffer(h)
	if e == nil {
		return
	}
	backend.waitAllFrames()
	backend.allocator.VmaDestroyBuffer(e.buffer, e.alloc)
	e.valid = false
}

// Resolves a buffer handle, returning nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) buffer(h renderer.BufferHandle) *bufEntry {
	if h == 0 || int(h) >= len(backend.buffers) || !backend.buffers[h].valid {
		return nil
	}
	return &backend.buffers[h]
}

// Pairs a shared vertex buffer with a layout and this face group's index buffer
//
// No VAO equivalent: the layout is baked into the pipeline, so the mesh carries
// it as the pipeline key.
func (backend *VKBackend) CreateMesh(vertexBuf renderer.BufferHandle, indices []uint32, layout renderer.VertexLayout) renderer.MeshHandle {
	indexed := len(indices) > 0
	count := uint32(len(indices))
	if !indexed {
		// No index list, so the draw sweeps the whole vertex buffer
		count = uint32(backend.buffer(vertexBuf).size) / uint32(layout.Floats()*4)
	}

	size := uint64(len(indices) * 4)
	if size == 0 {
		size = 4
	}
	buf, alloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageIndexBuffer},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create index buffer")
	if len(indices) > 0 {
		vk.MemCopy(info.MappedData, indices)
	}

	backend.meshes = append(backend.meshes, meshEntry{
		vbo: vertexBuf, indexBuffer: buf, indexAlloc: alloc,
		layout: layout, count: count, indexed: indexed, valid: true,
	})
	return renderer.MeshHandle(len(backend.meshes) - 1)
}

// Destroys a mesh's index buffer once the frames in flight have drained, leaving the shared vertex buffer alone
func (backend *VKBackend) DestroyMesh(m renderer.MeshHandle) {
	e := backend.mesh(m)
	if e == nil {
		return
	}
	backend.waitAllFrames()
	if e.indexBuffer != 0 {
		backend.allocator.VmaDestroyBuffer(e.indexBuffer, e.indexAlloc)
	}
	e.valid = false
}

// Resolves a mesh handle, returning nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) mesh(m renderer.MeshHandle) *meshEntry {
	if m == 0 || int(m) >= len(backend.meshes) || !backend.meshes[m].valid {
		return nil
	}
	return &backend.meshes[m]
}
