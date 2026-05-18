#pragma once
#include <glad/glad.h>
#include <glm/glm.hpp>
#include <string>

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

  // GPU resources
  GLuint depthMapFBO = 0;
  GLuint depthMap = 0;     // 2D (directional)
  GLuint depthCubeMap = 0; // cubemap (point)

  void setup();
  void destroy();

  // Returns lightSpaceMatrix (useful for directional); identity for point
  // lights.
  glm::mat4 renderLight(float nearPlane, float farPlane,
                        const Shader &depthShader,
                        const Shader &depthCubeShader,
                        const Scene &scene) const;

  void move(glm::vec3 delta) { pos += delta; }
};
