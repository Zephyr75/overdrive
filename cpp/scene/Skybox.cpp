#include "Skybox.hpp"
#include "Camera.hpp"
#include "renderer/Backend.hpp"
#include "renderer/Shader.hpp"
#include "settings/Settings.hpp"

#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>

constexpr float Skybox::vertices[];

void Skybox::setup(Backend &b) {
  backend = &b;

  b.createSkyboxMesh(vertices, sizeof(vertices), vao, vbo);

  texture = b.loadCubemap({
      "textures/skybox/right.png",
      "textures/skybox/left.png",
      "textures/skybox/top.png",
      "textures/skybox/bottom.png",
      "textures/skybox/front.png",
      "textures/skybox/back.png",
  });
}

void Skybox::destroy() {
  if (!backend)
    return;
  if (vao)
    backend->destroySkyboxMesh(vao, vbo);
  if (texture)
    backend->destroyTexture(texture);
}

void Skybox::render(const Shader &shader, const Camera &cam) const {
  backend->setDepthFunc(true);
  shader.use();

  glm::mat4 view =
      glm::mat4(glm::mat3(glm::lookAt(cam.pos, cam.pos + cam.front, cam.up)));
  glm::mat4 proj = glm::perspective(glm::radians(cam.fov),
                                    Settings::aspectRatio(), 0.1f, 100.0f);

  shader.setMat4("view", view);
  shader.setMat4("projection", proj);
  shader.setInt("skybox", 0);

  backend->bindCubemap(0, texture);
  backend->drawSkybox(vao);

  backend->setDepthFunc(false);
}
