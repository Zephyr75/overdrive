#pragma once
#include "Camera.hpp"
#include "Light.hpp"
#include "Mesh.hpp"
#include "Skybox.hpp"
#include <glm/glm.hpp>
#include <string>
#include <vector>

class Backend;
class Shader;

class Scene {
public:
  Camera camera;
  std::vector<Mesh> meshes;
  std::vector<Light> lights;
  Skybox skybox;

  explicit Scene(const std::string &xmlPath, Backend &backend);
  ~Scene();

  void renderScene(const Shader &shader, const glm::mat4 &lightSpaceMatrix,
                   float farPlane) const;
  void renderSkybox(const Shader &shader) const;
  void updateMeshes();

  Mesh *getMesh(const std::string &name);
  Light *getLight(const std::string &name);
};
