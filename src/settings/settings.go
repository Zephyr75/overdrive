// Package settings holds the engine's runtime configuration: the values every
// other package reads, their defaults, and the TOML file that overrides them.
//
// They are plain package variables rather than a struct threaded through the
// engine, because they are read from everywhere (camera aspect ratio, shadow
// pass extent, backend selection) and written exactly once, before the window
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

	// Which renderer.Backend core.createBackend builds: "gl" or "vulkan"
	Backend string = "gl"

	// Anti-aliasing on the backbuffer. Offscreen targets — shadow maps above
	// all — stay single-sampled whatever this says, since a later pass has to
	// sample them.
	//
	// Read once at Backend.Init, because both backends bake the choice into an
	// object they create there: GL into the window's default framebuffer,
	// Vulkan into the swapchain's colour and depth images plus every main-pass
	// pipeline. Changing either value afterwards does nothing.
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
