package opengl

import (
	"unsafe"

	"github.com/go-gl/gl/v4.1-core/gl"

	"github.com/Zephyr75/overdrive/renderer"
)

// Uploading the two uniform blocks, which is a plain memcpy on this backend.
//
// It is only a memcpy because common.slang declares both blocks in 16-byte
// cells (see its LAYOUT RULE): every float3 is followed by a scalar, loose
// scalars come in fours, and no member is a scalar array. Without that, std140
// pads a vec3 to 16 and an int[4] to a 64-byte stride, Go's packed struct does
// not, and every field would have to be written at a hand-computed offset.
//
// uniforms_test.go re-derives std140 from the generated GLSL and fails if a
// field ever lands somewhere Go does not put it.

// Binding points. The frame block is rewritten once per pass, the draw block
// once per draw — which is the whole point of the split.
const (
	bindingFrame = 0
	bindingDraw  = 1
)

// Block sizes, straight off the Go structs the shader mirrors
const (
	frameBlockSize = int(unsafe.Sizeof(renderer.FrameUniforms{}))
	drawBlockSize  = int(unsafe.Sizeof(renderer.DrawUniforms{}))
)

// Texture units, assigned to the generated samplers once at link time (see
// setupProgramInterface). Every cube sampler needs its own unit, because
// leaving shadowCubeMap[1..3] at unit 0 would collide with the 2D shadow
// sampler and GL rejects such a draw with GL_INVALID_OPERATION.
const (
	unitShadowMap    = 0
	unitOurTexture   = 1
	unitNormalMap    = 2
	unitShadowCube0  = 3 // .. unitShadowCube0 + MaxShadowCubes - 1
	unitSkybox       = unitShadowCube0 + renderer.MaxShadowCubes
	samplerUnitCount = unitSkybox + 1
)

// Uploads the pass-scoped block and binds the textures it names, once per pass
func (b *GLBackend) BindFrameUniforms(f *renderer.FrameUniforms) {
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.frameUBO)
	gl.BufferSubData(gl.UNIFORM_BUFFER, 0, frameBlockSize, unsafe.Pointer(f))
	gl.BindBuffer(gl.UNIFORM_BUFFER, 0)

	b.bind2D(unitShadowMap, f.TexShadowMap)

	// Bind the one point-shadow caster the scene layer tracks, then fill the
	// remaining cube units, which still need a valid binding of the right type
	b.bindCube(unitShadowCube0, f.TexShadowCubeMap)
	for i := 1; i < renderer.MaxShadowCubes; i++ {
		b.bindCube(unitShadowCube0+i, 0)
	}
	b.bindCube(unitSkybox, f.TexSkybox)
}

// Uploads the per-draw block and binds its two material textures
func (b *GLBackend) bindDrawUniforms(u *renderer.DrawUniforms) {
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.drawUBO)
	gl.BufferSubData(gl.UNIFORM_BUFFER, 0, drawBlockSize, unsafe.Pointer(u))
	gl.BindBuffer(gl.UNIFORM_BUFFER, 0)

	b.bind2D(unitOurTexture, u.TexDiffuse)
	b.bind2D(unitNormalMap, u.TexNormalMap)
}

// Binds a 2D texture to a unit, substituting the white pixel for handle 0, which reads as unlit-white or fully-lit in the shaders
func (b *GLBackend) bind2D(unit int, h renderer.TextureHandle) {
	tex := uint32(h)
	if tex == 0 {
		tex = b.whiteTex
	}
	gl.ActiveTexture(gl.TEXTURE0 + uint32(unit))
	gl.BindTexture(gl.TEXTURE_2D, tex)
}

// Binds a cubemap to a unit, substituting the black dummy cube for handle 0
func (b *GLBackend) bindCube(unit int, h renderer.TextureHandle) {
	tex := uint32(h)
	if tex == 0 {
		tex = b.blackCube
	}
	gl.ActiveTexture(gl.TEXTURE0 + uint32(unit))
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, tex)
}
