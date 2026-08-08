package scene

import (
	"math"

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
	Intensity float32 `xml:"intensity"`
	// Blender's spot terms, kept in its units so the export round-trips: Cone is
	// the full cone angle in degrees, ConeBlend the 0..1 soft-edge fraction
	Cone      float32 `xml:"cone"`
	ConeBlend float32 `xml:"coneBlend"`
}

type Light struct {
	Name      string
	Type      int // renderer.LightSun, LightPoint or LightSpot
	Pos       mgl32.Vec3
	Dir       mgl32.Vec3
	Color     mgl32.Vec3
	Diffuse   float32
	Intensity float32
	// Cone cosines, not angles: the shader compares them against a dot product
	Cutoff      float32
	OuterCutoff float32

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
	// Cone cosines, only meaningful for a spot. 1 and 1 make the smoothstep
	// degenerate rather than lighting nothing, so a malformed spot is visible
	cutoff, outerCutoff := float32(1.0), float32(1.0)
	switch l.Type {
	case "sun":
		t = renderer.LightSun
	case "point":
		t = renderer.LightPoint
		intensity /= 1000
	case "spot":
		t = renderer.LightSpot
		intensity /= 1000
		// Blender gives the full cone angle; the shader compares a half-angle
		// cosine against dot(-lightDir, direction)
		outer := mgl32.DegToRad(l.Cone) * 0.5
		outerCutoff = float32(math.Cos(float64(outer)))
		cutoff = float32(math.Cos(float64(outer * (1.0 - l.ConeBlend))))
	}

	return Light{
		Name:        name,
		Type:        t,
		Pos:         pos,
		Dir:         dir,
		Color:       color,
		Diffuse:     l.Diffuse,
		Intensity:   intensity,
		Cutoff:      cutoff,
		OuterCutoff: outerCutoff,
	}
}

// Allocates this light's shadow map, but only when the scene picked it as a caster
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

// Bakes this light's shadow map, drawing every mesh into its depth target
//
// Renders the scene from the light's point of view, not the light itself. The
// matrices are left behind in f rather than returned.
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
