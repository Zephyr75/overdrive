#pragma once
#include "Material.hpp"
#include <cstdint>
#include <glm/glm.hpp>
#include <string>
#include <vector>

class Backend;
class Scene;
class Shader;

struct SubMesh {
  uint32_t vao = 0, ebo = 0;
  std::vector<uint32_t> indices;
  int materialIndex = 0;
};

class Mesh {
public:
  std::string name;
  glm::vec3 position;
  glm::vec3 initialPosition;
  std::vector<Material> materials;
  std::vector<SubMesh> submeshes;

  void load(const std::string &objPath, const std::string &mtlDir,
            glm::vec3 pos);
  void setup(Backend &backend);
  void destroy();
  void draw(const Shader &shader, const Scene &scene) const;
  void moveTo(glm::vec3 dest);
  void moveBy(glm::vec3 delta);
  void updateVertices();

private:
  struct RawVertex {
    glm::vec3 basePos;
    glm::vec3 normal;
    glm::vec2 texcoord;
  };

  std::vector<RawVertex> rawVertices;
  Backend *backend = nullptr;
  uint32_t sharedVbo = 0;
  bool needsUpdate = false;

  void rebuildAndUpload();
};
