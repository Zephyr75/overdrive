package opengl

import (
	"fmt"
	"strings"

	"github.com/Zephyr75/overdrive/utils"
	"github.com/go-gl/gl/v4.1-core/gl"
	"os"
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

func CreateProgram(name string, addGeometry bool) (uint32, error) {
	vertexShaderFile, err := os.ReadFile("shaders/" + name + ".vert.glsl")
	utils.HandleError(err)
	vertexShaderSource := string(vertexShaderFile) + "\x00"

	fragmentShaderFile, err := os.ReadFile("shaders/" + name + ".frag.glsl")
	utils.HandleError(err)
	fragmentShaderSource := string(fragmentShaderFile) + "\x00"

	geometryShaderSource := ""
	if addGeometry {
		geometryShaderFile, err := os.ReadFile("shaders/" + name + ".geo.glsl")
		utils.HandleError(err)
		geometryShaderSource = string(geometryShaderFile) + "\x00"
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
