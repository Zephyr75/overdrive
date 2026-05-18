#include "Light.hpp"
#include "Mesh.hpp"
#include "Scene.hpp"
#include "opengl/Shader.hpp"
#include "settings/Settings.hpp"

#include <cstdio>
#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>

void Light::setup() {
  glGenFramebuffers(1, &depthMapFBO);

  if (type == LightType::Sun) {
    glGenTextures(1, &depthMap);
    glBindTexture(GL_TEXTURE_2D, depthMap);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_DEPTH_COMPONENT, Settings::SHADOW_WIDTH,
                 Settings::SHADOW_HEIGHT, 0, GL_DEPTH_COMPONENT, GL_FLOAT,
                 nullptr);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_BORDER);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_BORDER);
    float border[] = {1.0f, 1.0f, 1.0f, 1.0f};
    glTexParameterfv(GL_TEXTURE_2D, GL_TEXTURE_BORDER_COLOR, border);

    glBindFramebuffer(GL_FRAMEBUFFER, depthMapFBO);
    glFramebufferTexture2D(GL_FRAMEBUFFER, GL_DEPTH_ATTACHMENT, GL_TEXTURE_2D,
                           depthMap, 0);
  } else {
    glGenTextures(1, &depthCubeMap);
    glBindTexture(GL_TEXTURE_CUBE_MAP, depthCubeMap);
    for (int i = 0; i < 6; i++) {
      glTexImage2D(GL_TEXTURE_CUBE_MAP_POSITIVE_X + i, 0, GL_DEPTH_COMPONENT,
                   Settings::SHADOW_WIDTH, Settings::SHADOW_HEIGHT, 0,
                   GL_DEPTH_COMPONENT, GL_FLOAT, nullptr);
    }
    glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_WRAP_R, GL_CLAMP_TO_EDGE);

    glBindFramebuffer(GL_FRAMEBUFFER, depthMapFBO);
    glFramebufferTexture(GL_FRAMEBUFFER, GL_DEPTH_ATTACHMENT, depthCubeMap, 0);
  }

  glDrawBuffer(GL_NONE);
  glReadBuffer(GL_NONE);
  glBindFramebuffer(GL_FRAMEBUFFER, 0);
}

void Light::destroy() {
  if (depthMapFBO)
    glDeleteFramebuffers(1, &depthMapFBO);
  if (depthMap)
    glDeleteTextures(1, &depthMap);
  if (depthCubeMap)
    glDeleteTextures(1, &depthCubeMap);
}

glm::mat4 Light::renderLight(float nearPlane, float farPlane,
                             const Shader &depthShader,
                             const Shader &depthCubeShader,
                             const Scene &scene) const {
  glm::mat4 model(1.0f);
  glm::mat4 lightSpaceMatrix(1.0f);

  glViewport(0, 0, Settings::SHADOW_WIDTH, Settings::SHADOW_HEIGHT);
  glBindFramebuffer(GL_FRAMEBUFFER, depthMapFBO);
  glClear(GL_DEPTH_BUFFER_BIT);

  if (type == LightType::Sun) {
    glm::mat4 proj =
        glm::ortho(-10.0f, 10.0f, -10.0f, 10.0f, nearPlane, farPlane);
    glm::mat4 view = glm::lookAt(pos, pos - dir, {0.0f, 1.0f, 0.0f});
    lightSpaceMatrix = proj * view;

    glCullFace(GL_FRONT);
    depthShader.use();
    depthShader.setMat4("model", model);
    depthShader.setMat4("lightSpaceMatrix", lightSpaceMatrix);

    for (auto &mesh : scene.meshes)
      mesh.draw(depthShader, scene);

    glCullFace(GL_BACK);
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

  glBindFramebuffer(GL_FRAMEBUFFER, 0);
  return lightSpaceMatrix;
}
