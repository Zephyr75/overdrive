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
		AtlasSize     int
		SlotDivisors  []int
		SlotCounts    []int
		TierScores    []float64
		DynamicAtlas  bool
		BakeBudgetMiB int
		PCF           string
		NearPlane     float64
		FarPlane      float64
	}
	Renderer struct {
		Backend      string
		DepthPrepass bool
	}
	AntiAliasing struct {
		Mode    string
		Samples int
	} `toml:"antialiasing"`
	Textures struct {
		Anisotropy int
	}
	// Inspection switches, not rendering ones. Kept in the file rather than in
	// environment variables so a run's whole configuration is readable from it
	Debug struct {
		Validation bool
		LockCamera bool
		NoShadows  bool
	}
}

// Loads a settings file over the defaults
func Load(path string) error { // TODO: review
	cfg := loadDefaults()

	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	// Explicitly declare misspelt keys
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("settings %s: unknown key %q", path, undecoded[0].String())
	}
	if err := apply(cfg); err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	return nil
}

// Returns the loadDefaults settings in Config form, which is what makes an absent key mean "keep the default"
func loadDefaults() Config { // TODO: review
	var defaults Config
	defaults.Window.Width, defaults.Window.Height = WindowWidth, WindowHeight
	defaults.Shadows.AtlasSize = ShadowAtlasSize
	defaults.Shadows.SlotDivisors = ShadowSlotDivisors
	defaults.Shadows.SlotCounts = ShadowSlotCounts
	defaults.Shadows.TierScores = make([]float64, len(ShadowTierScores))
	for i, score := range ShadowTierScores {
		defaults.Shadows.TierScores[i] = float64(score)
	}
	defaults.Shadows.DynamicAtlas = ShadowDynamicAtlas
	defaults.Shadows.BakeBudgetMiB = ShadowBakeBudgetMiB
	defaults.Shadows.PCF = string(ShadowPCF)
	defaults.Shadows.NearPlane = float64(ShadowNearPlane)
	defaults.Shadows.FarPlane = float64(ShadowFarPlane)
	defaults.Renderer.Backend = Backend
	defaults.Renderer.DepthPrepass = DepthPrepass
	defaults.AntiAliasing.Mode = string(AntiAliasing)
	defaults.AntiAliasing.Samples = MSAASamples
	defaults.Textures.Anisotropy = Anisotropy
	defaults.Debug.Validation = Validation
	defaults.Debug.LockCamera = LockCamera
	defaults.Debug.NoShadows = NoShadows
	return defaults
}

// Validates a decoded config and writes it into the package variables, rejecting the whole file if any value is wrong
func apply(cfg Config) error { // TODO: review
	if cfg.Window.Width <= 0 || cfg.Window.Height <= 0 {
		return fmt.Errorf("window resolution must be positive, got %dx%d", cfg.Window.Width, cfg.Window.Height)
	}
	if err := checkShadowAtlas(cfg); err != nil {
		return err
	}
	pcf, err := normalisePCF(cfg.Shadows.PCF)
	if err != nil {
		return err
	}

	backend, err := normaliseBackend(cfg.Renderer.Backend)
	if err != nil {
		return err
	}
	mode, err := normaliseAAMode(cfg.AntiAliasing.Mode)
	if err != nil {
		return err
	}
	if mode == AAMSAA {
		if err := checkSamples(cfg.AntiAliasing.Samples); err != nil {
			return err
		}
	}

	if err := checkAnisotropy(cfg.Textures.Anisotropy); err != nil {
		return err
	}
	WindowWidth, WindowHeight = cfg.Window.Width, cfg.Window.Height
	ShadowAtlasSize = cfg.Shadows.AtlasSize
	ShadowSlotDivisors, ShadowSlotCounts = cfg.Shadows.SlotDivisors, cfg.Shadows.SlotCounts
	ShadowTierScores = make([]float32, len(cfg.Shadows.TierScores))
	for i, score := range cfg.Shadows.TierScores {
		ShadowTierScores[i] = float32(score)
	}
	ShadowDynamicAtlas = cfg.Shadows.DynamicAtlas
	ShadowBakeBudgetMiB = cfg.Shadows.BakeBudgetMiB
	ShadowPCF = pcf
	ShadowNearPlane = float32(cfg.Shadows.NearPlane)
	ShadowFarPlane = float32(cfg.Shadows.FarPlane)
	Backend = backend
	DepthPrepass = cfg.Renderer.DepthPrepass
	AntiAliasing = mode
	MSAASamples = cfg.AntiAliasing.Samples
	Anisotropy = cfg.Textures.Anisotropy
	Validation = cfg.Debug.Validation
	LockCamera = cfg.Debug.LockCamera
	NoShadows = cfg.Debug.NoShadows
	return nil
}

// Accepts the PCF quality names
func normalisePCF(name string) (PCFQuality, error) { // TODO: review
	switch PCFQuality(name) {
	case PCFFull:
		return PCFFull, nil
	case PCFCheap:
		return PCFCheap, nil
	}
	return "", fmt.Errorf("unknown shadows.pcf %q, want \"full\" or \"cheap\"", name)
}

// Rejects a shadow atlas or slot layout the allocator could not carve
//
// The only place that can: a layout that does not pack silently leaves every
// light with ShadowIndex = -1 and the scene unshadowed
func checkShadowAtlas(cfg Config) error { // TODO: review
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
func normaliseBackend(name string) (string, error) { // TODO: review
	switch name {
	case "", "vulkan", "vk":
		return "vulkan", nil
	}
	return "", fmt.Errorf("unknown backend %q, want \"vulkan\"", name)
}

// Accepts the anti-aliasing mode names
func normaliseAAMode(mode string) (AAMode, error) { // TODO: review
	switch AAMode(mode) {
	case AANone:
		return AANone, nil
	case AAMSAA:
		return AAMSAA, nil
	}
	return "", fmt.Errorf("unknown antialiasing mode %q, want \"msaa\" or \"none\"", mode)
}

// Rejects sample counts not supported by the backend
func checkSamples(samples int) error { // TODO: review
	switch samples {
	case 1, 2, 4, 8:
		return nil
	}
	return fmt.Errorf("antialiasing samples must be 1, 2, 4 or 8, got %d", samples)
}

// Accepts the anisotropy levels, 1 meaning off; the device limit is clamped later in createSamplers
func checkAnisotropy(level int) error { // TODO: review
	switch level {
	case 1, 2, 4, 8, 16:
		return nil
	}
	return fmt.Errorf("texture anisotropy must be 1, 2, 4, 8 or 16, got %d", level)
}
