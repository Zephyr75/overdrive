#pragma once
#include "renderer/Shader.hpp"
#include <array>
#include <glad/glad.h>
#include <string>
#include <unordered_map>

class GLBackend;

// The Slang-generated GLSL packs every non-opaque uniform into a single std140
// block; samplers stay as named uniforms bound to texture units. GLShader keeps
// a CPU mirror of that block (matching the std140 layout reflected at build),
// written by the GL-style named setters and uploaded to a UBO before each draw.
class GLShader final : public Shader {
public:
  GLShader(GLBackend &backend, const std::string &vertPath,
           const std::string &fragPath, const std::string &geoPath = "");
  ~GLShader() override;

  GLShader(const GLShader &) = delete;
  GLShader &operator=(const GLShader &) = delete;

  void use() const override;
  void setInt(const std::string &name, int value) const override;
  void setFloat(const std::string &name, float value) const override;
  void setVec3(const std::string &name, const glm::vec3 &v) const override;
  void setMat4(const std::string &name, const glm::mat4 &m) const override;

  // Uploads the dirty UBO mirror; the backend calls this before each draw.
  void flushUniforms() const;

private:
  static GLuint compileShader(const std::string &path, GLenum type);
  void write(const std::string &name, const void *data, size_t size) const;

  GLBackend &backend;
  GLuint id = 0;
  GLuint ubo = 0;

  // std140 size of the Uniforms block (shaders/slang/common.slang), reflected
  // by slangc. Kept in sync with opengl/Shader.cpp's offset map.
  static constexpr size_t kBlockSize = 944;
  mutable std::array<unsigned char, kBlockSize> mirror{};
  mutable bool dirty = true;

  // logical sampler name (e.g. "ourTexture") -> uniform location
  std::unordered_map<std::string, GLint> samplerLocations;
};
