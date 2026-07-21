package opengl

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Zephyr75/overdrive/renderer"
)

// The std140 offsets in uniforms.go are hand-written — the one layout in the
// engine that isn't derived from the source of truth (the Vulkan backend gets
// its scalar layout for free, because Go structs are already packed that way).
// If common.slang gains or reorders a field, every offset after it shifts and
// the GL backend silently renders garbage.
//
// This test re-derives the layout from the generated GLSL by applying the
// std140 rules, and fails if it disagrees with the constants.

var memberRe = regexp.MustCompile(`^\s*(\w+)\s+(\w+)\s*(?:\[(\d+)\])?\s*;`)

type member struct {
	typ, name string
	count     int // 0 = not an array
}

// parseBlock returns the members of the named brace-delimited declaration.
func parseBlock(src, header string) []member {
	i := strings.Index(src, header)
	if i < 0 {
		return nil
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		return nil
	}
	body := src[i+open+1:]
	if end := strings.Index(body, "}"); end >= 0 {
		body = body[:end]
	}

	var out []member
	for _, line := range strings.Split(body, "\n") {
		m := memberRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		mem := member{typ: m[1], name: m[2]}
		if m[3] != "" {
			mem.count, _ = strconv.Atoi(m[3])
		}
		out = append(out, mem)
	}
	return out
}

// std140 alignment and unpadded size of a scalar/vector/matrix type.
func baseTypeLayout(typ string, structs map[string][]member) (align, size int) {
	switch typ {
	case "float", "int", "uint", "bool":
		return 4, 4
	case "vec2":
		return 8, 8
	case "vec3":
		return 16, 12 // aligns to 16 but only occupies 12
	case "vec4":
		return 16, 16
	case "mat4x4", "mat4":
		return 16, 64
	}
	if members, ok := structs[typ]; ok {
		// A struct aligns to the largest member alignment, rounded up to 16,
		// and its size is rounded up to that alignment.
		a, s := 16, layoutOf(members, structs)
		return a, roundUp(s, a)
	}
	panic("unhandled GLSL type " + typ)
}

func roundUp(v, a int) int {
	if v%a == 0 {
		return v
	}
	return v + a - v%a
}

// layoutOf walks members applying std140 and returns the total size.
func layoutOf(members []member, structs map[string][]member) int {
	off := 0
	for _, m := range members {
		off = offsetOf(off, m, structs)
	}
	return off
}

// offsetOf places one member at or after off and returns the next free offset.
func offsetOf(off int, m member, structs map[string][]member) int {
	align, size := baseTypeLayout(m.typ, structs)
	if m.count > 0 {
		// Array elements always align to at least 16 and are padded to a
		// multiple of that alignment.
		if align < 16 {
			align = 16
		}
		stride := roundUp(size, align)
		return roundUp(off, align) + stride*m.count
	}
	return roundUp(off, align) + size
}

// offsets returns each member's std140 byte offset by name.
func offsets(members []member, structs map[string][]member) map[string]int {
	out := map[string]int{}
	off := 0
	for _, m := range members {
		align, _ := baseTypeLayout(m.typ, structs)
		if m.count > 0 && align < 16 {
			align = 16
		}
		out[m.name] = roundUp(off, align)
		off = offsetOf(off, m, structs)
	}
	return out
}

// slangc suffixes identifiers with _<n>; strip it to get the logical name.
func logical(name string) string { return stripSuffix(name) }

func loadGeneratedBlock(t *testing.T) (map[string]int, int, map[string][]member) {
	t.Helper()
	const path = "../shaders/gl/forward.frag.glsl"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("generated GLSL missing (%v); run ./build_shaders.sh", err)
	}
	text := string(src)

	lightMembers := parseBlock(text, "struct LightData_0")
	if len(lightMembers) == 0 {
		t.Fatal("could not parse struct LightData_0 from generated GLSL")
	}
	structs := map[string][]member{"LightData_0": lightMembers}

	blockMembers := parseBlock(text, "layout(std140) uniform block_Uniforms_0")
	if len(blockMembers) == 0 {
		t.Fatal("could not parse the std140 uniform block from generated GLSL")
	}

	byLogical := map[string]int{}
	for name, off := range offsets(blockMembers, structs) {
		byLogical[logical(name)] = off
	}
	return byLogical, layoutOf(blockMembers, structs), structs
}

func TestStd140BlockOffsets(t *testing.T) {
	got, size, _ := loadGeneratedBlock(t)

	want := map[string]int{
		"view":              offView,
		"projection":        offProjection,
		"model":             offModel,
		"lightSpaceMatrix":  offLightSpaceMatrix,
		"shadowMatrices":    offShadowMatrices,
		"viewPos":           offViewPos,
		"farPlane":          offFarPlane,
		"lightPos":          offLightPos,
		"matAmbient":        offMatAmbient,
		"matDiffuse":        offMatDiffuse,
		"matSpecular":       offMatSpecular,
		"matShininess":      offMatShininess,
		"lights":            offLights,
		"useNormalMap":      offUseNormalMap,
		"lightCount":        offLightCount,
		"shadowDirIndex":    offShadowDirIndex,
		"matMetallic":       offMatMetallic,
		"matRoughness":      offMatRoughness,
		"matAo":             offMatAo,
		"pointShadowLights": offPointShadowLights,
	}
	for name, expect := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("%s: missing from the generated block", name)
			continue
		}
		if actual != expect {
			t.Errorf("%s: generated GLSL puts it at %d, uniforms.go says %d", name, actual, expect)
		}
	}

	// blockSize must cover the whole block, or glBufferSubData truncates it.
	if roundUp(size, 16) != blockSize {
		t.Errorf("blockSize = %d, generated block needs %d", blockSize, roundUp(size, 16))
	}
}

func TestStd140LightStride(t *testing.T) {
	_, _, structs := loadGeneratedBlock(t)
	members := structs["LightData_0"]

	_, size := baseTypeLayout("LightData_0", structs)
	if size != lightStride {
		t.Errorf("lightStride = %d, generated LightData is %d bytes", lightStride, size)
	}

	want := map[string]int{
		"type": lOffType, "kConstant": lOffConstant, "kLinear": lOffLinear,
		"kQuadratic": lOffQuadratic, "cutoff": lOffCutoff, "color": lOffColor,
		"intensity": lOffIntensity, "diffuse": lOffDiffuse, "specular": lOffSpecular,
		"position": lOffPosition, "direction": lOffDirection,
	}
	for name, off := range offsets(members, structs) {
		expect, ok := want[logical(name)]
		if !ok {
			t.Errorf("%s: unexpected LightData member; uniforms.go does not write it", logical(name))
			continue
		}
		if off != expect {
			t.Errorf("light.%s: generated GLSL puts it at +%d, uniforms.go says +%d", logical(name), off, expect)
		}
	}
}

// The marshal must stay inside the buffer it is handed.
func TestMarshalStd140StaysInBounds(t *testing.T) {
	dst := make([]byte, blockSize)
	var u renderer.Uniforms
	u.LightCount = renderer.MaxLights
	marshalStd140(&u, dst) // panics on any out-of-range write

	last := offPointShadowLights + (renderer.MaxShadowCubes-1)*pointShadowStride + 4
	if last > blockSize {
		t.Fatal(fmt.Sprintf("last member ends at %d, past blockSize %d", last, blockSize))
	}
}
