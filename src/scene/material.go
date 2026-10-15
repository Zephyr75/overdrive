package scene

import (
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

type Material struct {
	// MTL d, multiplied into the alpha map: a fragment is opaque from
	// OpaqueAlpha, blended below it and absent at 0
	Alpha   float32
	Diffuse mgl32.Vec3

	// Metallic-roughness PBR scalars consumed by forward.slang's Cook-Torrance
	// BRDF. Diffuse doubles as the base colour / albedo.
	Metallic  float32
	Roughness float32
	Ao        float32

	// Texture file paths recorded at MTL-parse time, the GPU handles being
	// created in Mesh.setup once a backend is available.
	TexturePath      string
	NormalMapPath    string
	RoughnessMapPath string // sampled in .r and multiplied into Roughness
	MetallicMapPath  string // sampled in .r and multiplied into Metallic
	// MTL map_d, multiplied into Alpha
	AlphaMapPath string
	Texture      renderer.ImageHandle
	NormalMap    renderer.ImageHandle
	RoughnessMap renderer.ImageHandle
	MetallicMap  renderer.ImageHandle
	AlphaMap     renderer.ImageHandle
	// Which channel of AlphaMap holds the coverage: .a when the file has an
	// alpha channel, .r for a grayscale mask, which decodes to opaque gray RGB
	AlphaChannel int32
	// Some texel of AlphaMap's channel is below opaque
	alphaMapTranslucent bool
	// The shader-visible slots of the four, resolved once at load: a draw block
	// carries the slot, never the handle
	TextureSlot      int32
	NormalMapSlot    int32
	RoughnessMapSlot int32
	MetallicMapSlot  int32
	AlphaMapSlot     int32
}

// Returns the defaults a material carries before its MTL entry is parsed
//
// Roughness and Ao must not start at zero, or a material with no PBR keys reads
// as a perfect mirror with no ambient light. Nor Diffuse, which tints map_Kd:
// Blender writes no Kd when a texture drives the base colour.
func newMaterial() Material {
	return Material{Diffuse: mgl32.Vec3{1, 1, 1}, Metallic: 0, Roughness: 1, Ao: 1, Alpha: 1}
}

// At or above this a fragment is opaque, below it blended: common.slang's
// OPAQUE_ALPHA, just under 1 as filtering two opaque texels can land a hair below
const OpaqueAlpha = 0.99

// Whether any fragment can reach OpaqueAlpha, which decides if the opaque
// passes and the shadow bakes draw the material
func (mat *Material) opaquePart() bool {
	return mat.Alpha >= OpaqueAlpha
}

// Whether any fragment can fall between 0 and OpaqueAlpha, which decides if
// the transparent pass draws the material and if its opaque draws need the
// pipelines that discard
func (mat *Material) translucentPart() bool {
	return mat.Alpha > 0 && (mat.Alpha < OpaqueAlpha || mat.alphaMapTranslucent)
}
