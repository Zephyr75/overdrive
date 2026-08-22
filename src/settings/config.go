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
	c.Shadows.AtlasSize = ShadowAtlasSize
	c.Shadows.SlotDivisors = ShadowSlotDivisors
	c.Shadows.SlotCounts = ShadowSlotCounts
	c.Shadows.TierScores = make([]float64, len(ShadowTierScores))
	for i, v := range ShadowTierScores {
		c.Shadows.TierScores[i] = float64(v)
	}
	c.Shadows.DynamicAtlas = ShadowDynamicAtlas
	c.Shadows.BakeBudgetMiB = ShadowBakeBudgetMiB
	c.Shadows.PCF = string(ShadowPCF)
	c.Shadows.NearPlane = float64(ShadowNearPlane)
	c.Shadows.FarPlane = float64(ShadowFarPlane)
	c.Renderer.Backend = Backend
	c.Renderer.DepthPrepass = DepthPrepass
	c.AntiAliasing.Mode = string(AntiAliasing)
	c.AntiAliasing.Samples = MSAASamples
	c.Textures.Anisotropy = Anisotropy
	c.Debug.Validation = Validation
	c.Debug.LockCamera = LockCamera
	c.Debug.NoShadows = NoShadows
	return c
}

// Validates a decoded config and writes it into the package variables, rejecting the whole file if any value is wrong
func apply(c Config) error {
	if c.Window.Width <= 0 || c.Window.Height <= 0 {
		return fmt.Errorf("window resolution must be positive, got %dx%d", c.Window.Width, c.Window.Height)
	}
	if err := checkShadowAtlas(c); err != nil {
		return err
	}
	pcf, err := normalisePCF(c.Shadows.PCF)
	if err != nil {
		return err
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
	ShadowAtlasSize = c.Shadows.AtlasSize
	ShadowSlotDivisors, ShadowSlotCounts = c.Shadows.SlotDivisors, c.Shadows.SlotCounts
	ShadowTierScores = make([]float32, len(c.Shadows.TierScores))
	for i, v := range c.Shadows.TierScores {
		ShadowTierScores[i] = float32(v)
	}
	ShadowDynamicAtlas = c.Shadows.DynamicAtlas
	ShadowBakeBudgetMiB = c.Shadows.BakeBudgetMiB
	ShadowPCF = pcf
	ShadowNearPlane = float32(c.Shadows.NearPlane)
	ShadowFarPlane = float32(c.Shadows.FarPlane)
	Backend = backend
	DepthPrepass = c.Renderer.DepthPrepass
	AntiAliasing = mode
	MSAASamples = c.AntiAliasing.Samples
	Anisotropy = c.Textures.Anisotropy
	Validation = c.Debug.Validation
	LockCamera = c.Debug.LockCamera
	NoShadows = c.Debug.NoShadows
	return nil
}

// Accepts the PCF quality names
func normalisePCF(name string) (PCFQuality, error) {
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
// Every check here was an init() panic in scene/shadowatlas.go while the layout
// was compile-time. A layout that does not pack is not an error anywhere else:
// allocation simply refuses, every light ends up with ShadowIndex = -1, and the
// scene renders unshadowed with nothing logged
func checkShadowAtlas(c Config) error {
	n := c.Shadows.AtlasSize
	if n < 1024 || n > 8192 || n&(n-1) != 0 {
		return fmt.Errorf("shadows.atlasSize must be a power of two from 1024 to 8192, got %d", n)
	}
	div, count := c.Shadows.SlotDivisors, c.Shadows.SlotCounts
	if len(div) == 0 || len(div) != len(count) {
		return fmt.Errorf("shadows.slotDivisors and slotCounts must be the same non-empty length, got %d and %d",
			len(div), len(count))
	}
	if len(c.Shadows.TierScores) != len(div)-1 {
		return fmt.Errorf("shadows.tierScores must have one entry per slot row after the first, want %d, got %d",
			len(div)-1, len(c.Shadows.TierScores))
	}
	texels := 0
	for i, d := range div {
		// The layout is carved by halving, and buildLayout walks it in Z order:
		// a size that is not a power-of-two division of the atlas has no cell
		if d < 2 || d > n || d&(d-1) != 0 {
			return fmt.Errorf("shadows.slotDivisors[%d] = %d must be a power of two from 2 to %d", i, d, n)
		}
		if i > 0 && d <= div[i-1] {
			return fmt.Errorf("shadows.slotDivisors must ascend, so the rows run largest slot first; %d follows %d",
				d, div[i-1])
		}
		if count[i] <= 0 {
			return fmt.Errorf("shadows.slotCounts[%d] = %d must be positive", i, count[i])
		}
		texels += count[i] * (n / d) * (n / d)
	}
	if texels > n*n {
		return fmt.Errorf("the shadow slot layout asks for %d texels, more than the %d a %d atlas has",
			texels, n*n, n)
	}
	for i, sc := range c.Shadows.TierScores {
		if sc <= 0 {
			return fmt.Errorf("shadows.tierScores[%d] = %v must be positive", i, sc)
		}
		if i > 0 && sc >= c.Shadows.TierScores[i-1] {
			return fmt.Errorf("shadows.tierScores must descend; %v follows %v", sc, c.Shadows.TierScores[i-1])
		}
	}
	if c.Shadows.BakeBudgetMiB < 0 {
		return fmt.Errorf("shadows.bakeBudgetMiB must not be negative, got %d", c.Shadows.BakeBudgetMiB)
	}
	if c.Shadows.NearPlane <= 0 || c.Shadows.FarPlane <= c.Shadows.NearPlane {
		return fmt.Errorf("shadows.farPlane must exceed nearPlane and both must be positive, got %v and %v",
			c.Shadows.NearPlane, c.Shadows.FarPlane)
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
