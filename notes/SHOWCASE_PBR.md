# Showcase scene: PBR and environment lighting

What changed so that `assets/showcase/` renders in Overdrive with its PBR
materials and the `cedar_bridge_sunset_1` environment, and why each change is
built the way it is. Written 2026-10-08.

```sh
cd src
SLANGC=/opt/shader-slang-bin/bin/slangc ./build_shaders.sh   # common.slang changed
go run .                                  # showcase/showcase.xml is now the default scene
go run . -screenshot out.png              # frame 90, comparable with assets/showcase/showcase.png
```

## 1. What was broken

Before any change, `go run . -scene showcase/showcase.xml` panicked on the first
mesh (`open .../assets/meshes/Football.obj: no such file`). Behind that came a
chain of problems, each hiding the next:

| # | Problem | Where |
|---|---|---|
| 1 | Meshes resolved to `assets/meshes/`, textures to `assets/textures/`; the showcase keeps both under its own folder | `scene/mesh.go` `toMesh`, `texturePath` |
| 2 | `map_Bump -bm 1.0 file` was read as the file `-bm` | `scene/mesh.go` `parseMTL` |
| 3 | Roughness maps were ignored; Blender also wrote no `Pr`/`Pm` because the exporter did not ask for PBR extensions | `parseMTL`, `xml_export.py` |
| 4 | Roughness and normal maps are EXR (PIZ / DWAA compressed); Go decodes only PNG and JPEG | `scene/image.go` |
| 5 | The environment was six hard-coded LDR PNG cube faces from `assets/textures/skybox/`, which no longer exist | `scene/skybox.go` |
| 6 | Ambient light was one raw cubemap tap ×0.35 plus one mirror tap: no roughness | `forward.slang` |
| 7 | Images were single-mip: 4K textures alias to noise at distance | `vulkan/image.go` |
| 8 | **Every albedo was black**: Blender writes no `Kd` when a texture drives Base Color, and `newMaterial()` left `Diffuse` at zero, which multiplies the texture | `scene/material.go` |
| 9 | The camera looked straight down: the XML's `pitch` is Blender's X rotation (90° is level), read as an engine pitch (0° is level) | `scene/camera.go` |
| 10 | The scene had no lights, and the HDRI's sun overflows half-float to `+Inf` (the asset was multiplied by 347) | `showcase.xml`, the `.exr` |

Problems 8 and 9 only showed up once the scene loaded; the first screenshot was a
close-up of the floor, the second a grey, unlit-looking scene.

## 2. Changes

### 2.1 A scene is a folder

`scene/scene.go` `LoadScene` passes `filepath.Dir(path)` down, and everything is
resolved beside the XML:

```
assets/showcase/
  showcase.xml
  meshes/    *.obj, *.mtl
  textures/  *.png, *.jpg, the environment *.hdr
```

- `MeshXml.toMesh(sceneDir)` reads `sceneDir/meshes/<obj>` and `<mtl>`.
- `texturePath(sceneDir, ref)` keeps only the basename of Blender's absolute path, as before, under `sceneDir/textures/`.
- Every scene is a folder under `assets/`; there is no top-level layout to fall back to.
- `paths.Mesh` and `paths.Texture` had no callers left and are removed.
- `main.go`'s `-scene` default is now `showcase/showcase.xml`.

**Why not flatten into `assets/`:** the environment map belongs to a scene as much as its meshes, and a self-contained folder is what the exporter writes anyway.

### 2.2 MTL parsing (`scene/mesh.go` `parseMTL`)

- A map's file is the line's **last** field, so options such as `-bm 1.0` are skipped. A path with spaces is not supported, which it never was.
- `norm` is read as a normal map, beside `map_Bump`/`bump`.
- `map_Pr` is the roughness map. So is `map_Ns`, because that is where Blender puts the roughness image when PBR extensions are off.
- `newMaterial()` now starts `Diffuse` at white (and `Alpha` at 1). This was problem 8: `Kd` tints `map_Kd`, so a missing `Kd` must not mean black.

### 2.3 Roughness map

| Layer | Change |
|---|---|
| `scene/material.go` | `RoughnessMapPath`, `RoughnessMap`, `RoughnessMapSlot` |
| `renderer/uniforms.go` | `DrawUniforms.TexRoughness int32`, appended, 100 → 104 bytes |
| `common.slang` | `int texRoughness` at the same position, `TEX_ROUGHNESS` |
| `forward.slang` | `roughness = matRoughness * TEX_ROUGHNESS.Sample(uv).r` |
| `Mesh.draw` | forces `MatRoughness = 1` when a map exists |

**Why multiply and force 1:** slot 0 is the white pixel, so an unmapped material multiplies by 1 and needs no flag. Blender's PBR export still writes `Pr 0.5` (the socket's own value) when a texture drives the socket, so the scalar must be ignored once a map exists, exactly as Blender ignores it.

Metallic and AO maps are not added: none of the downloads ship one. `Pm` (scalar) already worked.

### 2.4 Mipmaps

Backend (`renderer/spec.go`, `vulkan/image.go`, `barrier.go`, `frame.go`):

- `ImageSpec.MipLevels` (0 means 1) goes into `VkImageCreateInfo.MipLevels`.
- `ImageData.Mip` is the level an `UpdateImage` writes, set as `BufferImageCopy.MipLevel`.
- The whole-image view, the barrier and `Frame.Clear` cover `image.levels()` instead of a hard-coded 1. `levels()` returns 1 for the placeholder and swapchain entries, which never set a count.

Scene (`scene/image.go` `mipChain`, `scene/mesh.go` `loadTexture`): the chain is built **on the CPU** with a 2×2 box filter, and uploaded one level per `UpdateImage`.

**Why CPU rather than `vkCmdBlitImage`:** `go-vulkan` has no blit binding (`notes/TODO.md` listed that as the first step). Uploading levels needed only a mip field on the existing upload, and the same mechanism is what the environment needs anyway, since its mips are a roughness series rather than a downscale.

Two caveats are recorded in `TODO.md`:

- The box filter averages gamma-encoded bytes, so albedo darkens slightly at distance.
- A *mid-frame* `UpdateImage` still keeps one pending copy per image, so writing two mips of one image in the same frame would lose the first. Load time is unaffected, because it uploads immediately.

### 2.5 Environment and image-based lighting

**Scene format.** A new optional `<environment>` element:

```xml
<environment>
  <file>cedar_bridge_sunset_1_4k.hdr</file>  <!-- in textures/ -->
  <strength>1.0</strength>                    <!-- Blender's Background strength -->
  <rotation>0.0</rotation>                    <!-- radians, Blender's Mapping Z rotation -->
  <clamp>20.0</clamp>                         <!-- caps radiance; 0 or absent = no cap -->
</environment>
```

Without the element, the scene is lit by a flat 0.05 grey environment (`flatEnvironment`).

**Loading and baking (`scene/ibl.go`, new, all CPU at load):**

1. `loadHDR` decodes Radiance `.hdr`, both flat and RLE scanlines.
   - **Why `.hdr`:** it is about 80 lines of Go. The source EXRs use PIZ and DWAA compression, for which there is no pure-Go decoder, and Blender can write `.hdr`.
2. `clamp`, when set, caps every channel. This takes the sun out of the image-based light so a scene light can carry it with a shadow. Without the cap, the sun is counted twice and lights the inside of shadows.
3. `buildChain` box-halves the source down to 4 texels wide.
4. Three RGBA16F equirect images are baked:
   - **sky**: the 2048-wide chain level, drawn behind the scene
   - **specular**: 6 mips from 512×256 down, mip *k* prefiltered for roughness *k*/5 with 128 GGX samples a texel. *Filtered importance sampling* makes each sample read the chain level whose texel matches its share of the lobe, which is what stops 128 samples from turning the sun into fireflies.
   - **irradiance**: 32×16, a brute-force cosine convolution of the 64-wide level, divided by π so the shader's diffuse is `irradiance * albedo`
5. Rows are baked in parallel goroutines. The whole run, to frame 90's screenshot, takes a few seconds.

**Why equirect rather than cubemaps:** the bake is plain Go over one array, with no compute pass, no cube faces and no seam handling between faces. The cost is one direction→uv function that the CPU and GPU must agree on: `equirectUV` in Go and `envUV` in `common.slang`. Both use u = 0.5 + atan2(z, x)/2π and v = 0.5 − asin(y)/π in engine space, which is Blender's mapping after the Z-up → Y-up swap.

**Uniforms.** `FrameUniforms.TexSkybox` (one cube slot) is replaced by `TexSky`, `TexSpecular` and `TexIrradiance` (2D slots), plus `EnvStrength` and `EnvRotation`: 4760 → 4776 bytes. `common.slang` matches field for field. I checked the emitted offsets with `spirv-dis`: `texSky` is at 4752 and `texRoughness` at 100.

**Shaders.**

- `common.slang` gains:
  - `envUV(d)`, which applies the rotation about +Y (Blender's Z)
  - `envSample(map, d, lod)`: always `SampleLevel`, because implicit derivatives jump at the u = 0/1 seam and would pick the smallest mip along a line
  - `toDisplay`: the Reinhard + gamma that `forward.slang` already did, now shared
- `forward.slang`'s ambient term is the split-sum: `kD·irradiance·albedo + prefiltered(R, roughness·5)·(F0·A + B)`. A and B come from Karis's analytic fit (`envBRDFApprox`) **instead of a BRDF LUT**, which saves a texture, a slot and a bake for a difference you can't see at this scale.
- `skybox.slang` samples the sky by the cube's direction and tonemaps like the scene. It used to output LDR bytes directly; an HDR sky that skipped the tonemap would clip to white.

The skybox cube mesh and its pipeline are unchanged. Only what it samples changed. The environment sampler wraps in u and clamps in v, so the poles do not bleed into each other.

### 2.6 Camera

`CameraXml.toCamera` derives yaw and pitch from the exported `<front>` vector, by inverting the formula `input.DefaultMouseCallback` uses, so the first mouse move does not jump. The XML's yaw happened to be right already; its pitch was off by 90° minus itself. A scene with no `<front>` falls back to `<yaw>`/`<pitch>` in the engine's convention, as before.

### 2.7 Exporter (`xml_export.py`)

- `obj_export(..., export_pbr_extensions=True)`: writes `Pr`, `Pm` and `map_Pr`.
- `localise_mtl`: after each OBJ export, every map in the MTL is copied or converted into `textures/`, and the line is rewritten to the bare filename. Anything that is not PNG or JPEG becomes a PNG, through a throwaway image datablock, so the user's images keep their path and settings. Every map except `map_Kd` is saved as `Non-Color`, so normals and roughness are not gamma-encoded on the way to 8 bits. A texture shared between meshes is converted once.
- `write_environment`: finds the World's Environment Texture, saves it as `.hdr` (or copies it if it already is one), and writes `<environment>` with the Background strength and the Mapping node's Z rotation.
- A new **Environment clamp** field in the export dialog writes `<clamp>`. Set it when a Sun lamp stands in for the HDRI's sun.

The camera's `<yaw>`/`<pitch>` are still written but are now only a fallback.

### 2.8 The showcase assets (one-off)

Blender is not installed on this machine, so the new exporter could not produce this scene. I did what it would do by hand:

- Each `*_nor_gl_4k.exr` and `*_rough_4k.exr` became an 8-bit PNG in `textures/`, written raw with no gamma: a flat normal reads (128, 128, 255). The tool was a 25-line C++ program against the installed OpenEXR library, plus a Go encoder.
- `cedar_bridge_sunset_1_4k.exr` became `textures/cedar_bridge_sunset_1_4k.hdr`. **Its 14 sun texels are `+Inf`** (half-float overflow from the ×347 in its history); I clamped them to 65504. Blender's own `.hdr` save will need the same care, and is worth checking.
- The `*_diff_4k.jpg` files are copied as they are.
- The five MTLs are rewritten as `localise_mtl` would write them: bare filenames, `.exr` → `.png`, `map_Ns` → `map_Pr`, plus `Pr 0.5` / `Pm 0` as Blender's PBR export writes them. Metallic is 0 everywhere, a guess: none of the downloads has a metal map, and the reference render's Suzanne reads as painted, not bare metal.

`textures/` is 257 MB. The original `*_4k.blend` folders and `.zip` files are untouched.

### 2.9 Lights in the XML (`showcase.xml`)

Added, as you asked, until you place them in Blender:

```xml
<light name="Sun">
  <type>sun</type>
  <position>7.8,-5.7,1.66</position>          <!-- ~10 m toward the sun, for the shadow camera -->
  <direction>-0.8017,0.5796,-0.1460</direction>
  <color>1.0,0.552,0.126</color>
  <diffuse>1.0</diffuse>
  <intensity>8.0</intensity>
</light>
```

- **Direction**: the centroid of the HDRI's brightest texels (the sun disc), mapped back to a Blender direction. It is about 8° above the horizon, a sunset. This assumes the `.blend`'s world has **no Mapping rotation**. I could not open the `.blend` to check; if it has one, rotate the sun by the same angle.
- **Colour**: the sun disc's measured RGB, normalised.
- **Intensity**: the disc integrates to about (3.8, 2.1, 0.5) in linear radiance units. That is a lower bound, because the source clipped. At 3.8 the environment's fill washed out the ball's shadow on the plank. 8.0 makes it read as it does in `showcase.png`. This value is tuned by eye, not measured.
- **`clamp` 20**: 99.99% of the environment is below 19. Only the sun and its immediate glow are above, which is exactly what the sun light replaces.

### 2.10 Light units (2026-10-09)

With your Sun (strength 1) and pink Point (10 W) exported from Blender, neither the shadows nor the pink light showed:

- **Point and spot**: `toLight` divided Blender's watts by 1000, and the falloff was `1/(1 + d²)`. At this scene's half-metre distances that left the 10 W light about 400× too dim. Now watts are divided by 4π (`wattsToIntensity`, W/sr for a source radiating over the whole sphere; a Blender spot is a masked point light, so it takes the same factor). `lightConstant` is 0.01, only a guard against d = 0. Both live in `scene/light.go`; the shader reads the constant through `LightData.Constant`.
- **Environment**: Blender's numbers already match the engine's. A sun of strength 1 is 1 W/m², and the HDRI's sky puts about 2.9 W/m² on the floor. Cycles' sky is occluded by the geometry, though, and the engine's image-based light is not, so it filled every shadow. `environmentLightScale` = 0.25 (`scene/skybox.go`) scales the baked irradiance and specular maps, never the visible sky. That takes the share of the frame the sun's shadows change from 0.22% to 1.87%. The value is tuned by eye; the real fix is in `notes/TODO.md` (the HDRI's sun as a shadow-casting light, then SSAO).

## 3. Verification

Verified:

- `go build ./...`. `go vet ./...` shows only the two known `unsafe.Pointer` reports.
- All `.spv` files pass `spirv-val --scalar-block-layout`.
- The uniform offsets match the Go structs (`spirv-dis`).
- **Vulkan validation layers**, with the `vk_layer_settings.txt` recipe from `CLAUDE.md`: zero errors, warnings or performance messages over a run to frame 90. I confirmed the layer was actually loaded with `VK_LOADER_DEBUG=layer`.
- The A/Bs with `-screenshot`:
  - `[debug] noShadows` on and off: the plank's shadow on Suzanne disappears with it. Note: before the `Kd` fix the two looked identical, which is how problem 8 was found.
  - Environment removed: the scene falls back to flat grey and still loads, and the sun alone shows the ball's shadow on the plank where the reference has it.
  - A temporary camera aimed at the computed sun direction: the sun disc lands on the crosshair, so the Go bake and `envUV` agree. The sky is upright, and the road arrows read correctly for left-hand traffic, so it is not mirrored.
  - `-config low.toml` runs.
  - Two consecutive runs produce byte-identical frames.

Not verified:

- **The exporter inside Blender.** I tested `localise_mtl`'s MTL rewriting against stubbed `bpy` modules; it produces exactly the hand-made MTLs. These run only in Blender and are untested:
  - `convert_image` (`Image.save` to PNG and to HDR)
  - `write_environment`'s node lookup
  - the exact keys `export_pbr_extensions` emits in Blender 5.2

  Re-export once and diff against the current `meshes/*.mtl` and `textures/`.
- **The sign of `<rotation>`.** It was only ever 0 here. If a rotated world comes out turned the wrong way, negate `s` in `envUV` (`common.slang`).

## 4. Known differences from the Cycles reference

- The ball's shadow on the plank is sharper and slightly offset: one 2048² sun tile covers ±10 m (`scene/shadowatlas.go`), about 1 cm a texel, and there is no soft contact shadow from sky occlusion. Cycles gets both from ray tracing.
- No ambient occlusion: creases under the plank and around Suzanne are lighter than in Cycles.
- `scene/shadowatlas.go` uses a 0.08 m normal offset, tuned for metre-scale scenes. This scene is 30 cm across, so a shadow can detach slightly from its caster.
- The window here was taller than 16:9, so the floor's front edge (its side face, with stretched UVs) shows at the bottom right. Cycles' wide crop hides it.

## 5. Next steps in Blender

1. Install the updated `xml_export.py` and re-export `showcase.blend` over `assets/showcase/showcase.xml`. Set **Environment clamp** to about 20 if you add a Sun lamp.
2. Add a Sun lamp aimed along the HDRI's sun (see 2.9), then remove the hand-written `<light>`. The exporter will write the lamp, and the engine reads Blender's sun strength directly as `intensity`.
3. Check that the re-exported `.hdr` has no `inf` in it (the sun). If the save fails or writes garbage, clamp the image in Blender first: a Math node, or re-save the EXR as float32.
