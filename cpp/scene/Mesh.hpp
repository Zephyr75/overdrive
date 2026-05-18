#pragma once
#include <string>
#include <vector>
#include <glad/glad.h>
#include <glm/glm.hpp>
#include "Material.hpp"

class Scene; // forward declaration to avoid circular include
class Shader;

struct SubMesh {
    GLuint vao = 0, vbo = 0, ebo = 0;
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

    void load(const std::string& objPath, const std::string& mtlDir, glm::vec3 pos);
    void setup();
    void draw(const Shader& shader, const Scene& scene) const;
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
    GLuint sharedVbo = 0;
    bool needsUpdate = false;

    void rebuildAndUpload();
};
