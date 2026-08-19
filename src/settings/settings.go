// Package settings holds the engine's runtime configuration:
package settings

// Anti-aliasing technique: None or MSAA
type AAMode string

const (
	AANone AAMode = "none"
	AAMSAA AAMode = "msaa"
)

var (
	WindowWidth  int = 1920
	WindowHeight int = 1080
	ShadowWidth  int = 1024
	ShadowHeight int = 1024

	// The graphics API: Vulkan is the only backend implemented so far
	Backend string = "vulkan"

	AntiAliasing AAMode = AAMSAA
	// Samples per pixel when AntiAliasing is AAMSAA: 2, 4 or 8
	MSAASamples int = 4

	// Anisotropic filtering on material textures: 1 (off), 2, 4, 8 or 16.
	// Lowered to the device limit at sampler creation rather than rejected
	Anisotropy int = 8
)

// The [debug] section: switches that change how a run is inspected, never what
// it renders — except NoShadows, which is an A/B and says so.
//
// These were environment variables until 2026-08-16. They are settings like any
// other now, so a run's whole configuration is readable from the one file it was
// given rather than from the shell history that launched it.
var (
	// Vulkan validation layers. Costs frame rate, reports API misuse
	Validation bool = false
	// Freeze the camera where the scene put it: no mouse look, no WASD.
	// Reproducible captures need it, the compositor otherwise delivering a
	// cursor event of its own choosing on the first frames
	LockCamera bool = false
	// Light every light unshadowed, while still baking every tile.
	// The A/B that separates "the scene is dark" from "every light is wrongly
	// occluded" — two failures that render identically and share no code
	NoShadows bool = false
)

// Reports whether material textures are sampled anisotropically, 1 meaning plain isotropic filtering
func AnisotropyEnabled() bool {
	return Anisotropy > 1
}

// Reports whether the backbuffer is multisampled, which is MSAA asked for and a count that actually multisamples
func MSAAEnabled() bool {
	return AntiAliasing == AAMSAA && MSAASamples > 1
}

// Returns the window's aspect ratio, for the camera projection
func AspectRatio() float32 {
	return float32(WindowWidth) / float32(WindowHeight)
}

// Returns the shadow map's aspect ratio, for the cube shadow projection
func ShadowAspectRatio() float32 {
	return float32(ShadowWidth) / float32(ShadowHeight)
}
