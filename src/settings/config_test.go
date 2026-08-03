package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// Writes a settings file and loads it, restoring the package variables afterwards
//
// They are globals, so a test that left them changed would leak into the next
// one — and into any test elsewhere that renders at the default resolution.
func load(t *testing.T, body string) error {
	t.Helper()

	w, h := WindowWidth, WindowHeight
	sw, sh := ShadowWidth, ShadowHeight
	backend, mode, samples := Backend, AntiAliasing, MSAASamples
	t.Cleanup(func() {
		WindowWidth, WindowHeight = w, h
		ShadowWidth, ShadowHeight = sw, sh
		Backend, AntiAliasing, MSAASamples = backend, mode, samples
	})

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// A file that names only some keys leaves the rest at their defaults
func TestLoadPartial(t *testing.T) {
	if err := load(t, "[window]\nwidth = 1280\nheight = 720\n"); err != nil {
		t.Fatal(err)
	}
	if WindowWidth != 1280 || WindowHeight != 720 {
		t.Errorf("window = %dx%d, want 1280x720", WindowWidth, WindowHeight)
	}
	if ShadowWidth != 1024 || Backend != "gl" || AntiAliasing != AAMSAA || MSAASamples != 4 {
		t.Errorf("defaults changed: shadow %d, backend %q, aa %q x%d",
			ShadowWidth, Backend, AntiAliasing, MSAASamples)
	}
}

func TestLoadFull(t *testing.T) {
	err := load(t, `
[window]
width = 800
height = 600
[shadows]
width = 2048
height = 2048
[renderer]
backend = "vk"
[antialiasing]
mode = "none"
samples = 2
`)
	if err != nil {
		t.Fatal(err)
	}
	// "vk" is an alias, normalised to the name createBackend switches on
	if Backend != "vulkan" {
		t.Errorf("backend = %q, want vulkan", Backend)
	}
	if MSAAEnabled() {
		t.Error("MSAAEnabled with mode none")
	}
	if ShadowWidth != 2048 || WindowWidth != 800 {
		t.Errorf("shadow %d, window %d", ShadowWidth, WindowWidth)
	}
}

// A rejected file must leave every variable alone, so the engine never starts half-configured
func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"unknown key":      "[window]\nwith = 800\n",
		"unknown backend":  "[renderer]\nbackend = \"d3d12\"\n",
		"unknown aa mode":  "[antialiasing]\nmode = \"fxaa\"\n",
		"odd sample count": "[antialiasing]\nsamples = 3\n",
		"zero resolution":  "[window]\nwidth = 0\n",
		"malformed toml":   "[window\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := load(t, body); err == nil {
				t.Fatal("want an error, got none")
			}
			if WindowWidth != 1920 || Backend != "gl" || MSAASamples != 4 {
				t.Errorf("settings changed by a rejected file: %d %q x%d",
					WindowWidth, Backend, MSAASamples)
			}
		})
	}
}

// The environment is one run's instruction, so it overrides the file
func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("OVERDRIVE_BACKEND", "vulkan")
	t.Setenv("OVERDRIVE_MSAA", "8")

	err := load(t, "[renderer]\nbackend = \"gl\"\n[antialiasing]\nmode = \"none\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if Backend != "vulkan" {
		t.Errorf("backend = %q, want vulkan", Backend)
	}
	if AntiAliasing != AAMSAA || MSAASamples != 8 {
		t.Errorf("aa = %q x%d, want msaa x8", AntiAliasing, MSAASamples)
	}
}

// OVERDRIVE_MSAA carries the mode as well as the count, 1 meaning off
func TestEnvDisablesMSAA(t *testing.T) {
	t.Setenv("OVERDRIVE_MSAA", "1")

	if err := load(t, "[antialiasing]\nmode = \"msaa\"\nsamples = 4\n"); err != nil {
		t.Fatal(err)
	}
	if MSAAEnabled() {
		t.Errorf("aa = %q x%d, want it off", AntiAliasing, MSAASamples)
	}
}
