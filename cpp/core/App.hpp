#pragma once
#include <string>
#include <glad/glad.h>
#include <GLFW/glfw3.h>

class App {
public:
    App(const std::string& title, int width, int height);
    ~App();

    void run(const std::string& scenePath);

    GLFWwindow* window = nullptr;

private:
    void initGLFW(const std::string& title, int width, int height);
    void initGL();
};
