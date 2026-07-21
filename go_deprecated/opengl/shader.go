package opengl

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
)

func createShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csources, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to compile %v: %v", source, log)
	}

	return shader, nil
}

func readStage(name, stage string) (string, error) {
	path := "shaders/gl/" + name + "." + stage + ".glsl"
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read GLSL %s: %w (run ./build_shaders.sh)", path, err)
	}
	return string(src) + "\x00", nil
}

// stripSuffix removes the "_<n>" slangc appends to disambiguate identifiers
// (ourTexture_0), recovering the logical name the engine binds by.
func stripSuffix(name string) string {
	us := strings.LastIndex(name, "_")
	if us < 0 || us+1 >= len(name) {
		return name
	}
	for _, r := range name[us+1:] {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:us]
}

// setupProgramInterface wires a freshly linked program to the shared uniform
// buffer and pins each sampler to its fixed texture unit. Both are link-time
// decisions, so nothing here repeats per draw.
func (b *GLBackend) setupProgramInterface(program uint32) {
	// Point every uniform block at binding 0; there is exactly one (the shared
	// Uniforms block from common.slang).
	var numBlocks int32
	gl.GetProgramiv(program, gl.ACTIVE_UNIFORM_BLOCKS, &numBlocks)
	for i := int32(0); i < numBlocks; i++ {
		gl.UniformBlockBinding(program, uint32(i), 0)
	}

	gl.UseProgram(program)
	var count int32
	gl.GetProgramiv(program, gl.ACTIVE_UNIFORMS, &count)
	for i := int32(0); i < count; i++ {
		var length, size int32
		var xtype uint32
		buf := make([]byte, 128)
		gl.GetActiveUniform(program, uint32(i), int32(len(buf)), &length, &size, &xtype, &buf[0])
		if xtype != gl.SAMPLER_2D && xtype != gl.SAMPLER_CUBE {
			continue
		}
		raw := string(buf[:length])

		// An array sampler is reported once, as "shadowCubeMap_0[0]" with
		// size > 1. Each element needs its own unit assigned by name.
		if bracket := strings.IndexByte(raw, '['); bracket >= 0 {
			mangled := raw[:bracket]
			logical := stripSuffix(mangled)
			for e := int32(0); e < size; e++ {
				elem := fmt.Sprintf("%s[%d]", mangled, e)
				if loc := gl.GetUniformLocation(program, gl.Str(elem+"\x00")); loc >= 0 {
					gl.Uniform1i(loc, samplerUnit(logical, int(e)))
				}
			}
			continue
		}
		if loc := gl.GetUniformLocation(program, gl.Str(raw+"\x00")); loc >= 0 {
			gl.Uniform1i(loc, samplerUnit(stripSuffix(raw), 0))
		}
	}
}

// samplerUnit maps a logical sampler name (and array index) to its texture unit.
func samplerUnit(logical string, index int) int32 {
	switch logical {
	case "shadowMap":
		return unitShadowMap
	case "ourTexture":
		return unitOurTexture
	case "normalMap":
		return unitNormalMap
	case "shadowCubeMap":
		return int32(unitShadowCube0 + index)
	case "skybox":
		return unitSkybox
	}
	return 0
}

func createProgram(name string, addGeometry bool) (uint32, error) {
	vertexShaderSource, err := readStage(name, "vert")
	if err != nil {
		return 0, err
	}
	fragmentShaderSource, err := readStage(name, "frag")
	if err != nil {
		return 0, err
	}
	geometryShaderSource := ""
	if addGeometry {
		geometryShaderSource, err = readStage(name, "geo")
		if err != nil {
			return 0, err
		}
	}

	vertexShader, err := createShader(vertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}

	fragmentShader, err := createShader(fragmentShaderSource, gl.FRAGMENT_SHADER)
	if err != nil {
		return 0, err
	}

	var geometryShader uint32
	if geometryShaderSource != "" {
		geometryShader, err = createShader(geometryShaderSource, gl.GEOMETRY_SHADER)
		if err != nil {
			return 0, err
		}
	}

	program := gl.CreateProgram()

	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	if geometryShaderSource != "" {
		gl.AttachShader(program, geometryShader)
	}
	gl.LinkProgram(program)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)
	if geometryShaderSource != "" {
		gl.DeleteShader(geometryShader)
	}

	return program, nil
}
