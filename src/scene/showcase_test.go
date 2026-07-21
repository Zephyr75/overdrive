package scene

import (
	"os"
	"testing"

	"github.com/Zephyr75/overdrive/renderer"
)

// The showcase is the only scene that exercises the full material path — PBR
// scalars, colour maps and normal maps — so it is what catches a material
// regression. Loading it here (no GPU, no backend) checks that the OBJ/MTL
// parse actually produced those values instead of silently falling back to
// defaults, and that every texture it names is present on disk.
//
// Tests run with the package dir as the working directory, but the loader
// resolves assets/ and textures/ relative to the module root — so move there
// once for the whole package rather than per test.
func TestMain(m *testing.M) {
	if err := os.Chdir(".."); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func loadShowcase(t *testing.T) Scene {
	t.Helper()
	if _, err := os.Stat("assets/showcase.xml"); err != nil {
		t.Skipf("showcase assets missing: %v", err)
	}
	return LoadScene("assets/showcase.xml")
}

func TestShowcaseLoads(t *testing.T) {
	s := loadShowcase(t)

	if len(s.Meshes) != 5 {
		t.Errorf("meshes = %d, want 5 (ground, suzanne, 2 spheres, cube)", len(s.Meshes))
	}
	if len(s.Lights) != 5 {
		t.Errorf("lights = %d, want 5 (4 point + 1 sun)", len(s.Lights))
	}

	// Shadow budget: the first directional and first point light, resolved by
	// index so XML ordering doesn't matter.
	s.pickShadowCasters()
	if s.shadowDirIndex < 0 {
		t.Error("no directional shadow caster picked, but the scene has a sun")
	} else if s.Lights[s.shadowDirIndex].Type != renderer.LightSun {
		t.Errorf("directional caster is light %d, which is not a sun", s.shadowDirIndex)
	}
	if s.shadowPointIndex < 0 {
		t.Error("no point shadow caster picked, but the scene has point lights")
	} else if s.Lights[s.shadowPointIndex].Type != renderer.LightPoint {
		t.Errorf("point caster is light %d, which is not a point light", s.shadowPointIndex)
	}
}

func TestShowcaseMaterials(t *testing.T) {
	s := loadShowcase(t)

	var withColour, withNormal, metallic, customRoughness int
	for _, m := range s.Meshes {
		for _, mat := range m.Materials {
			// Roughness 0 would make every surface a mirror — the bug that hid
			// while nothing set these scalars. Ao 0 kills the ambient term.
			if mat.Roughness == 0 {
				t.Errorf("%s: roughness is 0 — material defaults not applied?", m.Name)
			}
			if mat.Ao == 0 {
				t.Errorf("%s: ao is 0 — material defaults not applied?", m.Name)
			}
			// Every showcase material sets Pr to something below the 1.0
			// default, so an all-default result means Pr stopped being read.
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
