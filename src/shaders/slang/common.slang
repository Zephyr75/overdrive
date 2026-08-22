// Shared declarations for every Overdrive shader
//
// build_shaders.sh compiles this to SPIR-V: BDA push constant + bindless arrays
//
// The uniform data is split by update frequency: FrameUniforms once per pass,
// DrawUniforms once per draw. The backend memcpys both into a host-visible ring
// and pushes their device addresses, with no marshalling — Slang's scalar
// layout and Go's struct packing agree field for field

// Slang has no matrix inverse intrinsic, so here is the adjugate/det one
// Used to build the normal matrix transpose(inverse(mat3(model)))
float3x3 inverse3(float3x3 m) {
  float a00 = m[0][0], a01 = m[0][1], a02 = m[0][2];
  float a10 = m[1][0], a11 = m[1][1], a12 = m[1][2];
  float a20 = m[2][0], a21 = m[2][1], a22 = m[2][2];
  float det = a00 * (a11 * a22 - a12 * a21) - a01 * (a10 * a22 - a12 * a20) +
              a02 * (a10 * a21 - a11 * a20);
  float invDet = 1.0 / det;
  float3x3 r;
  r[0][0] = (a11 * a22 - a12 * a21) * invDet;
  r[0][1] = -(a01 * a22 - a02 * a21) * invDet;
  r[0][2] = (a01 * a12 - a02 * a11) * invDet;
  r[1][0] = -(a10 * a22 - a12 * a20) * invDet;
  r[1][1] = (a00 * a22 - a02 * a20) * invDet;
  r[1][2] = -(a00 * a12 - a02 * a10) * invDet;
  r[2][0] = (a10 * a21 - a11 * a20) * invDet;
  r[2][1] = -(a00 * a21 - a01 * a20) * invDet;
  r[2][2] = (a00 * a11 - a01 * a10) * invDet;
  return r;
}

// 72 bytes, mirrored field for field by renderer.LightData
//
// The last four members are read by nothing yet; they are the layout the
// lighting plan needs, grouped so the struct is disturbed once
struct LightData {
  float3 color;
  float intensity;
  // position is read by point and spot lights, direction by sun and spot
  float3 position;
  float diffuse;
  // Falloff is a fixed inverse square, so kConstant only keeps the
  // denominator off zero at distance zero
  float3 direction;
  float kConstant;
  // Spot cone cosines, inner then outer
  float cutoff;
  float outerCutoff;
  // shadowIndex points into the record array, shadowCount says how many
  // consecutive records this light owns; -1 when it casts nothing
  float radius;
  int shadowIndex;
  int shadowCount;
  int type; // 0 = directional, 1 = point, 2 = spot
};

// Keep in sync with LightSun/LightPoint/LightSpot in renderer/uniforms.go
#define LIGHT_SUN 0
#define LIGHT_POINT 1
#define LIGHT_SPOT 2

// Keep in sync with MaxLights in renderer/uniforms.go
#define MAX_LIGHTS 64

// 96 bytes, mirrored field for field by renderer.ShadowRecord
//
// One record per shadow tile: a sun or spot owns one, a point light six
// consecutive ones starting at its shadowIndex. lightSpace both bakes the tile
// and samples it, so the two cannot disagree
struct ShadowRecord {
  float4x4 lightSpace;
  // uv offset.xy, uv scale.xy: tile-local uv maps to atlas uv through it
  float4 atlasRect;
  // 1 / tile pixels, so the PCF step is taken in tile-local uv
  float texelSize;
  // Divides a cube face's radial depth, unused by a 2D tile
  float farPlane;
  // 0..5 for a cube face, -1 for a 2D tile: also the depth encoding selector
  int face;
  // bit 0: sample the dynamic atlas rather than the static one
  // bit 1: cheap PCF, the 4 corner taps without the 3x3 refinement
  int flags;
};

// The uniform data is split by how often it changes, not by what it describes
//
// FrameUniforms is written once per pass (three times a frame: two shadow bakes
// and the main pass), DrawUniforms once per draw (~15 times a frame). Before
// the split every draw re-sent all of it, ~1200 bytes of which never varied
// between draws.
//
// LAYOUT: keep each struct's field order identical to renderer/uniforms.go, and
// use only float/int scalars, vectors and matrices. Slang compiles with
// -fvk-use-scalar-layout, and scalar layout packs exactly as Go packs a struct
// of float32/int32 — so the two agree with no padding and no marshalling code,
// as long as the order matches. Nothing else is required: no 16-byte cells, no
// vec3-plus-scalar pairing, no vectors standing in for scalar arrays.

// Camera, lights and shadow maps: Update once per pass
struct FrameUniforms {
  float4x4 view;
  float4x4 projection;
  // The one tile a shadow pass is baking: meaningless in the main pass
  // The bake shaders never read the record array, so these cannot come from it
  float4x4 bakeMatrix;
  float3 bakeLightPos;
  float bakeFarPlane;
  float3 viewPos;
  // Active lights in lights[], 0 to MAX_LIGHTS
  int lightCount;
  LightData lights[MAX_LIGHTS];
  // The two shadow atlases, as dedicated bindings rather than bindless slots
  int texShadowStatic;
  int texShadowDynamic;
  // Bindless texture-array slot, an index into texturesCube
  int texSkybox;
  // Grows the shadow normal-offset bias when the atlas is smaller than the 4096
  // its world-space constants were tuned at
  float shadowNormalScale;
};

// Transform and material of one face group: Update once per draw
struct DrawUniforms {
  float4x4 model;
  // Metallic-roughness PBR scalars, matDiffuse is the base-colour tint
  float3 matAmbient;
  float matShininess;
  float3 matDiffuse;
  float matMetallic;
  float3 matSpecular;
  float matRoughness;
  float matAo;
  // Bindless texture-array slots, indices into textures2D
  int texOurTexture;
  int texNormalMap;
  // 1 when the bound material has a normal map, 0 uses the geometric normal
  int useNormalMap;
};

// BDA, one pointer per block lives in a 24-byte push constant
// All three point into the host-visible ring the backend uploads into: the
// frame entry is written once per pass and its address re-pushed per draw, the
// draw entry is written per draw, the record array once per frame The records
// are a variable-length array, which is why they are a pointer rather than a
// member of FrameUniforms
struct PushConstants {
  FrameUniforms *frame;
  DrawUniforms *draw;
  ShadowRecord *records;
};
[[vk::push_constant]]
PushConstants pc;

// One bindless descriptor set, sampler2D[] at binding 0 and samplerCube[] at 1
[[vk::binding(0, 0)]]
Sampler2D textures2D[];
[[vk::binding(1, 0)]]
SamplerCube texturesCube[];

// The two shadow atlases get dedicated non-bindless bindings
// PCF kernels tap them many times per fragment
// Intel's Vulkan driver re-fetches a dynamically indexed bindless descriptor
// per tap Going bindless here cost ~1.7x the frame time; a plain bound
// descriptor fixes it Every shadow in the scene is a sub-rect of one of these
// two, whatever the light type: a cube face is an ordinary tile with its own 90
// degree matrix
[[vk::binding(2, 0)]]
Sampler2D shadowAtlasStatic;
[[vk::binding(3, 0)]]
Sampler2D shadowAtlasDynamic;

#define FRAME (pc.frame)
#define DRAW (pc.draw)
#define RECORD(i) (pc.records[i])
#define TEX_OUR (textures2D[DRAW.texOurTexture])
#define TEX_NORMAL (textures2D[DRAW.texNormalMap])
#define TEX_SKYBOX (texturesCube[FRAME.texSkybox])

// The projection matrices built in scene/ are the OpenGL convention, giving
// clip z in [-w, w]; Vulkan clips to [0, w], so every vertex stage remaps
#define TO_VK_DEPTH(pos) ((pos).z = ((pos).z + (pos).w) * 0.5)
