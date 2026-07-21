package vulkan

import (
	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Vertex and index data live in host-visible, persistently mapped memory. At
// this engine's asset scale that is fast enough and keeps uploads to a memcpy;
// a device-local + staging-copy path is the upgrade when profiling asks for it
// (GO_BACKEND.md §6.2).
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

func (b *VKBackend) CreateBuffer(data []float32, dynamic bool) renderer.BufferHandle {
	// `dynamic` has no effect here: the allocation is host-visible either way,
	// so an update is a memcpy in both cases.
	return b.createBuffer(data, vk.BufferUsageVertexBuffer)
}

func (b *VKBackend) UpdateBuffer(h renderer.BufferHandle, data []float32) {
	e := b.buffer(h)
	if e == nil || len(data) == 0 {
		return
	}
	if uint64(len(data)*4) > e.size {
		return // a grown mesh would need a new allocation; the engine never does this
	}
	// No driver-side ghosting like glBufferData gets: the GPU may still be
	// reading this buffer, so drain the frames in flight first. Mesh vertex
	// rewrites are rare (MoveBy/MoveTo); per-frame motion belongs in the Model
	// matrix instead.
	b.waitAllFrames()
	vk.MemCopy(e.mapped, data)
}

func (b *VKBackend) DestroyBuffer(h renderer.BufferHandle) {
	e := b.buffer(h)
	if e == nil {
		return
	}
	b.waitAllFrames()
	b.allocator.VmaDestroyBuffer(e.buffer, e.alloc)
	e.valid = false
}

func (b *VKBackend) buffer(h renderer.BufferHandle) *bufEntry {
	if h == 0 || int(h) >= len(b.buffers) || !b.buffers[h].valid {
		return nil
	}
	return &b.buffers[h]
}

// CreateMesh pairs a shared vertex buffer with this face group's index buffer.
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

// CreateSkyboxMesh owns its vertex buffer (36 non-indexed positions) and has no
// index buffer, which is what marks it as the skybox vertex layout at draw time.
func (b *VKBackend) CreateSkyboxMesh(verts []float32) renderer.MeshHandle {
	vbo := b.createBuffer(verts, vk.BufferUsageVertexBuffer)
	b.meshes = append(b.meshes, meshEntry{vbo: vbo, valid: true})
	return renderer.MeshHandle(len(b.meshes) - 1)
}

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

func (b *VKBackend) mesh(m renderer.MeshHandle) *meshEntry {
	if m == 0 || int(m) >= len(b.meshes) || !b.meshes[m].valid {
		return nil
	}
	return &b.meshes[m]
}
