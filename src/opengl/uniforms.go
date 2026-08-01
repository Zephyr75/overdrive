package opengl

import (
	"encoding/binary"
	"math"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

// std140 layout of the two uniform blocks in shaders/slang/common.slang, as
// slangc reflects them for the GLSL target. This is the one hand-written layout
// in the engine. The Vulkan backend gets its (scalar) layout for free because
// Go structs are already packed that way, but OpenGL 4.1 uniform blocks must be
// std140, where vec3s pad to 16 bytes and array elements round up to 16.
//
// uniforms_test.go re-derives these from the generated GLSL and fails on drift.

// Binding points. The frame block is rewritten once per pass, the draw block
// once per draw — which is the whole point of the split.
const (
	bindingFrame = 0
	bindingDraw  = 1
)

// FrameUniforms — camera, lights, shadow maps
const (
	offView             = 0
	offProjection       = 64
	offLightSpaceMatrix = 128
	offShadowMatrices   = 192 // 6 x mat4
	offViewPos          = 576 // vec3
	offFarPlane         = 588
	offLightPos         = 592 // vec3, padded to 16
	offLightCount       = 604
	offLights           = 608 // MaxLights x lightStride
	lightStride         = 96

	// The three texture-slot ints are Vulkan-only (GL samples through named
	// samplers), but they occupy block space and so fix everything after them.
	offTexShadowMap     = 1376
	offTexShadowCubeMap = 1380
	offTexSkybox        = 1384
	offShadowDirIndex   = 1388
	// An int array in std140 has a 16-byte element stride and 16-byte alignment,
	// so it starts at 1392 and ends the block at 1456.
	offPointShadowLights = 1392
	pointShadowStride    = 16

	frameBlockSize = 1456
)

// DrawUniforms — model matrix and material
const (
	offModel        = 0
	offMatAmbient   = 64 // vec3
	offMatDiffuse   = 80 // vec3
	offMatSpecular  = 96 // vec3
	offMatShininess = 108
	// Vulkan-only slots again, but they fix the offsets that follow.
	offTexOurTexture = 112
	offTexNormalMap  = 116
	offUseNormalMap  = 120
	offMatMetallic   = 124
	offMatRoughness  = 128
	offMatAo         = 132

	// 136 rounded up to the 16-byte block alignment.
	drawBlockSize = 144
)

// Per-light member offsets, relative to that light's base
const (
	lOffType      = 0
	lOffConstant  = 4
	lOffLinear    = 8
	lOffQuadratic = 12
	lOffCutoff    = 16
	lOffColor     = 32 // vec3
	lOffIntensity = 44
	lOffDiffuse   = 48
	lOffSpecular  = 52
	lOffPosition  = 64 // vec3
	lOffDirection = 80 // vec3
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

// Writes one float at a byte offset
func putF32(dst []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
}

// Writes one int at a byte offset
func putI32(dst []byte, off int, v int32) {
	binary.LittleEndian.PutUint32(dst[off:], uint32(v))
}

// Writes three floats, the std140 padding to 16 bytes being left to the caller's offsets
func putVec3(dst []byte, off int, v [3]float32) {
	putF32(dst, off+0, v[0])
	putF32(dst, off+4, v[1])
	putF32(dst, off+8, v[2])
}

// Writes a column-major 4x4 matrix
func putMat4(dst []byte, off int, m mgl32.Mat4) {
	for i := 0; i < 16; i++ {
		putF32(dst, off+i*4, m[i])
	}
}

// Writes the pass snapshot into the frame block layout, dst being at least frameBlockSize bytes
func marshalFrameStd140(f *renderer.FrameUniforms, dst []byte) {
	putMat4(dst, offView, f.View)
	putMat4(dst, offProjection, f.Projection)
	putMat4(dst, offLightSpaceMatrix, f.LightSpaceMatrix)
	for i := 0; i < 6; i++ {
		putMat4(dst, offShadowMatrices+i*64, f.ShadowMatrices[i])
	}

	putVec3(dst, offViewPos, f.ViewPos)
	putF32(dst, offFarPlane, f.FarPlane)
	putVec3(dst, offLightPos, f.LightPos)
	putI32(dst, offLightCount, f.LightCount)

	for i := 0; i < renderer.MaxLights; i++ {
		base := offLights + i*lightStride
		l := &f.Lights[i]
		putI32(dst, base+lOffType, l.Type)
		putF32(dst, base+lOffConstant, l.Constant)
		putF32(dst, base+lOffLinear, l.Linear)
		putF32(dst, base+lOffQuadratic, l.Quadratic)
		putF32(dst, base+lOffCutoff, l.Cutoff)
		putVec3(dst, base+lOffColor, l.Color)
		putF32(dst, base+lOffIntensity, l.Intensity)
		putF32(dst, base+lOffDiffuse, l.Diffuse)
		putF32(dst, base+lOffSpecular, l.Specular)
		putVec3(dst, base+lOffPosition, l.Position)
		putVec3(dst, base+lOffDirection, l.Direction)
	}

	putI32(dst, offShadowDirIndex, f.ShadowDirIndex)
	for i := 0; i < renderer.MaxShadowCubes; i++ {
		putI32(dst, offPointShadowLights+i*pointShadowStride, f.PointShadowLights[i])
	}
}

// Writes the draw snapshot into the draw block layout, dst being at least drawBlockSize bytes
func marshalDrawStd140(u *renderer.DrawUniforms, dst []byte) {
	putMat4(dst, offModel, u.Model)
	putVec3(dst, offMatAmbient, u.MatAmbient)
	putVec3(dst, offMatDiffuse, u.MatDiffuse)
	putVec3(dst, offMatSpecular, u.MatSpecular)
	putF32(dst, offMatShininess, u.MatShininess)
	putI32(dst, offUseNormalMap, u.UseNormalMap)
	putF32(dst, offMatMetallic, u.MatMetallic)
	putF32(dst, offMatRoughness, u.MatRoughness)
	putF32(dst, offMatAo, u.MatAo)
}

// Uploads the pass-scoped block and binds the textures it names, once per pass
func (b *GLBackend) BindFrameUniforms(f *renderer.FrameUniforms) {
	marshalFrameStd140(f, b.frameScratch)
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.frameUBO)
	gl.BufferSubData(gl.UNIFORM_BUFFER, 0, frameBlockSize, gl.Ptr(b.frameScratch))
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
func (b *GLBackend) applyDrawUniforms(u *renderer.DrawUniforms) {
	marshalDrawStd140(u, b.drawScratch)
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.drawUBO)
	gl.BufferSubData(gl.UNIFORM_BUFFER, 0, drawBlockSize, gl.Ptr(b.drawScratch))
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
