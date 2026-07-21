package scene

import (
	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
	"github.com/Zephyr75/overdrive/utils"
)

type LightXml struct {
	Name      string  `xml:"name,attr"`
	Type      string  `xml:"type"`
	Pos       string  `xml:"position"`
	Dir       string  `xml:"direction"`
	Color     string  `xml:"color"`
	Diffuse   float32 `xml:"diffuse"`
	Specular  float32 `xml:"specular"`
	Intensity float32 `xml:"intensity"`
}

type Light struct {
	Name      string
	Type      int // renderer.LightSun or renderer.LightPoint
	Pos       mgl32.Vec3
	Dir       mgl32.Vec3
	Color     mgl32.Vec3
	Diffuse   float32
	Specular  float32
	Intensity float32

	backend      renderer.Backend
	shadowTarget renderer.FramebufferHandle
	depthMap     renderer.TextureHandle // sun: 2D depth map
	depthCubeMap renderer.TextureHandle // point: depth cubemap
	castsShadow  bool                   // set by Scene at load time
}

func (l *Light) Move(x float32, y float32, z float32) {
	l.Pos = l.Pos.Add(mgl32.Vec3{x, y, z})
}

func (l LightXml) toLight() Light {
	t := renderer.LightSun
	name := l.Name
	pos := utils.ParseVec3(l.Pos)
	dir := utils.ParseVec3(l.Dir)
	color := utils.ParseVec3(l.Color)

	pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
	dir = mgl32.Vec3{-dir[0], -dir[2], dir[1]}
	intensity := l.Intensity
	switch l.Type {
	case "sun":
		t = renderer.LightSun
	case "point":
		t = renderer.LightPoint
		intensity /= 1000
	}

	return Light{
		Name:      name,
		Type:      t,
		Pos:       pos,
		Dir:       dir,
		Color:     color,
		Diffuse:   l.Diffuse,
		Specular:  l.Specular,
		Intensity: intensity,
	}
}

// setup allocates this light's shadow map, but only if the scene picked it as
// a caster: shadow maps and their depth passes are the expensive part, so
// non-casters cost nothing beyond the forward-pass lighting term.
func (l *Light) setup(b renderer.Backend, castsShadow bool) {
	l.backend = b
	l.castsShadow = castsShadow
	if !castsShadow {
		return
	}
	if l.Type == renderer.LightSun {
		l.shadowTarget, l.depthMap = b.CreateShadowMap2D(settings.ShadowWidth, settings.ShadowHeight)
	} else {
		l.shadowTarget, l.depthCubeMap = b.CreateShadowCubemap(settings.ShadowWidth, settings.ShadowHeight)
	}
}

// RenderLight runs this light's shadow pass: one BeginPass/EndPass on its
// depth target, drawing every mesh with the matching depth shader. Returns
// the light-space matrix (identity for point lights) for the main pass.
func (l *Light) RenderLight(nearPlane, farPlane float32,
	depthShader, depthCubeShader renderer.ShaderHandle,
	s *Scene, u *renderer.Uniforms) mgl32.Mat4 {

	b := l.backend
	lightSpaceMatrix := mgl32.Ident4()
	u.Model = mgl32.Scale3D(1.0, 1.0, 1.0)

	b.BeginPass(l.shadowTarget, settings.ShadowWidth, settings.ShadowHeight, nil)

	if l.Type == renderer.LightSun {
		lightProjection := mgl32.Ortho(-10.0, 10.0, -10.0, 10.0, nearPlane, farPlane)
		lightView := mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), mgl32.Vec3{0.0, 1.0, 0.0})
		lightSpaceMatrix = lightProjection.Mul4(lightView)
		u.LightSpaceMatrix = lightSpaceMatrix

		// Front-face culling avoids peter-panning on the shadow's near edge.
		b.SetCullFace(true)
		for i := range s.Meshes {
			s.Meshes[i].draw(depthShader, u)
		}
		b.SetCullFace(false)
	} else {
		shadowProjection := mgl32.Perspective(mgl32.DegToRad(90.0), settings.ShadowAspectRatio(), nearPlane, farPlane)
		shadowTransforms := [6]mgl32.Mat4{
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{1.0, 0.0, 0.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{-1.0, 0.0, 0.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, 1.0, 0.0}), mgl32.Vec3{0.0, 0.0, 1.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, -1.0, 0.0}), mgl32.Vec3{0.0, 0.0, -1.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, 0.0, 1.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, 0.0, -1.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
		}

		u.FarPlane = farPlane
		u.LightPos = l.Pos
		u.ShadowMatrices = shadowTransforms

		for i := range s.Meshes {
			s.Meshes[i].draw(depthCubeShader, u)
		}
	}

	b.EndPass()
	return lightSpaceMatrix
}
