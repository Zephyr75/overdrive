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

// LightData mirrors the LightData struct in common.slang (80 bytes)
//
// Field order is not descriptive, it is the 16-byte cell rule — which is now
// vestigial, see the comment below.
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
	// Padding to a multiple of 16, kept from the std140 era. Free for
	// spot-light outer cutoff, point-light radius, whatever is next
	Reserved0, Reserved1, Reserved2 float32
}

// The uniform data is split by how often it changes, not by what it describes.
// FrameUniforms goes out once per pass, DrawUniforms once per draw — before the
// split one 1312-byte block went out on every draw, ~1200 bytes of which never
// varied within a pass.
//
// Both mirror common.slang field for field, and the backend memcpys them —
// there is no marshalling code. Slang compiles with -fvk-use-scalar-layout, and
// scalar layout is exactly Go's packing for float32/int32 structs, so the two
// agree by construction as long as the field *order* matches.
//
// Both structs are still laid out in 16-byte cells — every [3]float32 followed
// by a scalar, loose scalars in fours, no scalar arrays. That was std140's rule,
// required while OpenGL was a backend. It no longer constrains anything and the
// structs can be reordered freely; see notes/BACKEND_DECISION.md §5.3.
//
// The init below guards the sizes, which is what catches editing one side and
// not the other.
//
// The Tex* fields hold plain TextureHandles, where 0 means "white pixel".

// FrameUniforms is camera, lights and shadow maps: constant across a pass
//
// Per *pass* rather than strictly per frame — each shadow bake overwrites the
// light matrices, and the sun's pass leaves its matrix behind for the main pass.
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

// DrawUniforms is the transform and material of one face group
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

func init() {
	// Go packs float32/int32 structs with no padding, which is exactly Vulkan's
	// scalar layout. These sizes are the tripwire for editing this file without
	// editing common.slang, or the other way round
	if unsafe.Sizeof(LightData{}) != 80 {
		panic("renderer.LightData no longer matches common.slang")
	}
	if unsafe.Sizeof(FrameUniforms{}) != 1280 {
		panic("renderer.FrameUniforms no longer matches common.slang")
	}
	if unsafe.Sizeof(DrawUniforms{}) != 128 {
		panic("renderer.DrawUniforms no longer matches common.slang")
	}
}
