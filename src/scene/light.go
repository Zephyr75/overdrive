package scene

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/utils"
)

// The constant term of the falloff forward.slang implements, 1/(kConstant + d²)
//
// One copy, because lightRadius solves the same expression as the shading does
const lightConstant = float32(1.0)

// Radiance below which a light is treated as contributing nothing
//
// Linear, so conservative rather than perceptual: fsMain's Reinhard plus 1/2.2
// gamma lifts a linear 1/255 to roughly 21/255 on screen
const lightCutoff = float32(1.0 / 255.0)

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
	Type      int        // renderer.LightSun, LightPoint or LightSpot
	Pos       mgl32.Vec3 // shading ignores this for a sun; its shadow camera still sits here
	Dir       mgl32.Vec3 // sun and spot only, points away from the light
	Color     mgl32.Vec3
	Diffuse   float32 // second radiance multiplier beside Intensity
	Intensity float32
	// Cone cosines, not angles: the shader compares them against a dot product
	Cutoff      float32
	OuterCutoff float32
	// Distance past which this light is skipped, 0 for a sun because a
	// directional light does not attenuate. Derived at load from Color,
	// Diffuse and Intensity, so anything mutating those must recompute it
	Radius float32

	// Where this light's tiles are in the shadow atlas: the index of its first
	// record and how many it owns, 1 for a sun or spot and 6 for a point light.
	// -1 and 0 when the allocator gave it none, which lights it unshadowed
	shadowIndex int32
	shadowCount int32
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

	// After the /1000 above, never before: the raw Blender energy would give a
	// radius sqrt(1000) too large
	radius := float32(0)
	if t != renderer.LightSun {
		radius = lightRadius(color, l.Diffuse, intensity)
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
		Radius:      radius,
	}
}

// Solves forward.slang's falloff for where a light drops below lightCutoff
//
// Peak channel, not the average, which would cull a saturated light while its
// strong channel is still visible
func lightRadius(color mgl32.Vec3, diffuse, intensity float32) float32 {
	peak := float32(math.Max(math.Max(float64(color[0]), float64(color[1])),
		float64(color[2]))) * diffuse * intensity
	return float32(math.Sqrt(math.Max(0, float64(peak/lightCutoff-lightConstant))))
}

// Shadow tiles are allocated per frame by the atlas, so a light owns no GPU
// resource of its own any more: scene/shadowatlas.go carries what RenderShadowMap
// and Light.setup used to do.
