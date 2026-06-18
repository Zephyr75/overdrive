#pragma once
#include <cstdint>
#include <glm/glm.hpp>
#include <string>

class Backend;
class Scene;
class Shader;

enum class LightType { Sun = 0, Point = 1 };

struct Light {
  std::string name;
  LightType type = LightType::Sun;
  glm::vec3 pos = {0.0f, 5.0f, 0.0f};
  glm::vec3 dir = {0.0f, -1.0f, 0.0f};
  glm::vec3 color = {1.0f, 1.0f, 1.0f};
  float diffuse = 1.0f;
  float specular = 1.0f;
  float intensity = 1.0f;

  // Only shadow-casting lights allocate a shadow map and run a depth pass.
  // Scene picks the first directional + first point light as the casters.
  bool castsShadow = false;

  uint32_t depthMapFBO = 0;
  uint32_t depthMap = 0;
  uint32_t depthCubeMap = 0;

  void setup(Backend &backend);
  void destroy();

  glm::mat4 renderLight(float nearPlane, float farPlane,
                        const Shader &depthShader,
                        const Shader &depthCubeShader,
                        const Scene &scene) const;

  void move(glm::vec3 delta) { pos += delta; }

private:
  Backend *backend = nullptr;
};
