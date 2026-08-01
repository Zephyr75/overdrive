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

// LightData mirrors the Light struct in common.slang (scalar layout, 68 bytes)
type LightData struct {
	Type                         int32
	Constant, Linear, Quadratic  float32
	Cutoff                       float32
	Color                        [3]float32
	Intensity, Diffuse, Specular float32
	Position, Direction          [3]float32
}

// The uniform data is split by how often it changes, not by what it describes.
// FrameUniforms goes out once per pass, DrawUniforms once per draw — before the
// split one 1312-byte block went out on every draw, ~1200 bytes of which never
// varied within a pass.
//
// Both mirror common.slang field for field. Go packs float32/int32 structs with
// no padding, which is exactly Vulkan's scalar block layout, so each memcpys
// straight into the ring; the init below guards that. OpenGL marshals them into
// two std140 UBOs by hand in opengl/uniforms.go.
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
	MatDiffuse   [3]float32
	MatSpecular  [3]float32
	MatShininess float32
	TexDiffuse   TextureHandle
	TexNormalMap TextureHandle
	UseNormalMap int32
	MatMetallic  float32
	MatRoughness float32
	MatAo        float32
}

func init() {
	// Go packs float32/int32 structs with no padding, which is exactly Vulkan's
	// scalar block layout, so guard that this stays true
	if unsafe.Sizeof(LightData{}) != 68 {
		panic("renderer.LightData no longer matches common.slang scalar layout")
	}
	if unsafe.Sizeof(FrameUniforms{}) != 1184 {
		panic("renderer.FrameUniforms no longer matches common.slang scalar layout")
	}
	if unsafe.Sizeof(DrawUniforms{}) != 128 {
		panic("renderer.DrawUniforms no longer matches common.slang scalar layout")
	}
}
