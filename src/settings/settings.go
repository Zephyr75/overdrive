// Package settings holds the engine's runtime configuration: the values every
// other package reads, their defaults, and the TOML file that overrides them.
//
// They are plain package variables rather than a struct threaded through the
// engine, because they are read from everywhere (camera aspect ratio, shadow
// pass extent, anti-aliasing) and written exactly once, before the window
// exists. Load must therefore run before core.NewApp; nothing re-reads them.
package settings

// AAMode is the anti-aliasing technique the backends set up at initialisation
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

	// The graphics API. Vulkan is the only backend; the key survives so a
	// config naming another one is rejected rather than silently ignored
	Backend string = "vulkan"

	// Anti-aliasing on the backbuffer. Offscreen targets — shadow maps above
	// all — stay single-sampled whatever this says, since a later pass has to
	// sample them.
	//
	// Read once at Backend.Init, because the backend bakes the choice into the
	// swapchain's colour and depth images plus every main-pass pipeline.
	// Changing either value afterwards does nothing.
	AntiAliasing AAMode = AAMSAA
	// Samples per pixel when AntiAliasing is AAMSAA: 2, 4 or 8, Vulkan clamping
	// the request to what the device reports
	MSAASamples int = 4
)

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
