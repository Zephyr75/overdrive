package scene

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/paths"
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/utils"
)

type MeshXml struct {
	Name     string `xml:"name,attr"`
	Position string `xml:"position"`
	Obj      string `xml:"obj"`
	Mtl      string `xml:"mtl"`
	// A pointer so an absent element is distinguishable from an explicit false,
	// which is what lets the default be true
	CastsShadow *bool `xml:"castsShadow"`
	// Same pointer trick, but the default is false
	Movable *bool `xml:"movable"`
}

type Mesh struct {
	Name          string
	Vertices      []mgl32.Vec3
	NormalCoords  []mgl32.Vec3
	TextureCoords []mgl32.Vec2
	Faces         [][]uint32 // one []uint32 per material group, pos/tex/norm indices interleaved in threes
	Materials     []Material
	Position      mgl32.Vec3
	// Whether the shadow bake draws this mesh, default true; false is for a
	// receiver that would contribute nothing but its own acne
	CastsShadow bool
	// Whether this mesh belongs in the dynamic shadow atlas rather than the
	// static one; MoveBy/MoveTo also set it
	Movable bool

	// World-space bounding sphere, rebuilt by fillVertices so it follows a move
	//
	// prevCenter is where it was before: a caster leaving a light's range has to
	// dirty the tile it left, which its new centre alone would not say
	boundsCenter, prevCenter mgl32.Vec3
	boundsRadius             float32

	vertexData  []float32  // Faces flattened by fillVertices into pos/normal/uv triples, interleaved
	indexGroups [][]uint32 // one index list per material group, indexing into vertexData

	backend     renderer.Backend
	vertexBuf   renderer.BufferHandle
	gpu         []renderer.MeshHandle // one handle per material face group
	needsUpdate bool                  // MoveBy/MoveTo was called since the last upload

	initialPosition mgl32.Vec3 // Position at load, what MoveBy/MoveTo offsets are relative to
}

// Offsets the mesh and rebuilds its vertex data for the next upload
func (mesh *Mesh) MoveBy(x float32, y float32, z float32) { // TODO: review
	mesh.Movable = true
	mesh.Position[0] += x
	mesh.Position[1] += y
	mesh.Position[2] += z
	mesh.fillVertices()
	mesh.needsUpdate = true
}

// Moves the mesh to a position and rebuilds its vertex data for the next upload
func (mesh *Mesh) MoveTo(dest mgl32.Vec3) { // TODO: review
	mesh.Movable = true
	mesh.Position = dest
	mesh.fillVertices()
	mesh.needsUpdate = true
}

// Parses the OBJ and MTL files an XML mesh names into geometry and materials
func (mXml MeshXml) toMesh() (Mesh, error) { // TODO: review
	obj, err := parseOBJ(paths.Mesh(mXml.Obj))
	if err != nil {
		return Mesh{}, err
	}
	materials, err := parseMTL(paths.Mesh(mXml.mtlPath()))
	if err != nil {
		return Mesh{}, err
	}
	return mXml.assemble(obj, materials), nil
}

// The MTL a mesh names, falling back to the .obj basename because an OBJ names
// its own material library and it conventionally matches
func (mXml MeshXml) mtlPath() string { // TODO: review
	if mXml.Mtl != "" {
		return mXml.Mtl
	}
	return strings.TrimSuffix(mXml.Obj, ".obj") + ".mtl"
}

// The i'th field of a line as a float32, 0 when the line is too short
func f32(fields []string, i int) float32 { // TODO: review
	if i >= len(fields) {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[i], 32)
	return float32(value)
}

// Fields 1 to 3 of a line as a vector, the form every OBJ and MTL triple takes
func vec3(fields []string) mgl32.Vec3 { // TODO: review
	return mgl32.Vec3{f32(fields, 1), f32(fields, 2), f32(fields, 3)}
}

// The shared vertex streams of an OBJ plus one index list per material group
type objData struct {
	positions     []mgl32.Vec3
	normalCoords  []mgl32.Vec3
	textureCoords []mgl32.Vec2
	faces         [][]uint32
}

// Reads an OBJ file's vertex streams and face groups
func parseOBJ(path string) (objData, error) { // TODO: review
	file, err := os.Open(path)
	if err != nil {
		return objData{}, fmt.Errorf("open OBJ: %w", err)
	}
	defer file.Close()

	var obj objData
	var face []uint32

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "v":
			obj.positions = append(obj.positions, vec3(fields))
		case "vt":
			obj.textureCoords = append(obj.textureCoords, mgl32.Vec2{f32(fields, 1), f32(fields, 2)})
		case "vn":
			obj.normalCoords = append(obj.normalCoords, vec3(fields))
		case "usemtl":
			// A new material closes the group before it, one index list each
			if len(face) > 0 {
				obj.faces = append(obj.faces, face)
				face = nil
			}
		case "f":
			face = append(face, triangleIndices(fields)...)
		}
	}
	// A read error or an over-long line ends the loop like a clean EOF, which
	// would otherwise load a silently truncated mesh
	if err := scanner.Err(); err != nil {
		return objData{}, fmt.Errorf("read OBJ: %w", err)
	}
	obj.faces = append(obj.faces, face)
	return obj, nil
}

// One face's pos/tex/norm indices, interleaved in threes
//
// Triangles carrying all three indices only, which is what the exporter writes;
// anything else is dropped rather than left to break fillVertices' stride
func triangleIndices(fields []string) []uint32 { // TODO: review
	if len(fields) < 4 {
		return nil
	}
	idx := make([]uint32, 0, 9)
	for _, corner := range fields[1:4] {
		parts := strings.Split(corner, "/")
		if len(parts) < 3 {
			return nil
		}
		for _, part := range parts[:3] {
			n, _ := strconv.ParseUint(part, 10, 32)
			idx = append(idx, uint32(n))
		}
	}
	return idx
}

// Reads an MTL file's material definitions, in the order the OBJ's groups use them
func parseMTL(path string) ([]Material, error) { // TODO: review
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open MTL: %w", err)
	}
	defer file.Close()

	var materials []Material
	material := newMaterial()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Every key below takes at least one argument
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "newmtl":
			materials = append(materials, material)
			material = newMaterial()
		case "Ns":
			material.Shininess = f32(fields, 1)
		case "Ka":
			material.Ambient = vec3(fields)
		case "Kd":
			material.Diffuse = vec3(fields)
		case "Ks":
			material.Specular = vec3(fields)
		case "d":
			material.Alpha = f32(fields, 1)
		case "Pm": // MTL PBR extension: metalness
			material.Metallic = f32(fields, 1)
		case "Pr": // MTL PBR extension: roughness
			material.Roughness = f32(fields, 1)
		case "map_Kd":
			material.TexturePath = texturePath(fields[1])
		case "map_Bump", "bump":
			material.NormalMapPath = texturePath(fields[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read MTL: %w", err)
	}
	// Drop the empty material the loop opened with, and close the last one
	materials = append(materials, material)
	return materials[1:], nil
}

// Builds the mesh from the two parsed halves and the XML's own fields
func (mXml MeshXml) assemble(obj objData, materials []Material) Mesh { // TODO: review
	pos := utils.ParseVec3(mXml.Position)
	pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}

	mesh := Mesh{
		Name:            mXml.Name,
		Vertices:        obj.positions,
		NormalCoords:    obj.normalCoords,
		TextureCoords:   obj.textureCoords,
		Faces:           obj.faces,
		Materials:       materials,
		Position:        pos,
		initialPosition: pos,
		CastsShadow:     mXml.CastsShadow == nil || *mXml.CastsShadow,
		Movable:         mXml.Movable != nil && *mXml.Movable,
	}
	mesh.fillVertices()
	mesh.prevCenter = mesh.boundsCenter
	return mesh
}

// Resolves an MTL texture reference to a project-local path
//
// Blender bakes the exporting machine's absolute path, so only the basename
// survives — otherwise a scene loads nowhere but where it was authored.
func texturePath(ref string) string { // TODO: review
	ref = strings.ReplaceAll(ref, "\\", "/")
	return paths.Texture(path.Base(ref))
}

// Flattens the OBJ face lists into the interleaved vertex array and per-group index lists
func (mesh *Mesh) fillVertices() { // TODO: review
	var value []float32
	var faces [][]uint32
	var index uint32
	index = 0
	var lo, hi mgl32.Vec3
	first := true
	for i := 0; i < len(mesh.Faces); i++ {
		var face []uint32
		for j := 0; j < len(mesh.Faces[i]); j += 3 {
			posIndex := mesh.Faces[i][j] - 1
			texIndex := mesh.Faces[i][j+1] - 1
			normIndex := mesh.Faces[i][j+2] - 1
			position := mesh.Position.Sub(mesh.initialPosition).Add(mesh.Vertices[posIndex])
			if first {
				lo, hi, first = position, position, false
			}
			for k := 0; k < 3; k++ {
				if position[k] < lo[k] {
					lo[k] = position[k]
				}
				if position[k] > hi[k] {
					hi[k] = position[k]
				}
			}
			value = append(value, position[0])
			value = append(value, position[1])
			value = append(value, position[2])
			value = append(value, mesh.NormalCoords[normIndex][0])
			value = append(value, mesh.NormalCoords[normIndex][1])
			value = append(value, mesh.NormalCoords[normIndex][2])
			value = append(value, mesh.TextureCoords[texIndex][0])
			value = append(value, mesh.TextureCoords[texIndex][1])
			face = append(face, index)
			index++
		}
		faces = append(faces, face)
	}
	mesh.vertexData = value
	mesh.indexGroups = faces

	// The AABB's bounding sphere, not a tight one: this culls casters against a
	// light's radius and a tile's frustum, where over-including is only a wasted
	// draw and under-including is a missing shadow
	mesh.boundsCenter = lo.Add(hi).Mul(0.5)
	mesh.boundsRadius = hi.Sub(lo).Len() * 0.5
}

// Uploads the mesh's vertex buffer, one mesh handle per face group, and its material textures
func (mesh *Mesh) setup(backend renderer.Backend) error { // TODO: review
	mesh.backend = backend

	// Share one vertex buffer across the face groups, each group owning only
	// its index list
	mesh.vertexBuf, _ = backend.CreateBuffer(renderer.BufferInfo{
		Name: mesh.Name, Usage: renderer.BufferVertex, Location: renderer.LocationHost,
		Data: mesh.vertexData,
	})
	mesh.gpu = make([]renderer.MeshHandle, len(mesh.indexGroups))
	for i, face := range mesh.indexGroups {
		mesh.gpu[i] = backend.CreateMesh(renderer.MeshInfo{
			Name: mesh.Name, Vertices: mesh.vertexBuf, Indices: face, Stride: meshStride,
		})
	}

	// Load the material textures recorded at parse time
	for i := range mesh.Materials {
		mat := &mesh.Materials[i]
		if mat.TexturePath != "" {
			pix, width, height, err := loadRGBA(mat.TexturePath)
			if err != nil {
				return fmt.Errorf("texture %s: %w", mat.TexturePath, err)
			}
			mat.Texture = uploadTexture(backend, mat.TexturePath, pix, width, height)
			mat.TextureSlot = int32(backend.Slot(mat.Texture))
		}
		if mat.NormalMapPath != "" {
			pix, width, height, err := loadRGBA(mat.NormalMapPath)
			if err != nil {
				return fmt.Errorf("normal map %s: %w", mat.NormalMapPath, err)
			}
			mat.NormalMap = uploadTexture(backend, mat.NormalMapPath, pix, width, height)
			mat.NormalMapSlot = int32(backend.Slot(mat.NormalMap))
		}
	}
	return nil
}

// Uploads tightly packed RGBA8 pixels as a sampled 2D image
func uploadTexture(backend renderer.Backend, name string, pixels []byte, width, height int) renderer.ImageHandle { // TODO: review
	img := backend.CreateImage(renderer.ImageInfo{
		Name: name, Width: width, Height: height, Format: renderer.FormatRGBA8,
		Usage: renderer.ImageSampled | renderer.ImageCopyDst,
	})
	backend.UpdateImage(img, renderer.ImageData{Pixels: pixels, Width: width, Height: height})
	return img
}

// Reuploads the vertex buffer when a Move marked it dirty
func (mesh *Mesh) updateVertices() { // TODO: review
	if !mesh.needsUpdate {
		return
	}
	mesh.backend.UpdateBuffer(mesh.vertexBuf, 0, mesh.vertexData)
	mesh.needsUpdate = false
}

// Draws every face group, writing its material fields into u first; the caller owns u.Model
func (mesh *Mesh) draw(ctx *drawContext, uniforms *renderer.DrawUniforms) { // TODO: review
	for i := range mesh.indexGroups {
		mat := mesh.Materials[i]

		uniforms.MatAmbient = mat.Ambient
		uniforms.MatDiffuse = mat.Diffuse
		uniforms.MatSpecular = mat.Specular
		uniforms.MatShininess = mat.Shininess
		uniforms.MatMetallic = mat.Metallic
		uniforms.MatRoughness = mat.Roughness
		uniforms.MatAo = mat.Ao
		uniforms.TexDiffuse = mat.TextureSlot // 0 is the backend's white pixel

		// Flag normal mapping per face group, the shader falling back to the
		// interpolated geometric normal without a map
		uniforms.TexNormalMap = mat.NormalMapSlot
		uniforms.UseNormalMap = 0
		if mat.NormalMap != 0 {
			uniforms.UseNormalMap = 1
		}

		ctx.draw(mesh.gpu[i], uniforms)
	}
}
