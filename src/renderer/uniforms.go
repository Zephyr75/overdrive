package renderer

import (
	"unsafe"

	"github.com/go-gl/mathgl/mgl32"
)

// Must match MAX_LIGHTS in shaders/slang/common.slang
const MaxLights = 64

// Light types, matching the integer the shaders switch on
const (
	LightSun   = 0
	LightPoint = 1
	LightSpot  = 2
)

// LightData mirrors the LightData struct in common.slang (72 bytes)
type LightData struct {
	Color     [3]float32
	Intensity float32
	Position  [3]float32 // point and spot lights
	Diffuse   float32    // second radiance multiplier beside Intensity
	Direction [3]float32 // sun and spot lights, points away from the light
	Constant  float32    // keeps the falloff denominator off zero

	Cutoff      float32 // spot only, inner cone cosine
	OuterCutoff float32 // spot only, outer cone cosine, where the falloff ends
	Radius      float32 // attenuation cutoff, for the shading early-out
	ShadowIndex int32   // into ShadowRecord[], -1 when unshadowed
	ShadowCount int32   // records from ShadowIndex on: 1 sun/spot, 6 point
	Type        int32
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

// One shadow tile: where it lives in the atlas and how to project into it
// A sun or a spot owns one and a point light six consecutive ones
type ShadowRecord struct {
	WorldToTile mgl32.Mat4 // world to this tile's clip space, both baking and sampling
	AtlasCoords [4]float32 // uv offset.xy, uv scale.xy
	PCFStep     float32    // Percentage-Closer Filtering step for soft edges: high smooths more
	FarPlane    float32    // far plane distance to divide radial distance into [0, 1]
	FaceIndex   int32      // 0..5 for a cube face, -1 for a 2D tile
	Flags       int32      // bit 0: sample the dynamic or static atlas
}

// Camera, lights and shadow maps: Update once per pass
type FrameUniforms struct {
	View             mgl32.Mat4           // world to camera space
	Projection       mgl32.Mat4           // camera to clip space, z in [-w, w]
	CurWorldToTile   mgl32.Mat4           // WorldToTile of the tile being baked
	CurLightPos      [3]float32           // world position of the light being baked
	CurFarPlane      float32              // FarPlane of the tile being baked
	ViewPos          [3]float32           // world position of the camera
	LightCount       int32                // live entries in Lights, 0 to MaxLights
	Lights           [MaxLights]LightData // every light in the scene, shadow-casting or not
	TexShadowStatic  TextureHandle        // depth atlas sampled when Flags bit 0 is 0
	TexShadowDynamic TextureHandle        // depth atlas sampled when Flags bit 0 is 1
	TexSkybox        TextureHandle        // cubemap drawn as the sky and sampled for ambient
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
	if unsafe.Sizeof(LightData{}) != 72 {
		panic("renderer.LightData no longer matches common.slang")
	}
	if unsafe.Sizeof(FrameUniforms{}) != 4844 {
		panic("renderer.FrameUniforms no longer matches common.slang")
	}
	if unsafe.Sizeof(ShadowRecord{}) != 96 {
		panic("renderer.ShadowRecord no longer matches common.slang")
	}
	if unsafe.Sizeof(DrawUniforms{}) != 128 {
		panic("renderer.DrawUniforms no longer matches common.slang")
	}
}
