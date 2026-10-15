package scene

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/Zephyr75/overdrive/renderer"
)

type MeshToml struct {
	Name     string     `toml:"name"`
	Position mgl32.Vec3 `toml:"position"`
	// Degrees, Blender's Euler XYZ; unread, since the OBJ is exported in world space
	Rotation mgl32.Vec3 `toml:"rotation"`
	Obj      string     `toml:"obj"`
	Mtl      string     `toml:"mtl"`
	// A pointer so an absent key is distinguishable from an explicit false,
	// which is what lets the default be true
	CastsShadow *bool `toml:"castsShadow"`
	// Same pointer trick, but the default is false
	Movable *bool `toml:"movable"`
}

type Mesh struct {
	Name          string
	Vertices      []mgl32.Vec3
	NormalCoords  []mgl32.Vec3
	TextureCoords []mgl32.Vec2
	Faces         [][]uint32 // one []uint32 per material group, pos/tex/norm indices interleaved in threes
	Materials     []Material
	Position      mgl32.Vec3
	// Turn about Position since load, carried by model like a move
	Rotation mgl32.Quat
	// Whether the shadow bake draws this mesh, default true; false is for a
	// receiver that would contribute nothing but its own acne
	CastsShadow bool
	// Whether this mesh belongs in the dynamic shadow atlas rather than the
	// static one; Place also sets it
	Movable bool

	// Takes the vertices from where the OBJ put them to where Place did:
	// identity for a mesh that never moved, so static geometry is drawn exactly
	// as exported
	model mgl32.Mat4

	// World-space bounding sphere, its centre carried by model so it follows a move
	//
	// prevCenter is where it was before: a caster leaving a light's range has to
	// dirty the tile it left, which its new centre alone would not say
	boundsCenter, prevCenter mgl32.Vec3
	boundsRadius             float32
	// boundsCenter at load, which model carries to the current one
	loadCenter mgl32.Vec3

	vertexData  []float32  // Faces flattened by fillVertices into pos/normal/uv triples, interleaved
	indexGroups [][]uint32 // one index list per material group, indexing into vertexData

	vertexBuffer      renderer.BufferHandle
	meshesPerMaterial []renderer.MeshHandle // one handle per material face group
	moved             bool                  // Place was called since UpdateMeshes last looked
	// A change the static atlas holds baked in: a static mesh made movable,
	// hidden or shown. Its tiles are not tracked, so the whole atlas re-bakes
	staticChanged bool
	// Skipped by every draw and every bake, SetVisible being the only writer
	hidden bool

	initialPosition mgl32.Vec3 // Position at load, where the OBJ's world-space vertices sit
}

// Offsets the mesh and rebuilds its vertex data for the next upload
// func (mesh *Mesh) MoveBy(x float32, y float32, z float32) {
// 	mesh.Movable = true
// 	mesh.Position[0] += x
// 	mesh.Position[1] += y
// 	mesh.Position[2] += z
// 	mesh.fillVertices()
// 	mesh.needsUpdate = true
// }

// Moves and turns the mesh, rotation being about its own Position
//
// Only the model matrix changes: the vertices stay as uploaded, so a move costs
// no rebuild and no upload whatever the triangle count
func (mesh *Mesh) Place(dest mgl32.Vec3, rotation mgl32.Quat) {
	// Leaving the static atlas, where it is still baked at its old place
	if !mesh.Movable {
		mesh.staticChanged = true
	}
	mesh.Movable = true
	mesh.Position = dest
	mesh.Rotation = rotation
	// Back from where the OBJ put the mesh to the origin, turned, then out to dest
	toOrigin := mgl32.Translate3D(-mesh.initialPosition[0], -mesh.initialPosition[1], -mesh.initialPosition[2])
	mesh.model = mgl32.Translate3D(dest[0], dest[1], dest[2]).Mul4(rotation.Mat4()).Mul4(toOrigin)
	mesh.boundsCenter = mesh.model.Mul4x1(mesh.loadCenter.Vec4(1)).Vec3()
	mesh.moved = true
}

// Shows or hides the mesh in every pass, its shadow included, from the next frame
func (mesh *Mesh) SetVisible(visible bool) {
	if visible == !mesh.hidden {
		return
	}
	mesh.hidden = !visible
	// Dirties the dynamic tiles it is in, like a move
	mesh.moved = true
	if !mesh.Movable {
		mesh.staticChanged = true
	}
}

// Whether the mesh is drawn, true unless SetVisible hid it
func (mesh *Mesh) Visible() bool { return !mesh.hidden }

// The corners of the box around the vertices as loaded, before any Place
func (mesh *Mesh) Bounds() (lo, hi mgl32.Vec3) {
	if len(mesh.Vertices) == 0 {
		return lo, hi
	}
	lo, hi = mesh.Vertices[0], mesh.Vertices[0]
	for _, v := range mesh.Vertices {
		for k := range 3 {
			lo[k] = min(lo[k], v[k])
			hi[k] = max(hi[k], v[k])
		}
	}
	return lo, hi
}

// Parses the OBJ and MTL files a mesh entry names into geometry and materials
//
// Both live in meshes/ beside the scene file, so a scene folder is self-contained
func (entry MeshToml) toMesh(sceneDir string) (Mesh, error) {
	obj, err := parseOBJ(filepath.Join(sceneDir, "meshes", entry.Obj))
	if err != nil {
		return Mesh{}, err
	}
	materials, err := parseMTL(filepath.Join(sceneDir, "meshes", entry.mtlPath()), sceneDir)
	if err != nil {
		return Mesh{}, err
	}
	return entry.assemble(obj, materials), nil
}

// The MTL a mesh names, falling back to the .obj basename because an OBJ names
// its own material library and it conventionally matches
func (entry MeshToml) mtlPath() string {
	if entry.Mtl != "" {
		return entry.Mtl
	}
	return strings.TrimSuffix(entry.Obj, ".obj") + ".mtl"
}

// The i'th field of a line as a float32, 0 when the line is too short
func f32(fields []string, i int) float32 {
	if i >= len(fields) {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[i], 32)
	return float32(value)
}

// Fields 1 to 3 of a line as a vector, the form every OBJ and MTL triple takes
func vec3(fields []string) mgl32.Vec3 {
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
func parseOBJ(path string) (objData, error) {
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
func triangleIndices(fields []string) []uint32 {
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
func parseMTL(path string, sceneDir string) ([]Material, error) {
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
		case "Kd":
			material.Diffuse = vec3(fields)
		case "d":
			material.Alpha = f32(fields, 1)
		case "Pm": // MTL PBR extension: metalness
			material.Metallic = f32(fields, 1)
		case "Pr": // MTL PBR extension: roughness
			material.Roughness = f32(fields, 1)
		// A map line may carry options before its file (map_Bump -bm 1.0 f.png),
		// so the file is the last field: a path with spaces is not supported
		case "map_Kd":
			material.TexturePath = texturePath(sceneDir, fields[len(fields)-1])
		case "map_Bump", "bump", "norm":
			material.NormalMapPath = texturePath(sceneDir, fields[len(fields)-1])
		// map_Pr is the PBR extension's key; Blender writes the same roughness
		// image under map_Ns when the extension is off
		case "map_Pr", "map_Ns":
			material.RoughnessMapPath = texturePath(sceneDir, fields[len(fields)-1])
		case "map_Pm":
			material.MetallicMapPath = texturePath(sceneDir, fields[len(fields)-1])
		case "map_d":
			material.AlphaMapPath = texturePath(sceneDir, fields[len(fields)-1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read MTL: %w", err)
	}
	// Drop the empty material the loop opened with, and close the last one
	materials = append(materials, material)
	return materials[1:], nil
}

// Builds the mesh from the two parsed halves and the entry's own fields
func (entry MeshToml) assemble(obj objData, materials []Material) Mesh {
	pos := mgl32.Vec3{entry.Position[0], entry.Position[2], -entry.Position[1]}

	mesh := Mesh{
		Name:            entry.Name,
		Vertices:        obj.positions,
		NormalCoords:    obj.normalCoords,
		TextureCoords:   obj.textureCoords,
		Faces:           obj.faces,
		Materials:       materials,
		Position:        pos,
		Rotation:        mgl32.QuatIdent(),
		model:           mgl32.Ident4(),
		initialPosition: pos,
		CastsShadow:     entry.CastsShadow == nil || *entry.CastsShadow,
		Movable:         entry.Movable != nil && *entry.Movable,
	}
	mesh.fillVertices()
	mesh.loadCenter = mesh.boundsCenter
	mesh.prevCenter = mesh.boundsCenter
	return mesh
}

// Resolves an MTL texture reference to textures/ beside the scene file
//
// Blender bakes the exporting machine's absolute path, so only the basename
// survives — otherwise a scene loads nowhere but where it was authored.
func texturePath(sceneDir, ref string) string {
	ref = strings.ReplaceAll(ref, "\\", "/")
	return filepath.Join(sceneDir, "textures", path.Base(ref))
}

// Flattens the OBJ face lists into the interleaved vertex array and per-group index lists
func (mesh *Mesh) fillVertices() {
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
			position, normal := mesh.Vertices[posIndex], mesh.NormalCoords[normIndex]
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
			value = append(value, normal[0])
			value = append(value, normal[1])
			value = append(value, normal[2])
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
func (mesh *Mesh) setup(backend renderer.Backend, textures map[string]*decodedTexture) error {

	// Share one vertex buffer across the face groups, each group owning only its index list
	mesh.vertexBuffer, _ = backend.CreateBuffer(renderer.BufferSpec{
		Name: mesh.Name, Usage: renderer.BufferVertex, Location: renderer.LocationHost,
		InitialData: mesh.vertexData,
	})
	mesh.meshesPerMaterial = make([]renderer.MeshHandle, len(mesh.indexGroups))
	for i, face := range mesh.indexGroups {
		mesh.meshesPerMaterial[i] = backend.CreateMesh(renderer.MeshSpec{
			Name: mesh.Name, Vertices: mesh.vertexBuffer, Indices: face, Stride: meshStride,
		})
	}

	// Load the material textures recorded at parse time
	for i := range mesh.Materials {
		mat := &mesh.Materials[i]
		var err error
		mat.Texture, mat.TextureSlot, err = loadTexture(backend, textures, mat.TexturePath)
		if err != nil {
			return fmt.Errorf("texture %s: %w", mat.TexturePath, err)
		}
		mat.NormalMap, mat.NormalMapSlot, err = loadTexture(backend, textures, mat.NormalMapPath)
		if err != nil {
			return fmt.Errorf("normal map %s: %w", mat.NormalMapPath, err)
		}
		mat.RoughnessMap, mat.RoughnessMapSlot, err = loadTexture(backend, textures, mat.RoughnessMapPath)
		if err != nil {
			return fmt.Errorf("roughness map %s: %w", mat.RoughnessMapPath, err)
		}
		mat.MetallicMap, mat.MetallicMapSlot, err = loadTexture(backend, textures, mat.MetallicMapPath)
		if err != nil {
			return fmt.Errorf("metallic map %s: %w", mat.MetallicMapPath, err)
		}
		mat.AlphaMap, mat.AlphaMapSlot, err = loadTexture(backend, textures, mat.AlphaMapPath)
		if err != nil {
			return fmt.Errorf("alpha map %s: %w", mat.AlphaMapPath, err)
		}
		if mat.AlphaMap != 0 {
			mat.AlphaChannel = textures[mat.AlphaMapPath].alphaChannel
			mat.alphaMapTranslucent = textures[mat.AlphaMapPath].translucent
		}
	}
	return nil
}

// Uploads one material texture decodeTextures prepared, slot 0 (the white pixel) when path is empty
func loadTexture(backend renderer.Backend, textures map[string]*decodedTexture, path string) (renderer.ImageHandle, int32, error) {
	if path == "" {
		return 0, 0, nil
	}
	decoded := textures[path]
	if decoded.err != nil {
		return 0, 0, decoded.err
	}
	levels := decoded.levels
	img := backend.CreateImage(renderer.ImageSpec{
		Name: path, Width: levels[0].width, Height: levels[0].height, Format: renderer.FormatRGBA8,
		Usage: renderer.ImageSampled | renderer.ImageCopyDst, MipLevels: len(levels),
	})
	for mip, level := range levels {
		backend.UpdateImage(img, renderer.ImageData{Pixels: level.pixels, Width: level.width, Height: level.height, Mip: mip})
	}
	return img, int32(backend.Slot(img)), nil
}

// Draws the opaque fragments of every face group that has some. The rest are
// RenderTransparent's, drawn sorted after everything opaque and casting no shadow
func (mesh *Mesh) draw(ctx *drawContext, uniforms *renderer.DrawUniforms) {
	if mesh.hidden {
		return
	}
	for i := range mesh.indexGroups {
		mat := &mesh.Materials[i]
		if !mat.opaquePart() {
			continue
		}
		// A group with translucent fragments needs the pipeline whose fragment
		// stage discards them, which the rest are kept off: a discard costs a
		// pipeline its early depth test
		pipeline := ctx.pipeline
		if mat.translucentPart() {
			pipeline = ctx.cutout
		}
		mesh.drawGroup(ctx, pipeline, uniforms, i)
	}
}

// Draws one face group, writing its model matrix and material fields into uniforms first
func (mesh *Mesh) drawGroup(ctx *drawContext, pipeline renderer.PipelineHandle, uniforms *renderer.DrawUniforms, i int) {
	mat := &mesh.Materials[i]
	uniforms.Model = mesh.model
	uniforms.MatDiffuse = mat.Diffuse
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
	// The shader multiplies map and scalar, so a mapped material's scalar must
	// be 1, as Blender ignores a socket's own value once a texture drives it
	uniforms.TexRoughness = mat.RoughnessMapSlot
	if mat.RoughnessMap != 0 {
		uniforms.MatRoughness = 1
	}
	uniforms.TexMetallic = mat.MetallicMapSlot
	if mat.MetallicMap != 0 {
		uniforms.MatMetallic = 1
	}
	uniforms.TexAlpha = mat.AlphaMapSlot
	uniforms.AlphaChannel = mat.AlphaChannel
	uniforms.MatAlpha = mat.Alpha

	ctx.draw(pipeline, mesh.meshesPerMaterial[i], uniforms)
}
