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

	// Texture file paths recorded at MTL-parse time; the GPU handles are
	// created in Mesh.setup once a backend is available.
	TexturePath   string
	NormalMapPath string
	Texture       renderer.TextureHandle
	NormalMap     renderer.TextureHandle
}
