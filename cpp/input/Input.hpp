#pragma once
#include <GLFW/glfw3.h>
#include "scene/Camera.hpp"

namespace Input {
    extern Camera* camera;
    extern float   sensitivity;
    extern float   speed;

    void setCamera(Camera* cam);

    void processKeyboard(GLFWwindow* window, float deltaTime);
    void mouseCallback  (GLFWwindow* window, double xpos, double ypos);
    void scrollCallback (GLFWwindow* window, double xoffset, double yoffset);
    void framebufferSizeCallback(GLFWwindow* window, int width, int height);
}
