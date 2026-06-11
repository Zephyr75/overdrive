// Shared declarations for the Vulkan shaders.
// The UBO block must match VKUniformBlock (vulkan/Uniforms.hpp) exactly:
// both use scalar layout, so member order and types are identical.

#extension GL_EXT_buffer_reference : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require

struct LightData {
    int type; // 0 = directional, 1 = point
    float kConstant;
    float kLinear;
    float kQuadratic;
    float cutoff;
    vec3 color;
    float intensity;
    float diffuse;
    float specular;
    vec3 position;
    vec3 direction;
};

layout(buffer_reference, scalar, buffer_reference_align = 64) readonly buffer UBO {
    mat4 view;
    mat4 projection;
    mat4 model;
    mat4 lightSpaceMatrix;
    mat4 shadowMatrices[6];
    vec3 viewPos;
    float farPlane;
    vec3 lightPos;
    vec3 matAmbient;
    vec3 matDiffuse;
    vec3 matSpecular;
    float matShininess;
    LightData lights[2];
    int texShadowMap;
    int texOurTexture;
    int texShadowCubeMap;
    int texSkybox;
};

layout(push_constant) uniform PushConstants { UBO ubo; } pc;

layout(set = 0, binding = 0) uniform sampler2D textures2D[];
layout(set = 0, binding = 1) uniform samplerCube texturesCube[];

// GL projection matrices produce z in [-w, w]; Vulkan clips to [0, w].
#define TO_VK_DEPTH(pos) ((pos).z = ((pos).z + (pos).w) * 0.5)
