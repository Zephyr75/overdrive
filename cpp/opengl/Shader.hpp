#pragma once
#include "renderer/Shader.hpp"
#include <glad/glad.h>
#include <string>

class GLShader final : public Shader {
public:
  GLuint id = 0;

  GLShader() = default;
  GLShader(const std::string &vertPath, const std::string &fragPath,
           const std::string &geoPath = "");
  ~GLShader() override;

  GLShader(const GLShader &) = delete;
  GLShader &operator=(const GLShader &) = delete;

  void use() const override;
  void setInt(const std::string &name, int value) const override;
  void setFloat(const std::string &name, float value) const override;
  void setVec3(const std::string &name, const glm::vec3 &v) const override;
  void setMat4(const std::string &name, const glm::mat4 &m) const override;

private:
  static GLuint compileShader(const std::string &path, GLenum type);
};
