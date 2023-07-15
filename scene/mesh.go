package scene

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/go-gl/glfw/v3.3/glfw"
  "overdrive/opengl"
)

type MeshXml struct {
  Obj string `xml:"obj"`
  Mtl string `xml:"mtl"`
}

type Mesh struct {
  Positions []mgl32.Vec3
  NormalCoords []mgl32.Vec3
  TextureCoords []mgl32.Vec2

  Vertices []float32
  Faces [][]uint32

  vbo uint32
  vao uint32
  ebo uint32
}



func (mXml MeshXml) ToMesh() Mesh {
  objFile, err := os.Open("assets/meshes/" + mXml.Obj)
  if err != nil {
    fmt.Println("Error opening file:", err)
    return Mesh{}
  }
  defer objFile.Close()

  // var positions []float32
  // var normals []float32
  // var textures []float32
  // var vertices []float32
  var faces [][]uint32
  var face []uint32

  var positions []mgl32.Vec3
  var normalCoords []mgl32.Vec3
  var textureCoords []mgl32.Vec2

  objScanner := bufio.NewScanner(objFile) 
  for objScanner.Scan() {
    line := objScanner.Text()
    split_line := strings.Split(line[2:], " ")
    // remove leading space
    if split_line[0] == "" {
      split_line = split_line[1:]
    }
    switch line[0] {
    case 'v':
      first, _ := strconv.ParseFloat(split_line[0], 32)
      second, _ := strconv.ParseFloat(split_line[1], 32)
      switch line[1] {
      case ' ': 
        third, _ := strconv.ParseFloat(split_line[2], 32)
        positions = append(positions, mgl32.Vec3{float32(first), float32(second), float32(third)})
        // positions = append(positions, float32(first))
        // positions = append(positions, float32(second))
        // positions = append(positions, float32(third))
      case 't':
        textureCoords = append(textureCoords, mgl32.Vec2{float32(first), float32(second)})
        // textures = append(textures, float32(first))
        // textures = append(textures, float32(second))
      case 'n':
        third, _ := strconv.ParseFloat(split_line[2], 32)
        normalCoords = append(normalCoords, mgl32.Vec3{float32(first), float32(second), float32(third)})
        // normals = append(normals, float32(first))
        // normals = append(normals, float32(second))
        // normals = append(normals, float32(third))
      }
    case 'u':
      if len(face) > 0 {
        faces = append(faces, face)
        face = nil
      }
    case 'f':
      for i := 0; i < 3; i++ {
        split_face := strings.Split(split_line[i], "/")
        first, _ := strconv.ParseUint(split_face[0], 10, 32)
        second, _ := strconv.ParseUint(split_face[1], 10, 32)
        third, _ := strconv.ParseUint(split_face[2], 10, 32)
        face = append(face, uint32(first))
        face = append(face, uint32(second))
        face = append(face, uint32(third))
      }
    }
  }
  faces = append(faces, face)

  // for i := 0; i < len(faces); i++ {
  //   for j := 0; j < len(faces[i]); j+=3 {
  //     posIndex := faces[i][j] - 1
  //     texIndex := faces[i][j+1] - 1
  //     normIndex := faces[i][j+2] - 1
  //     vertices = append(vertices, positions[posIndex*3])
  //     vertices = append(vertices, positions[posIndex*3+1])
  //     vertices = append(vertices, positions[posIndex*3+2])
  //     vertices = append(vertices, normals[normIndex*3])
  //     vertices = append(vertices, normals[normIndex*3+1])
  //     vertices = append(vertices, normals[normIndex*3+2])
  //     vertices = append(vertices, textures[texIndex*2])
  //     vertices = append(vertices, textures[texIndex*2+1])
  //   }
  // }

  // m.Vertices = append(m.Vertices, vertices)
  var m Mesh
  m.Faces = faces
  m.Positions = positions
  m.NormalCoords = normalCoords
  m.TextureCoords = textureCoords

  m.fillVertices()

  return m
}

func (m *Mesh) fillVertices() {
  for i := 0; i < len(m.Faces); i++ {
    for j := 0; j < len(m.Faces[i]); j+=3 {
      posIndex := m.Faces[i][j] - 1
      texIndex := m.Faces[i][j+1] - 1
      normIndex := m.Faces[i][j+2] - 1
      m.Vertices = append(m.Vertices, m.Positions[posIndex][0])
      m.Vertices = append(m.Vertices, m.Positions[posIndex][1])
      m.Vertices = append(m.Vertices, m.Positions[posIndex][2])
      m.Vertices = append(m.Vertices, m.NormalCoords[normIndex][0])
      m.Vertices = append(m.Vertices, m.NormalCoords[normIndex][1])
      m.Vertices = append(m.Vertices, m.NormalCoords[normIndex][2])
      m.Vertices = append(m.Vertices, m.TextureCoords[texIndex][0])
      m.Vertices = append(m.Vertices, m.TextureCoords[texIndex][1])
    }
  }
}


func (m *Mesh) Setup() {
  // Declare VBO, EBO and VAO
  gl.GenBuffers(1, &m.ebo)
  gl.GenBuffers(1, &m.vbo)
  gl.GenVertexArrays(1, &m.vao)

  faces := m.Faces[0]

  fmt.Println("faces: ", m.Faces)

  // Bind VAO to VBO and gl.VertexAttribPointer, gl.EnableVertexAttribArray calls
  gl.BindVertexArray(m.vao)
  // Copy VBO to an OpenGL buffer
  gl.BindBuffer(gl.ARRAY_BUFFER, m.vbo)
  // Define OpenGL buffer structure
  gl.BufferData(gl.ARRAY_BUFFER, len(m.Vertices)*4, gl.Ptr(m.Vertices), gl.STATIC_DRAW)
  // Copy EBO to an OpenGL buffer
  gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, m.ebo)
  // Define OpenGL buffer structure
  gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(faces)*4, gl.Ptr(faces), gl.STATIC_DRAW)

  // Define Vertex Attrib to be used by the shader
  gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
  gl.EnableVertexAttribArray(0)
  gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(3*4))
  gl.EnableVertexAttribArray(1)
  gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))
  gl.EnableVertexAttribArray(2)

  // Clear VAO binding
  gl.BindVertexArray(0)
}

func (m *Mesh) Draw(program uint32, scene *Scene) {

   lightColorLoc := gl.GetUniformLocation(program, gl.Str("lightColor\x00"))
   gl.Uniform3f(lightColorLoc, 1.0, 0.0, 1.0)

   lightPosLoc := gl.GetUniformLocation(program, gl.Str("lightPos\x00"))
   gl.Uniform3f(lightPosLoc, 1.2, float32(glfw.GetTime()) - 5.0, 1.0)

   viewPosLoc := gl.GetUniformLocation(program, gl.Str("viewPos\x00"))
   gl.Uniform3f(viewPosLoc, scene.Cam.Pos.X(), scene.Cam.Pos.Y(), scene.Cam.Pos.Z())


  // assign specular, diffuse and whatever
  // assign lights vector

  texture, err := opengl.CreateTexture("textures/square.png")
  if err != nil {
    panic(err)
  }
  gl.BindTexture(gl.TEXTURE_2D, texture)

  gl.BindVertexArray(m.vao)
  gl.DrawElements(gl.TRIANGLES, int32(len(m.Faces)), gl.UNSIGNED_INT, gl.PtrOffset(0))
  gl.BindVertexArray(0)

}
