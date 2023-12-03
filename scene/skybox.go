package scene

import (
	"overdrive/opengl"
  "overdrive/settings"

	"github.com/go-gl/gl/v4.1-core/gl"
  "github.com/go-gl/mathgl/mgl32"
)

type Skybox struct {
  Vbo uint32
  Vao uint32
  vertices []float32
  Texture uint32
}

func (s *Skybox) Setup() {
  s.vertices = []float32{
    // positions
    -1.0,  1.0, -1.0,
    -1.0, -1.0, -1.0,
     1.0, -1.0, -1.0,
     1.0, -1.0, -1.0,
     1.0,  1.0, -1.0,
    -1.0,  1.0, -1.0,

    -1.0, -1.0,  1.0,
    -1.0, -1.0, -1.0,
    -1.0,  1.0, -1.0,
    -1.0,  1.0, -1.0,
    -1.0,  1.0,  1.0,
    -1.0, -1.0,  1.0,

     1.0, -1.0, -1.0,
     1.0, -1.0,  1.0,
     1.0,  1.0,  1.0,
     1.0,  1.0,  1.0,
     1.0,  1.0, -1.0,
     1.0, -1.0, -1.0,

    -1.0, -1.0,  1.0,
    -1.0,  1.0,  1.0,
     1.0,  1.0,  1.0,
     1.0,  1.0,  1.0,
     1.0, -1.0,  1.0,
    -1.0, -1.0,  1.0,

    -1.0,  1.0, -1.0,
     1.0,  1.0, -1.0,
     1.0,  1.0,  1.0,
     1.0,  1.0,  1.0,
    -1.0,  1.0,  1.0,
    -1.0,  1.0, -1.0,

    -1.0, -1.0, -1.0,
    -1.0, -1.0,  1.0,
     1.0, -1.0, -1.0,
     1.0, -1.0, -1.0,
    -1.0, -1.0,  1.0,
     1.0, -1.0,  1.0,
  }

  gl.GenVertexArrays(1, &s.Vao)
  gl.GenBuffers(1, &s.Vbo)
  gl.BindVertexArray(s.Vao)
  gl.BindBuffer(gl.ARRAY_BUFFER, s.Vbo)
  gl.BufferData(gl.ARRAY_BUFFER, len(s.vertices) * 4, gl.Ptr(s.vertices), gl.STATIC_DRAW)
  gl.EnableVertexAttribArray(0)
  gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 3 * 4, gl.PtrOffset(0))
  
  s.Texture = opengl.CreateCubemap([]string{
    "/home/zeph/GitHub/overdrive/textures/skybox/right.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/left.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/top.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/bottom.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/front.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/back.png",
  })
}

func (s Skybox) RenderSkybox(skyboxProgram uint32) {
  gl.DepthFunc(gl.LEQUAL)
  gl.UseProgram(skyboxProgram)

  view := mgl32.LookAtV(Cam.Pos, Cam.Pos.Add(Cam.Front), Cam.Up)
  view = view.Mat3().Mat4()
  viewLoc := gl.GetUniformLocation(skyboxProgram, gl.Str("view\x00"))
  gl.UniformMatrix4fv(viewLoc, 1, false, &view[0])

  projection := mgl32.Perspective(mgl32.DegToRad(Cam.Fov), float32(settings.WindowWidth)/float32(settings.WindowHeight), 0.1, 100.0)
  projectionLoc := gl.GetUniformLocation(skyboxProgram, gl.Str("projection\x00"))
  gl.UniformMatrix4fv(projectionLoc, 1, false, &projection[0])

  skyboxLoc := gl.GetUniformLocation(skyboxProgram, gl.Str("skybox\x00"))
  gl.Uniform1i(skyboxLoc, 0)

  gl.BindVertexArray(s.Vao)
  gl.ActiveTexture(gl.TEXTURE0)
  gl.BindTexture(gl.TEXTURE_CUBE_MAP, s.Texture)
  gl.DrawArrays(gl.TRIANGLES, 0, 36)
  gl.BindVertexArray(0)
  gl.DepthFunc(gl.LESS)
}
