package opengl

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/Zephyr75/overdrive/renderer"
)

// The OpenGL backend uploads renderer.FrameUniforms and renderer.DrawUniforms
// by memcpy, which is only correct because common.slang declares both blocks in
// 16-byte cells — see its LAYOUT RULE. Then std140, which an OpenGL 4.1 uniform
// block must use, comes out byte-identical to the packed layout Go gives us.
//
// Nothing enforces that at compile time. Add a float3 without a scalar behind
// it, or an int[4] instead of an int4, and std140 inserts padding Go does not
// have: every field after it shifts and the GL backend silently renders garbage.
//
// So this test re-derives the std140 layout from the generated GLSL by applying
// the rules, and checks every member lands exactly where unsafe.Offsetof puts
// the matching Go field.

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
	case "vec2", "ivec2", "uvec2", "bvec2":
		return 8, 8
	case "vec3", "ivec3", "uvec3", "bvec3":
		return 16, 12 // aligns to 16 but only occupies 12
	case "vec4", "ivec4", "uvec4", "bvec4":
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

// Compares a derived std140 layout against the Go struct it must be identical to
//
// want maps each generated member to the byte offset unsafe.Offsetof reports for
// the matching Go field, and wantSize is unsafe.Sizeof of the whole struct.
func checkAgainstGoStruct(t *testing.T, what string, got map[string]int, gotSize int, want map[string]uintptr, wantSize uintptr) {
	t.Helper()

	for name, expect := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("%s.%s: in the Go struct but not in the generated block", what, name)
			continue
		}
		if uintptr(actual) != expect {
			t.Errorf("%s.%s: std140 puts it at %d, the Go struct at %d — a 16-byte cell is not full, so memcpy is wrong",
				what, name, actual, expect)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s.%s: in the generated block but the Go struct has no field for it", what, name)
		}
	}

	// A size that is not a multiple of 16 means the last cell was left unfilled
	if roundUp(gotSize, 16) != int(wantSize) {
		t.Errorf("%s: std140 needs %d bytes, the Go struct is %d — sizes must match for memcpy",
			what, roundUp(gotSize, 16), wantSize)
	}
}

// Checks the frame block's std140 layout is byte-identical to renderer.FrameUniforms
func TestStd140FrameBlockMatchesGoStruct(t *testing.T) {
	got, size, _ := loadGeneratedBlock(t, frameBlockHeader)

	var f renderer.FrameUniforms
	checkAgainstGoStruct(t, "FrameUniforms", got, size, map[string]uintptr{
		"view":              unsafe.Offsetof(f.View),
		"projection":        unsafe.Offsetof(f.Projection),
		"lightSpaceMatrix":  unsafe.Offsetof(f.LightSpaceMatrix),
		"shadowMatrices":    unsafe.Offsetof(f.ShadowMatrices),
		"viewPos":           unsafe.Offsetof(f.ViewPos),
		"farPlane":          unsafe.Offsetof(f.FarPlane),
		"lightPos":          unsafe.Offsetof(f.LightPos),
		"lightCount":        unsafe.Offsetof(f.LightCount),
		"lights":            unsafe.Offsetof(f.Lights),
		"texShadowMap":      unsafe.Offsetof(f.TexShadowMap),
		"texShadowCubeMap":  unsafe.Offsetof(f.TexShadowCubeMap),
		"texSkybox":         unsafe.Offsetof(f.TexSkybox),
		"shadowDirIndex":    unsafe.Offsetof(f.ShadowDirIndex),
		"pointShadowLights": unsafe.Offsetof(f.PointShadowLights),
	}, unsafe.Sizeof(f))
}

// Checks the draw block's std140 layout is byte-identical to renderer.DrawUniforms
func TestStd140DrawBlockMatchesGoStruct(t *testing.T) {
	got, size, _ := loadGeneratedBlock(t, drawBlockHeader)

	var u renderer.DrawUniforms
	checkAgainstGoStruct(t, "DrawUniforms", got, size, map[string]uintptr{
		"model":         unsafe.Offsetof(u.Model),
		"matAmbient":    unsafe.Offsetof(u.MatAmbient),
		"matShininess":  unsafe.Offsetof(u.MatShininess),
		"matDiffuse":    unsafe.Offsetof(u.MatDiffuse),
		"matMetallic":   unsafe.Offsetof(u.MatMetallic),
		"matSpecular":   unsafe.Offsetof(u.MatSpecular),
		"matRoughness":  unsafe.Offsetof(u.MatRoughness),
		"matAo":         unsafe.Offsetof(u.MatAo),
		"texOurTexture": unsafe.Offsetof(u.TexDiffuse),
		"texNormalMap":  unsafe.Offsetof(u.TexNormalMap),
		"useNormalMap":  unsafe.Offsetof(u.UseNormalMap),
	}, unsafe.Sizeof(u))
}

// Checks the per-light stride and member offsets against renderer.LightData
//
// This is the one that pays for itself: LightData sits in an array, so std140
// rounds its stride up to 16 and a single unfilled cell shifts every light after
// the first.
func TestStd140LightDataMatchesGoStruct(t *testing.T) {
	_, _, structs := loadGeneratedBlock(t, frameBlockHeader)
	members := structs["LightData_0"]

	byLogical := map[string]int{}
	for name, off := range offsets(members, structs) {
		byLogical[logical(name)] = off
	}
	_, size := baseTypeLayout("LightData_0", structs)

	var l renderer.LightData
	checkAgainstGoStruct(t, "LightData", byLogical, size, map[string]uintptr{
		"color":      unsafe.Offsetof(l.Color),
		"intensity":  unsafe.Offsetof(l.Intensity),
		"position":   unsafe.Offsetof(l.Position),
		"diffuse":    unsafe.Offsetof(l.Diffuse),
		"direction":  unsafe.Offsetof(l.Direction),
		"specular":   unsafe.Offsetof(l.Specular),
		"kConstant":  unsafe.Offsetof(l.Constant),
		"kLinear":    unsafe.Offsetof(l.Linear),
		"kQuadratic": unsafe.Offsetof(l.Quadratic),
		"cutoff":     unsafe.Offsetof(l.Cutoff),
		"type":       unsafe.Offsetof(l.Type),
		"reserved0":  unsafe.Offsetof(l.Reserved0),
		"reserved1":  unsafe.Offsetof(l.Reserved1),
		"reserved2":  unsafe.Offsetof(l.Reserved2),
	}, unsafe.Sizeof(l))
}

// Checks every block is a whole number of 16-byte cells, the property the whole
// memcpy path rests on
func TestBlocksAreWholeCells(t *testing.T) {
	sizes := map[string]uintptr{
		"LightData":     unsafe.Sizeof(renderer.LightData{}),
		"FrameUniforms": unsafe.Sizeof(renderer.FrameUniforms{}),
		"DrawUniforms":  unsafe.Sizeof(renderer.DrawUniforms{}),
	}
	for name, size := range sizes {
		if size%16 != 0 {
			t.Errorf("%s is %d bytes, not a multiple of 16 — some cell is unfilled", name, size)
		}
	}
}
