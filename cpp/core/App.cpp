#include "App.hpp"
#include "input/Input.hpp"
#include "opengl/Shader.hpp"
#include "scene/Scene.hpp"
#include "settings/Settings.hpp"

#include <cstdio>
#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>
#include <iostream>

App::App(const std::string &title, int width, int height) {
  initGLFW(title, width, height);
  initGL();
}

App::~App() {
  glfwDestroyWindow(window);
  glfwTerminate();
}

void App::initGLFW(const std::string &title, int width, int height) {
  if (!glfwInit()) {
    std::cerr << "GLFW init failed\n";
    return;
  }

  glfwWindowHint(GLFW_CONTEXT_VERSION_MAJOR, 4);
  glfwWindowHint(GLFW_CONTEXT_VERSION_MINOR, 1);
  glfwWindowHint(GLFW_OPENGL_PROFILE, GLFW_OPENGL_CORE_PROFILE);
#ifdef __APPLE__
  glfwWindowHint(GLFW_OPENGL_FORWARD_COMPAT, GL_TRUE);
#endif
  glfwWindowHint(GLFW_SAMPLES, 4);

  window = glfwCreateWindow(width, height, title.c_str(), nullptr, nullptr);
  if (!window) {
    std::cerr << "Window creation failed\n";
    glfwTerminate();
    return;
  }

  glfwMakeContextCurrent(window);
  glfwSetInputMode(window, GLFW_CURSOR, GLFW_CURSOR_DISABLED);

  glfwSetFramebufferSizeCallback(window, Input::framebufferSizeCallback);
  glfwSetCursorPosCallback(window, Input::mouseCallback);
  glfwSetScrollCallback(window, Input::scrollCallback);

  Settings::windowWidth = width;
  Settings::windowHeight = height;
}

void App::initGL() {
  if (!gladLoadGLLoader((GLADloadproc)glfwGetProcAddress)) {
    std::cerr << "glad init failed\n";
    return;
  }

  glEnable(GL_DEPTH_TEST);
  glEnable(GL_CULL_FACE);
  glEnable(GL_BLEND);
  glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);
}

void App::run(const std::string &scenePath) {
  Scene scene(scenePath);
  Input::setCamera(&scene.camera);

  Shader forwardShader("shaders/forward.vert.glsl",
                       "shaders/forward.frag.glsl");
  Shader depthShader("shaders/depth.vert.glsl", "shaders/depth.frag.glsl");
  Shader depthCubeShader("shaders/depth_cube.vert.glsl",
                         "shaders/depth_cube.frag.glsl",
                         "shaders/depth_cube.geo.glsl");
  Shader skyboxShader("shaders/skybox.vert.glsl", "shaders/skybox.frag.glsl");

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

    glClearColor(0.1f, 0.1f, 0.1f, 1.0f);
    glClear(GL_COLOR_BUFFER_BIT | GL_DEPTH_BUFFER_BIT);

    // Shadow passes — iterate all lights, track directional light space matrix
    glm::mat4 lightSpaceMatrix(1.0f);
    for (auto &light : scene.lights) {
      glm::mat4 mat = light.renderLight(nearPlane, farPlane, depthShader,
                                        depthCubeShader, scene);
      if (light.type == LightType::Sun)
        lightSpaceMatrix = mat;
    }

    // Main pass
    glViewport(0, 0, Settings::windowWidth, Settings::windowHeight);
    glClear(GL_COLOR_BUFFER_BIT | GL_DEPTH_BUFFER_BIT);

    scene.renderSkybox(skyboxShader);
    scene.renderScene(forwardShader, lightSpaceMatrix, farPlane);

    glfwSwapBuffers(window);
    glfwPollEvents();
  }
}
