#pragma once
#include "Shader.hpp"
#include <cstdint>
#include <memory>
#include <string>
#include <vector>

struct GLFWwindow;

class Backend {
public:
  virtual ~Backend() = default;

  // Called after glfwInit() but before glfwCreateWindow(): sets the
  // API-specific window hints (GL context version, or GLFW_NO_API for Vulkan).
  virtual void configureWindow() = 0;
  // Called once after window creation: context/device/swapchain setup.
  virtual void init(GLFWwindow *window) = 0;

  virtual void beginFrame() = 0;
  // Submits the frame and presents it (GL: swap buffers).
  virtual void endFrame() = 0;

  // framebuffer == 0 targets the backbuffer/swapchain. Depth is always
  // cleared; color only when clearColor is set.
  virtual void beginPass(uint32_t framebuffer, int w, int h, bool clearColor,
                         float r = 0, float g = 0, float b = 0,
                         float a = 1) = 0;
  virtual void endPass() = 0;

  virtual void setCullFace(bool front) = 0;
  virtual void setDepthFunc(bool lequal) = 0;

  // Loads the compiled shader set named <name> (e.g. "forward"). Each backend
  // resolves the per-stage files it needs (GLSL for GL, SPIR-V for Vulkan).
  virtual std::unique_ptr<Shader> createShader(const std::string &name,
                                               bool hasGeometry = false) = 0;

  virtual uint32_t loadTexture(const std::string &path) = 0;
  virtual uint32_t loadCubemap(const std::vector<std::string> &faces) = 0;
  virtual uint32_t whiteTexture() = 0;
  virtual void destroyTexture(uint32_t handle) = 0;
  virtual void bindTexture2D(int unit, uint32_t handle) = 0;
  virtual void bindCubemap(int unit, uint32_t handle) = 0;

  virtual uint32_t createBuffer(const float *data, size_t byteSize,
                                bool dynamic) = 0;
  virtual void updateBuffer(uint32_t handle, const float *data,
                            size_t byteSize) = 0;
  virtual void destroyBuffer(uint32_t handle) = 0;

  virtual void createMesh(uint32_t vbo, const uint32_t *indices, size_t count,
                          uint32_t &vao, uint32_t &ebo) = 0;
  virtual void destroyMesh(uint32_t vao, uint32_t ebo) = 0;

  virtual void createSkyboxMesh(const float *verts, size_t byteSize,
                                uint32_t &vao, uint32_t &vbo) = 0;
  virtual void destroySkyboxMesh(uint32_t vao, uint32_t vbo) = 0;

  virtual void drawMesh(uint32_t vao, size_t indexCount) = 0;
  virtual void drawSkybox(uint32_t vao) = 0;

  virtual void createShadowMap2D(int w, int h, uint32_t &fbo,
                                 uint32_t &tex) = 0;
  virtual void createShadowCubemap(int w, int h, uint32_t &fbo,
                                   uint32_t &cube) = 0;
  virtual void destroyFramebuffer(uint32_t fbo) = 0;
};

std::unique_ptr<Backend> createBackend();
