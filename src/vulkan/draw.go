package vulkan

import (
	"fmt"
	"os"
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Byte sizes of the two snapshotted uniform blocks. Neither Go struct has
// compiler padding, which is exactly Vulkan's scalar block layout, so both
// memcpy straight into the ring (renderer/uniforms.go guards that).
const (
	frameUniformSize = uint64(unsafe.Sizeof(renderer.FrameUniforms{}))
	drawUniformSize  = uint64(unsafe.Sizeof(renderer.DrawUniforms{}))
)

// The push constant both shaders read: one device address per block
type pushAddresses struct {
	frame, draw uint64
}

// The UI overlay's screen-covering quad as two triangles, clip-space
// position(3) | uv(2). Wound counter-clockwise, matching the front face the
// main pass declares. Same geometry the OpenGL backend uses, so both backends
// composite the overlay identically.
var quadVertices = []float32{
	-1, 1, 0, 0, 1,
	-1, -1, 0, 0, 0,
	1, 1, 0, 1, 1,

	1, 1, 0, 1, 1,
	-1, -1, 0, 0, 0,
	1, -1, 0, 1, 0,
}

// Binds the pipeline for the current pass, pushes both uniform addresses and draws one mesh
//
// The mesh carries its own vertex layout and count, so this is the only draw
// entry point: a face group, the skybox cube and the overlay quad differ in
// what was recorded at creation, not in how they are issued.
func (b *VKBackend) Draw(s renderer.ShaderHandle, m renderer.MeshHandle, u *renderer.DrawUniforms) {
	sh, me, cb := b.prepareDraw(s, m)
	if sh == nil {
		return
	}
	b.bindPipeline(cb, sh, me.layout)
	b.bindDrawUniforms(cb, u)

	vk.CmdBindVertexBuffer(cb, 0, b.buffers[me.vbo].buffer, 0)
	if me.indexed {
		vk.CmdBindIndexBuffer(cb, me.indexBuffer, 0, vk.IndexTypeUint32)
		vk.CmdDrawIndexed(cb, me.count, 1, 0, 0, 0)
		return
	}
	vk.CmdDraw(cb, me.count, 1, 0, 0)
}

// Resolves the shader and mesh handles and returns this frame's command buffer, or nils when the draw must be skipped
func (b *VKBackend) prepareDraw(s renderer.ShaderHandle, m renderer.MeshHandle) (*shaderEntry, *meshEntry, vk.CommandBuffer) {
	if !b.frameActive {
		return nil, nil, 0
	}
	sh := b.shader(s)
	me := b.mesh(m)
	if sh == nil || me == nil {
		return nil, nil, 0
	}
	return sh, me, b.frames[b.frameIndex].cb
}

// Binds the pipeline for this shader, pass and vertex layout, skipping the call when it is already bound
func (b *VKBackend) bindPipeline(cb vk.CommandBuffer, sh *shaderEntry, layout renderer.VertexLayout) {
	p := b.getPipeline(sh, b.currentPass, layout)
	if p != b.boundPipeline {
		vk.CmdBindPipeline(cb, vk.PipelineBindPointGraphics, p)
		b.boundPipeline = p
	}
}

// Snapshots the pass-scoped block into the ring once, caching its address for every draw of the pass
//
// This is the point of the frame/draw split: ~1.2 KB of camera and light data
// is written three times a frame instead of fifteen.
func (b *VKBackend) BindFrameUniforms(u *renderer.FrameUniforms) {
	if !b.frameActive {
		return
	}
	block := *u
	// Translate the skybox handle into its bindless slot. The two shadow-map
	// fields are left alone, as those maps have dedicated bindings and the
	// shader ignores their slot values
	block.TexSkybox = renderer.TextureHandle(b.slotCube(u.TexSkybox))
	b.bindShadowMaps(u)

	b.frameUniformAddr = writeRing(b, block)
}

// Snapshots *u into the ring and pushes both block addresses, which is how the shaders reach them
//
// The push constant is a pair of pointers, so neither block needs a descriptor,
// and the caller may reuse u immediately afterwards.
func (b *VKBackend) bindDrawUniforms(cb vk.CommandBuffer, u *renderer.DrawUniforms) {
	block := *u
	// Translate the texture fields in this copy, the shader indexing the
	// bindless arrays by slot rather than by engine handle
	block.TexDiffuse = renderer.TextureHandle(b.slot2D(u.TexDiffuse))
	block.TexNormalMap = renderer.TextureHandle(b.slot2D(u.TexNormalMap))

	addrs := pushAddresses{
		frame: b.frameUniformAddr,
		draw:  writeRing(b, block),
	}
	vk.CmdPushConstants(cb, b.pipelineLayout, pushStages, 0, 16, unsafe.Pointer(&addrs))
}

// Copies one block into this frame's ring and returns its device address
//
// Generic rather than a method, because a method cannot take a type parameter
// and vk.MemCopy is itself generic over the element type.
func writeRing[T any](b *VKBackend, block T) uint64 {
	f := &b.frames[b.frameIndex]
	size := uint64(unsafe.Sizeof(block))

	// Align to 64 bytes, keeping each entry on a cache line as the C++ ring did
	f.ringOffset = (f.ringOffset + 63) &^ 63
	if f.ringOffset+size > ringSize {
		fmt.Fprintln(os.Stderr, "vulkan: uniform ring overflow, wrapping (draws this frame may be wrong)")
		f.ringOffset = 0
	}
	vk.MemCopy(unsafe.Add(f.ringMapped, f.ringOffset), []T{block})

	addr := f.ringAddr + f.ringOffset
	f.ringOffset += size
	return addr
}

// Mirrors the scene's current shadow maps into the dedicated bindings 2 and 3, rewriting them only when a handle changes
//
// Handle 0 means "this draw carries no shadow map" — the UI overlay's quad
// fills only TexDiffuse — and must leave the bindings alone. Treating it as the
// white pixel (which is what texture(0) resolves to) would rewrite binding 2
// every frame, and the forward pass of the frame still in flight is dynamically
// sampling it.
func (b *VKBackend) bindShadowMaps(u *renderer.FrameUniforms) {
	if u.TexShadowMap != 0 && u.TexShadowMap != b.shadow2DHandle {
		if e := b.texture(u.TexShadowMap); e != nil && !e.cube {
			b.shadow2DHandle = u.TexShadowMap
			b.writeDedicatedTexture(2, 0, e.view, b.samplerShadow2D)
		}
	}
	// Give the scene layer's single point-shadow caster cube slot 0.
	// Uniforms.PointShadowLights maps the remaining slots once more casters
	// are wired up
	if u.TexShadowCubeMap != 0 && u.TexShadowCubeMap != b.shadowCubeHandle[0] {
		if e := b.texture(u.TexShadowCubeMap); e != nil && e.cube {
			b.shadowCubeHandle[0] = u.TexShadowCubeMap
			b.writeDedicatedTexture(3, 0, e.view, b.samplerShadowCube)
		}
	}
}
