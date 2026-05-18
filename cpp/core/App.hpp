#pragma once
#include <glad/glad.h>
#define GLFW_INCLUDE_NONE
#include <GLFW/glfw3.h>
#include <string>

class App {
public:
  App(const std::string &title, int width, int height);
  ~App();

  void run(const std::string &scenePath);

  GLFWwindow *window = nullptr;

private:
  void initGLFW(const std::string &title, int width, int height);
  void initGL();
};
