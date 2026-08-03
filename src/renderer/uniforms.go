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
// Field order is not descriptive, it is the 16-byte cell rule — see the comment
// below and the LAYOUT RULE in common.slang.
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
	// std140 rounds a struct's array stride up to 16 and 17 fields do not
	// divide by 4, so the remainder is declared rather than left implicit.
	// Free for spot-light outer cutoff, point-light radius, whatever is next
	Reserved0, Reserved1, Reserved2 float32
}

// The uniform data is split by how often it changes, not by what it describes.
// FrameUniforms goes out once per pass, DrawUniforms once per draw — before the
// split one 1312-byte block went out on every draw, ~1200 bytes of which never
// varied within a pass.
//
// Both mirror common.slang field for field, and both are laid out in 16-byte
// cells: every [3]float32 is immediately followed by one 4-byte scalar, loose
// scalars come in fours, and no member is a scalar array. That is what makes
// std140 (which OpenGL 4.1 uniform blocks must use, and which pads a vec3 to 16
// and a scalar array to a 16-byte stride) come out byte-identical to Vulkan's
// scalar layout, which is what Go packing already gives us. So *neither* backend
// marshals: both memcpy the struct. The init below guards the sizes, and
// opengl/uniforms_test.go re-derives std140 from the generated GLSL and checks
// every member offset against unsafe.Offsetof.
//
// Reordering a field, or inserting one without keeping its cell full, silently
// breaks the OpenGL backend. Read the LAYOUT RULE in common.slang first.
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
	// scalar layout — and, given the 16-byte cell rule above, std140 too. Guard
	// that both stay true: a size that is not a multiple of 16 means some cell
	// was left unfilled and the two layouts have diverged
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
