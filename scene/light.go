package scene

import (
	"github.com/Zephyr75/overdrive/utils"
	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/mathgl/mgl32"

	"fmt"
	"github.com/Zephyr75/overdrive/settings"
)

type LightXml struct {
	Name      string  `xml:"name,attr"`
	Type      string  `xml:"type"`
	Pos       string  `xml:"position"`
	Dir       string  `xml:"direction"`
	Color     string  `xml:"color"`
	Diffuse   float32 `xml:"diffuse"`
	Specular  float32 `xml:"specular"`
	Intensity float32 `xml:"intensity"`
}

type Light struct {
	Name         string
	Type         int
	Pos          mgl32.Vec3
	Dir          mgl32.Vec3
	Color        mgl32.Vec3
	Diffuse      float32
	Specular     float32
	Intensity    float32
	depthMapFBO  uint32
	depthMap     uint32
	depthCubeMap uint32
}

func (l *Light) Move(x float32, y float32, z float32) {
	l.Pos = l.Pos.Add(mgl32.Vec3{x, y, z})

}

func (l LightXml) toLight() Light {
	t := 0
	name := l.Name
	pos := utils.ParseVec3(l.Pos)
	dir := utils.ParseVec3(l.Dir)
	color := utils.ParseVec3(l.Color)

	pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
	// dir = utils.EulerToDirection(dir[0], dir[1], dir[2])
	dir = mgl32.Vec3{-dir[0], -dir[2], dir[1]}
	// dir = dir.Add(mgl32.Vec3{0, 1, 0})
	// dir = mgl32.Vec3{0, 1, 0}
	// dir = dir.Mul(180.0 / 3.14)
	intensity := l.Intensity
	switch l.Type {
	case "sun":
		t = 0
	case "point":
		t = 1
		intensity /= 1000
	}

	return Light{
		Name:      name,
		Type:      t,
		Pos:       pos,
		Dir:       dir,
		Color:     color,
		Diffuse:   l.Diffuse,
		Specular:  l.Specular,
		Intensity: intensity,
	}
}

func (l *Light) setup() {
	gl.GenFramebuffers(1, &l.depthMapFBO)

	if l.Type == 0 {
		// Directional light
		gl.GenTextures(1, &l.depthMap)
		gl.BindTexture(gl.TEXTURE_2D, l.depthMap)
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT, int32(settings.ShadowWidth), int32(settings.ShadowHeight), 0, gl.DEPTH_COMPONENT, gl.FLOAT, nil)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_BORDER)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_BORDER)
		borderColor := []float32{1.0, 1.0, 1.0, 1.0}
		gl.TexParameterfv(gl.TEXTURE_2D, gl.TEXTURE_BORDER_COLOR, &borderColor[0])
	} else {
		// Point light
		gl.GenTextures(1, &l.depthCubeMap)
		gl.BindTexture(gl.TEXTURE_CUBE_MAP, l.depthCubeMap)
		for i := 0; i < 6; i++ {
			gl.TexImage2D(gl.TEXTURE_CUBE_MAP_POSITIVE_X+uint32(i), 0, gl.DEPTH_COMPONENT, int32(settings.ShadowWidth), int32(settings.ShadowHeight), 0, gl.DEPTH_COMPONENT, gl.FLOAT, nil)
		}
		gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
		gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
		gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_R, gl.CLAMP_TO_EDGE)
	}

	gl.BindFramebuffer(gl.FRAMEBUFFER, l.depthMapFBO)
	if l.Type == 0 {
		gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, l.depthMap, 0)
	} else {
		gl.FramebufferTexture(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, l.depthCubeMap, 0)
	}
	gl.DrawBuffer(gl.NONE)
	gl.ReadBuffer(gl.NONE)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

func (l Light) RenderLight(nearPlane, farPlane float32, depthProgram, depthCubeProgram uint32, s *Scene) mgl32.Mat4 {
	lightProjection := mgl32.Ortho(-10.0, 10.0, -10.0, 10.0, nearPlane, farPlane) // increase 10 to 20 for a wider angle
	lightView := mgl32.LookAtV(l.Pos, l.Pos.Sub(l.Dir), mgl32.Vec3{0.0, 1.0, 0.0})
	lightSpaceMatrix := lightProjection.Mul4(lightView)
	model := mgl32.Scale3D(1.0, 1.0, 1.0)

	gl.Viewport(0, 0, int32(settings.ShadowWidth), int32(settings.ShadowHeight))
	gl.BindFramebuffer(gl.FRAMEBUFFER, l.depthMapFBO)
	gl.Clear(gl.DEPTH_BUFFER_BIT)

	if l.Type == 0 {
		// Render scene from directional light's perspective
		gl.CullFace(gl.FRONT)
		gl.UseProgram(depthProgram)

		modelLoc := gl.GetUniformLocation(depthProgram, gl.Str("model\x00"))
		gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

		lightSpaceMatrixLoc := gl.GetUniformLocation(depthProgram, gl.Str("lightSpaceMatrix\x00"))
		gl.UniformMatrix4fv(lightSpaceMatrixLoc, 1, false, &lightSpaceMatrix[0])

		for i := 0; i < len(s.Meshes); i++ {
			s.Meshes[i].draw(depthProgram, s)
		}

		gl.CullFace(gl.BACK)
	} else {
		// Render scene from point light's perspective
		shadowProjection := mgl32.Perspective(mgl32.DegToRad(90.0), settings.ShadowAspectRatio(), nearPlane, farPlane)
		shadowTransforms := []mgl32.Mat4{
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{1.0, 0.0, 0.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{-1.0, 0.0, 0.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, 1.0, 0.0}), mgl32.Vec3{0.0, 0.0, 1.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, -1.0, 0.0}), mgl32.Vec3{0.0, 0.0, -1.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, 0.0, 1.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
			shadowProjection.Mul4(mgl32.LookAtV(l.Pos, l.Pos.Add(mgl32.Vec3{0.0, 0.0, -1.0}), mgl32.Vec3{0.0, -1.0, 0.0})),
		}

		gl.UseProgram(depthCubeProgram)

		farPlaneLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("farPlane\x00"))
		gl.Uniform1f(farPlaneLoc, farPlane)

		lightPosLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("lightPos\x00"))
		gl.Uniform3fv(lightPosLoc, 1, &l.Pos[0])

		modelLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str("model\x00"))
		gl.UniformMatrix4fv(modelLoc, 1, false, &model[0])

		for i := 0; i < 6; i++ {
			shadowTransformLoc := gl.GetUniformLocation(depthCubeProgram, gl.Str(fmt.Sprintf("shadowMatrices[%d]\x00", i)))
			gl.UniformMatrix4fv(shadowTransformLoc, 1, false, &shadowTransforms[i][0])
		}

		for i := 0; i < len(s.Meshes); i++ {
			s.Meshes[i].draw(depthCubeProgram, s)
		}
	}

	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)

	return lightSpaceMatrix
}
