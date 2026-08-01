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

// Returns the members of the named brace-delimited declaration
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

// Returns the std140 alignment and unpadded size of a scalar, vector, matrix or struct type
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
		// Align a struct to the largest member alignment rounded up to 16, and
		// round its size up to that alignment
		a, s := 16, layoutOf(members, structs)
		return a, roundUp(s, a)
	}
	panic("unhandled GLSL type " + typ)
}

// Rounds a value up to a multiple of an alignment
func roundUp(v, a int) int {
	if v%a == 0 {
		return v
	}
	return v + a - v%a
}

// Walks members applying std140 and returns the total size
func layoutOf(members []member, structs map[string][]member) int {
	off := 0
	for _, m := range members {
		off = offsetOf(off, m, structs)
	}
	return off
}

// Places one member at or after off and returns the next free offset
func offsetOf(off int, m member, structs map[string][]member) int {
	align, size := baseTypeLayout(m.typ, structs)
	if m.count > 0 {
		// Align array elements to at least 16 and pad them to a multiple of
		// that alignment
		if align < 16 {
			align = 16
		}
		stride := roundUp(size, align)
		return roundUp(off, align) + stride*m.count
	}
	return roundUp(off, align) + size
}

// Returns each member's std140 byte offset by name
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

// Strips the _<n> slangc suffixes identifiers with, giving the logical name
func logical(name string) string { return stripSuffix(name) }

// Parses the generated GLSL and returns one block's offsets, size and struct table
//
// header names the block: common.slang declares two, split by update frequency.
func loadGeneratedBlock(t *testing.T, header string) (map[string]int, int, map[string][]member) {
	t.Helper()
	const path = "../shaders/gl/forward.frag.glsl"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("generated GLSL missing (%v), run ./build_shaders.sh", err)
	}
	text := string(src)

	lightMembers := parseBlock(text, "struct LightData_0")
	if len(lightMembers) == 0 {
		t.Fatal("could not parse struct LightData_0 from generated GLSL")
	}
	structs := map[string][]member{"LightData_0": lightMembers}

	blockMembers := parseBlock(text, header)
	if len(blockMembers) == 0 {
		t.Fatalf("could not parse %s from generated GLSL", header)
	}

	byLogical := map[string]int{}
	for name, off := range offsets(blockMembers, structs) {
		byLogical[logical(name)] = off
	}
	return byLogical, layoutOf(blockMembers, structs), structs
}

const (
	frameBlockHeader = "layout(std140) uniform block_FrameUniforms_0"
	drawBlockHeader  = "layout(std140) uniform block_DrawUniforms_0"
)

// Compares one block's derived offsets and size against the hand-written constants
func checkBlock(t *testing.T, header string, want map[string]int, wantSize int) {
	t.Helper()
	got, size, _ := loadGeneratedBlock(t, header)

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
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: in the generated block but uniforms.go has no offset for it", name)
		}
	}

	// Check the block size covers the whole block, or glBufferSubData truncates it
	if roundUp(size, 16) != wantSize {
		t.Errorf("%s: block size constant is %d, generated block needs %d", header, wantSize, roundUp(size, 16))
	}
}

// Checks every hand-written frame-block offset against the generated GLSL
func TestStd140FrameBlockOffsets(t *testing.T) {
	checkBlock(t, frameBlockHeader, map[string]int{
		"view":              offView,
		"projection":        offProjection,
		"lightSpaceMatrix":  offLightSpaceMatrix,
		"shadowMatrices":    offShadowMatrices,
		"viewPos":           offViewPos,
		"farPlane":          offFarPlane,
		"lightPos":          offLightPos,
		"lightCount":        offLightCount,
		"lights":            offLights,
		"texShadowMap":      offTexShadowMap,
		"texShadowCubeMap":  offTexShadowCubeMap,
		"texSkybox":         offTexSkybox,
		"shadowDirIndex":    offShadowDirIndex,
		"pointShadowLights": offPointShadowLights,
	}, frameBlockSize)
}

// Checks every hand-written draw-block offset against the generated GLSL
func TestStd140DrawBlockOffsets(t *testing.T) {
	checkBlock(t, drawBlockHeader, map[string]int{
		"model":         offModel,
		"matAmbient":    offMatAmbient,
		"matDiffuse":    offMatDiffuse,
		"matSpecular":   offMatSpecular,
		"matShininess":  offMatShininess,
		"texOurTexture": offTexOurTexture,
		"texNormalMap":  offTexNormalMap,
		"useNormalMap":  offUseNormalMap,
		"matMetallic":   offMatMetallic,
		"matRoughness":  offMatRoughness,
		"matAo":         offMatAo,
	}, drawBlockSize)
}

// Checks the per-light stride and member offsets against the generated LightData struct
func TestStd140LightStride(t *testing.T) {
	_, _, structs := loadGeneratedBlock(t, frameBlockHeader)
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
			t.Errorf("%s: unexpected LightData member, which uniforms.go does not write", logical(name))
			continue
		}
		if off != expect {
			t.Errorf("light.%s: generated GLSL puts it at +%d, uniforms.go says +%d", logical(name), off, expect)
		}
	}
}

// Checks both marshals stay inside the buffers they are handed
func TestMarshalStd140StaysInBounds(t *testing.T) {
	var f renderer.FrameUniforms
	f.LightCount = renderer.MaxLights
	marshalFrameStd140(&f, make([]byte, frameBlockSize)) // panics on any out-of-range write

	var u renderer.DrawUniforms
	marshalDrawStd140(&u, make([]byte, drawBlockSize))

	last := offPointShadowLights + (renderer.MaxShadowCubes-1)*pointShadowStride + 4
	if last > frameBlockSize {
		t.Fatal(fmt.Sprintf("last frame member ends at %d, past frameBlockSize %d", last, frameBlockSize))
	}
	if offMatAo+4 > drawBlockSize {
		t.Fatal(fmt.Sprintf("last draw member ends at %d, past drawBlockSize %d", offMatAo+4, drawBlockSize))
	}
}
