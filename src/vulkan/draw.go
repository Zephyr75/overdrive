package vulkan

import (
	"fmt"
	"os"
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Byte sizes of the two snapshotted blocks. Go's packing is Vulkan's scalar layout, so both memcpy straight into the arena
const (
	frameUniformSize = uint64(unsafe.Sizeof(renderer.FrameUniforms{}))
	drawUniformSize  = uint64(unsafe.Sizeof(renderer.DrawUniforms{}))
)

// The push constant every shader reads: one device address per block
//
// The records are a pointer rather than a member of FrameUniforms because the
// array is sized by the scene, not by a constant.
type pushAddresses struct {
	frame, draw, records uint64
}

// Size of the push constant range, well inside the 128-byte guaranteed minimum
const pushConstantSize = uint32(unsafe.Sizeof(pushAddresses{}))

// Binds the pipeline for the current pass, pushes both uniform addresses and draws one mesh
func (backend *VKBackend) Draw(m renderer.MeshHandle, u *renderer.DrawUniforms) {
	sh, me, cb := backend.prepareDraw(backend.boundShader, m)
	if sh == nil {
		return
	}
	backend.bindPipeline(cb, sh, me.layout)
	backend.bindDrawUniforms(cb, u)

	vk.CmdBindVertexBuffer(cb, 0, backend.buffers[me.vbo].buffer, 0)
	if me.indexed {
		vk.CmdBindIndexBuffer(cb, me.indexBuffer, 0, vk.IndexTypeUint32)
		vk.CmdDrawIndexed(cb, me.count, 1, 0, 0, 0)
		return
	}
	vk.CmdDraw(cb, me.count, 1, 0, 0)
}

// Selects the shader set the following draws use
//
// Recorded rather than acted on: the pipeline also depends on the pass and the
// mesh's layout, both known only at Draw.
func (backend *VKBackend) BindShader(s renderer.ShaderHandle) { backend.boundShader = s }

// Resolves the shader and mesh handles and returns this frame's command buffer, or nils when the draw must be skipped
func (backend *VKBackend) prepareDraw(s renderer.ShaderHandle, m renderer.MeshHandle) (*shaderEntry, *meshEntry, vk.CommandBuffer) {
	if !backend.frameActive {
		return nil, nil, 0
	}
	sh := backend.shader(s)
	me := backend.mesh(m)
	if sh == nil || me == nil {
		return nil, nil, 0
	}
	return sh, me, backend.frames[backend.frameIndex].cb
}

// Binds the pipeline for this shader, pass and vertex layout, skipping the call when it is already bound
func (backend *VKBackend) bindPipeline(cb vk.CommandBuffer, sh *shaderEntry, layout renderer.VertexLayout) {
	p := backend.getPipeline(sh, backend.currentPass, layout)
	if p != backend.boundPipeline {
		vk.CmdBindPipeline(cb, vk.PipelineBindPointGraphics, p)
		backend.boundPipeline = p
	}
}

// Snapshots the pass-scoped block into the arena once, caching its address for every draw of the pass
func (backend *VKBackend) BindFrameUniforms(u *renderer.FrameUniforms) {
	if !backend.frameActive {
		return
	}
	block := *u
	// Skybox handle to bindless slot. The atlas fields are left alone: those have dedicated bindings
	block.TexSkybox = renderer.TextureHandle(backend.slotCube(u.TexSkybox))
	backend.bindShadowMaps(u)

	backend.frameUniformAddr = writeArena(backend, block)
}

// Snapshots *u into the arena and pushes both block addresses, leaving u reusable
func (backend *VKBackend) bindDrawUniforms(cb vk.CommandBuffer, u *renderer.DrawUniforms) {
	block := *u
	// Translate the texture fields in this copy, the shader indexing the
	// bindless arrays by slot rather than by engine handle
	block.TexDiffuse = renderer.TextureHandle(backend.slot2D(u.TexDiffuse))
	block.TexNormalMap = renderer.TextureHandle(backend.slot2D(u.TexNormalMap))

	addrs := pushAddresses{
		frame:   backend.frameUniformAddr,
		draw:    writeArena(backend, block),
		records: backend.recordAddr,
	}
	vk.CmdPushConstants(cb, backend.pipelineLayout, pushStages, 0, pushConstantSize, unsafe.Pointer(&addrs))
}

// Copies one block into this frame's arena and returns its device address
func writeArena[T any](b *VKBackend, block T) uint64 {
	return writeArenaSlice(b, []T{block})
}

// Copies a contiguous run of blocks into this frame's arena and returns the address of the first
func writeArenaSlice[T any](b *VKBackend, blocks []T) uint64 {
	f := &b.frames[b.frameIndex]
	var zero T
	size := uint64(unsafe.Sizeof(zero)) * uint64(len(blocks))

	// Align to 64 bytes, keeping each entry on a cache line as the C++ predecessor did
	f.arenaUsed = (f.arenaUsed + 63) &^ 63
	if f.arenaUsed+size > arenaSize {
		fmt.Fprintln(os.Stderr, "vulkan: uniform arena overflow, restarting at 0 (draws this frame may be wrong)")
		f.arenaUsed = 0
	}
	vk.MemCopy(unsafe.Add(f.arenaMapped, f.arenaUsed), blocks)

	addr := f.arenaAddr + f.arenaUsed
	f.arenaUsed += size
	return addr
}

// Snapshots this frame's shadow records into the arena, for every draw of the frame to reach by address
func (backend *VKBackend) BindShadowRecords(records []renderer.ShadowTile) {
	if !backend.frameActive || len(records) == 0 {
		return
	}
	backend.recordAddr = writeArenaSlice(backend, records)
}

// Mirrors the two shadow atlases into the dedicated bindings 2 and 3, rewriting them only when a handle changes
func (backend *VKBackend) bindShadowMaps(u *renderer.FrameUniforms) {
	atlases := [2]struct {
		handle renderer.TextureHandle
		cached *renderer.TextureHandle
	}{
		{u.TexShadowStatic, &backend.shadowStaticHandle},
		{u.TexShadowDynamic, &backend.shadowDynamicHandle},
	}
	for i, a := range atlases {
		if a.handle == 0 || a.handle == *a.cached {
			continue
		}
		// A cube would be the wrong descriptor type for these bindings, and the
		// atlas retired the last reason to create one
		if e := backend.texture(a.handle); e != nil && !e.cube {
			*a.cached = a.handle
			backend.writeDedicatedTexture(uint32(2+i), 0, e.view, backend.samplerShadow2D)
		}
	}
}
