package renderer

import (
	"unsafe"

	"github.com/go-gl/mathgl/mgl32"
)

// Must match MAX_LIGHTS / MAX_SHADOW_CUBES in shaders/slang/common.slang
const (
	MaxLights      = 8
	MaxShadowCubes = 4
)

// Light types, matching the integer the shaders switch on
const (
	LightSun   = 0
	LightPoint = 1
)

// LightData mirrors the LightData struct in common.slang (68 bytes)
type LightData struct {
	Color     [3]float32
	Intensity float32
	Position  [3]float32
	Diffuse   float32
	Direction [3]float32
	Specular  float32

	Constant, Linear  float32
	Quadratic, Cutoff float32
	Type              int32
}

// Split by how often the data changes: FrameUniforms once per pass, DrawUniforms
// once per draw.
//
// INVARIANT: keep the field order identical to common.slang, and use only
// float32/int32, arrays of those, and mgl32 matrices. Scalar layout then matches
// Go's packing exactly, so both sides memcpy with no marshalling. Order drifting
// renders garbage silently; the init below only catches a size change.
//
// Tex* fields hold plain TextureHandles, where 0 means "white pixel".

// Camera, lights and shadow maps: Update once per pass
type FrameUniforms struct {
	View, Projection  mgl32.Mat4
	LightSpaceMatrix  mgl32.Mat4
	ShadowMatrices    [6]mgl32.Mat4
	ViewPos           [3]float32
	FarPlane          float32
	LightPos          [3]float32
	LightCount        int32
	Lights            [MaxLights]LightData
	TexShadowMap      TextureHandle
	TexShadowCubeMap  TextureHandle
	TexSkybox         TextureHandle
	ShadowDirIndex    int32
	PointShadowLights [MaxShadowCubes]int32
}

// Transform and material of one face group: Update once per draw
type DrawUniforms struct {
	Model        mgl32.Mat4
	MatAmbient   [3]float32
	MatShininess float32
	MatDiffuse   [3]float32
	MatMetallic  float32
	MatSpecular  [3]float32
	MatRoughness float32
	MatAo        float32
	TexDiffuse   TextureHandle
	TexNormalMap TextureHandle
	UseNormalMap int32
}

func init() { // TODO: where is it called
	// Go packs float32/int32 structs with no padding, which matches Vulkan's layout
	// These tests check that uniforms.go and common.slang layout always match
	if unsafe.Sizeof(LightData{}) != 68 {
		panic("renderer.LightData no longer matches common.slang")
	}
	if unsafe.Sizeof(FrameUniforms{}) != 1184 {
		panic("renderer.FrameUniforms no longer matches common.slang")
	}
	if unsafe.Sizeof(DrawUniforms{}) != 128 {
		panic("renderer.DrawUniforms no longer matches common.slang")
	}
}
