package scene

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/utils"
)

type MeshXml struct {
	Name     string `xml:"name,attr"`
	Position string `xml:"position"`
	Obj      string `xml:"obj"`
	Mtl      string `xml:"mtl"`
}

type Mesh struct {
	Name          string
	Vertices      []mgl32.Vec3
	NormalCoords  []mgl32.Vec3
	TextureCoords []mgl32.Vec2
	Faces         [][]uint32
	Materials     []Material
	Position      mgl32.Vec3

	vertexData  []float32
	indexGroups [][]uint32

	backend     renderer.Backend
	vertexBuf   renderer.BufferHandle
	gpu         []renderer.MeshHandle // one handle per material face group
	needsUpdate bool

	initialPosition mgl32.Vec3
}

func (m *Mesh) MoveBy(x float32, y float32, z float32) {
	m.Position[0] += x
	m.Position[1] += y
	m.Position[2] += z
	m.fillVertices()
	m.needsUpdate = true
}

func (m *Mesh) MoveTo(dest mgl32.Vec3) {
	m.Position = dest
	m.fillVertices()
	m.needsUpdate = true
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
			material.TexturePath = split_line[1]
		case "map_Bump":
			material.NormalMapPath = split_line[1]
		}
	}
	materials = append(materials, material)
	materials = materials[1:]

	m.Materials = materials

	return m
}

func (m *Mesh) fillVertices() {
	var value []float32
	var faces [][]uint32
	var index uint32
	index = 0
	for i := 0; i < len(m.Faces); i++ {
		var face []uint32
		for j := 0; j < len(m.Faces[i]); j += 3 {
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
			face = append(face, index)
			index++
		}
		faces = append(faces, face)
	}
	m.vertexData = value
	m.indexGroups = faces
}

func (m *Mesh) setup(b renderer.Backend) {
	m.backend = b

	// One shared vertex buffer; one mesh handle (index list) per face group.
	m.vertexBuf = b.CreateBuffer(m.vertexData, true)
	m.gpu = make([]renderer.MeshHandle, len(m.indexGroups))
	for i, face := range m.indexGroups {
		m.gpu[i] = b.CreateMesh(m.vertexBuf, face)
	}

	// GPU-load the material textures recorded at parse time.
	for i := range m.Materials {
		mat := &m.Materials[i]
		if mat.TexturePath != "" {
			tex, err := b.LoadTexture(mat.TexturePath)
			if err != nil {
				fmt.Println("Error loading texture:", err)
			}
			mat.Texture = tex
		}
		if mat.NormalMapPath != "" {
			tex, err := b.LoadTexture(mat.NormalMapPath)
			if err != nil {
				fmt.Println("Error loading normal map:", err)
			}
			mat.NormalMap = tex
		}
	}
}

func (m *Mesh) updateVertices() {
	if !m.needsUpdate {
		return
	}
	m.backend.UpdateBuffer(m.vertexBuf, m.vertexData)
	m.needsUpdate = false
}

// draw renders every face group of the mesh: the per-group material fields
// are written into u, then the group is drawn through the backend.
func (m *Mesh) draw(shader renderer.ShaderHandle, u *renderer.Uniforms) {
	for i, face := range m.indexGroups {
		mat := m.Materials[i]

		u.MatAmbient = mat.Ambient
		u.MatDiffuse = mat.Diffuse
		u.MatSpecular = mat.Specular
		u.MatShininess = mat.Shininess
		u.TexDiffuse = mat.Texture // 0 = the backend's white pixel

		m.backend.DrawMesh(shader, m.gpu[i], len(face), u)
	}
}
