package vulkan

import (
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// One buffer, its allocation and its persistent mapping when it has one
type bufEntry struct {
	name   string
	buffer vk.Buffer
	alloc  vk.VmaAllocation
	mapped unsafe.Pointer
	size   uint64
	addr   uint64
	use    use
	valid  bool
}

// One drawable: a shared vertex buffer, this face group's indices, and
// everything a draw needs. Several meshes may name one vertex buffer, which is
// how a multi-material OBJ loads
type meshEntry struct {
	name        string
	vertices    renderer.BufferHandle
	indexBuffer vk.Buffer
	indexAlloc  vk.VmaAllocation
	count       uint32
	indexed     bool
	valid       bool
}

// Creates a buffer and returns its device address
//
// Every buffer is address-capable: that is how a shader reaches one here, so
// there is no reason for a second kind
func (backend *VKBackend) CreateBuffer(spec renderer.BufferInfo) (renderer.BufferHandle, renderer.Address) {
	size := spec.Size
	var src unsafe.Pointer
	var dataLen uint64
	if spec.Data != nil {
		src, dataLen = dataPtr(spec.Data)
		if dataLen > size {
			size = dataLen
		}
	}
	if size == 0 {
		size = 4 // a zero-sized buffer is not allowed
	}

	usage := bufferUsage(spec.Usage) | vk.BufferUsageShaderDeviceAddress
	aci := vk.VmaAllocationCreateInfo{Usage: vk.VmaMemoryUsageAuto}
	if spec.Location == renderer.LocationHost {
		aci.Flags = vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped
		// A buffer the CPU reads back wants cached memory, not write-combined
		if spec.Usage&renderer.BufferCopyDst != 0 {
			aci.Flags = vk.VmaAllocationCreateHostAccessRandom | vk.VmaAllocationCreateMapped
		}
	} else {
		usage |= vk.BufferUsageTransferDst
	}

	buf, alloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: size, Usage: usage}, aci)
	fatal(err, "create buffer "+spec.Name)
	if src != nil && info.MappedData != nil {
		memcpy(info.MappedData, src, minU64(size, dataLen))
	}

	entry := &bufEntry{
		name: spec.Name, buffer: buf, alloc: alloc, mapped: info.MappedData,
		size: size, addr: vk.GetBufferDeviceAddress(backend.device, buf), valid: true,
	}
	backend.buffers = append(backend.buffers, entry)

	handle := renderer.BufferHandle(len(backend.buffers) - 1)
	// A device-local buffer has no mapping, so its initial contents go through
	// the same staged path an update does
	if src != nil && info.MappedData == nil {
		backend.UpdateBuffer(handle, 0, spec.Data)
	}
	return handle, renderer.Address(entry.addr)
}

func minU64(first, second uint64) uint64 {
	if first < second {
		return first
	}
	return second
}

// Rewrites part of a buffer
//
// A host buffer is a memcpy after the frames that might read it have drained;
// a device buffer goes through a staging copy. Rare by design: per-frame motion
// belongs in a model matrix, not a vertex rewrite
func (backend *VKBackend) UpdateBuffer(bufferHandle renderer.BufferHandle, offset uint64, data any) {
	entry := backend.buffer(bufferHandle)
	if entry == nil || data == nil {
		return
	}
	src, n := dataPtr(data)
	if n == 0 || offset+n > entry.size {
		return
	}
	if entry.mapped != nil {
		// No driver-side ghosting, and the GPU may still be reading
		backend.waitAllFrames()
		memcpy(unsafe.Add(entry.mapped, offset), src, n)
		return
	}

	staging, alloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: n, Usage: vk.BufferUsageTransferSrc},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessSequentialWrite | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create buffer staging")
	memcpy(info.MappedData, src, n)
	backend.immediateSubmit(func(commandBuffer vk.CommandBuffer) {
		vk.CmdCopyBuffer(commandBuffer, staging, entry.buffer, []vk.BufferCopy{{DstOffset: offset, Size: n}})
	})
	backend.allocator.VmaDestroyBuffer(staging, alloc)
}

// Copies a buffer back to the CPU
//
// Stalls on the frames in flight before mapping. Right for a screenshot or an
// image test, wrong inside a frame loop — that is the intended trade
func (backend *VKBackend) ReadBuffer(handle renderer.BufferHandle) []byte {
	entry := backend.buffer(handle)
	if entry == nil {
		return nil
	}
	backend.waitAllFrames()
	_ = vk.DeviceWaitIdle(backend.device)

	if entry.mapped != nil {
		out := make([]byte, entry.size)
		memcpy(unsafe.Pointer(&out[0]), entry.mapped, entry.size)
		return out
	}

	staging, alloc, info, err := backend.allocator.VmaCreateBuffer(
		vk.BufferCreateInfo{Size: entry.size, Usage: vk.BufferUsageTransferDst},
		vk.VmaAllocationCreateInfo{
			Flags: vk.VmaAllocationCreateHostAccessRandom | vk.VmaAllocationCreateMapped,
			Usage: vk.VmaMemoryUsageAuto,
		})
	fatal(err, "create readback staging")
	backend.immediateSubmit(func(commandBuffer vk.CommandBuffer) {
		vk.CmdCopyBuffer(commandBuffer, entry.buffer, staging, []vk.BufferCopy{{Size: entry.size}})
	})
	out := make([]byte, entry.size)
	memcpy(unsafe.Pointer(&out[0]), info.MappedData, entry.size)
	backend.allocator.VmaDestroyBuffer(staging, alloc)
	return out
}

// Resolves a buffer handle, nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) buffer(handle renderer.BufferHandle) *bufEntry {
	if handle == 0 || int(handle) >= len(backend.buffers) || !backend.buffers[handle].valid {
		return nil
	}
	return backend.buffers[handle]
}

// Pairs a shared vertex buffer with one face group's index list
func (backend *VKBackend) CreateMesh(spec renderer.MeshInfo) renderer.MeshHandle {
	// A mesh may name no vertex buffer at all: a fullscreen pass whose shader
	// generates its own positions still needs a vertex count to draw
	vertexBuffer := backend.buffer(spec.Vertices)
	indexed := len(spec.Indices) > 0
	count := uint32(len(spec.Indices))
	if !indexed {
		count = uint32(spec.Count)
		if count == 0 && vertexBuffer != nil && spec.Stride > 0 {
			count = uint32(vertexBuffer.size) / uint32(spec.Stride)
		}
	}

	size := uint64(len(spec.Indices) * 4)
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
	if indexed {
		memcpy(info.MappedData, unsafe.Pointer(&spec.Indices[0]), uint64(len(spec.Indices)*4))
	}

	backend.meshes = append(backend.meshes, &meshEntry{
		name: spec.Name, vertices: spec.Vertices, indexBuffer: buf, indexAlloc: alloc,
		count: count, indexed: indexed, valid: true,
	})
	return renderer.MeshHandle(len(backend.meshes) - 1)
}

// Resolves a mesh handle, nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) mesh(handle renderer.MeshHandle) *meshEntry {
	if handle == 0 || int(handle) >= len(backend.meshes) || !backend.meshes[handle].valid {
		return nil
	}
	return backend.meshes[handle]
}
