package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// Writes a settings file and loads it, restoring the package defaults afterwards
//
// The package variables are global, so a test that changed one and left would
// leak into the next.
func load(t *testing.T, body string) error {
	t.Helper()
	before := loadDefaults()
	t.Cleanup(func() { _ = apply(before) })

	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// Every shipped config must load, or a `go run . -config` of it is broken
func TestShippedConfigsLoad(t *testing.T) {
	for _, name := range []string{"vulkan.toml", "low.toml"} {
		t.Run(name, func(t *testing.T) {
			before := loadDefaults()
			t.Cleanup(func() { _ = apply(before) })

			// The settings package cannot import paths without a cycle, and this
			// test runs from src/settings
			if err := Load("../../configs/" + name); err != nil {
				t.Fatalf("configs/%s does not load: %v", name, err)
			}
		})
	}
}

// The low tier must actually be the low tier
//
// A quality file that parses and turns nothing down is the failure this whole
// part exists to prevent, and it looks exactly like a working one.
func TestLowConfigTurnsThingsDown(t *testing.T) {
	before := loadDefaults()
	t.Cleanup(func() { _ = apply(before) })

	if err := Load("../../configs/low.toml"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ShadowAtlasSize >= before.Shadows.AtlasSize {
		t.Errorf("low.toml atlas is %d, not smaller than the default %d",
			ShadowAtlasSize, before.Shadows.AtlasSize)
	}
	if ShadowDynamicAtlas {
		t.Error("low.toml left the dynamic atlas on, which is the low-end switch")
	}
	if ShadowPCF != PCFCheap {
		t.Errorf("low.toml PCF is %q, want %q", ShadowPCF, PCFCheap)
	}
	if MSAAEnabled() || AnisotropyEnabled() {
		t.Error("low.toml left MSAA or anisotropy on")
	}
	// The light budget is the one thing it must not give up: same slot counts,
	// so the same number of lights still cast, just at half the resolution
	if len(ShadowSlotCounts) != len(before.Shadows.SlotCounts) {
		t.Fatalf("low.toml has %d slot rows, the default %d",
			len(ShadowSlotCounts), len(before.Shadows.SlotCounts))
	}
	for i, c := range ShadowSlotCounts {
		if c != before.Shadows.SlotCounts[i] {
			t.Errorf("low.toml slot row %d holds %d slots, the default %d — the light budget should be untouched",
				i, c, before.Shadows.SlotCounts[i])
		}
	}
}

// A smaller atlas must grow the shadow bias, or it re-introduces the acne the
// world-space constants in forward.slang were tuned to hide
func TestShadowNormalScaleTracksAtlasSize(t *testing.T) {
	before := loadDefaults()
	t.Cleanup(func() { _ = apply(before) })

	if got := ShadowNormalScale(); got != 1 {
		t.Errorf("the default atlas scales the bias by %v, want 1 — the constants are tuned at it", got)
	}
	if err := load(t, "[shadows]\natlasSize = 2048\n"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := ShadowNormalScale(); got != 2 {
		t.Errorf("a half-size atlas scales the bias by %v, want 2", got)
	}
}

// A key must reach its package variable, and an absent one must keep its default
//
// A knob that parses and then does nothing is the failure mode a settings file
// exists to prevent, and it is invisible: the file looks right and the run
// ignores it.
func TestKeysReachTheirVariables(t *testing.T) {
	if err := load(t, "[window]\nwidth = 800\nheight = 600\n[debug]\nnoShadows = true\n"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if WindowWidth != 800 || WindowHeight != 600 {
		t.Errorf("window = %dx%d, want 800x600", WindowWidth, WindowHeight)
	}
	if !NoShadows {
		t.Error("debug.noShadows did not reach settings.NoShadows")
	}
	if Validation || LockCamera {
		t.Error("an absent [debug] key turned something on")
	}
	if !DepthPrepass {
		t.Error("an absent renderer.depthPrepass did not keep its default of on")
	}
}

// Every [shadows] key must reach its variable
//
// They are read once per frame from scene/shadowatlas.go, so one that parses and
// does not land is a quality tier that silently does nothing.
func TestShadowKeysReachTheirVariables(t *testing.T) {
	body := "[shadows]\n" +
		"atlasSize = 2048\n" +
		"slotDivisors = [2, 4]\n" +
		"slotCounts = [1, 2]\n" +
		"tierScores = [0.3]\n" +
		"dynamicAtlas = false\n" +
		"bakeBudgetMiB = 3\n" +
		"pcf = \"cheap\"\n" +
		"nearPlane = 0.5\n" +
		"farPlane = 120.0\n"
	if err := load(t, body); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ShadowAtlasSize != 2048 {
		t.Errorf("atlasSize = %d, want 2048", ShadowAtlasSize)
	}
	if len(ShadowSlotDivisors) != 2 || ShadowSlotDivisors[1] != 4 {
		t.Errorf("slotDivisors = %v, want [2 4]", ShadowSlotDivisors)
	}
	if len(ShadowSlotCounts) != 2 || ShadowSlotCounts[1] != 2 {
		t.Errorf("slotCounts = %v, want [1 2]", ShadowSlotCounts)
	}
	if len(ShadowTierScores) != 1 || ShadowTierScores[0] != 0.3 {
		t.Errorf("tierScores = %v, want [0.3]", ShadowTierScores)
	}
	if ShadowDynamicAtlas {
		t.Error("shadows.dynamicAtlas = false did not reach the variable")
	}
	if ShadowBakeBudget() != 3<<20 {
		t.Errorf("bake budget = %d texels, want %d", ShadowBakeBudget(), 3<<20)
	}
	if ShadowPCF != PCFCheap {
		t.Errorf("pcf = %q, want %q", ShadowPCF, PCFCheap)
	}
	if ShadowNearPlane != 0.5 || ShadowFarPlane != 120 {
		t.Errorf("planes = %v, %v, want 0.5, 120", ShadowNearPlane, ShadowFarPlane)
	}
}

// The depth prepass must be switchable off, since "the same image with it on and
// off" is the only check that the two passes agree bit for bit
func TestDepthPrepassTurnsOff(t *testing.T) {
	if err := load(t, "[renderer]\ndepthPrepass = false\n"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if DepthPrepass {
		t.Error("renderer.depthPrepass = false did not reach settings.DepthPrepass")
	}
}

// A bad key or value rejects the file rather than being ignored or clamped
func TestInvalidConfigsAreRejected(t *testing.T) {
	for _, body := range []string{
		"[debug]\nlock_camera = true\n", // a misspelt key is a switch that silently does nothing
		// A layout the allocator could not carve renders unshadowed with nothing
		// logged, so every one of these has to reject the file instead
		"[shadows]\natlasSize = 3000\n",                           // not a power of two
		"[shadows]\natlasSize = 512\n",                            // below the floor
		"[shadows]\nslotDivisors = [2, 8]\nslotCounts = [1]\n",    // lengths disagree
		"[shadows]\nslotDivisors = [8, 2]\nslotCounts = [1, 1]\n", // not largest first
		"[shadows]\nslotDivisors = [2, 6]\nslotCounts = [1, 1]\n", // 6 is not a power of two
		"[shadows]\nslotCounts = [2, 16, 64, 256]\n",              // two suns will not pack
		"[shadows]\ntierScores = [0.5, 0.6, 0.08]\n",              // not descending
		"[shadows]\ntierScores = [0.5, 0.2]\n",                    // one short of the rows
		"[shadows]\npcf = \"medium\"\n",                           // not a quality name
		"[shadows]\nnearPlane = 60.0\n",                           // beyond the far plane
		"[window]\nwidth = 0\nheight = 1080\n",
		"[shadows]\nwidth = -1\nheight = 1024\n",
		"[renderer]\nbackend = \"opengl\"\n",
		"[antialiasing]\nmode = \"fxaa\"\n",
		"[antialiasing]\nmode = \"msaa\"\nsamples = 3\n",
		"[textures]\nanisotropy = 5\n",
	} {
		if err := load(t, body); err == nil {
			t.Errorf("accepted an invalid config: %q", body)
		}
	}
}
