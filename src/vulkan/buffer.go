package vulkan

import (
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// One buffer, its allocation and its persistent mapping when it has one
type bufferInfo struct {
	name   string
	vkBuffer vk.Buffer
	vmaAlloc  vk.VmaAllocation
	mappedData unsafe.Pointer
	size   uint64
	addr   uint64
	use    use
	valid  bool
}

// One drawable: a shared vertex buffer, this face group's indices, and
// everything a draw needs. Several meshes may name one vertex buffer, which is
// how a multi-material OBJ loads
type meshInfo struct {
	name        string
	vertices    renderer.BufferHandle
	vkIndexBuffer vk.Buffer
	vmaIndexAlloc  vk.VmaAllocation
	count       uint32
	indexed     bool
	valid       bool
}

// Creates a buffer and returns its device address
func (backend *VKBackend) CreateBuffer(bufferSpec renderer.BufferSpec) (renderer.BufferHandle, renderer.Address) { 
	// Determine the buffer size: use spec.Size unless data is larger
	size := bufferSpec.Size
	var initialData unsafe.Pointer
	var initialDataLen uint64
	if bufferSpec.InitialData != nil {
		initialData, initialDataLen = getDataPointer(bufferSpec.InitialData)
		if initialDataLen > size {
			size = initialDataLen
		}
	}
	if size == 0 {
		size = 4 // a zero-sized buffer is not allowed
	}
	
	// Build Vulkan usage flags and always request ShaderDeviceAddress so the CPU can read the buffer address directly
	usage := toVkBufferUsageFlags(bufferSpec.Usage) | vk.BufferUsageShaderDeviceAddress
	vmaAllocCI := vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto}

	// Choose allocation flags based on the desired device location and usage
	// If it needs to be accessed by the CPU: can be RAM or VRAM depending on device
	if bufferSpec.Location == renderer.LocationHost {
		// Make allocation CPU readable and persistently mapped
		vmaAllocCI.Flags = vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped
		// If it is intended to be a copy destination: use a cached random-access allocation and keep it persistently mapped
		if bufferSpec.Usage&renderer.BufferCopyDst != 0 {
			vmaAllocCI.Flags = vk.VmaAllocationCreateHostAccessRandom | vk.VmaAllocationCreateMapped
		}
	} else { 
		// If it lives fully in VRAM and does not need to be accessed by CPU
		// Mark it as a TransferDst so it can receive data from a staging buffer (temporary storage to transfer data from CPU to GPU)
		usage |= vk.BufferUsageTransferDst
	}

	// Allocate the buffer and its memory
	vkBuffer, vmaAlloc, vmaAllocInfo, err := backend.vmaAllocator.VmaCreateBuffer(vk.BufferCreateInfo{Size: size, Usage: usage}, vmaAllocCI)
	fatalVk(err, "create buffer "+bufferSpec.Name)

	// If the buffer is host‑mapped, copy initial data directly
	if initialData != nil && vmaAllocInfo.MappedData != nil {
		n := func(a, b uint64) uint64 { if a < b { return a }; return b }(size, initialDataLen)
		memoryCopy(vmaAllocInfo.MappedData, initialData, n)
	}

	// Store the buffer in the backend’s list and record its address
	bufferInfo := &bufferInfo{
		name: bufferSpec.Name, vkBuffer: vkBuffer, vmaAlloc: vmaAlloc, mappedData: vmaAllocInfo.MappedData,
		size: size, addr: vk.GetBufferDeviceAddress(backend.vkDevice, vkBuffer), valid: true,
	}
	backend.buffers = append(backend.buffers, bufferInfo)

	// Create a handle for the caller and if the buffer is device‑local,
	// perform an initial staged update to fill it with data
	handle := renderer.BufferHandle(len(backend.buffers) - 1)
	if initialData != nil && vmaAllocInfo.MappedData == nil {
		backend.UpdateBuffer(handle, 0, bufferSpec.InitialData)
	}
	return handle, renderer.Address(bufferInfo.addr)
}

// Rewrites part of a buffer
func (backend *VKBackend) UpdateBuffer(bufferHandle renderer.BufferHandle, offset uint64, data any) {
	bufferInfo := backend.buffer(bufferHandle)
	if bufferInfo == nil || data == nil {
		return
	}
	src, n := getDataPointer(data)
	if n == 0 || offset+n > bufferInfo.size {
		return
	}
	if bufferInfo.mappedData != nil {
		// Wait until all in‑flight frames finish using this buffer
		backend.waitAllFrames()
		// Write directly into the mapped region at the given offset
		memoryCopy(unsafe.Add(bufferInfo.mappedData, offset), src, n)
		return
	}
	
	// ----- device‑only path -------------------------------------------
    // Create a temporary staging buffer (CPU‑visible) to hold the new data
	staging, alloc, info, err := backend.vmaAllocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: n, Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatalVk(err, "create buffer staging")
	
	// Copy the new data into the staging buffer
	memoryCopy(info.MappedData, src, n)
	backend.immediateSubmit(func(commandBuffer vk.CommandBuffer) {
		vk.CmdCopyBuffer(commandBuffer, staging, bufferInfo.vkBuffer, []vk.BufferCopy{{DstOffset: offset, Size: n}})
	})

	// Destroy the staging buffer now that the copy has been issued
	backend.vmaAllocator.VmaDestroyBuffer(staging, alloc)
}

// Copies a buffer back to the CPU
//
// Stalls on the frames in flight before mapping. Right for a screenshot or an
// image test, wrong inside a frame loop — that is the intended trade
func (backend *VKBackend) ReadBuffer(handle renderer.BufferHandle) []byte { // TODO: review
	entry := backend.buffer(handle)
	if entry == nil {
		return nil
	}
	backend.waitAllFrames()
	_ = vk.DeviceWaitIdle(backend.vkDevice)

	if entry.mappedData != nil {
		out := make([]byte, entry.size)
		memoryCopy(unsafe.Pointer(&out[0]), entry.mappedData, entry.size)
		return out
	}

	staging, alloc, info, err := backend.vmaAllocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: entry.size, Usage: vk.BufferUsageTransferDst},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessRandom | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatalVk(err, "create readback staging")
	backend.immediateSubmit(func(commandBuffer vk.CommandBuffer) {
		vk.CmdCopyBuffer(commandBuffer, entry.vkBuffer, staging, []vk.BufferCopy{{Size: entry.size}})
	})
	out := make([]byte, entry.size)
	memoryCopy(unsafe.Pointer(&out[0]), info.MappedData, entry.size)
	backend.vmaAllocator.VmaDestroyBuffer(staging, alloc)
	return out
}

// Resolves a buffer handle, nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) buffer(handle renderer.BufferHandle) *bufferInfo { // TODO: review
	if handle == 0 || int(handle) >= len(backend.buffers) || !backend.buffers[handle].valid {
		return nil
	}
	return backend.buffers[handle]
}

// Pairs a shared vertex buffer with one face group's index list
func (backend *VKBackend) CreateMesh(spec renderer.MeshSpec) renderer.MeshHandle { 
	// Resolve the vertex buffer that holds all vertices shared by this mesh
	vertexBuffer := backend.buffer(spec.Vertices)

	// Determine whether the mesh uses an index buffer or is a vertex‑counted draw
	indexed := len(spec.Indices) > 0
	count := uint32(len(spec.Indices))
	if !indexed { // no explicit indices: vertices are read 3 by 3 to be drawn as triangles
		count = uint32(spec.Count) // fallback to a draw count
		if count == 0 && vertexBuffer != nil && spec.Stride > 0 {
			// If the vertex buffer has a stride (bytes per vertex),
            // the count can be inferred from the buffer size
			count = uint32(vertexBuffer.size) / uint32(spec.Stride)
		}
	}
	
	// Allocate a small, host‑mapped index buffer of the right size
	size := uint64(len(spec.Indices) * 4) // 4 bytes per uint32 index
	if size == 0 {
		size = 4 // never allow a zero-size buffer, Vulkan forbids it
	}
	vkBuffer, vmaAlloc, vmaAllocInfo, err := backend.vmaAllocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: vk.BufferUsageIndexBuffer},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatalVk(err, "create index buffer")

	// If indices were supplied, copy them into the newly created buffer
	if indexed {
		memoryCopy(vmaAllocInfo.MappedData, unsafe.Pointer(&spec.Indices[0]), uint64(len(spec.Indices)*4))
	}

	// Register the mesh in the backend’s list and return its handle
	backend.meshes = append(backend.meshes, &meshInfo{
		name: spec.Name, vertices: spec.Vertices, vkIndexBuffer: vkBuffer, vmaIndexAlloc: vmaAlloc,
		count: count, indexed: indexed, valid: true,
	})
	return renderer.MeshHandle(len(backend.meshes) - 1)
}

// Resolves a mesh handle, nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) mesh(handle renderer.MeshHandle) *meshInfo { // TODO: review
	if handle == 0 || int(handle) >= len(backend.meshes) || !backend.meshes[handle].valid {
		return nil
	}
	return backend.meshes[handle]
}
