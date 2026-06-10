#pragma once
#include <memory>
#include <string>

struct GLFWwindow;
class Backend;

class App {
public:
  App(const std::string &title, int width, int height);
  ~App();

  void run(const std::string &scenePath);

  GLFWwindow *window = nullptr;

private:
  void initGLFW(const std::string &title, int width, int height);

  std::unique_ptr<Backend> backend;
};
