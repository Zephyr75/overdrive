package opengl

import (
	"fmt"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"

	"github.com/Zephyr75/overdrive/renderer"
)

// Fixed texture units, matching the sampler uniforms in the GLSL sources:
// 0 = shadowMap (2D), 1 = ourTexture (2D), 2 = shadowCubeMap (cube),
// 3 = skybox (cube).

type lightLocs struct {
	typ, constant, linear, quadratic       int32
	cutoff, color, intensity               int32
	diffuse, specular, position, direction int32
}

// progLocs caches every uniform location of one program, resolved once.
// Uniforms a given shader doesn't declare resolve to -1, which GL ignores —
// so all programs (depth, skybox, forward, ...) share this one struct.
type progLocs struct {
	view, projection, model int32
	lightSpaceMatrix        int32
	shadowMatrices          [6]int32
	viewPos, lightPos       int32
	farPlane, timeLoc       int32
	matAmbient, matDiffuse  int32
	matSpecular, matShine   int32
	lights                  [renderer.MaxLights]lightLocs
}

func loc(program uint32, name string) int32 {
	return gl.GetUniformLocation(program, gl.Str(name+"\x00"))
}

func resolveLocs(program uint32) *progLocs {
	l := &progLocs{
		view:             loc(program, "view"),
		projection:       loc(program, "projection"),
		model:            loc(program, "model"),
		lightSpaceMatrix: loc(program, "lightSpaceMatrix"),
		viewPos:          loc(program, "viewPos"),
		lightPos:         loc(program, "lightPos"),
		farPlane:         loc(program, "farPlane"),
		timeLoc:          loc(program, "time"),
		matAmbient:       loc(program, "material.ambient"),
		matDiffuse:       loc(program, "material.diffuse"),
		matSpecular:      loc(program, "material.specular"),
		matShine:         loc(program, "material.shininess"),
	}
	for i := 0; i < 6; i++ {
		l.shadowMatrices[i] = loc(program, fmt.Sprintf("shadowMatrices[%d]", i))
	}
	for i := 0; i < renderer.MaxLights; i++ {
		p := fmt.Sprintf("lights[%d].", i)
		l.lights[i] = lightLocs{
			typ:       loc(program, p+"type"),
			constant:  loc(program, p+"constant"),
			linear:    loc(program, p+"linear"),
			quadratic: loc(program, p+"quadratic"),
			cutoff:    loc(program, p+"cutoff"),
			color:     loc(program, p+"color"),
			intensity: loc(program, p+"intensity"),
			diffuse:   loc(program, p+"diffuse"),
			specular:  loc(program, p+"specular"),
			position:  loc(program, p+"position"),
			direction: loc(program, p+"direction"),
		}
	}

	// Sampler units never change: set them once here.
	gl.UseProgram(program)
	if s := loc(program, "shadowMap"); s >= 0 {
		gl.Uniform1i(s, 0)
	}
	if s := loc(program, "ourTexture"); s >= 0 {
		gl.Uniform1i(s, 1)
	}
	if s := loc(program, "shadowCubeMap"); s >= 0 {
		gl.Uniform1i(s, 2)
	}
	if s := loc(program, "skybox"); s >= 0 {
		gl.Uniform1i(s, 3)
	}
	return l
}

// applyUniforms uploads the renderer.Uniforms snapshot into the program's
// loose GLSL 3.3 uniforms and binds the referenced textures to their fixed
// units. This is the Phase 1 bridge; the Slang migration (GO_BACKEND.md
// Phase 3) replaces it with a single std140 uniform-buffer upload.
func (b *GLBackend) applyUniforms(s renderer.ShaderHandle, u *renderer.Uniforms) {
	program := uint32(s)
	l, ok := b.locs[s]
	if !ok {
		l = resolveLocs(program)
		b.locs[s] = l
	}

	gl.UniformMatrix4fv(l.view, 1, false, &u.View[0])
	gl.UniformMatrix4fv(l.projection, 1, false, &u.Projection[0])
	gl.UniformMatrix4fv(l.model, 1, false, &u.Model[0])
	gl.UniformMatrix4fv(l.lightSpaceMatrix, 1, false, &u.LightSpaceMatrix[0])
	for i := 0; i < 6; i++ {
		gl.UniformMatrix4fv(l.shadowMatrices[i], 1, false, &u.ShadowMatrices[i][0])
	}

	gl.Uniform3f(l.viewPos, u.ViewPos[0], u.ViewPos[1], u.ViewPos[2])
	gl.Uniform3f(l.lightPos, u.LightPos[0], u.LightPos[1], u.LightPos[2])
	gl.Uniform1f(l.farPlane, u.FarPlane)
	if l.timeLoc >= 0 {
		gl.Uniform1f(l.timeLoc, float32(time.Since(b.start).Seconds()))
	}

	gl.Uniform3f(l.matAmbient, u.MatAmbient[0], u.MatAmbient[1], u.MatAmbient[2])
	gl.Uniform3f(l.matDiffuse, u.MatDiffuse[0], u.MatDiffuse[1], u.MatDiffuse[2])
	gl.Uniform3f(l.matSpecular, u.MatSpecular[0], u.MatSpecular[1], u.MatSpecular[2])
	gl.Uniform1f(l.matShine, u.MatShininess)

	for i := 0; i < int(u.LightCount) && i < renderer.MaxLights; i++ {
		ll, ld := &l.lights[i], &u.Lights[i]
		gl.Uniform1i(ll.typ, ld.Type)
		gl.Uniform1f(ll.constant, ld.Constant)
		gl.Uniform1f(ll.linear, ld.Linear)
		gl.Uniform1f(ll.quadratic, ld.Quadratic)
		gl.Uniform1f(ll.cutoff, ld.Cutoff)
		gl.Uniform3f(ll.color, ld.Color[0], ld.Color[1], ld.Color[2])
		gl.Uniform1f(ll.intensity, ld.Intensity)
		gl.Uniform1f(ll.diffuse, ld.Diffuse)
		gl.Uniform1f(ll.specular, ld.Specular)
		gl.Uniform3f(ll.position, ld.Position[0], ld.Position[1], ld.Position[2])
		gl.Uniform3f(ll.direction, ld.Direction[0], ld.Direction[1], ld.Direction[2])
	}

	// Texture units (handle 0 = white pixel for the diffuse slot).
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, uint32(u.TexShadowMap))
	diffuse := uint32(u.TexDiffuse)
	if diffuse == 0 {
		diffuse = b.whiteTex
	}
	gl.ActiveTexture(gl.TEXTURE1)
	gl.BindTexture(gl.TEXTURE_2D, diffuse)
	gl.ActiveTexture(gl.TEXTURE2)
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, uint32(u.TexShadowCubeMap))
	gl.ActiveTexture(gl.TEXTURE3)
	gl.BindTexture(gl.TEXTURE_CUBE_MAP, uint32(u.TexSkybox))
}
