package scene

import (
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/paths"
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

	// The cube owns its own buffer and carries no indices, unlike scene meshes
	// which share one buffer across their face groups
	s.mesh = b.CreateMesh(b.CreateBuffer(vertices), nil, renderer.LayoutPosition)
	faces, w, h, err := loadCubeFaces([6]string{
		paths.Texture("skybox/right.png"),
		paths.Texture("skybox/left.png"),
		paths.Texture("skybox/top.png"),
		paths.Texture("skybox/bottom.png"),
		paths.Texture("skybox/front.png"),
		paths.Texture("skybox/back.png"),
	})
	if err != nil {
		println("Error loading skybox:", err.Error())
		return
	}
	s.Texture = b.CreateCubemap(faces, w, h)
}

// Draws the skybox first in the main pass, with a depth test that lets it fill the far plane
func (s *Scene) RenderSkybox(shader renderer.ShaderHandle, f *renderer.FrameUniforms) {
	// Bind a copy of the pass block with the view translation stripped, so the
	// skybox follows the camera. RenderScene rebinds the real one afterwards
	sky := *f
	view := mgl32.LookAtV(s.Cam.Pos, s.Cam.Pos.Add(s.Cam.Front), s.Cam.Up)
	sky.View = view.Mat3().Mat4()
	sky.Projection = mgl32.Perspective(mgl32.DegToRad(s.Cam.Fov),
		float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
	sky.TexSkybox = s.Skybox.Texture
	s.backend.BindFrameUniforms(&sky)

	u := renderer.DrawUniforms{Model: mgl32.Ident4()}
	// Ties pass, so the cube can sit exactly on the far plane
	s.backend.SetDepthCompare(renderer.CompareLessEqual)
	s.backend.BindShader(shader)
	s.backend.Draw(s.Skybox.mesh, &u)
	s.backend.SetDepthCompare(renderer.CompareLess)
}
