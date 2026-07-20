package renderer

import (
	"unsafe"

	"github.com/go-gl/mathgl/mgl32"
)

// Must match MAX_LIGHTS / MAX_SHADOW_CUBES in cpp/shaders/slang/common.slang.
const (
	MaxLights      = 8
	MaxShadowCubes = 4
)

// Light types, matching the integer the shaders switch on.
const (
	LightSun   = 0
	LightPoint = 1
)

// LightData mirrors the Light struct in common.slang (scalar layout, 68 bytes).
type LightData struct {
	Type                         int32
	Constant, Linear, Quadratic  float32
	Cutoff                       float32
	Color                        [3]float32
	Intensity, Diffuse, Specular float32
	Position, Direction          [3]float32
}

// Uniforms mirrors the Uniforms struct in common.slang field for field
// (scalar layout, 1312 bytes). Scene code fills fields and passes the struct
// to each draw; backends translate it (GL: std140/loose-uniform upload +
// fixed texture units; VK: ring-buffer memcpy + bindless slot patching).
//
// The Tex* fields hold plain TextureHandles; 0 means "white pixel".
type Uniforms struct {
	View, Projection, Model mgl32.Mat4
	LightSpaceMatrix        mgl32.Mat4
	ShadowMatrices          [6]mgl32.Mat4
	ViewPos                 [3]float32
	FarPlane                float32
	LightPos                [3]float32
	MatAmbient, MatDiffuse  [3]float32
	MatSpecular             [3]float32
	MatShininess            float32
	Lights                  [MaxLights]LightData
	TexShadowMap            TextureHandle
	TexDiffuse              TextureHandle
	TexShadowCubeMap        TextureHandle
	TexSkybox               TextureHandle
	TexNormalMap            TextureHandle
	UseNormalMap            int32
	LightCount              int32
	ShadowDirIndex          int32
	MatMetallic             float32
	MatRoughness            float32
	MatAo                   float32
	PointShadowLights       [MaxShadowCubes]int32
}

func init() {
	// Go packs float32/int32 structs with no padding, which is exactly
	// Vulkan's scalar block layout — guard that this stays true.
	if unsafe.Sizeof(LightData{}) != 68 || unsafe.Sizeof(Uniforms{}) != 1312 {
		panic("renderer.Uniforms no longer matches common.slang scalar layout")
	}
}
