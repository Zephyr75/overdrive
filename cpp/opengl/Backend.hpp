#pragma once
#include "renderer/Backend.hpp"

class GLBackend final : public Backend {
public:
  void configureWindow() override;
  void init(GLFWwindow *window) override;

  void beginFrame() override;
  void endFrame() override;

  void beginPass(uint32_t framebuffer, int w, int h, bool clearColor, float r,
                 float g, float b, float a) override;
  void endPass() override;

  void setCullFace(bool front) override;
  void setDepthFunc(bool lequal) override;

  std::unique_ptr<Shader> createShader(const std::string &vert,
                                       const std::string &frag,
                                       const std::string &geo) override;

  uint32_t loadTexture(const std::string &path) override;
  uint32_t loadCubemap(const std::vector<std::string> &faces) override;
  uint32_t whiteTexture() override;
  void destroyTexture(uint32_t handle) override;
  void bindTexture2D(int unit, uint32_t handle) override;
  void bindCubemap(int unit, uint32_t handle) override;

  uint32_t createBuffer(const float *data, size_t byteSize,
                        bool dynamic) override;
  void updateBuffer(uint32_t handle, const float *data,
                    size_t byteSize) override;
  void destroyBuffer(uint32_t handle) override;

  void createMesh(uint32_t vbo, const uint32_t *indices, size_t count,
                  uint32_t &vao, uint32_t &ebo) override;
  void destroyMesh(uint32_t vao, uint32_t ebo) override;

  void createSkyboxMesh(const float *verts, size_t byteSize, uint32_t &vao,
                        uint32_t &vbo) override;
  void destroySkyboxMesh(uint32_t vao, uint32_t vbo) override;

  void drawMesh(uint32_t vao, size_t indexCount) override;
  void drawSkybox(uint32_t vao) override;

  void createShadowMap2D(int w, int h, uint32_t &fbo, uint32_t &tex) override;
  void createShadowCubemap(int w, int h, uint32_t &fbo,
                           uint32_t &cube) override;
  void destroyFramebuffer(uint32_t fbo) override;

private:
  GLFWwindow *window = nullptr;
  uint32_t whiteTex = 0;
};
