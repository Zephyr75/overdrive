package settings

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Loads a settings file over the defaults, changing nothing if any key or value is wrong
func Load(path string) error {
	// Start from fresh defaults rather than a copy of Current, so the decoder
	// never writes into slices Current still shares
	cfg := defaults()

	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	// Explicitly declare misspelt keys
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("settings %s: unknown key %q", path, undecoded[0].String())
	}
	if err := validate(&cfg); err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	Current = cfg
	return nil
}

// Rejects any value the engine cannot use, normalising the backend name in place
func validate(cfg *Config) error {
	if cfg.Window.Width <= 0 || cfg.Window.Height <= 0 {
		return fmt.Errorf("window resolution must be positive, got %dx%d", cfg.Window.Width, cfg.Window.Height)
	}
	if err := checkShadowAtlas(cfg); err != nil {
		return err
	}
	if err := checkPCF(cfg.Shadows.PCF); err != nil {
		return err
	}
	backend, err := normaliseBackend(cfg.Renderer.Backend)
	if err != nil {
		return err
	}
	cfg.Renderer.Backend = backend
	if err := checkAAMode(cfg.AntiAliasing.Mode); err != nil {
		return err
	}
	if cfg.AntiAliasing.Mode == AAMSAA {
		if err := checkSamples(cfg.AntiAliasing.Samples); err != nil {
			return err
		}
	}
	return checkAnisotropy(cfg.Textures.Anisotropy)
}

// Accepts the PCF quality names
func checkPCF(pcf PCFQuality) error {
	switch pcf {
	case PCFFull, PCFCheap:
		return nil
	}
	return fmt.Errorf("unknown shadows.pcf %q, want \"full\" or \"cheap\"", pcf)
}

// Rejects a shadow atlas or slot layout the allocator could not carve
//
// The only place that can: a layout that does not pack silently leaves every
// light with ShadowIndex = -1 and the scene unshadowed
func checkShadowAtlas(cfg *Config) error {
	atlasSize := cfg.Shadows.AtlasSize
	if atlasSize < 1024 || atlasSize > 8192 || atlasSize&(atlasSize-1) != 0 {
		return fmt.Errorf("shadows.atlasSize must be a power of two from 1024 to 8192, got %d", atlasSize)
	}
	div, count := cfg.Shadows.SlotDivisors, cfg.Shadows.SlotCounts
	if len(div) == 0 || len(div) != len(count) {
		return fmt.Errorf("shadows.slotDivisors and slotCounts must be the same non-empty length, got %d and %d",
			len(div), len(count))
	}
	if len(cfg.Shadows.TierScores) != len(div)-1 {
		return fmt.Errorf("shadows.tierScores must have one entry per slot row after the first, want %d, got %d",
			len(div)-1, len(cfg.Shadows.TierScores))
	}
	texels := 0
	for i, divisor := range div {
		// The layout is carved by halving, and buildLayout walks it in Z order:
		// a size that is not a power-of-two division of the atlas has no cell
		if divisor < 2 || divisor > atlasSize || divisor&(divisor-1) != 0 {
			return fmt.Errorf("shadows.slotDivisors[%d] = %d must be a power of two from 2 to %d", i, divisor, atlasSize)
		}
		if i > 0 && divisor <= div[i-1] {
			return fmt.Errorf("shadows.slotDivisors must ascend, so the rows run largest slot first; %d follows %d",
				divisor, div[i-1])
		}
		if count[i] <= 0 {
			return fmt.Errorf("shadows.slotCounts[%d] = %d must be positive", i, count[i])
		}
		texels += count[i] * (atlasSize / divisor) * (atlasSize / divisor)
	}
	if texels > atlasSize*atlasSize {
		return fmt.Errorf("the shadow slot layout asks for %d texels, more than the %d a %d atlas has",
			texels, atlasSize*atlasSize, atlasSize)
	}
	for i, score := range cfg.Shadows.TierScores {
		if score <= 0 {
			return fmt.Errorf("shadows.tierScores[%d] = %v must be positive", i, score)
		}
		if i > 0 && score >= cfg.Shadows.TierScores[i-1] {
			return fmt.Errorf("shadows.tierScores must descend; %v follows %v", score, cfg.Shadows.TierScores[i-1])
		}
	}
	if cfg.Shadows.BakeBudgetMiB < 0 {
		return fmt.Errorf("shadows.bakeBudgetMiB must not be negative, got %d", cfg.Shadows.BakeBudgetMiB)
	}
	if cfg.Shadows.NearPlane <= 0 || cfg.Shadows.FarPlane <= cfg.Shadows.NearPlane {
		return fmt.Errorf("shadows.farPlane must exceed nearPlane and both must be positive, got %v and %v",
			cfg.Shadows.NearPlane, cfg.Shadows.FarPlane)
	}
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
func checkAAMode(mode AAMode) error {
	switch mode {
	case AANone, AAMSAA:
		return nil
	}
	return fmt.Errorf("unknown antialiasing mode %q, want \"msaa\" or \"none\"", mode)
}

// Rejects sample counts not supported by the backend
func checkSamples(samples int) error { 
	switch samples {
	case 1, 2, 4, 8:
		return nil
	}
	return fmt.Errorf("antialiasing samples must be 1, 2, 4 or 8, got %d", samples)
}

// Accepts the anisotropy levels, 1 meaning off; the device limit is clamped later in createSamplers
func checkAnisotropy(level int) error { 
	switch level {
	case 1, 2, 4, 8, 16:
		return nil
	}
	return fmt.Errorf("texture anisotropy must be 1, 2, 4, 8 or 16, got %d", level)
}
