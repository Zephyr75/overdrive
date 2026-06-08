#include "Light.hpp"
#include "Mesh.hpp"
#include "Scene.hpp"
#include "renderer/Backend.hpp"
#include "renderer/Shader.hpp"
#include "settings/Settings.hpp"

#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>

void Light::setup(Backend &b) {
  backend = &b;

  if (type == LightType::Sun) {
    b.createShadowMap2D(Settings::SHADOW_WIDTH, Settings::SHADOW_HEIGHT,
                        depthMapFBO, depthMap);
  } else {
    b.createShadowCubemap(Settings::SHADOW_WIDTH, Settings::SHADOW_HEIGHT,
                          depthMapFBO, depthCubeMap);
  }
}

void Light::destroy() {
  if (!backend)
    return;
  if (depthMapFBO)
    backend->destroyFramebuffer(depthMapFBO);
  if (depthMap)
    backend->destroyTexture(depthMap);
  if (depthCubeMap)
    backend->destroyTexture(depthCubeMap);
}

glm::mat4 Light::renderLight(float nearPlane, float farPlane,
                             const Shader &depthShader,
                             const Shader &depthCubeShader,
                             const Scene &scene) const {
  glm::mat4 model(1.0f);
  glm::mat4 lightSpaceMatrix(1.0f);

  backend->setViewport(0, 0, Settings::SHADOW_WIDTH, Settings::SHADOW_HEIGHT);
  backend->bindFramebuffer(depthMapFBO);
  backend->clearDepth();

  if (type == LightType::Sun) {
    glm::mat4 proj =
        glm::ortho(-10.0f, 10.0f, -10.0f, 10.0f, nearPlane, farPlane);
    glm::mat4 view = glm::lookAt(pos, pos - dir, {0.0f, 1.0f, 0.0f});
    lightSpaceMatrix = proj * view;

    backend->setCullFace(true);
    depthShader.use();
    depthShader.setMat4("model", model);
    depthShader.setMat4("lightSpaceMatrix", lightSpaceMatrix);

    for (auto &mesh : scene.meshes)
      mesh.draw(depthShader, scene);

    backend->setCullFace(false);
  } else {
    glm::mat4 shadowProj =
        glm::perspective(glm::radians(90.0f), Settings::shadowAspectRatio(),
                         nearPlane, farPlane);

    glm::mat4 transforms[6] = {
        shadowProj * glm::lookAt(pos, pos + glm::vec3(1, 0, 0), {0, -1, 0}),
        shadowProj * glm::lookAt(pos, pos + glm::vec3(-1, 0, 0), {0, -1, 0}),
        shadowProj * glm::lookAt(pos, pos + glm::vec3(0, 1, 0), {0, 0, 1}),
        shadowProj * glm::lookAt(pos, pos + glm::vec3(0, -1, 0), {0, 0, -1}),
        shadowProj * glm::lookAt(pos, pos + glm::vec3(0, 0, 1), {0, -1, 0}),
        shadowProj * glm::lookAt(pos, pos + glm::vec3(0, 0, -1), {0, -1, 0}),
    };

    depthCubeShader.use();
    depthCubeShader.setMat4("model", model);
    depthCubeShader.setVec3("lightPos", pos);
    depthCubeShader.setFloat("farPlane", farPlane);
    for (int i = 0; i < 6; i++)
      depthCubeShader.setMat4("shadowMatrices[" + std::to_string(i) + "]",
                              transforms[i]);

    for (auto &mesh : scene.meshes)
      mesh.draw(depthCubeShader, scene);
  }

  backend->bindFramebuffer(0);
  return lightSpaceMatrix;
}
