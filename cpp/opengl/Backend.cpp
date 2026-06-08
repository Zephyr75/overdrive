#include "Backend.hpp"
#include "Shader.hpp"
#include "Texture.hpp"
#include <glad/glad.h>
#include <memory>

std::unique_ptr<Backend> createBackend() {
  return std::make_unique<GLBackend>();
}

void GLBackend::init() {
  glEnable(GL_DEPTH_TEST);
  glEnable(GL_CULL_FACE);
  glEnable(GL_BLEND);
  glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);
}

void GLBackend::setClearColor(float r, float g, float b, float a) {
  glClearColor(r, g, b, a);
}

void GLBackend::clear(bool color, bool depth) {
  GLbitfield mask = 0;
  if (color)
    mask |= GL_COLOR_BUFFER_BIT;
  if (depth)
    mask |= GL_DEPTH_BUFFER_BIT;
  glClear(mask);
}

void GLBackend::setViewport(int x, int y, int w, int h) {
  glViewport(x, y, w, h);
}

void GLBackend::setCullFace(bool front) {
  glCullFace(front ? GL_FRONT : GL_BACK);
}

void GLBackend::setDepthFunc(bool lequal) {
  glDepthFunc(lequal ? GL_LEQUAL : GL_LESS);
}

std::unique_ptr<Shader> GLBackend::createShader(const std::string &vert,
                                                const std::string &frag,
                                                const std::string &geo) {
  return std::make_unique<GLShader>(vert, frag, geo);
}

uint32_t GLBackend::loadTexture(const std::string &path) {
  return Texture::load(path);
}

uint32_t GLBackend::loadCubemap(const std::vector<std::string> &faces) {
  return Texture::loadCubemap(faces);
}

uint32_t GLBackend::whiteTexture() { return Texture::white(); }

void GLBackend::destroyTexture(uint32_t handle) {
  GLuint tex = handle;
  glDeleteTextures(1, &tex);
}

void GLBackend::bindTexture2D(int unit, uint32_t handle) {
  glActiveTexture(GL_TEXTURE0 + unit);
  glBindTexture(GL_TEXTURE_2D, handle);
}

void GLBackend::bindCubemap(int unit, uint32_t handle) {
  glActiveTexture(GL_TEXTURE0 + unit);
  glBindTexture(GL_TEXTURE_CUBE_MAP, handle);
}

uint32_t GLBackend::createBuffer(const float *data, size_t byteSize,
                                 bool dynamic) {
  GLuint vbo;
  glGenBuffers(1, &vbo);
  glBindBuffer(GL_ARRAY_BUFFER, vbo);
  glBufferData(GL_ARRAY_BUFFER, static_cast<GLsizeiptr>(byteSize), data,
               dynamic ? GL_DYNAMIC_DRAW : GL_STATIC_DRAW);
  return vbo;
}

void GLBackend::updateBuffer(uint32_t handle, const float *data,
                             size_t byteSize) {
  glBindBuffer(GL_ARRAY_BUFFER, handle);
  glBufferSubData(GL_ARRAY_BUFFER, 0, static_cast<GLsizeiptr>(byteSize), data);
}

void GLBackend::destroyBuffer(uint32_t handle) {
  GLuint vbo = handle;
  glDeleteBuffers(1, &vbo);
}

void GLBackend::createMesh(uint32_t vbo, const uint32_t *indices, size_t count,
                           uint32_t &vao, uint32_t &ebo) {
  glGenVertexArrays(1, &vao);
  glGenBuffers(1, &ebo);

  glBindVertexArray(vao);
  glBindBuffer(GL_ARRAY_BUFFER, vbo);

  glBindBuffer(GL_ELEMENT_ARRAY_BUFFER, ebo);
  glBufferData(GL_ELEMENT_ARRAY_BUFFER,
               static_cast<GLsizeiptr>(count * sizeof(uint32_t)), indices,
               GL_STATIC_DRAW);

  // pos(3) + normal(3) + texcoord(2) = 8 floats stride
  glVertexAttribPointer(0, 3, GL_FLOAT, GL_FALSE, 8 * sizeof(float), (void *)0);
  glEnableVertexAttribArray(0);
  glVertexAttribPointer(1, 3, GL_FLOAT, GL_FALSE, 8 * sizeof(float),
                        (void *)(3 * sizeof(float)));
  glEnableVertexAttribArray(1);
  glVertexAttribPointer(2, 2, GL_FLOAT, GL_FALSE, 8 * sizeof(float),
                        (void *)(6 * sizeof(float)));
  glEnableVertexAttribArray(2);

  glBindVertexArray(0);
}

void GLBackend::destroyMesh(uint32_t vao, uint32_t ebo) {
  glDeleteVertexArrays(1, &vao);
  glDeleteBuffers(1, &ebo);
}

void GLBackend::createSkyboxMesh(const float *verts, size_t byteSize,
                                 uint32_t &vao, uint32_t &vbo) {
  glGenVertexArrays(1, &vao);
  glGenBuffers(1, &vbo);

  glBindVertexArray(vao);
  glBindBuffer(GL_ARRAY_BUFFER, vbo);
  glBufferData(GL_ARRAY_BUFFER, static_cast<GLsizeiptr>(byteSize), verts,
               GL_STATIC_DRAW);

  glVertexAttribPointer(0, 3, GL_FLOAT, GL_FALSE, 3 * sizeof(float), (void *)0);
  glEnableVertexAttribArray(0);
  glBindVertexArray(0);
}

void GLBackend::destroySkyboxMesh(uint32_t vao, uint32_t vbo) {
  glDeleteVertexArrays(1, &vao);
  glDeleteBuffers(1, &vbo);
}

void GLBackend::drawMesh(uint32_t vao, size_t indexCount) {
  glBindVertexArray(vao);
  glDrawElements(GL_TRIANGLES, static_cast<GLsizei>(indexCount),
                 GL_UNSIGNED_INT, 0);
  glBindVertexArray(0);
}

void GLBackend::drawSkybox(uint32_t vao) {
  glBindVertexArray(vao);
  glDrawArrays(GL_TRIANGLES, 0, 36);
  glBindVertexArray(0);
}

void GLBackend::createShadowMap2D(int w, int h, uint32_t &fbo, uint32_t &tex) {
  glGenTextures(1, &tex);
  glBindTexture(GL_TEXTURE_2D, tex);
  glTexImage2D(GL_TEXTURE_2D, 0, GL_DEPTH_COMPONENT, w, h, 0,
               GL_DEPTH_COMPONENT, GL_FLOAT, nullptr);
  glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
  glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
  glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_BORDER);
  glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_BORDER);
  float border[] = {1.0f, 1.0f, 1.0f, 1.0f};
  glTexParameterfv(GL_TEXTURE_2D, GL_TEXTURE_BORDER_COLOR, border);

  glGenFramebuffers(1, &fbo);
  glBindFramebuffer(GL_FRAMEBUFFER, fbo);
  glFramebufferTexture2D(GL_FRAMEBUFFER, GL_DEPTH_ATTACHMENT, GL_TEXTURE_2D,
                         tex, 0);
  glDrawBuffer(GL_NONE);
  glReadBuffer(GL_NONE);
  glBindFramebuffer(GL_FRAMEBUFFER, 0);
}

void GLBackend::createShadowCubemap(int w, int h, uint32_t &fbo,
                                    uint32_t &cube) {
  glGenTextures(1, &cube);
  glBindTexture(GL_TEXTURE_CUBE_MAP, cube);
  for (int i = 0; i < 6; i++) {
    glTexImage2D(GL_TEXTURE_CUBE_MAP_POSITIVE_X + i, 0, GL_DEPTH_COMPONENT, w,
                 h, 0, GL_DEPTH_COMPONENT, GL_FLOAT, nullptr);
  }
  glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
  glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
  glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
  glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
  glTexParameteri(GL_TEXTURE_CUBE_MAP, GL_TEXTURE_WRAP_R, GL_CLAMP_TO_EDGE);

  glGenFramebuffers(1, &fbo);
  glBindFramebuffer(GL_FRAMEBUFFER, fbo);
  glFramebufferTexture(GL_FRAMEBUFFER, GL_DEPTH_ATTACHMENT, cube, 0);
  glDrawBuffer(GL_NONE);
  glReadBuffer(GL_NONE);
  glBindFramebuffer(GL_FRAMEBUFFER, 0);
}

void GLBackend::destroyFramebuffer(uint32_t fbo) {
  glDeleteFramebuffers(1, &fbo);
}

void GLBackend::bindFramebuffer(uint32_t fbo) {
  glBindFramebuffer(GL_FRAMEBUFFER, fbo);
}

void GLBackend::clearDepth() { glClear(GL_DEPTH_BUFFER_BIT); }
