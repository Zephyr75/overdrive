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
	shadowTarget renderer.RenderTargetHandle
	depthMap     renderer.TextureHandle // sun: 2D depth map
	depthCubeMap renderer.TextureHandle // point: depth cubemap
	castsShadow  bool                   // set by Scene at load time
}

// Offsets the light's position
func (l *Light) Move(x float32, y float32, z float32) {
	l.Pos = l.Pos.Add(mgl32.Vec3{x, y, z})
}

// Converts a parsed XML light into engine coordinates and units
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

// Allocates this light's shadow map, but only when the scene picked it as a caster
//
// Shadow maps and their depth passes are the expensive part, so non-casters
// cost nothing beyond the forward-pass lighting term.
func (l *Light) setup(b renderer.Backend, castsShadow bool) {
	l.backend = b
	l.castsShadow = castsShadow
	if !castsShadow {
		return
	}
	spec := renderer.RenderTargetSpec{
		Width:  settings.ShadowWidth,
		Height: settings.ShadowHeight,
		Format: renderer.TargetDepth,
		Cube:   l.Type != renderer.LightSun,
	}
	if l.Type == renderer.LightSun {
		l.shadowTarget, l.depthMap = b.CreateRenderTarget(spec)
	} else {
		l.shadowTarget, l.depthCubeMap = b.CreateRenderTarget(spec)
	}
}

// Bakes this light's shadow map, drawing every mesh into its depth target with the matching depth shader
//
// It renders the scene from the light's point of view, it does not render the
// light itself. The matrices it needs are left behind in f — the sun's
// light-space matrix for the main pass, the six face matrices for the geometry
// stage — so there is nothing to return.
func (l *Light) RenderShadowMap(nearPlane, farPlane float32,
	depthShader, depthCubeShader renderer.ShaderHandle,
	s *Scene, f *renderer.FrameUniforms) {

	b := l.backend

	// Static mesh geometry is baked into the OBJ vertices, so the depth passes
	// draw everything with an identity model matrix and no material at all
	u := renderer.DrawUniforms{Model: mgl32.Ident4()}

	b.BeginPass(l.shadowTarget, nil)

	if l.Type == renderer.LightSun { // TODO: enum
		lightProjection := mgl32.Ortho(-10.0, 10.0, -10.0, 10.0, nearPlane, farPlane)
		lightView := mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), mgl32.Vec3{0.0, 1.0, 0.0})
		f.LightSpaceMatrix = lightProjection.Mul4(lightView)
		b.BindFrameUniforms(f)

		// Cull front faces, which avoids peter-panning on the shadow's near edge
		b.SetCullMode(renderer.CullFront)
		b.BindShader(depthShader)
		for i := range s.Meshes {
			s.Meshes[i].draw(&u)
		}
		b.SetCullMode(renderer.CullBack)
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

		f.FarPlane = farPlane
		f.LightPos = l.Pos
		f.ShadowMatrices = shadowTransforms
		b.BindFrameUniforms(f)

		b.BindShader(depthCubeShader)
		for i := range s.Meshes {
			s.Meshes[i].draw(&u)
		}
	}

	b.EndPass()
}
