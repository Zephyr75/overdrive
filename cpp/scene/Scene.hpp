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

  // Index into lights[] of the single shadow-casting directional / point light,
  // or -1 if none. Set during construction; consumed by the forward shader.
  int shadowDirIndex = -1;
  int shadowPointIndex = -1;

  explicit Scene(const std::string &xmlPath, Backend &backend);
  ~Scene();

  void renderScene(const Shader &shader, const glm::mat4 &lightSpaceMatrix,
                   float farPlane) const;
  void renderSkybox(const Shader &shader) const;
  void updateMeshes();

  Mesh *getMesh(const std::string &name);
  Light *getLight(const std::string &name);
};
