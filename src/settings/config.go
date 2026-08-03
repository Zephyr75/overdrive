package settings

import (
	"fmt"
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk form of the package's variables, one TOML file passed
// to the binary with -config
//
// It exists so that resolution, backend and anti-aliasing are runtime inputs:
// the same build runs on either backend at any resolution. Every field is
// optional — decoding starts from the defaults above, so a file that names only
// what it changes is valid.
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

// Loads a settings file over the defaults, then lets the environment override both
//
// The environment wins because it is the more specific instruction: a config
// file is the project's setup, OVERDRIVE_BACKEND / OVERDRIVE_MSAA are one run's.
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

	applyEnv()
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

// Accepts the backend names the engine has always taken from OVERDRIVE_BACKEND, folding the aliases
func normaliseBackend(name string) (string, error) {
	switch name {
	case "", "gl", "opengl":
		return "gl", nil
	case "vulkan", "vk":
		return "vulkan", nil
	}
	return "", fmt.Errorf("unknown backend %q, want \"gl\" or \"vulkan\"", name)
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

// Applies the per-run environment overrides, which are the file's values' only competition
//
// Invalid values are ignored rather than fatal: an environment variable is a
// convenience for one run, and the config file has already given a usable answer.
func applyEnv() {
	if v, ok := os.LookupEnv("OVERDRIVE_BACKEND"); ok {
		if name, err := normaliseBackend(v); err == nil {
			Backend = name
		}
	}
	// A sample count, 0 or 1 meaning "no anti-aliasing" — the one variable
	// covers both the mode and the count, since MSAA is the only mode there is
	if v, ok := os.LookupEnv("OVERDRIVE_MSAA"); ok {
		if n, err := strconv.Atoi(v); err == nil && checkSamples(n) == nil {
			MSAASamples = n
			if n > 1 {
				AntiAliasing = AAMSAA
			} else {
				AntiAliasing = AANone
			}
		}
	}
}
