#include "Scene.hpp"
#include "opengl/Shader.hpp"
#include "settings/Settings.hpp"

#include <cmath>
#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>
#include <iostream>
#include <pugixml.hpp>
#include <sstream>

// ---------- helpers ----------------------------------------------------------

static glm::vec3 parseVec3(const std::string &s) {
  std::istringstream ss(s);
  float x, y, z;
  char comma;
  ss >> x >> comma >> y >> comma >> z;
  return {x, y, z};
}

static glm::vec3 blenderToGL(glm::vec3 v) {
  // Blender: X right, Y forward, Z up → OpenGL: X right, Y up, Z back
  return {v.x, v.z, -v.y};
}

// ---------- construction / destruction ---------------------------------------

Scene::Scene(const std::string &xmlPath) {
  pugi::xml_document doc;
  auto result = doc.load_file(xmlPath.c_str());
  if (!result) {
    std::cerr << "Failed to load scene: " << xmlPath << " — "
              << result.description() << "\n";
    return;
  }

  auto sceneNode = doc.child("scene");

  // Camera
  auto camNode = sceneNode.child("camera");
  if (camNode) {
    glm::vec3 pos = parseVec3(camNode.child_value("position"));
    pos = blenderToGL(pos);

    float yaw = camNode.child("yaw").text().as_float();
    float pitch = camNode.child("pitch").text().as_float();
    float fov = camNode.child("fov").text().as_float();

    camera.pos = pos;
    camera.yaw = yaw;
    camera.pitch = pitch;
    camera.fov = fov;
    camera.up = {0.0f, 1.0f, 0.0f};

    // Compute front from yaw/pitch (matches Go camera.go)
    float pitchRad = glm::radians(pitch);
    float yawRad = glm::radians(yaw);
    camera.front = glm::normalize(
        glm::vec3{-std::cos(pitchRad) * std::sin(yawRad), -std::sin(pitchRad),
                  -std::cos(pitchRad) * std::cos(yawRad)});
  }

  // Meshes
  for (auto meshNode : sceneNode.children("mesh")) {
    std::string name = meshNode.attribute("name").as_string();
    std::string obj = meshNode.child_value("obj");
    std::string mtl = meshNode.child_value("mtl");
    glm::vec3 pos = parseVec3(meshNode.child_value("position"));
    pos = blenderToGL(pos);

    const std::string meshDir = "assets/meshes/";
    Mesh mesh;
    mesh.name = name;
    mesh.load(meshDir + obj, meshDir, pos);
    mesh.setup();
    meshes.push_back(std::move(mesh));
  }

  // Lights
  for (auto lightNode : sceneNode.children("light")) {
    Light light;
    light.name = lightNode.attribute("name").as_string();

    std::string typeStr = lightNode.child_value("type");
    light.type = (typeStr == "point") ? LightType::Point : LightType::Sun;

    glm::vec3 pos = parseVec3(lightNode.child_value("position"));
    glm::vec3 dir = parseVec3(lightNode.child_value("direction"));
    pos = blenderToGL(pos);
    dir = glm::vec3{-dir.x, -dir.z, dir.y}; // matches Go's dir transform

    light.pos = pos;
    light.dir = dir;
    light.color = parseVec3(lightNode.child_value("color"));
    light.diffuse = lightNode.child("diffuse").text().as_float(1.0f);
    light.specular = lightNode.child("specular").text().as_float(1.0f);
    light.intensity = lightNode.child("intensity").text().as_float(1.0f);

    if (light.type == LightType::Point)
      light.intensity /= 1000.0f;

    light.setup();
    lights.push_back(std::move(light));
  }

  skybox.setup();
}

Scene::~Scene() {
  for (auto &l : lights)
    l.destroy();
  skybox.destroy();
}

// ---------- update -----------------------------------------------------------

void Scene::updateMeshes() {
  for (auto &m : meshes)
    m.updateVertices();
}

// ---------- accessors --------------------------------------------------------

Mesh *Scene::getMesh(const std::string &n) {
  for (auto &m : meshes)
    if (m.name == n)
      return &m;
  return nullptr;
}

Light *Scene::getLight(const std::string &n) {
  for (auto &l : lights)
    if (l.name == n)
      return &l;
  return nullptr;
}

// ---------- rendering --------------------------------------------------------

void Scene::renderScene(const Shader &shader, const glm::mat4 &lightSpaceMatrix,
                        float farPlane) const {
  shader.use();

  glm::mat4 view =
      glm::lookAt(camera.pos, camera.pos + camera.front, camera.up);
  glm::mat4 proj = glm::perspective(glm::radians(camera.fov),
                                    Settings::aspectRatio(), 0.1f, 100.0f);
  glm::mat4 model(1.0f);

  shader.setMat4("view", view);
  shader.setMat4("projection", proj);
  shader.setMat4("model", model);
  shader.setMat4("lightSpaceMatrix", lightSpaceMatrix);
  shader.setFloat("farPlane", farPlane);

  for (auto &m : meshes)
    m.draw(shader, *this);
}

void Scene::renderSkybox(const Shader &shader) const {
  skybox.render(shader, camera);
}
