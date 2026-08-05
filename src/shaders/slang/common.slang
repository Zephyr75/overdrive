// Shared declarations for every Overdrive shader
//
// build_shaders.sh compiles this to SPIR-V: BDA push constant + bindless arrays
//
// The uniform data is split by update frequency: FrameUniforms once per pass,
// DrawUniforms once per draw. The backend memcpys both into a host-visible ring
// and pushes their device addresses, with no marshalling — Slang's scalar layout
// and Go's struct packing agree field for field

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

// Declared in 16-byte cells, see the layout note above FrameUniforms
struct LightData {
    float3 color;     float intensity;
    float3 position;  float diffuse;
    float3 direction; float specular;
    float kConstant;  float kLinear;
    float kQuadratic; float cutoff;
    int type; // 0 = directional, 1 = point
    // 17 fields is not a multiple of 4, and std140 rounds a struct's array
    // stride up to 16 regardless. Declaring the remainder keeps Go's mirror the
    // same size instead of leaving an invisible hole. Spend it on spot lights
    // (outer cutoff), point-light radius, or whatever comes next
    float reserved0, reserved1, reserved2;
};

// Keep in sync with MaxLights in renderer/uniforms.go
#define MAX_LIGHTS 8

// Point lights that own a cube shadow map
// Each caster bakes into its own cube and binds to a dedicated sampler slot
// The rest light unshadowed
// Keep in sync with MaxShadowCubes in renderer/uniforms.go
#define MAX_SHADOW_CUBES 4

// The uniform data is split by how often it changes, not by what it describes
//
// FrameUniforms is written once per pass (three times a frame: two shadow bakes
// and the main pass), DrawUniforms once per draw (~15 times a frame). Before the
// split every draw re-sent all of it, ~1200 bytes of which never varied between
// draws. Keep each struct's field order identical to renderer/uniforms.go — that
// order *is* the Vulkan scalar layout.
//
// The reason the field order below looks arbitrary: every declaration group
// fills a whole 16-byte cell — a float3 always followed by a scalar, loose
// scalars in fours, scalar arrays declared as vectors (int4, not int[4]). That
// was std140's rule, mandatory while OpenGL was a backend. Scalar layout has no
// such requirement, so the rule is now vestigial and these structs can be
// reordered freely; see notes/BACKEND_DECISION.md §5.3.

// Camera, lights and shadow maps: constant across every draw of a pass
struct FrameUniforms {
    float4x4 view;
    float4x4 projection;
    float4x4 lightSpaceMatrix;
    float4x4 shadowMatrices[6];
    float3 viewPos;
    float farPlane;
    float3 lightPos;
    // Active lights in lights[], 0 to MAX_LIGHTS
    int lightCount;
    LightData lights[MAX_LIGHTS];
    // Bindless texture-array slots, indices into textures2D/texturesCube
    int texShadowMap;
    int texShadowCubeMap;
    int texSkybox;
    // The one directional light owning the 2D shadow map, -1 for no caster
    // Every other directional light renders unshadowed
    int shadowDirIndex;
    // Light index owning each cube slot, -1 when the slot is unused
    // Point light l casts a cube shadow when some slot s holds l
    // It then samples shadowCubeMap[s]
    //
    // A vector rather than int[MAX_SHADOW_CUBES], from the std140 era; scalar
    // layout would pack the array fine
    int4 pointShadowLights;
};

// Transform and material of one face group: the only things a draw changes
struct DrawUniforms {
    float4x4 model;
    // Each float3 carries a scalar in its 16-byte cell, see the layout rule
    // above. Metallic-roughness PBR scalars, matDiffuse is the base-colour tint
    float3 matAmbient;  float matShininess;
    float3 matDiffuse;  float matMetallic;
    float3 matSpecular; float matRoughness;
    float matAo;
    // Bindless texture-array slots, indices into textures2D
    int texOurTexture;
    int texNormalMap;
    // 1 when the bound material has a normal map, 0 uses the geometric normal
    int useNormalMap;
};

// BDA, one pointer per block lives in a 16-byte push constant
// Both point into the host-visible ring the backend uploads into: the frame
// entry is written once per pass and its address re-pushed per draw, the draw
// entry is written per draw
struct PushConstants { FrameUniforms *frame; DrawUniforms *draw; };
[[vk::push_constant]] PushConstants pc;

// One bindless descriptor set, sampler2D[] at binding 0 and samplerCube[] at 1
[[vk::binding(0, 0)]] Sampler2D textures2D[];
[[vk::binding(1, 0)]] SamplerCube texturesCube[];

// Shadow maps get dedicated non-bindless bindings
// PCF kernels tap them many times per fragment
// Intel's Vulkan driver re-fetches a dynamically indexed bindless descriptor per tap
// Going bindless here cost ~1.7x the frame time; a plain bound descriptor fixes it
[[vk::binding(2, 0)]] Sampler2D shadowMap2D;
[[vk::binding(3, 0)]] SamplerCube shadowCubeMap[MAX_SHADOW_CUBES];

#define FRAME (pc.frame)
#define DRAW (pc.draw)
#define TEX_OUR (textures2D[DRAW.texOurTexture])
#define TEX_NORMAL (textures2D[DRAW.texNormalMap])
#define TEX_SHADOWMAP (shadowMap2D)
#define TEX_SKYBOX (texturesCube[FRAME.texSkybox])

// The projection matrices built in scene/ are the OpenGL convention, giving
// clip z in [-w, w]; Vulkan clips to [0, w], so every vertex stage remaps
#define TO_VK_DEPTH(pos) ((pos).z = ((pos).z + (pos).w) * 0.5)
