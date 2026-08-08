package settings

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Runtime inputs to use for the current execution
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
	Textures struct {
		Anisotropy int
	}
}

// Loads a settings file over the defaults
func Load(path string) error {
	cfg := loadDefaults()

	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	// Explicitly declare misspelt keys
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("settings %s: unknown key %q", path, undecoded[0].String())
	}
	if err := apply(cfg); err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	return nil
}

// Returns the loadDefaults settings in Config form, which is what makes an absent key mean "keep the default"
func loadDefaults() Config {
	var c Config
	c.Window.Width, c.Window.Height = WindowWidth, WindowHeight
	c.Shadows.Width, c.Shadows.Height = ShadowWidth, ShadowHeight
	c.Renderer.Backend = Backend
	c.AntiAliasing.Mode = string(AntiAliasing)
	c.AntiAliasing.Samples = MSAASamples
	c.Textures.Anisotropy = Anisotropy
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

	if err := checkAnisotropy(c.Textures.Anisotropy); err != nil {
		return err
	}

	WindowWidth, WindowHeight = c.Window.Width, c.Window.Height
	ShadowWidth, ShadowHeight = c.Shadows.Width, c.Shadows.Height
	Backend = backend
	AntiAliasing = mode
	MSAASamples = c.AntiAliasing.Samples
	Anisotropy = c.Textures.Anisotropy
	return nil
}

// Accepts the backend names
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

// Rejects sample counts not supported by the backend
func checkSamples(n int) error {
	switch n {
	case 1, 2, 4, 8:
		return nil
	}
	return fmt.Errorf("antialiasing samples must be 1, 2, 4 or 8, got %d", n)
}

// Accepts the anisotropy levels, 1 meaning off; the device limit is clamped later in createSamplers
func checkAnisotropy(n int) error {
	switch n {
	case 1, 2, 4, 8, 16:
		return nil
	}
	return fmt.Errorf("texture anisotropy must be 1, 2, 4, 8 or 16, got %d", n)
}
