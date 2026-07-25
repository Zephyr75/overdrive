package vulkan

import (
	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Creates a host-visible, persistently mapped buffer and fills it
//
// At this engine's asset scale that is fast enough and keeps uploads to a
// memcpy. A device-local plus staging-copy path is the upgrade when profiling
// asks for it (GO_BACKEND.md §6.2).
func (b *VKBackend) createBuffer(data []float32, usage vk.BufferUsageFlags) renderer.BufferHandle {
	size := uint64(len(data) * 4)
	if size == 0 {
		size = 4 // zero-sized buffers are not allowed
	}
	buf, alloc, info, err := b.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: usage},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create buffer")
	if len(data) > 0 {
		vk.MemCopy(info.MappedData, data)
	}

	b.buffers = append(b.buffers, bufEntry{
		buffer: buf, alloc: alloc, mapped: info.MappedData, size: size, valid: true,
	})
	return renderer.BufferHandle(len(b.buffers) - 1)
}

// Creates a vertex buffer, ignoring dynamic because the allocation is host-visible either way
func (b *VKBackend) CreateBuffer(data []float32, dynamic bool) renderer.BufferHandle {
	return b.createBuffer(data, vk.BufferUsageVertexBuffer)
}

// Memcpys new contents into a buffer's mapping, after draining the frames that might still read it
func (b *VKBackend) UpdateBuffer(h renderer.BufferHandle, data []float32) {
	e := b.buffer(h)
	if e == nil || len(data) == 0 {
		return
	}
	if uint64(len(data)*4) > e.size {
		return // a grown mesh would need a new allocation, which the engine never does
	}
	// Drain the frames in flight, there being no driver-side ghosting like
	// glBufferData gets and the GPU possibly still reading this buffer. Mesh
	// vertex rewrites are rare (MoveBy/MoveTo), per-frame motion belonging in
	// the Model matrix instead
	b.waitAllFrames()
	vk.MemCopy(e.mapped, data)
}

// Destroys a buffer once the frames in flight have drained
func (b *VKBackend) DestroyBuffer(h renderer.BufferHandle) {
	e := b.buffer(h)
	if e == nil {
		return
	}
	b.waitAllFrames()
	b.allocator.VmaDestroyBuffer(e.buffer, e.alloc)
	e.valid = false
}

// Resolves a buffer handle, returning nil for 0, out-of-range or destroyed entries
func (b *VKBackend) buffer(h renderer.BufferHandle) *bufEntry {
	if h == 0 || int(h) >= len(b.buffers) || !b.buffers[h].valid {
		return nil
	}
	return &b.buffers[h]
}

// Pairs a shared vertex buffer with this face group's index buffer
//
// There is no VAO equivalent in Vulkan — the vertex layout is baked into the
// pipeline instead — so a mesh is just that pair, bound per draw.
func (b *VKBackend) CreateMesh(vertexBuf renderer.BufferHandle, indices []uint32) renderer.MeshHandle {
	size := uint64(len(indices) * 4)
	if size == 0 {
		size = 4
	}
	buf, alloc, info, err := b.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageIndexBuffer},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create index buffer")
	if len(indices) > 0 {
		vk.MemCopy(info.MappedData, indices)
	}

	b.meshes = append(b.meshes, meshEntry{
		vbo: vertexBuf, indexBuffer: buf, indexAlloc: alloc, valid: true,
	})
	return renderer.MeshHandle(len(b.meshes) - 1)
}

// Creates the skybox mesh, which owns its 36 non-indexed positions and has no index buffer
func (b *VKBackend) CreateSkyboxMesh(verts []float32) renderer.MeshHandle {
	vbo := b.createBuffer(verts, vk.BufferUsageVertexBuffer)
	b.meshes = append(b.meshes, meshEntry{vbo: vbo, valid: true})
	return renderer.MeshHandle(len(b.meshes) - 1)
}

// Destroys a mesh's index buffer once the frames in flight have drained, leaving the shared vertex buffer alone
func (b *VKBackend) DestroyMesh(m renderer.MeshHandle) {
	e := b.mesh(m)
	if e == nil {
		return
	}
	b.waitAllFrames()
	if e.indexBuffer != 0 {
		b.allocator.VmaDestroyBuffer(e.indexBuffer, e.indexAlloc)
	}
	e.valid = false
}

// Resolves a mesh handle, returning nil for 0, out-of-range or destroyed entries
func (b *VKBackend) mesh(m renderer.MeshHandle) *meshEntry {
	if m == 0 || int(m) >= len(b.meshes) || !b.meshes[m].valid {
		return nil
	}
	return &b.meshes[m]
}
