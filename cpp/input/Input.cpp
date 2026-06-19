#include "Input.hpp"
#include "settings/Settings.hpp"

#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>
#include <cmath>

namespace Input {

Camera* camera    = nullptr;
float   sensitivity = 0.1f;
float   speed       = 5.0f;

static float lastX     = 0.0f;
static float lastY     = 0.0f;
static bool  firstMouse = true;

void setCamera(Camera* cam) {
    camera = cam;
}

void processKeyboard(GLFWwindow* window, float deltaTime) {
    if (!camera) return;

    float currentSpeed = speed;
    if (glfwGetKey(window, GLFW_KEY_LEFT_SHIFT) == GLFW_PRESS)
        currentSpeed *= 3.0f;

    glm::vec3 right = glm::normalize(glm::cross(camera->front, camera->up));

    if (glfwGetKey(window, GLFW_KEY_W) == GLFW_PRESS)
        camera->pos += camera->front * currentSpeed * deltaTime;
    if (glfwGetKey(window, GLFW_KEY_S) == GLFW_PRESS)
        camera->pos -= camera->front * currentSpeed * deltaTime;
    if (glfwGetKey(window, GLFW_KEY_A) == GLFW_PRESS)
        camera->pos -= right * currentSpeed * deltaTime;
    if (glfwGetKey(window, GLFW_KEY_D) == GLFW_PRESS)
        camera->pos += right * currentSpeed * deltaTime;
    if (glfwGetKey(window, GLFW_KEY_Q) == GLFW_PRESS)
        camera->pos -= camera->up * currentSpeed * deltaTime;
    if (glfwGetKey(window, GLFW_KEY_E) == GLFW_PRESS)
        camera->pos += camera->up * currentSpeed * deltaTime;

    // if (glfwGetKey(window, GLFW_KEY_ESCAPE) == GLFW_PRESS)
    //     glfwSetWindowShouldClose(window, true);
}

void mouseCallback(GLFWwindow* /*window*/, double xpos, double ypos) {
    if (!camera) return;

    if (firstMouse) {
        lastX = static_cast<float>(xpos);
        lastY = static_cast<float>(ypos);
        firstMouse = false;
    }

    float xoffset = static_cast<float>(xpos) - lastX;
    float yoffset = lastY - static_cast<float>(ypos); // reversed: y goes bottom→up
    lastX = static_cast<float>(xpos);
    lastY = static_cast<float>(ypos);

    xoffset *= sensitivity;
    yoffset *= sensitivity;

    camera->yaw   += xoffset;
    camera->pitch += yoffset;

    if (camera->pitch >  89.0f) camera->pitch =  89.0f;
    if (camera->pitch < -89.0f) camera->pitch = -89.0f;

    float pitchRad = glm::radians(camera->pitch);
    float yawRad   = glm::radians(camera->yaw);
    camera->front = glm::normalize(glm::vec3{
        -std::cos(pitchRad) * std::sin(yawRad),
        -std::sin(pitchRad),
        -std::cos(pitchRad) * std::cos(yawRad)
    });
}

void scrollCallback(GLFWwindow* /*window*/, double /*xoffset*/, double yoffset) {
    if (!camera) return;
    camera->fov -= static_cast<float>(yoffset);
    if (camera->fov <  1.0f) camera->fov =  1.0f;
    if (camera->fov > 90.0f) camera->fov = 90.0f;
}

void framebufferSizeCallback(GLFWwindow* /*window*/, int width, int height) {
    Settings::windowWidth  = width;
    Settings::windowHeight = height;
}

} // namespace Input
