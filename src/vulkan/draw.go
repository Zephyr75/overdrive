package vulkan

import (
	"fmt"
	"os"
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Byte size of one snapshotted uniform block. The Go struct has no compiler
// padding, which is exactly Vulkan's scalar block layout, so it memcpys
// straight into the ring (renderer/uniforms.go guards that).
const uniformSize = uint64(unsafe.Sizeof(renderer.Uniforms{}))

// Binds the pipeline for the current pass, pushes the uniform address and draws one indexed face group
func (b *VKBackend) DrawMesh(s renderer.ShaderHandle, m renderer.MeshHandle, indexCount int, u *renderer.Uniforms) {
	sh, me, cb := b.prepareDraw(s, m)
	if sh == nil {
		return
	}
	b.bindPipeline(cb, sh, layoutMesh)
	b.pushUniforms(cb, u)

	vk.CmdBindVertexBuffer(cb, 0, b.buffers[me.vbo].buffer, 0)
	vk.CmdBindIndexBuffer(cb, me.indexBuffer, 0, vk.IndexTypeUint32)
	vk.CmdDrawIndexed(cb, uint32(indexCount), 1, 0, 0, 0)
}

// Draws the skybox cube through the position-only pipeline, non-indexed
func (b *VKBackend) DrawSkybox(s renderer.ShaderHandle, m renderer.MeshHandle, u *renderer.Uniforms) {
	sh, me, cb := b.prepareDraw(s, m)
	if sh == nil {
		return
	}
	b.bindPipeline(cb, sh, layoutSkybox)
	b.pushUniforms(cb, u)

	vk.CmdBindVertexBuffer(cb, 0, b.buffers[me.vbo].buffer, 0)
	vk.CmdDraw(cb, 36, 1, 0, 0) // non-indexed cube
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

// Composites the UI overlay over the finished scene, creating the quad buffer on first use
func (b *VKBackend) DrawFullscreenQuad(s renderer.ShaderHandle, tex renderer.TextureHandle) {
	if !b.frameActive {
		return
	}
	sh := b.shader(s)
	if sh == nil {
		return
	}
	if b.quadBuffer == 0 {
		b.quadBuffer = b.createBuffer(quadVertices, vk.BufferUsageVertexBuffer)
	}

	cb := b.frames[b.frameIndex].cb
	b.bindPipeline(cb, sh, layoutFullscreen)

	// Send the overlay's texture through the same uniform block as every other
	// draw, so this pass needs no special descriptor or push-constant path
	u := renderer.Uniforms{TexDiffuse: tex}
	b.pushUniforms(cb, &u)

	vk.CmdBindVertexBuffer(cb, 0, b.buffers[b.quadBuffer].buffer, 0)
	vk.CmdDraw(cb, 6, 1, 0, 0)
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
func (b *VKBackend) bindPipeline(cb vk.CommandBuffer, sh *shaderEntry, layout vertexLayout) {
	p := b.getPipeline(sh, b.currentPass, layout)
	if p != b.boundPipeline {
		vk.CmdBindPipeline(cb, vk.PipelineBindPointGraphics, p)
		b.boundPipeline = p
	}
}

// Snapshots *u into this frame's ring buffer and pushes the entry's device address, which is how the shaders reach the block
//
// The push constant is a pointer, so the uniform data itself needs no
// descriptor, and the caller may reuse u immediately afterwards.
func (b *VKBackend) pushUniforms(cb vk.CommandBuffer, u *renderer.Uniforms) {
	f := &b.frames[b.frameIndex]

	block := *u
	// Translate the texture fields in this copy, the shader indexing the
	// bindless arrays by slot rather than by engine handle. The two shadow-map
	// fields are left alone, as those maps have dedicated bindings and the
	// shader ignores their slot values
	block.TexDiffuse = renderer.TextureHandle(b.slot2D(u.TexDiffuse))
	block.TexNormalMap = renderer.TextureHandle(b.slot2D(u.TexNormalMap))
	block.TexSkybox = renderer.TextureHandle(b.slotCube(u.TexSkybox))
	b.bindShadowMaps(u)

	// Align to 64 bytes, keeping each entry on a cache line as the C++ ring did
	f.ringOffset = (f.ringOffset + 63) &^ 63
	if f.ringOffset+uniformSize > ringSize {
		fmt.Fprintln(os.Stderr, "vulkan: uniform ring overflow, wrapping (draws this frame may be wrong)")
		f.ringOffset = 0
	}
	vk.MemCopy(unsafe.Add(f.ringMapped, f.ringOffset), []renderer.Uniforms{block})

	addr := f.ringAddr + f.ringOffset
	f.ringOffset += uniformSize
	vk.CmdPushConstants(cb, b.pipelineLayout, pushStages, 0, 8, unsafe.Pointer(&addr))
}

// Mirrors the scene's current shadow maps into the dedicated bindings 2 and 3, rewriting them only when a handle changes
func (b *VKBackend) bindShadowMaps(u *renderer.Uniforms) {
	if u.TexShadowMap != b.shadow2DHandle {
		if e := b.texture(u.TexShadowMap); e != nil && !e.cube {
			b.shadow2DHandle = u.TexShadowMap
			b.writeDedicatedTexture(2, 0, e.view, b.samplerShadow2D)
		}
	}
	// Give the scene layer's single point-shadow caster cube slot 0.
	// Uniforms.PointShadowLights maps the remaining slots once more casters
	// are wired up
	if u.TexShadowCubeMap != b.shadowCubeHandle[0] {
		if e := b.texture(u.TexShadowCubeMap); e != nil && e.cube {
			b.shadowCubeHandle[0] = u.TexShadowCubeMap
			b.writeDedicatedTexture(3, 0, e.view, b.samplerShadowCube)
		}
	}
}
