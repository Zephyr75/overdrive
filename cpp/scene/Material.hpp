#pragma once
#include <glad/glad.h>
#include <glm/glm.hpp>

struct Material {
  glm::vec3 ambient = {0.2f, 0.2f, 0.2f};
  glm::vec3 diffuse = {0.8f, 0.8f, 0.8f};
  glm::vec3 specular = {0.5f, 0.5f, 0.5f};
  float shininess = 32.0f;
  float alpha = 1.0f;
  GLuint texture = 0;
  GLuint normalMap = 0;
};
