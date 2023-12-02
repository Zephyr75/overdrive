package scene

import (
	"overdrive/opengl"

	"github.com/go-gl/gl/v4.1-core/gl"
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
    // "/home/zeph/GitHub/overdrive/textures/square.png",
    // "/home/zeph/GitHub/overdrive/textures/square.png",
    // "/home/zeph/GitHub/overdrive/textures/square.png",
    // "/home/zeph/GitHub/overdrive/textures/square.png",
    // "/home/zeph/GitHub/overdrive/textures/square.png",
    // "/home/zeph/GitHub/overdrive/textures/square.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/right.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/left.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/top.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/bottom.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/front.png",
    "/home/zeph/GitHub/overdrive/textures/skybox/back.png",
  })
}
