package renderer

import (
	"unsafe"

	"github.com/go-gl/mathgl/mgl32"
)

// The uniform blocks the scene fills and uploads. They live here because
// everything above renderer/ shares them, not because the backend knows them:
// Frame.Upload takes bytes and returns an address, and nothing below this file
// reads a field.

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
	ShadowIndex int32   // into the shadow tile array, -1 when unshadowed
	ShadowCount int32   // tiles from ShadowIndex on: 1 sun/spot, 6 point
	Type        int32
}

// INVARIANT: the blocks below mirror common.slang field for field, in
// float32/int32/arrays/mgl32 matrices only, so scalar layout matches Go's
// packing and both sides memcpy (notes/ENGINE_FLOW.md §5). Every Tex* member is
// a shader-visible slot from Backend.Slot, not a handle.

// One shadow tile: where it lives in the atlas and how to project into it
// A sun or a spot owns one and a point light six consecutive ones
type ShadowTile struct {
	WorldToTile mgl32.Mat4 // world to this tile's clip space, both baking and sampling
	AtlasCoords [4]float32 // uv offset.xy, uv scale.xy
	PCFStep     float32    // Percentage-Closer Filtering step for soft edges: high smooths more
	FarPlane    float32    // far plane distance to divide radial distance into [0, 1]
	FaceIndex   int32      // 0..5 for a cube face, -1 for a 2D tile
	Flags       int32      // bit 0: sample the dynamic atlas; bit 1: cheap PCF
}

// Camera and lights: uploaded once per pass
type FrameUniforms struct {
	View       mgl32.Mat4           // world to camera space
	Projection mgl32.Mat4           // camera to clip space, z in [-w, w]
	ViewPos    [3]float32           // world position of the camera
	LightCount int32                // live entries in Lights, 0 to MaxLights
	Lights     [MaxLights]LightData // every light in the scene, shadow-casting or not
	TexSkybox  int32                // cube slot drawn as the sky and sampled for ambient
	// How much to grow the shadow normal-offset bias at this atlas size, the
	// offsets in forward.slang being world-space constants tuned at 4096
	ShadowNormalScale float32
}

// The one tile a depth pass is baking: uploaded per tile, ~80 bytes rather than
// the whole frame block, which is what let the arena shrink
type BakeUniforms struct {
	WorldToTile mgl32.Mat4 // the tile's projection, the same matrix that samples it
	LightPos    [3]float32 // world position of the light being baked
	FarPlane    float32    // divides radial distance into [0, 1] on a cube face
}

// Transform and material of one face group: uploaded once per draw
type DrawUniforms struct {
	Model        mgl32.Mat4
	MatAmbient   [3]float32
	MatShininess float32
	MatDiffuse   [3]float32
	MatMetallic  float32
	MatSpecular  [3]float32
	MatRoughness float32
	MatAo        float32
	TexDiffuse   int32 // 2D slot : 0 is the backend's white pixel
	TexNormalMap int32
	UseNormalMap int32
}

// Guards the common.slang correspondence on every build
func init() { // TODO: review
	if unsafe.Sizeof(LightData{}) != 72 {
		panic("renderer.LightData no longer matches common.slang")
	}
	if unsafe.Sizeof(FrameUniforms{}) != 4760 {
		panic("renderer.FrameUniforms no longer matches common.slang")
	}
	if unsafe.Sizeof(BakeUniforms{}) != 80 {
		panic("renderer.BakeUniforms no longer matches common.slang")
	}
	if unsafe.Sizeof(ShadowTile{}) != 96 {
		panic("renderer.ShadowTile no longer matches common.slang")
	}
	if unsafe.Sizeof(DrawUniforms{}) != 128 {
		panic("renderer.DrawUniforms no longer matches common.slang")
	}
}
