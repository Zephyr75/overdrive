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

	// Anisotropic filtering on material textures: 1 (off), 2, 4, 8 or 16
	//
	// Clamped to the device's maxSamplerAnisotropy when the sampler is created,
	// so a file asking for more than the GPU allows is quietly lowered rather
	// than rejected.
	Anisotropy int = 8
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
