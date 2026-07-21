package scene

import (
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

type Material struct {
	Alpha     float32
	Ambient   mgl32.Vec3
	Diffuse   mgl32.Vec3
	Specular  mgl32.Vec3
	Shininess float32

	// Metallic-roughness PBR scalars consumed by forward.slang's Cook-Torrance
	// BRDF. Diffuse doubles as the base colour / albedo.
	Metallic  float32
	Roughness float32
	Ao        float32

	// Texture file paths recorded at MTL-parse time; the GPU handles are
	// created in Mesh.setup once a backend is available.
	TexturePath   string
	NormalMapPath string
	Texture       renderer.TextureHandle
	NormalMap     renderer.TextureHandle
}

// newMaterial returns the defaults a material carries before its MTL entry is
// parsed. Roughness and Ao must not start at zero: a legacy material with no
// PBR keys would otherwise read as a perfect mirror with no ambient light.
// Matches the C++ engine's dielectric/matte default.
func newMaterial() Material {
	return Material{Metallic: 0, Roughness: 1, Ao: 1}
}
