#pragma once
#include <string>
#include <vector>
#include "Camera.hpp"
#include "Mesh.hpp"
#include "Light.hpp"
#include "Skybox.hpp"

class Shader;

class Scene {
public:
    Camera            camera;
    std::vector<Mesh> meshes;
    std::vector<Light> lights;
    Skybox            skybox;

    explicit Scene(const std::string& xmlPath);
    ~Scene();

    void renderScene(const Shader& shader, const glm::mat4& lightSpaceMatrix, float farPlane) const;
    void renderSkybox(const Shader& shader) const;
    void updateMeshes();

    Mesh*  getMesh (const std::string& name);
    Light* getLight(const std::string& name);
};
