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

// Checks the scene parses into the expected meshes and lights, and picks both shadow casters
func TestShowcaseLoads(t *testing.T) {
	s := loadShowcase(t)

	if len(s.Meshes) != 5 {
		t.Errorf("meshes = %d, want 5 (ground, suzanne, 2 spheres, cube)", len(s.Meshes))
	}
	if len(s.Lights) != 5 {
		t.Errorf("lights = %d, want 5 (4 point + 1 sun)", len(s.Lights))
	}

	// Check the shadow budget goes to the first directional and first point
	// light, resolved by index so XML ordering does not matter
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
