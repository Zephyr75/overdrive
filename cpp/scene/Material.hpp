#pragma once
#include <cstdint>
#include <glm/glm.hpp>
#include <string>

struct Material {
  glm::vec3 ambient = {0.2f, 0.2f, 0.2f};
  glm::vec3 diffuse = {0.8f, 0.8f, 0.8f};
  glm::vec3 specular = {0.5f, 0.5f, 0.5f};
  float shininess = 32.0f;
  float alpha = 1.0f;
  // Metallic-roughness PBR scalars (glTF/Disney workflow). diffuse doubles as
  // the base-colour/albedo tint. roughness defaults to 1 (matte) and metallic
  // to 0 (dielectric) so non-PBR .mtl materials keep a plausible look.
  float metallic = 0.0f;
  float roughness = 1.0f;
  float ao = 1.0f;
  uint32_t texture = 0;
  uint32_t normalMap = 0;
  std::string texturePath;
  std::string normalMapPath;
};
