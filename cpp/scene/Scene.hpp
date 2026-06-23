#pragma once
#include "Camera.hpp"
#include "Light.hpp"
#include "Mesh.hpp"
#include "Skybox.hpp"
#include "settings/Settings.hpp"
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

  // Index into lights[] of the single shadow-casting directional light, or -1.
  int shadowDirIndex = -1;
  // Light index owning each point-shadow cube slot, or -1 if the slot is unused.
  // Up to MAX_SHADOW_CUBES point lights cast cube shadows. Set during
  // construction; consumed by the forward shader (see Mesh::draw).
  int pointShadowLights[Settings::MAX_SHADOW_CUBES] = {-1, -1, -1, -1};

  explicit Scene(const std::string &xmlPath, Backend &backend);
  ~Scene();

  void renderScene(const Shader &shader, const glm::mat4 &lightSpaceMatrix,
                   float farPlane) const;
  void renderSkybox(const Shader &shader) const;
  void updateMeshes();

  Mesh *getMesh(const std::string &name);
  Light *getLight(const std::string &name);
};
