// Shared declarations for every Overdrive shader
//
// One Slang source feeds both backends, build_shaders.sh compiles it twice
//   -DTARGET_VK -target spirv -> Vulkan, BDA push constant + bindless arrays
//   no define   -target glsl  -> OpenGL, std140 UBO + named samplers
//
// Uniforms is the same struct either way, only the binding model differs
// Vulkan memcpys renderer.Uniforms straight in, Go packing is scalar layout
// OpenGL marshals the same struct into std140 by hand in opengl/uniforms.go

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

// Keep in sync with MaxLights in renderer/uniforms.go
#define MAX_LIGHTS 8

// Point lights that own a cube shadow map
// Each caster bakes into its own cube and binds to a dedicated sampler slot
// The rest light unshadowed
// Keep in sync with MaxShadowCubes in renderer/uniforms.go
#define MAX_SHADOW_CUBES 4

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
    LightData lights[MAX_LIGHTS];
    // Bindless texture-array slots, Vulkan only
    // The GL shader samples the named samplers below instead
    int texShadowMap;
    int texOurTexture;
    int texShadowCubeMap;
    int texSkybox;
    int texNormalMap;
    // 1 when the bound material has a normal map, 0 uses the geometric normal
    int useNormalMap;
    // Active lights in lights[], 0 to MAX_LIGHTS
    int lightCount;
    // The one directional light owning the 2D shadow map, -1 for no caster
    // Every other directional light renders unshadowed
    int shadowDirIndex;
    // Metallic-roughness PBR scalars, matDiffuse is the base-colour tint
    float matMetallic;
    float matRoughness;
    float matAo;
    // Light index owning each cube slot, -1 when the slot is unused
    // Point light l casts a cube shadow when some slot s holds l
    // It then samples shadowCubeMap[s]
    int pointShadowLights[MAX_SHADOW_CUBES];
};

#ifdef TARGET_VK

// BDA, a pointer to the uniform block lives in a push constant
// It matches the buffer_reference UBO the backend uploads into a host-visible ring
struct PushConstants { Uniforms *ubo; };
[[vk::push_constant]] PushConstants pc;

// One bindless descriptor set, sampler2D[] at binding 0 and samplerCube[] at 1
[[vk::binding(0, 0)]] Sampler2D textures2D[];
[[vk::binding(1, 0)]] SamplerCube texturesCube[];

// Shadow maps get dedicated non-bindless bindings
// PCF kernels tap them many times per fragment
// Intel's Vulkan driver re-fetches a dynamically indexed bindless descriptor per tap
// Going bindless here made Vulkan run ~1.7x slower than OpenGL
// A plain bound descriptor, what the GL backend uses, fixes it
[[vk::binding(2, 0)]] Sampler2D shadowMap2D;
[[vk::binding(3, 0)]] SamplerCube shadowCubeMap[MAX_SHADOW_CUBES];

#define U (pc.ubo)
#define TEX_OUR (textures2D[U.texOurTexture])
#define TEX_NORMAL (textures2D[U.texNormalMap])
#define TEX_SHADOWMAP (shadowMap2D)
#define TEX_SKYBOX (texturesCube[U.texSkybox])

// GL projection matrices give clip z in [-w, w], Vulkan clips to [0, w]
#define TO_VK_DEPTH(pos) ((pos).z = ((pos).z + (pos).w) * 0.5)

#else // OpenGL

ConstantBuffer<Uniforms> ubo;

// GL keeps texture unit semantics
// The backend binds each sampler to a unit with glUniform1i
// So they stay separate named samplers
Sampler2D shadowMap;
Sampler2D ourTexture;
Sampler2D normalMap;
SamplerCube shadowCubeMap[MAX_SHADOW_CUBES];
SamplerCube skybox;

#define U (ubo)
#define TEX_OUR (ourTexture)
#define TEX_NORMAL (normalMap)
#define TEX_SHADOWMAP (shadowMap)
#define TEX_SKYBOX (skybox)

// GL clip space already matches, no remap
#define TO_VK_DEPTH(pos)

#endif
