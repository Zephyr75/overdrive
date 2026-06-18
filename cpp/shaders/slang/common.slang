// Shared declarations for every Overdrive shader.
//
// One Slang source feeds both backends. The build compiles it twice:
//   * -DTARGET_VK -target spirv  -> Vulkan (BDA push-constant + bindless arrays)
//   * (no define) -target glsl   -> OpenGL (std140 UBO + named samplers)
//
// The Uniforms struct is identical for both targets; only the resource binding
// model differs (selected below). The VK scalar layout is byte-identical to
// VKUniformBlock (vulkan/Uniforms.hpp); the GL std140 layout is mirrored by
// GLUniformBlock (opengl/Shader.cpp). Both are checked by reflection at build.

// Slang has no matrix inverse intrinsic; this is the standard adjugate/det
// inverse, used to build the normal matrix transpose(inverse(mat3(model))).
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

struct LightData {
    int type; // 0 = directional, 1 = point
    float kConstant;
    float kLinear;
    float kQuadratic;
    float cutoff;
    float3 color;
    float intensity;
    float diffuse;
    float specular;
    float3 position;
    float3 direction;
};

struct Uniforms {
    float4x4 view;
    float4x4 projection;
    float4x4 model;
    float4x4 lightSpaceMatrix;
    float4x4 shadowMatrices[6];
    float3 viewPos;
    float farPlane;
    float3 lightPos;
    float3 matAmbient;
    float3 matDiffuse;
    float3 matSpecular;
    float matShininess;
    LightData lights[2];
    // Bindless texture-array slots (Vulkan). Unused by the GL shader, which
    // samples the named samplers below instead.
    int texShadowMap;
    int texOurTexture;
    int texShadowCubeMap;
    int texSkybox;
    int texNormalMap;
    // 1 when the bound material has a normal map; 0 = use the geometric normal.
    int useNormalMap;
};

#ifdef TARGET_VK

// BDA: a pointer to the uniform block lives in a push constant, matching the
// buffer_reference UBO the Vulkan backend uploads into a host-visible ring.
struct PushConstants { Uniforms *ubo; };
[[vk::push_constant]] PushConstants pc;

// One bindless descriptor set: sampler2D[] at binding 0, samplerCube[] at 1.
[[vk::binding(0, 0)]] Sampler2D textures2D[];
[[vk::binding(1, 0)]] SamplerCube texturesCube[];

#define U (pc.ubo)
#define TEX_OUR (textures2D[U.texOurTexture])
#define TEX_NORMAL (textures2D[U.texNormalMap])
#define TEX_SHADOWMAP (textures2D[U.texShadowMap])
#define TEX_SKYBOX (texturesCube[U.texSkybox])
#define TEX_SHADOWCUBE (texturesCube[U.texShadowCubeMap])

// GL projection matrices produce clip z in [-w, w]; Vulkan clips to [0, w].
#define TO_VK_DEPTH(pos) ((pos).z = ((pos).z + (pos).w) * 0.5)

#else // OpenGL

ConstantBuffer<Uniforms> ubo;

// GL keeps GL "texture unit" semantics: each sampler is bound to a unit by the
// backend (glUniform1i), so they stay separate named samplers.
Sampler2D shadowMap;
Sampler2D ourTexture;
Sampler2D normalMap;
SamplerCube shadowCubeMap;
SamplerCube skybox;

#define U (ubo)
#define TEX_OUR (ourTexture)
#define TEX_NORMAL (normalMap)
#define TEX_SHADOWMAP (shadowMap)
#define TEX_SKYBOX (skybox)
#define TEX_SHADOWCUBE (shadowCubeMap)

// GL clip space already matches; no remap.
#define TO_VK_DEPTH(pos)

#endif
