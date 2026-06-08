#pragma once
#include "Shader.hpp"
#include <cstdint>
#include <memory>
#include <string>
#include <vector>

class Backend {
public:
  virtual ~Backend() = default;

  virtual void init() = 0;
  virtual void setClearColor(float r, float g, float b, float a) = 0;
  virtual void clear(bool color, bool depth) = 0;
  virtual void setViewport(int x, int y, int w, int h) = 0;
  virtual void setCullFace(bool front) = 0;
  virtual void setDepthFunc(bool lequal) = 0;

  virtual std::unique_ptr<Shader> createShader(const std::string &vert,
                                               const std::string &frag,
                                               const std::string &geo = "") = 0;

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
  virtual void bindFramebuffer(uint32_t fbo) = 0;
  virtual void clearDepth() = 0;
};

std::unique_ptr<Backend> createBackend();
