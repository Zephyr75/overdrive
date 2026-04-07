# Changelog

## Cleanup & Bug Fixes — 2026-04-07

---

### Bug fix — `scene/scene.go`: `UpdateMeshes` never cleared the dirty flag

**File:** `scene/scene.go`

`UpdateMeshes` iterated with `for _, mesh := range s.Meshes`, which copies each `Mesh` value.
Calling `mesh.updateVertices()` on the copy cleared `needsUpdate` on the copy, not on the real element in the slice.
Result: once a mesh was marked dirty (e.g. by physics), it re-uploaded its VBO and EBO every single frame for the rest of the session.

**Fix:** Changed to index-based iteration so the pointer receiver operates on the original:

```go
// before
for _, mesh := range s.Meshes {
    mesh.updateVertices()
}

// after
for i := range s.Meshes {
    s.Meshes[i].updateVertices()
}
```

---

### Bug fix — `opengl/texture.go`: JPEG textures silently returned 0

**File:** `opengl/texture.go`

`CreateTexture` called `image.Decode` but the package only imported `_ "image/png"`.
Any `.jpg` texture (e.g. `container.jpg`, `white.jpg`, skybox faces) would fail to decode and return a zero texture ID, causing meshes to render without their diffuse map.

**Fix:** Added the missing blank import:

```go
_ "image/jpeg"
```

---

### Refactor — `input/callback.go` + `input/input.go`: encapsulate the scene global

**Files:** `input/callback.go`, `input/input.go`, `core/app.go`

The package-level variable `S *scene.Scene` was exported, letting `core/app.go` write directly into the `input` package's state (`input.S = s`).

**Fix:** Renamed `S` to the unexported `s` and added a `SetScene()` setter:

```go
// input/callback.go
func SetScene(scene *scene.Scene) {
    s = scene
}
```

`core/app.go` now calls `input.SetScene(s)` instead of assigning the field directly.

---

### Cleanup — `physics/sphere.go`: removed startup debug prints

**File:** `physics/sphere.go`

`NewSphereFromMesh` printed `radius` and `pos` to stdout on every call (i.e. at scene load time). Removed:

```go
println("radius", radius)
println("pos", mesh.Position[0], mesh.Position[1], mesh.Position[2])
```

---

### Cleanup — `physics/plane.go`: removed startup debug print and unused import

**File:** `physics/plane.go`

`NewPlaneFromMesh` printed the four corner vertices to stdout on every call. Removed the `fmt.Println(...)` call and the now-unused `"fmt"` import.

---

### Bug fix — `scene/mesh.go`: white fallback texture recreated on every mesh load

**File:** `scene/mesh.go`

The package-level `white` texture was re-created with `opengl.CreateTexture("textures/white.png")` at the end of every `toMesh()` call, leaking a new OpenGL texture object for each mesh in the scene.

**Fix:** Guard the creation with a zero-check so it only happens once:

```go
if white == 0 {
    white = opengl.CreateTexture("textures/white.png")
}
```

---

### Refactor — `scene/mesh.go`: hoist per-mesh uniforms out of the face loop

**File:** `scene/mesh.go`

In `Mesh.draw()`, light uniforms (`lights[i].type/color/intensity/…`), the camera `viewPos`, all four sampler unit assignments, and all three shadow texture bindings were re-uploaded inside the `for i, face := range m.openGLFaces` loop — meaning they were set redundantly for every material group of every mesh.

**Fix:** Moved everything that does not change between face groups to before the loop. Only material properties (`ambient/diffuse/specular/shininess`) and the diffuse texture (TEXTURE1) remain inside the loop since they vary per group.

Also simplified the fallback/texture binding:

```go
// before (set TEXTURE1 twice when a texture exists)
gl.ActiveTexture(gl.TEXTURE1)
gl.BindTexture(gl.TEXTURE_2D, white)
if mat.Texture != 0 {
    gl.ActiveTexture(gl.TEXTURE1)
    gl.BindTexture(gl.TEXTURE_2D, mat.Texture)
}

// after
gl.ActiveTexture(gl.TEXTURE1)
if mat.Texture != 0 {
    gl.BindTexture(gl.TEXTURE_2D, mat.Texture)
} else {
    gl.BindTexture(gl.TEXTURE_2D, white)
}
```

---

### Bug fix — `scene/scene.go`: `xml.Unmarshal` error was silently ignored

**File:** `scene/scene.go`

`xml.Unmarshal` returned an error that was discarded. A malformed scene file would silently produce an empty scene with no diagnostic.

**Fix:** The error is now checked and, if non-nil, logged and an empty `Scene` is returned:

```go
if err := xml.Unmarshal(xmlData, &sceneXml); err != nil {
    fmt.Println("Error parsing scene XML:", err)
    return Scene{}
}
```

---

### Cleanup — `core/app.go`: removed stale commented-out debug code

**File:** `core/app.go`

Removed several blocks of commented-out code that had no purpose:

- Inline `// println(...)` calls referencing removed mesh/light debug prints
- The entire commented-out depth-debug shader block (shadow map visualisation); the shader files are still present and can be re-enabled by uncommenting the program declaration
- A stale `// draw a circle` comment
- A redundant `var window *glfw.Window = app.Window` local variable inside the loop (replaced with direct use of `app.Window`)

---

### Plugin — `plugin/xml_export.py`: Blender 4.0 compatibility (previous session)

- Updated `bl_info["blender"]` from `(3, 6, 0)` to `(4, 0, 0)`
- Replaced removed `bpy.ops.export_scene.obj` with `bpy.ops.wm.obj_export` and updated all parameter names to their Blender 4.x equivalents
- `light.data.diffuse_factor` / `light.data.specular_factor` (removed in Blender 4.0) are now read via `getattr(..., 1.0)` with a safe default
