#include "Shader.hpp"

#include <fstream>
#include <glm/gtc/type_ptr.hpp>
#include <iostream>
#include <sstream>

Shader::Shader(const std::string &vertPath, const std::string &fragPath,
               const std::string &geoPath) {
  GLuint vert = compileShader(vertPath, GL_VERTEX_SHADER);
  GLuint frag = compileShader(fragPath, GL_FRAGMENT_SHADER);
  GLuint geo = 0;
  if (!geoPath.empty())
    geo = compileShader(geoPath, GL_GEOMETRY_SHADER);

  id = glCreateProgram();
  glAttachShader(id, vert);
  glAttachShader(id, frag);
  if (geo)
    glAttachShader(id, geo);
  glLinkProgram(id);

  GLint status;
  glGetProgramiv(id, GL_LINK_STATUS, &status);
  if (!status) {
    char log[512];
    glGetProgramInfoLog(id, 512, nullptr, log);
    std::cerr << "Shader link error: " << log << "\n";
  }

  glDeleteShader(vert);
  glDeleteShader(frag);
  if (geo)
    glDeleteShader(geo);
}

Shader::~Shader() {
  if (id)
    glDeleteProgram(id);
}

void Shader::use() const { glUseProgram(id); }

void Shader::setInt(const std::string &name, int value) const {
  glUniform1i(glGetUniformLocation(id, name.c_str()), value);
}

void Shader::setFloat(const std::string &name, float value) const {
  glUniform1f(glGetUniformLocation(id, name.c_str()), value);
}

void Shader::setVec3(const std::string &name, const glm::vec3 &v) const {
  glUniform3fv(glGetUniformLocation(id, name.c_str()), 1, glm::value_ptr(v));
}

void Shader::setMat4(const std::string &name, const glm::mat4 &m) const {
  glUniformMatrix4fv(glGetUniformLocation(id, name.c_str()), 1, GL_FALSE,
                     glm::value_ptr(m));
}

GLuint Shader::compileShader(const std::string &path, GLenum type) {
  std::ifstream file(path);
  if (!file.is_open()) {
    std::cerr << "Cannot open shader: " << path << "\n";
    return 0;
  }
  std::ostringstream ss;
  ss << file.rdbuf();
  std::string src = ss.str();
  const char *c = src.c_str();

  GLuint shader = glCreateShader(type);
  glShaderSource(shader, 1, &c, nullptr);
  glCompileShader(shader);

  GLint status;
  glGetShaderiv(shader, GL_COMPILE_STATUS, &status);
  if (!status) {
    char log[512];
    glGetShaderInfoLog(shader, 512, nullptr, log);
    std::cerr << "Shader compile error (" << path << "): " << log << "\n";
  }
  return shader;
}
