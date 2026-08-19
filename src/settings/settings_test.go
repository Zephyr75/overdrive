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

// The shipped config must load, or every `go run .` is broken
func TestShippedConfigLoads(t *testing.T) {
	before := loadDefaults()
	t.Cleanup(func() { _ = apply(before) })

	// The settings package cannot import paths without a cycle, and this test
	// runs from src/settings
	if err := Load("../../configs/vulkan.toml"); err != nil {
		t.Fatalf("configs/vulkan.toml does not load: %v", err)
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
}

// A bad key or value rejects the file rather than being ignored or clamped
func TestInvalidConfigsAreRejected(t *testing.T) {
	for _, body := range []string{
		"[debug]\nlock_camera = true\n", // a misspelt key is a switch that silently does nothing
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
