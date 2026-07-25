package scene

import (
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

type Skybox struct {
	mesh    renderer.MeshHandle
	Texture renderer.TextureHandle
}

// Uploads the skybox cube and loads its six face images as a cubemap
func (s *Skybox) setup(b renderer.Backend) {
	vertices := []float32{
		// positions
		-1.0, 1.0, -1.0,
		-1.0, -1.0, -1.0,
		1.0, -1.0, -1.0,
		1.0, -1.0, -1.0,
		1.0, 1.0, -1.0,
		-1.0, 1.0, -1.0,

		-1.0, -1.0, 1.0,
		-1.0, -1.0, -1.0,
		-1.0, 1.0, -1.0,
		-1.0, 1.0, -1.0,
		-1.0, 1.0, 1.0,
		-1.0, -1.0, 1.0,

		1.0, -1.0, -1.0,
		1.0, -1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, -1.0,
		1.0, -1.0, -1.0,

		-1.0, -1.0, 1.0,
		-1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		1.0, -1.0, 1.0,
		-1.0, -1.0, 1.0,

		-1.0, 1.0, -1.0,
		1.0, 1.0, -1.0,
		1.0, 1.0, 1.0,
		1.0, 1.0, 1.0,
		-1.0, 1.0, 1.0,
		-1.0, 1.0, -1.0,

		-1.0, -1.0, -1.0,
		-1.0, -1.0, 1.0,
		1.0, -1.0, -1.0,
		1.0, -1.0, -1.0,
		-1.0, -1.0, 1.0,
		1.0, -1.0, 1.0,
	}

	s.mesh = b.CreateSkyboxMesh(vertices)
	tex, err := b.LoadCubemap([6]string{
		"./textures/skybox/right.png",
		"./textures/skybox/left.png",
		"./textures/skybox/top.png",
		"./textures/skybox/bottom.png",
		"./textures/skybox/front.png",
		"./textures/skybox/back.png",
	})
	if err != nil {
		println("Error loading skybox:", err.Error())
	}
	s.Texture = tex
}

// Draws the skybox first in the main pass, with a depth test that lets it fill the far plane
func (s *Scene) RenderSkybox(shader renderer.ShaderHandle, u *renderer.Uniforms) {
	// Strip the view translation, so the skybox follows the camera
	view := mgl32.LookAtV(s.Cam.Pos, s.Cam.Pos.Add(s.Cam.Front), s.Cam.Up)
	u.View = view.Mat3().Mat4()
	u.Projection = mgl32.Perspective(mgl32.DegToRad(s.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
	u.TexSkybox = s.Skybox.Texture

	s.backend.SetDepthFunc(true) // depth <= 1.0 passes, so the far plane is drawable
	s.backend.DrawSkybox(shader, s.Skybox.mesh, u)
	s.backend.SetDepthFunc(false)
}
