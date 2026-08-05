package settings

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk form of the package's variables, one TOML file passed
// to the binary with -config
//
// It exists so that resolution and anti-aliasing are runtime inputs: one build
// runs at any resolution. Every field is optional — decoding starts from the
// defaults above, so a file that names only what it changes is valid.
type Config struct {
	Window struct {
		Width  int
		Height int
	}
	Shadows struct {
		Width  int
		Height int
	}
	Renderer struct {
		Backend string
	}
	AntiAliasing struct {
		Mode    string
		Samples int
	} `toml:"antialiasing"`
}

// Loads a settings file over the defaults
//
// The file is the only source: no environment variable competes with it, so what
// a run was configured with is always readable from the file it was given.
func Load(path string) error {
	cfg := current()

	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	// A misspelt key would otherwise be silently ignored, which is the worst
	// possible outcome for a file whose whole job is to change behaviour
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("settings %s: unknown key %q", path, undecoded[0].String())
	}
	if err := apply(cfg); err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	return nil
}

// Returns the current settings in Config form, which is what makes an absent key mean "keep the default"
func current() Config {
	var c Config
	c.Window.Width, c.Window.Height = WindowWidth, WindowHeight
	c.Shadows.Width, c.Shadows.Height = ShadowWidth, ShadowHeight
	c.Renderer.Backend = Backend
	c.AntiAliasing.Mode = string(AntiAliasing)
	c.AntiAliasing.Samples = MSAASamples
	return c
}

// Validates a decoded config and writes it into the package variables, rejecting the whole file if any value is wrong
func apply(c Config) error {
	if c.Window.Width <= 0 || c.Window.Height <= 0 {
		return fmt.Errorf("window resolution must be positive, got %dx%d", c.Window.Width, c.Window.Height)
	}
	if c.Shadows.Width <= 0 || c.Shadows.Height <= 0 {
		return fmt.Errorf("shadow resolution must be positive, got %dx%d", c.Shadows.Width, c.Shadows.Height)
	}

	backend, err := normaliseBackend(c.Renderer.Backend)
	if err != nil {
		return err
	}
	mode, err := normaliseAAMode(c.AntiAliasing.Mode)
	if err != nil {
		return err
	}
	if mode == AAMSAA {
		if err := checkSamples(c.AntiAliasing.Samples); err != nil {
			return err
		}
	}

	WindowWidth, WindowHeight = c.Window.Width, c.Window.Height
	ShadowWidth, ShadowHeight = c.Shadows.Width, c.Shadows.Height
	Backend = backend
	AntiAliasing = mode
	MSAASamples = c.AntiAliasing.Samples
	return nil
}

// Accepts the backend names
//
// Vulkan is the only backend, so this only rejects. It is kept so that a config
// still naming OpenGL fails with an explanation rather than running Vulkan
// silently, and so a second backend has a place to be named later.
func normaliseBackend(name string) (string, error) {
	switch name {
	case "", "vulkan", "vk":
		return "vulkan", nil
	}
	return "", fmt.Errorf("unknown backend %q, want \"vulkan\"", name)
}

// Accepts the anti-aliasing mode names
func normaliseAAMode(mode string) (AAMode, error) {
	switch AAMode(mode) {
	case AANone:
		return AANone, nil
	case AAMSAA:
		return AAMSAA, nil
	}
	return "", fmt.Errorf("unknown antialiasing mode %q, want \"msaa\" or \"none\"", mode)
}

// Rejects sample counts no backend can honour, 8 being the highest Vulkan will try before clamping to the device's limit
func checkSamples(n int) error {
	switch n {
	case 1, 2, 4, 8:
		return nil
	}
	return fmt.Errorf("antialiasing samples must be 1, 2, 4 or 8, got %d", n)
}
