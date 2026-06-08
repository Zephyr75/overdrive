#pragma once
#include <glm/glm.hpp>
#include <string>

class Shader {
public:
  virtual ~Shader() = default;

  virtual void use() const = 0;
  virtual void setInt(const std::string &name, int value) const = 0;
  virtual void setFloat(const std::string &name, float value) const = 0;
  virtual void setVec3(const std::string &name, const glm::vec3 &v) const = 0;
  virtual void setMat4(const std::string &name, const glm::mat4 &m) const = 0;
};
