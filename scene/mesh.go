package scene

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/mathgl/mgl32"

	// "github.com/go-gl/glfw/v3.3/glfw"
  "overdrive/opengl"
  "overdrive/utils"

	"math"
)

var (
  white uint32
  // white = opengl.CreateTexture("/home/zeph/Pictures/black_on_white.png")//"textures/white.png")
)

type MeshXml struct {
  Name string `xml:"name,attr"`
  Position string `xml:"position"`
  Obj string `xml:"obj"`
  Mtl string `xml:"mtl"`
}

type Mesh struct {
  Name string
  Vertices []mgl32.Vec3
  NormalCoords []mgl32.Vec3
  TextureCoords []mgl32.Vec2
  Faces [][]uint32
  Materials []Material
  Position mgl32.Vec3

  openGLVertices []float32
  openGLFaces [][]uint32

  vbo uint32
  vao []uint32
  ebo uint32
	// depthMapFBO uint32
  needsUpdate bool

  initialPosition mgl32.Vec3
}

func (m *Mesh) MoveBy(x float32, y float32, z float32) {
  m.Position[0] += x
  m.Position[1] += y
  m.Position[2] += z
  m.fillVertices()
  m.needsUpdate = true
  // m.UpdateVertices()
}

func (m *Mesh) MoveTo(dest mgl32.Vec3) {
  m.Position = dest
  m.fillVertices()
  m.needsUpdate = true
  // m.UpdateVertices()
}


func (mXml MeshXml) toMesh() Mesh {
  objFile, err := os.Open("assets/meshes/" + mXml.Obj)
  if err != nil {
    fmt.Println("Error opening file:", err)
    return Mesh{}
  }
  defer objFile.Close()

  var faces [][]uint32
  var face []uint32

  var positions []mgl32.Vec3
  var normalCoords []mgl32.Vec3
  var textureCoords []mgl32.Vec2

  objScanner := bufio.NewScanner(objFile) 
  for objScanner.Scan() {
    line := objScanner.Text()
    split_line := strings.Split(line, " ")
    // // remove leading space
    // if split_line[0] == "" {
    //   split_line = split_line[1:]
    // }
    switch line[0] {
    case 'v':
      first, _ := strconv.ParseFloat(split_line[1], 32)
      second, _ := strconv.ParseFloat(split_line[2], 32)
      switch line[1] {
      case ' ': 
        third, _ := strconv.ParseFloat(split_line[3], 32)
        positions = append(positions, mgl32.Vec3{float32(first), float32(second), float32(third)})
      case 't':
        textureCoords = append(textureCoords, mgl32.Vec2{float32(first), float32(second)})
      case 'n':
        third, _ := strconv.ParseFloat(split_line[3], 32)
        normalCoords = append(normalCoords, mgl32.Vec3{float32(first), float32(second), float32(third)})
      }
    case 'u':
      if len(face) > 0 {
        faces = append(faces, face)
        face = nil
      }
    case 'f':
      for i := 0; i < 3; i++ {
        split_face := strings.Split(split_line[i+1], "/")
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

  pos := utils.ParseVec3(mXml.Position)
  pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}

  var m Mesh
  m.Faces = faces
  m.Vertices = positions
  m.NormalCoords = normalCoords
  m.TextureCoords = textureCoords
  m.Name = mXml.Name
  m.Position = pos
  m.initialPosition = pos

  m.fillVertices()
  // m.fillFaces()

  // fmt.Println(m.OpenGLVertices)
  // fmt.Println(m.OpenGLFaces)

  
  mtlFile, err := os.Open("assets/meshes/" + mXml.Mtl)
  if err != nil {
    fmt.Println("Error opening file:", err)
    return Mesh{}
  }
  defer mtlFile.Close()

  var materials []Material
  var material Material

  mtlScanner := bufio.NewScanner(mtlFile)
  for mtlScanner.Scan() {
    line := mtlScanner.Text()
    split_line := strings.Split(line, " ")
    switch split_line[0] {
    case "newmtl":
      materials = append(materials, material)
      material = Material{}
    case "Ns":
      first, _ := strconv.ParseFloat(split_line[1], 32)
      material.Shininess = float32(first)
    case "Ka":
      first, _ := strconv.ParseFloat(split_line[1], 32)
      second, _ := strconv.ParseFloat(split_line[2], 32)
      third, _ := strconv.ParseFloat(split_line[3], 32)
      material.Ambient = mgl32.Vec3{float32(first), float32(second), float32(third)}
    case "Kd":
      first, _ := strconv.ParseFloat(split_line[1], 32)
      second, _ := strconv.ParseFloat(split_line[2], 32)
      third, _ := strconv.ParseFloat(split_line[3], 32)
      material.Diffuse = mgl32.Vec3{float32(first), float32(second), float32(third)}
    case "Ks":
      first, _ := strconv.ParseFloat(split_line[1], 32)
      second, _ := strconv.ParseFloat(split_line[2], 32)
      third, _ := strconv.ParseFloat(split_line[3], 32)
      material.Specular = mgl32.Vec3{float32(first), float32(second), float32(third)}
    case "d":
      first, _ := strconv.ParseFloat(split_line[1], 32)
      material.Alpha = float32(first)
    case "map_Kd":
      texture := opengl.CreateTexture(split_line[1])
      material.Texture = texture
    case "map_Bump":
      texture := opengl.CreateTexture(split_line[1])
      material.NormalMap = texture
    }
  }
  materials = append(materials, material)
  materials = materials[1:]

  m.Materials = materials
      

  // for i := 0; i < len(m.Materials); i++ {
  //   fmt.Println(m.Materials[i])
  // }

  // TODO: find better place for this
  white = opengl.CreateTexture("textures/white.png")


  return m
}

func (m *Mesh) fillVertices() {
  var value []float32
  var faces [][]uint32
  var index uint32
  // m.OpenGLVertices = nil
  // m.OpenGLFaces = nil
  index = 0
  for i := 0; i < len(m.Faces); i++ {
    var face []uint32
    for j := 0; j < len(m.Faces[i]); j+=3 {
      posIndex := m.Faces[i][j] - 1
      texIndex := m.Faces[i][j+1] - 1
      normIndex := m.Faces[i][j+2] - 1
      position := m.Position.Sub(m.initialPosition).Add(m.Vertices[posIndex])
      value = append(value, position[0])
      value = append(value, position[1])
      value = append(value, position[2])
      value = append(value, m.NormalCoords[normIndex][0])
      value = append(value, m.NormalCoords[normIndex][1])
      value = append(value, m.NormalCoords[normIndex][2])
      value = append(value, m.TextureCoords[texIndex][0])
      value = append(value, m.TextureCoords[texIndex][1])
      // mapVertices[int(posIndex)] = value
      face = append(face, index)
      index++
    }
    faces = append(faces, face)
    // m.OpenGLFaces = append(m.OpenGLFaces, face)
  }
  m.openGLVertices = value
  m.openGLFaces = faces
}

func (m *Mesh) fillFaces() {
  for i := 0; i < len(m.Faces); i++ {
    var faces []uint32
    for j := 0; j < len(m.Faces[i]); j+=3 {
      faces = append(faces, m.Faces[i][j]-1)
    }
    m.openGLFaces = append(m.openGLFaces, faces)
  }
}


func (m *Mesh) setup() {
  m.vao = make([]uint32, len(m.Faces))
  for i, face := range m.openGLFaces {
    // Select submesh faces
    // faces := m.OpenGLFaces[i]

    // Declare VBO, EBO and VAO
    gl.GenBuffers(1, &m.ebo)
    gl.GenBuffers(1, &m.vbo)
    gl.GenVertexArrays(1, &m.vao[i])

    // Bind VAO to VBO and gl.VertexAttribPointer, gl.EnableVertexAttribArray calls
    gl.BindVertexArray(m.vao[i])
    // Copy VBO to an OpenGL buffer
    gl.BindBuffer(gl.ARRAY_BUFFER, m.vbo)
    // Define OpenGL buffer structure
    gl.BufferData(gl.ARRAY_BUFFER, len(m.openGLVertices)*4, gl.Ptr(m.openGLVertices), gl.STATIC_DRAW)
    // Copy EBO to an OpenGL buffer
    gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, m.ebo)
    // Define OpenGL buffer structure
    gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(face)*4, gl.Ptr(face), gl.STATIC_DRAW)

    // Define Vertex Attrib to be used by the shader
    gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
    gl.EnableVertexAttribArray(0)
    gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(3*4))
    gl.EnableVertexAttribArray(1)
    gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))
    gl.EnableVertexAttribArray(2)

    // Clear VAO binding
    gl.BindVertexArray(0)


		// gl.GenFramebuffers(1, &m.fbo)

  }
}

func (m *Mesh) updateVertices() {
  if !m.needsUpdate {
    return
  }
  for i, face := range m.openGLFaces {
    gl.BindVertexArray(m.vao[i])
    gl.BindBuffer(gl.ARRAY_BUFFER, m.vbo)
    gl.BufferData(gl.ARRAY_BUFFER, len(m.openGLVertices)*4, gl.Ptr(m.openGLVertices), gl.STATIC_DRAW)
    gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, m.ebo)
    gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(face)*4, gl.Ptr(face), gl.STATIC_DRAW)
    gl.BindVertexArray(0)
  }
  // println("updated")
  m.needsUpdate = false

}

func (m *Mesh) draw(program uint32, scene *Scene) {
  for i, face := range m.openGLFaces{
    mat := m.Materials[i]

    // Define light properties
		for i, light := range scene.Lights {
			// fmt.Println(light.Dir)


			lightTypeLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].type\x00", i)))
			gl.Uniform1i(lightTypeLoc, int32(light.Type))

			lightConstantLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].constant\x00", i)))
			gl.Uniform1f(lightConstantLoc, 1.0)

			lightLinearLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].linear\x00", i)))
			gl.Uniform1f(lightLinearLoc, 0.09)

			lightQuadraticLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].quadratic\x00", i)))
			gl.Uniform1f(lightQuadraticLoc, 0.032)

			lightCutoffLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].cutoff\x00", i)))
			gl.Uniform1f(lightCutoffLoc, float32(math.Cos(math.Pi/4)))

			lightColorLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].color\x00", i)))
			gl.Uniform3f(lightColorLoc, light.Color.X(), light.Color.Y(), light.Color.Z())

			lightIntensityLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].intensity\x00", i)))
			gl.Uniform1f(lightIntensityLoc, light.Intensity)

			lightDiffuseLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].diffuse\x00", i)))
			gl.Uniform1f(lightDiffuseLoc, light.Diffuse)

			lightSpecularLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].specular\x00", i)))
			gl.Uniform1f(lightSpecularLoc, light.Specular)

			lightPosLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].position\x00", i)))
			gl.Uniform3f(lightPosLoc, light.Pos.X(), light.Pos.Y(), light.Pos.Z())
			// gl.Uniform3f(lightPosLoc, Cam.Pos.X(), Cam.Pos.Y(), Cam.Pos.Z())

			lightDirLoc := gl.GetUniformLocation(program, gl.Str(fmt.Sprintf("lights[%d].direction\x00", i)))
			gl.Uniform3f(lightDirLoc, light.Dir.X(), light.Dir.Y(), light.Dir.Z())

			// fmt.Println(light.Dir)
		}

    viewPosLoc := gl.GetUniformLocation(program, gl.Str("viewPos\x00"))
    gl.Uniform3f(viewPosLoc, scene.Cam.Pos.X(), scene.Cam.Pos.Y(), scene.Cam.Pos.Z())

    // Define material properties
    matAmbientLoc := gl.GetUniformLocation(program, gl.Str("material.ambient\x00"))
    gl.Uniform3f(matAmbientLoc, mat.Ambient.X(), mat.Ambient.Y(), mat.Ambient.Z())

    matDiffuseLoc := gl.GetUniformLocation(program, gl.Str("material.diffuse\x00"))
    gl.Uniform3f(matDiffuseLoc, mat.Diffuse.X(), mat.Diffuse.Y(), mat.Diffuse.Z())

    matSpecularLoc := gl.GetUniformLocation(program, gl.Str("material.specular\x00"))
    gl.Uniform3f(matSpecularLoc, mat.Specular.X(), mat.Specular.Y(), mat.Specular.Z())

    matShineLoc := gl.GetUniformLocation(program, gl.Str("material.shininess\x00"))
    gl.Uniform1f(matShineLoc, mat.Shininess)


    shadowMapLoc := gl.GetUniformLocation(program, gl.Str("shadowMap\x00"))
    gl.Uniform1i(shadowMapLoc, 0)

    ourTextureLoc := gl.GetUniformLocation(program, gl.Str("ourTexture\x00"))
    gl.Uniform1i(ourTextureLoc, 1)

    shadowCubeMapLoc := gl.GetUniformLocation(program, gl.Str("shadowCubeMap\x00"))
    gl.Uniform1i(shadowCubeMapLoc, 2)

    skyboxLoc := gl.GetUniformLocation(program, gl.Str("skybox\x00"))
    gl.Uniform1i(skyboxLoc, 3)

    gl.ActiveTexture(gl.TEXTURE0)
    gl.BindTexture(gl.TEXTURE_2D, scene.Lights[1].depthMap)

    gl.ActiveTexture(gl.TEXTURE1)
    gl.BindTexture(gl.TEXTURE_2D, white)
    if mat.Texture != 0 {
      gl.ActiveTexture(gl.TEXTURE1)
      gl.BindTexture(gl.TEXTURE_2D, mat.Texture)
    }

    gl.ActiveTexture(gl.TEXTURE2)
    gl.BindTexture(gl.TEXTURE_CUBE_MAP, scene.Lights[0].depthCubeMap)

    gl.ActiveTexture(gl.TEXTURE3)
    gl.BindTexture(gl.TEXTURE_CUBE_MAP, scene.Skybox.Texture)


    // if mat.NormalMap != 0 {
    //   gl.BindTexture(gl.TEXTURE_2D, mat.NormalMap)
    // }

    // faces := m.OpenGLFaces[i]
    // if len(m.OpenGLFaces) > 1 {
    //   faces = append(faces, m.OpenGLFaces[1]...)
    // }

    // fmt.Println("vertices: ", m.OpenGLVertices)
    // fmt.Println("faces: ", m.OpenGLFaces)

    gl.BindVertexArray(m.vao[i])
    gl.DrawElements(gl.TRIANGLES, int32(len(face)), gl.UNSIGNED_INT, gl.PtrOffset(0))
    gl.BindVertexArray(0)

  }

}

