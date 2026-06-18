#include "App.hpp"
#include "input/Input.hpp"
#include "renderer/Backend.hpp"
#include "scene/Scene.hpp"
#include "settings/Settings.hpp"

#define GLFW_INCLUDE_NONE
#include <GLFW/glfw3.h>
#include <cstdio>
#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>
#include <iostream>

App::App(const std::string &title, int width, int height) {
  backend = createBackend();
  initGLFW(title, width, height);
}

App::~App() {
  // Destroy the backend first: it owns the Vulkan surface/swapchain, which must
  // be torn down while the window (and its wayland connection) is still alive.
  backend.reset();
  glfwDestroyWindow(window);
  glfwTerminate();
}

void App::initGLFW(const std::string &title, int width, int height) {
  if (!glfwInit()) {
    std::cerr << "GLFW init failed\n";
    return;
  }

  backend->configureWindow();

  window = glfwCreateWindow(width, height, title.c_str(), nullptr, nullptr);
  if (!window) {
    std::cerr << "Window creation failed\n";
    glfwTerminate();
    return;
  }

  glfwSetInputMode(window, GLFW_CURSOR, GLFW_CURSOR_DISABLED);

  glfwSetFramebufferSizeCallback(window, Input::framebufferSizeCallback);
  glfwSetCursorPosCallback(window, Input::mouseCallback);
  glfwSetScrollCallback(window, Input::scrollCallback);

  Settings::windowWidth = width;
  Settings::windowHeight = height;
}

void App::run(const std::string &scenePath) {
  backend->init(window);

  Scene scene(scenePath, *backend);
  Input::setCamera(&scene.camera);

  auto forwardShader = backend->createShader("forward");
  auto depthShader = backend->createShader("depth");
  auto depthCubeShader = backend->createShader("depth_cube", /*hasGeometry=*/true);
  auto skyboxShader = backend->createShader("skybox");

  float lastFrame = 0.0f;
  float fpsTimer = 0.0f;
  int frames = 0;

  constexpr float nearPlane = 1.0f;
  constexpr float farPlane = 50.0f;

  while (!glfwWindowShouldClose(window)) {
    float currentFrame = static_cast<float>(glfwGetTime());
    float deltaTime = currentFrame - lastFrame;
    lastFrame = currentFrame;

    frames++;
    if (currentFrame - fpsTimer >= 1.0f) {
      std::printf("\rFPS: %d  ", frames);
      std::fflush(stdout);
      frames = 0;
      fpsTimer = currentFrame;
    }

    Input::processKeyboard(window, deltaTime);

    scene.updateMeshes();

    backend->beginFrame();

    // Shadow passes — only the designated shadow casters have a depth map.
    glm::mat4 lightSpaceMatrix(1.0f);
    for (auto &light : scene.lights) {
      if (!light.castsShadow)
        continue;
      glm::mat4 mat = light.renderLight(nearPlane, farPlane, *depthShader,
                                        *depthCubeShader, scene);
      if (light.type == LightType::Sun)
        lightSpaceMatrix = mat;
    }

    // Main pass
    backend->beginPass(0, Settings::windowWidth, Settings::windowHeight, true,
                       0.1f, 0.1f, 0.1f, 1.0f);

    scene.renderSkybox(*skyboxShader);
    scene.renderScene(*forwardShader, lightSpaceMatrix, farPlane);

    backend->endPass();
    backend->endFrame();

    glfwPollEvents();
  }
}
