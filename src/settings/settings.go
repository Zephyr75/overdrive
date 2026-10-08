// Package settings holds the engine's runtime configuration:
package settings

// Anti-aliasing technique: None or MSAA
type AAMode string

const (
	AANone AAMode = "none"
	AAMSAA AAMode = "msaa"
)

// PCF quality: how many taps a shadow lookup spends
type PCFQuality string

const (
	// 4 corner taps, then a 3x3 kernel only where they disagree
	PCFFull PCFQuality = "full"
	// The 4 corner taps alone, so a penumbra quantises to quarters
	PCFCheap PCFQuality = "cheap"
)

// The atlas size the shadow bias constants in forward.slang were tuned at
const shadowReferenceAtlas = 4096

// The shape of a settings file, one struct per TOML section
type Config struct {
	Window struct {
		// Updated on resize by input.FramebufferSizeCallback
		Width  int
		Height int
		// Wait for the display's refresh: false renders as fast as the GPU can,
		// through mailbox (no tearing) or immediate (tears) when supported
		VSync bool
	}
	Renderer struct {
		// The graphics API: Vulkan is the only backend implemented so far
		Backend string
	}
	// The shadow atlas, what it is carved into, and what a frame may spend rebuilding it
	Shadows struct {
		// Side of the one shadow atlas, in texels. A power of two, 1024 to 8192
		AtlasSize int
		// Slot row i has slots of AtlasSize / SlotDivisors[i], SlotCounts[i] of them
		SlotDivisors []int
		SlotCounts   []int
		// The score (radius / distance to camera) at which a light earns each row after the first, descending
		TierScores []float32
		// Build the second atlas, so a moving object casts a moving shadow
		DynamicAtlas bool
		// Dynamic shadow texels a frame may re-bake, in units of 2^20
		BakeBudgetMiB int
		// Taps a shadow lookup spends
		PCF PCFQuality
		// The near and far planes every shadow projection is built with
		NearPlane float32
		FarPlane  float32
	}
	AntiAliasing struct {
		Mode AAMode
		// Samples per pixel when Mode is AAMSAA: 1, 2, 4 or 8
		Samples int
	} `toml:"antialiasing"`
	Textures struct {
		// Anisotropic filtering on material textures: 1 (off), 2, 4, 8 or 16,
		// lowered to the device limit at sampler creation
		Anisotropy int
	}
	// Inspection switches, kept in the file so a run's whole configuration is readable from it
	Debug struct {
		// Vulkan validation layers. Costs frame rate, reports API misuse
		Validation bool
		// Freeze the camera on start to allow reproducible captures
		LockCamera bool
		// Light every light unshadowed, while still baking every tile
		NoShadows bool
		// Skip the depth prepass and depth-test the main pass with LESS, to check the two paths render alike
		NoPrepass bool
	}
}

// The live settings: the defaults below until Load replaces them with a file
var Current = defaults()

// The value of every key a settings file leaves out
func defaults() Config {
	var cfg Config
	cfg.Window.Width, cfg.Window.Height = 1920, 1080
	cfg.Window.VSync = true
	cfg.Renderer.Backend = "vulkan"
	cfg.Shadows.AtlasSize = 4096
	cfg.Shadows.SlotDivisors = []int{2, 8, 16, 32}
	cfg.Shadows.SlotCounts = []int{1, 16, 64, 256}
	cfg.Shadows.TierScores = []float32{0.50, 0.20, 0.08}
	cfg.Shadows.DynamicAtlas = true
	cfg.Shadows.BakeBudgetMiB = 8
	cfg.Shadows.PCF = PCFFull
	cfg.Shadows.NearPlane, cfg.Shadows.FarPlane = 1.0, 50.0
	cfg.AntiAliasing.Mode = AAMSAA
	cfg.AntiAliasing.Samples = 4
	cfg.Textures.Anisotropy = 8
	return cfg
}

// Reports whether material textures are sampled anisotropically, 1 meaning plain isotropic filtering
func AnisotropyEnabled() bool {
	return Current.Textures.Anisotropy > 1
}

// Reports whether the backbuffer is multisampled
func IsMSAAEnabled() bool {
	return Current.AntiAliasing.Mode == AAMSAA && Current.AntiAliasing.Samples > 1
}

// Returns the combined resolution of dynamic shadow tiles a frame may spend rebuilding
func ShadowBakeBudget() int {
	return Current.Shadows.BakeBudgetMiB << 20
}

// Returns how much the shadow normal-offset bias must grow at this atlas size
//
// forward.slang's offsets are world-space constants tuned at 4096, so a smaller
// atlas doubles a texel's world footprint and brings back the acne they hide
func ShadowNormalScale() float32 {
	return float32(shadowReferenceAtlas) / float32(Current.Shadows.AtlasSize)
}
