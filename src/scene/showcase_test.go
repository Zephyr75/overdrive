package scene

import (
	"os"
	"testing"

	"github.com/Zephyr75/overdrive/paths"
	"github.com/Zephyr75/overdrive/renderer"
)

// The showcase is the only scene exercising the full material path, so it is
// what catches a material regression: that the OBJ/MTL parse produced real PBR
// values rather than falling back to defaults, and that every texture exists.
func TestMain(m *testing.M) {
	if err := os.Chdir(".."); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// Loads the showcase scene, skipping the test when its assets are absent
func loadShowcase(t *testing.T) Scene {
	t.Helper()
	if _, err := os.Stat(paths.Asset("showcase.xml")); err != nil {
		t.Skipf("showcase assets missing: %v", err)
	}
	return LoadScene(paths.Asset("showcase.xml"))
}

// Checks the scene parses into meshes and lights of all three types, and that a
// spot's derived cone terms survived toLight
//
// One of each type matters because the bake has a distinct projection per type,
// and a scene missing one leaves that path untested. A malformed cone degrades
// to cutoff == outerCutoff rather than failing, and a radius of 0 culls the
// light everywhere.
func TestShowcaseLoads(t *testing.T) {
	s := loadShowcase(t)

	if len(s.Meshes) != 5 {
		t.Errorf("meshes = %d, want 5 (ground, suzanne, 2 spheres, cube)", len(s.Meshes))
	}
	if len(s.Lights) != 9 {
		t.Errorf("lights = %d, want 9 (5 point + 3 spot + 1 sun)", len(s.Lights))
	}

	counts := map[int]int{}
	for _, l := range s.Lights {
		counts[l.Type]++

		if l.Type != renderer.LightSpot {
			continue
		}
		// A sun is unbounded, but a spot's radius is what the shading early-out
		// tests: 0 culls the light everywhere
		if l.Radius <= 0 {
			t.Errorf("%s: radius %v, so the shading early-out culls it everywhere", l.Name, l.Radius)
		}
		// Inner cone is narrower than the outer one, so its cosine is larger;
		// equal cosines mean <cone>/<coneBlend> never reached toLight
		if !(l.Cutoff > l.OuterCutoff) {
			t.Errorf("%s: cutoff %v not greater than outerCutoff %v", l.Name, l.Cutoff, l.OuterCutoff)
		}
		if l.OuterCutoff <= 0 || l.OuterCutoff >= 1 {
			t.Errorf("%s: outerCutoff %v is not a half-angle cosine of a real cone", l.Name, l.OuterCutoff)
		}
	}
	for _, c := range []struct {
		t    int
		name string
	}{
		{renderer.LightSun, "sun"},
		{renderer.LightPoint, "point"},
		{renderer.LightSpot, "spot"},
	} {
		if counts[c.t] == 0 {
			t.Errorf("the showcase has no %s light, so its shadow path goes untested", c.name)
		}
	}
}

// Checks the caster flag: absent means true, and the ground opts out
//
// The default is the load-bearing half. `CastsShadow *bool` in MeshXml exists
// only so an absent element is distinguishable from an explicit false — get that
// wrong and every mesh silently stops casting, which looks like the shadow pass
// broke rather than like a parse bug.
func TestShowcaseShadowCasters(t *testing.T) {
	s := loadShowcase(t)

	ground := s.Mesh("Ground")
	if ground == nil {
		t.Fatal("the showcase has no Ground mesh")
	}
	if ground.CastsShadow {
		t.Error("the ground casts shadows: a single-sided plane under the whole scene can only occlude itself, which is acne")
	}
	for i := range s.Meshes {
		m := &s.Meshes[i]
		if m.Name == "Ground" {
			continue
		}
		if !m.CastsShadow {
			t.Errorf("%s does not cast, but its XML says nothing — the default is not true", m.Name)
		}
	}
}

// Checks the MTL parse produced real PBR scalars and that every texture it names exists
func TestShowcaseMaterials(t *testing.T) {
	s := loadShowcase(t)

	var withColour, withNormal, metallic, customRoughness int
	for _, m := range s.Meshes {
		for _, mat := range m.Materials {
			// Catch a roughness of 0, which would make every surface a mirror,
			// and an Ao of 0, which kills the ambient term
			if mat.Roughness == 0 {
				t.Errorf("%s: roughness is 0 — material defaults not applied?", m.Name)
			}
			if mat.Ao == 0 {
				t.Errorf("%s: ao is 0 — material defaults not applied?", m.Name)
			}
			// Count materials below the 1.0 default, every showcase material
			// setting Pr, so an all-default result means Pr stopped being read
			if mat.Roughness != 1 {
				customRoughness++
			}
			if mat.Metallic > 0 {
				metallic++
			}
			for _, p := range []string{mat.TexturePath, mat.NormalMapPath} {
				if p == "" {
					continue
				}
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%s: texture %q missing: %v", m.Name, p, err)
				}
			}
			if mat.TexturePath != "" {
				withColour++
			}
			if mat.NormalMapPath != "" {
				withNormal++
			}
		}
	}

	if withColour == 0 {
		t.Error("no material has a colour map — map_Kd not parsed?")
	}
	if withNormal == 0 {
		t.Error("no material has a normal map — map_Bump not parsed?")
	}
	if metallic == 0 {
		t.Error("no material is metallic — the chrome and metal props need Pm > 0")
	}
	if customRoughness == 0 {
		t.Error("every material has the default roughness — Pr not parsed?")
	}
}
