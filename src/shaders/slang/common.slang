// Shared declarations for every Overdrive shader
// Every struct mirrors renderer/uniforms.go field for field: keep the order identical

// Slang has no 3x3 inverse, needed for the normal matrix
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

// 72 bytes, renderer.LightData
struct LightData {
  float3 color;
  float intensity;
  float3 position;
  float diffuse;
  float3 direction;
  // Keeps the inverse-square falloff off zero at distance zero
  float kConstant;
  // Spot cone cosines, inner then outer
  float cutoff;
  float outerCutoff;
  float radius;
  // First record in the record array and how many follow, -1 when unshadowed
  int shadowIndex;
  int shadowCount;
  int type;
};

// Keep in sync with renderer/uniforms.go
#define LIGHT_SUN 0
#define LIGHT_SPOT 2
#define MAX_LIGHTS 64

// 96 bytes, renderer.ShadowRecord: one per tile, six in a row for a point light
struct ShadowRecord {
  // Bakes the tile and samples it, so the two cannot disagree
  float4x4 lightSpace;
  // xy offset, zw scale: tile uv to atlas uv
  float4 atlasRect;
  float texelSize;
  float farPlane;
  // 0..5 for a cube face, -1 for a 2D tile
  int face;
  // bit 0: dynamic atlas, bit 1: cheap PCF
  int flags;
};

// renderer.FrameUniforms: camera and lights, once per pass
struct FrameUniforms {
  float4x4 view;
  float4x4 projection;
  float3 viewPos;
  int lightCount;
  LightData lights[MAX_LIGHTS];
  int texSkybox;
  // 4096 / atlasSize, widens the normal-offset bias on a smaller atlas
  float shadowNormalScale;
};

// 80 bytes, renderer.BakeUniforms: the tile a depth pass is baking
struct BakeUniforms {
  float4x4 worldToTile;
  float3 lightPos;
  float farPlane;
};

// 100 bytes, renderer.DrawUniforms: transform and material, once per draw
struct DrawUniforms {
  float4x4 model;
  float3 matDiffuse;
  float matMetallic;
  float matRoughness;
  float matAo;
  int texDiffuse;
  int texNormalMap;
  int useNormalMap;
};

// Four addresses in one push constant, in the order scene/pipelines.go fills them
struct PushConstants {
  FrameUniforms *frame;
  DrawUniforms *draw;
  ShadowRecord *records;
  BakeUniforms *bake;
};
[[vk::push_constant]]
PushConstants pushConstants;

// Bindless arrays, indexed by the slots in the uniform blocks
[[vk::binding(0, 0)]]
Sampler2D textures2D[];
[[vk::binding(1, 0)]]
SamplerCube texturesCube[];

// Dedicated descriptors for the shadow atlases: index by a literal only,
// a dynamic index made the frame ~1.7x slower
[[vk::binding(3, 0)]]
Sampler2D hotTextures[4];

#define HOT_SHADOW_STATIC (hotTextures[0])
#define HOT_SHADOW_DYNAMIC (hotTextures[1])

#define TEX_DIFFUSE (textures2D[pushConstants.draw.texDiffuse])
#define TEX_NORMAL (textures2D[pushConstants.draw.texNormalMap])
#define TEX_SKYBOX (texturesCube[pushConstants.frame.texSkybox])

// Projections are OpenGL style, clip z in [-w, w]; Vulkan wants [0, w]
#define TO_VK_DEPTH(pos) ((pos).z = ((pos).z + (pos).w) * 0.5)
