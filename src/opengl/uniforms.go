package opengl

import (
	"encoding/binary"
	"math"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

// std140 layout of the Uniforms block in shaders/slang/common.slang, as
// slangc reflects it for the GLSL target. This is the one hand-written layout
// in the engine: the Vulkan backend gets its (scalar) layout for free because
// Go structs are already packed that way, but OpenGL 4.1 uniform blocks must be
// std140, where vec3s pad to 16 bytes and array elements round up to 16.
//
// uniforms_test.go re-derives these from the generated GLSL and fails on drift.
const (
	offView             = 0
	offProjection       = 64
	offModel            = 128
	offLightSpaceMatrix = 192
	offShadowMatrices   = 256 // 6 x mat4
	offViewPos          = 640 // vec3
	offFarPlane         = 652
	offLightPos         = 656 // vec3, padded to 16
	offMatAmbient       = 672 // vec3
	offMatDiffuse       = 688 // vec3
	offMatSpecular      = 704 // vec3
	offMatShininess     = 716
	offLights           = 720 // MaxLights x lightStride
	lightStride         = 96

	// The five texture-slot ints are Vulkan-only (GL samples through named
	// samplers), but they occupy block space and so fix everything after them.
	offUseNormalMap   = 1508
	offLightCount     = 1512
	offShadowDirIndex = 1516
	offMatMetallic    = 1520
	offMatRoughness   = 1524
	offMatAo          = 1528
	// An int array in std140 has a 16-byte element stride and 16-byte alignment,
	// so it starts at 1536 (1532 rounded up) and ends the block at 1600.
	offPointShadowLights = 1536
	pointShadowStride    = 16

	blockSize = 1600
)

// Per-light member offsets, relative to that light's base.
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

// Texture units, assigned to the generated samplers once at link time
// (see assignSamplerUnits). Every cube sampler needs its own unit: leaving
// shadowCubeMap[1..3] at unit 0 would collide with the 2D shadow sampler and
// GL rejects the draw with GL_INVALID_OPERATION.
const (
	unitShadowMap    = 0
	unitOurTexture   = 1
	unitNormalMap    = 2
	unitShadowCube0  = 3 // .. unitShadowCube0 + MaxShadowCubes - 1
	unitSkybox       = unitShadowCube0 + renderer.MaxShadowCubes
	samplerUnitCount = unitSkybox + 1
)

func putF32(dst []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
}

func putI32(dst []byte, off int, v int32) {
	binary.LittleEndian.PutUint32(dst[off:], uint32(v))
}

func putVec3(dst []byte, off int, v [3]float32) {
	putF32(dst, off+0, v[0])
	putF32(dst, off+4, v[1])
	putF32(dst, off+8, v[2])
}

func putMat4(dst []byte, off int, m mgl32.Mat4) {
	for i := 0; i < 16; i++ {
		putF32(dst, off+i*4, m[i])
	}
}

// marshalStd140 writes the uniform snapshot into the block layout above.
// dst must be at least blockSize bytes.
func marshalStd140(u *renderer.Uniforms, dst []byte) {
	putMat4(dst, offView, u.View)
	putMat4(dst, offProjection, u.Projection)
	putMat4(dst, offModel, u.Model)
	putMat4(dst, offLightSpaceMatrix, u.LightSpaceMatrix)
	for i := 0; i < 6; i++ {
		putMat4(dst, offShadowMatrices+i*64, u.ShadowMatrices[i])
	}

	putVec3(dst, offViewPos, u.ViewPos)
	putF32(dst, offFarPlane, u.FarPlane)
	putVec3(dst, offLightPos, u.LightPos)

	putVec3(dst, offMatAmbient, u.MatAmbient)
	putVec3(dst, offMatDiffuse, u.MatDiffuse)
	putVec3(dst, offMatSpecular, u.MatSpecular)
	putF32(dst, offMatShininess, u.MatShininess)

	for i := 0; i < renderer.MaxLights; i++ {
		base := offLights + i*lightStride
		l := &u.Lights[i]
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

	putI32(dst, offUseNormalMap, u.UseNormalMap)
	putI32(dst, offLightCount, u.LightCount)
	putI32(dst, offShadowDirIndex, u.ShadowDirIndex)
	putF32(dst, offMatMetallic, u.MatMetallic)
	putF32(dst, offMatRoughness, u.MatRoughness)
	putF32(dst, offMatAo, u.MatAo)
	for i := 0; i < renderer.MaxShadowCubes; i++ {
		putI32(dst, offPointShadowLights+i*pointShadowStride, u.PointShadowLights[i])
	}
}

// applyUniforms uploads the snapshot into the shared uniform buffer and binds
// the referenced textures to the units their samplers were assigned at link
// time. Replaces the Phase 1 loose-uniform bridge.
func (b *GLBackend) applyUniforms(u *renderer.Uniforms) {
	marshalStd140(u, b.blockScratch)
	gl.BindBuffer(gl.UNIFORM_BUFFER, b.ubo)
	gl.BufferSubData(gl.UNIFORM_BUFFER, 0, blockSize, gl.Ptr(b.blockScratch))
	gl.BindBuffer(gl.UNIFORM_BUFFER, 0)

	b.bind2D(unitShadowMap, u.TexShadowMap)
	b.bind2D(unitOurTexture, u.TexDiffuse)
	b.bind2D(unitNormalMap, u.TexNormalMap)

	// Only one point-shadow caster is tracked by the scene layer today; the
	// remaining cube units still need a valid binding of the right type.
	b.bindCube(unitShadowCube0, u.TexShadowCubeMap)
	for i := 1; i < renderer.MaxShadowCubes; i++ {
		b.bindCube(unitShadowCube0+i, 0)
	}
	b.bindCube(unitSkybox, u.TexSkybox)
}

// bind2D binds a 2D texture, substituting the built-in white pixel for handle
// 0 ("no texture"), which reads as unlit-white / fully-lit in the shaders.
func (b *GLBackend) bind2D(unit int, h renderer.TextureHandle) {
	tex := uint32(h)
	if tex == 0 {
		tex = b.whiteTex
	}
	gl.ActiveTexture(gl.TEXTURE0 + uint32(unit))
	gl.BindTexture(gl.TEXTURE_2D, tex)
}

func (b *GLBackend) bindCube(unit int, h renderer.TextureHandle) {
	tex := uint32(h)
	if tex == 0 {
		tex = b.blackCube
	}
	gl.ActiveTexture(gl.TEXTURE0 + uint32(unit))
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, tex)
}
