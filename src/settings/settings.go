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

	// The graphics API: Vulkan is the only backend implemented so far
	Backend string = "vulkan"

	AntiAliasing AAMode = AAMSAA
	// Samples per pixel when AntiAliasing is AAMSAA: 2, 4 or 8
	MSAASamples int = 4

	// Anisotropic filtering on material textures: 1 (off), 2, 4, 8 or 16.
	// Lowered to the device limit at sampler creation rather than rejected
	Anisotropy int = 8

	// Draw depth first and shade only what survives, with an EQUAL depth test
	DepthPrepass bool = true
)

// The [debug] section: switches that change how a run is inspected
var (
	// Vulkan validation layers. Costs frame rate, reports API misuse
	Validation bool = false
	// Freeze the camera on start to allow reproducible captures
	LockCamera bool = false
	// Light every light unshadowed, while still baking every tile
	NoShadows bool = false
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

// The [shadows] section: the shadow atlas, what it is carved into, and what a
// frame may spend rebuilding it
var (
	// Side of the one shadow atlas, in texels. A power of two, 1024 to 8192
	ShadowAtlasSize int = 4096

	// Slot i has size ShadowAtlasSize / ShadowSlotDivisors[i]
	ShadowSlotDivisors []int = []int{2, 8, 16, 32}
	// There are ShadowSlotCounts[i] slots of size i
	ShadowSlotCounts []int = []int{1, 16, 64, 256}

	// The score (radius / distance to camera) at which a light earns each
	// non-sun row above, descending. One per row after the first
	ShadowTierScores []float32 = []float32{0.50, 0.20, 0.08}

	// Build the second atlas, so a moving object casts a moving shadow.
	// False is the low-end switch: every record falls back to the static atlas,
	// movers cast nothing, and the per-frame shadow cost goes to zero
	ShadowDynamicAtlas bool = true

	// Texels, in MiB, a frame may spend rebuilding dynamic tiles. What does not
	// fit waits, ranked by score, keeping the tile it already has
	ShadowBakeBudgetMiB int = 8

	// Taps a shadow lookup spends
	ShadowPCF PCFQuality = PCFFull

	// The near and far planes every shadow projection is built with
	ShadowNearPlane float32 = 1.0
	ShadowFarPlane  float32 = 50.0
)

// Reports whether material textures are sampled anisotropically, 1 meaning plain isotropic filtering
func AnisotropyEnabled() bool {
	return Anisotropy > 1
}

// Reports whether the backbuffer is multisampled, which is MSAA asked for and a count that actually multisamples
func MSAAEnabled() bool {
	return AntiAliasing == AAMSAA && MSAASamples > 1
}

// Returns the combined resolution of dynamic shadow tiles a frame may spend rebuilding
func ShadowBakeBudget() int {
	return ShadowBakeBudgetMiB << 20
}

// Returns how much the shadow normal-offset bias must grow at this atlas size
//
// forward.slang's offsets are world-space constants tuned at 4096, so a smaller
// atlas doubles a texel's world footprint and brings back the acne they hide
func ShadowNormalScale() float32 {
	return float32(shadowReferenceAtlas) / float32(ShadowAtlasSize)
}
