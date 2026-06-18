#pragma once
#include <cstddef>
#include <cstdint>
#include <glm/glm.hpp>
#include <string>
#include <unordered_map>

// CPU mirror of the shader uniform block in shaders/vulkan/common.glsl.
// Both sides use scalar block layout, so member order and packing must match
// exactly (everything is 4-byte aligned, vec3 occupies 12 bytes, no padding).

struct VKLightData {
  int32_t type;
  float constant;
  float linear;
  float quadratic;
  float cutoff;
  glm::vec3 color;
  float intensity;
  float diffuse;
  float specular;
  glm::vec3 position;
  glm::vec3 direction;
};

struct VKUniformBlock {
  glm::mat4 view;
  glm::mat4 projection;
  glm::mat4 model;
  glm::mat4 lightSpaceMatrix;
  glm::mat4 shadowMatrices[6];
  glm::vec3 viewPos;
  float farPlane;
  glm::vec3 lightPos;
  glm::vec3 matAmbient;
  glm::vec3 matDiffuse;
  glm::vec3 matSpecular;
  float matShininess;
  VKLightData lights[2];
  // Bindless texture array slots, resolved from bound units at draw time
  int32_t texShadowMap;
  int32_t texOurTexture;
  int32_t texShadowCubeMap;
  int32_t texSkybox;
  int32_t texNormalMap;
  int32_t useNormalMap;
};

static_assert(sizeof(VKLightData) == 68, "scalar layout mismatch");
static_assert(sizeof(VKUniformBlock) == 868, "scalar layout mismatch");

struct VKUniformField {
  size_t offset;
  size_t size;
};

struct VKSamplerSlot {
  size_t offset; // offset of the int slot inside VKUniformBlock
  bool cube;
};

// GL-style uniform name -> location inside the block
const std::unordered_map<std::string, VKUniformField> &vkUniformFields();
// GL-style sampler uniform name -> texture index slot inside the block
const std::unordered_map<std::string, VKSamplerSlot> &vkSamplerSlots();
