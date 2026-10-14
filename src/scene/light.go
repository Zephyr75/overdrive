package scene

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

// The constant term of the falloff forward.slang implements, 1/(kConstant + d²)
//
// One copy, because lightRadius solves the same expression as the shading does.
// Only a guard against d = 0, so small: 1 dimmed a light half a metre away 5x
const lightConstant = float32(0.01)

// Blender's point and spot power in watts to the engine's intensity in W/sr:
// spread over the whole sphere, a spot being a point light with a cone mask
const wattsToIntensity = float32(1 / (4 * math.Pi))

// Radiance below which a light is treated as contributing nothing
//
// Linear, so conservative rather than perceptual: fsMain's Reinhard plus 1/2.2
// gamma lifts a linear 1/255 to roughly 21/255 on screen
const lightCutoff = float32(1.0 / 255.0)

type LightToml struct {
	Name      string     `toml:"name"`
	Type      string     `toml:"type"`
	Pos       mgl32.Vec3 `toml:"position"`
	Dir       mgl32.Vec3 `toml:"direction"`
	Color     mgl32.Vec3 `toml:"color"`
	Diffuse   float32    `toml:"diffuse"`
	Intensity float32    `toml:"intensity"`
	// Blender's spot terms, kept in its units so the export round-trips: Cone is
	// the full cone angle in degrees, ConeBlend the 0..1 soft-edge fraction
	Cone      float32 `toml:"cone"`
	ConeBlend float32 `toml:"coneBlend"`
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

	// Where a sun's shadow box is centred, which followCamera moves in steps;
	// shadowCentred is false until the first frame has placed it
	shadowCentre  mgl32.Vec3
	shadowCentered bool
}

// Offsets the light's position; its shadow follows on the next frame, like any change to Pos or Dir
func (light *Light) Move(x float32, y float32, z float32) {
	light.Pos = light.Pos.Add(mgl32.Vec3{x, y, z})
}

// What a light's tiles are projected from: when any of it changes, every tile it holds is stale
type shadowPose struct {
	pos, dir    mgl32.Vec3
	outerCutoff float32
}

// The fields of the light its shadow tiles depend on, so a light the game
// moved or turned is re-baked whether it went through Move or set Pos directly
func (light *Light) shadowPose() shadowPose {
	switch light.Type {
	case renderer.LightSun:
		// A sun's position means nothing to its shadow: the box sits on the camera
		return shadowPose{pos: light.shadowCentre, dir: light.Dir}
	case renderer.LightPoint:
		return shadowPose{pos: light.Pos}
	}
	return shadowPose{light.Pos, light.Dir, light.OuterCutoff}
}

// Re-centres a sun's shadow box on the camera, in whole steps of half the box,
// and only once the camera is three quarters of a step from the centre
//
// A box sliding with the camera would re-bake the static atlas every frame,
// and one snapping at the midpoint would flip back and forth across it. This
// way the camera stays within 0.375 half-extents of the centre on each axis
func (light *Light) followCamera(camPos mgl32.Vec3, halfExtent float32) {
	step := halfExtent / 2
	for k := range 3 {
		if !light.shadowCentered || math.Abs(float64(camPos[k]-light.shadowCentre[k])) > 0.75*float64(step) {
			light.shadowCentre[k] = float32(math.Round(float64(camPos[k]/step))) * step
		}
	}
	light.shadowCentered = true
}

// Converts a parsed light into engine coordinates and units
func (light LightToml) toLight() Light {
	kind := renderer.LightSun
	name := light.Name
	pos, dir, color := light.Pos, light.Dir, light.Color

	pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
	dir = mgl32.Vec3{-dir[0], -dir[2], dir[1]}
	intensity := light.Intensity
	// Cone cosines, only meaningful for a spot. 1 and 1 make the smoothstep
	// degenerate rather than lighting nothing, so a malformed spot is visible
	cutoff, outerCutoff := float32(1.0), float32(1.0)
	switch light.Type {
	case "sun":
		kind = renderer.LightSun
	case "point":
		kind = renderer.LightPoint
		intensity *= wattsToIntensity
	case "spot":
		kind = renderer.LightSpot
		intensity *= wattsToIntensity
		// Blender gives the full cone angle; the shader compares a half-angle
		// cosine against dot(-lightDir, direction)
		outer := mgl32.DegToRad(light.Cone) * 0.5
		outerCutoff = float32(math.Cos(float64(outer)))
		cutoff = float32(math.Cos(float64(outer * (1.0 - light.ConeBlend))))
	}

	// After the watts conversion above, never before: the raw Blender energy
	// would give a radius sqrt(4 pi) too large
	radius := float32(0)
	if kind != renderer.LightSun {
		radius = lightRadius(color, light.Diffuse, intensity)
	}

	return Light{
		Name:        name,
		Type:        kind,
		Pos:         pos,
		Dir:         dir,
		Color:       color,
		Diffuse:     light.Diffuse,
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
