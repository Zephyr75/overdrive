#include "Shader.hpp"
#include "Backend.hpp"

#include <cctype>
#include <cstring>
#include <fstream>
#include <glm/gtc/type_ptr.hpp>
#include <iostream>
#include <set>
#include <sstream>

// Must match MAX_LIGHTS in shaders/slang/common.slang.
static constexpr int MAX_LIGHTS = 8;

// GL-style uniform name -> byte offset inside the std140 Uniforms block.
// These offsets are the std140 layout slangc reflects for the block in
// shaders/slang/common.slang. The scalar-layout Vulkan mirror
// (vulkan/Uniforms.hpp) uses different offsets; only the logical names are
// shared.
static const std::unordered_map<std::string, size_t> &glUniformOffsets() {
  static const auto map = [] {
    std::unordered_map<std::string, size_t> m;
    m["view"] = 0;
    m["projection"] = 64;
    m["model"] = 128;
    m["lightSpaceMatrix"] = 192;
    for (int i = 0; i < 6; i++)
      m["shadowMatrices[" + std::to_string(i) + "]"] = 256 + i * 64;
    m["viewPos"] = 640;
    m["farPlane"] = 652;
    m["lightPos"] = 656;
    m["material.ambient"] = 672;
    m["material.diffuse"] = 688;
    m["material.specular"] = 704;
    m["material.shininess"] = 716;
    for (int i = 0; i < MAX_LIGHTS; i++) {
      const std::string base = "lights[" + std::to_string(i) + "].";
      const size_t off = 720 + i * 96; // std140 LightData stride = 96
      m[base + "type"] = off + 0;
      m[base + "constant"] = off + 4;
      m[base + "linear"] = off + 8;
      m[base + "quadratic"] = off + 12;
      m[base + "cutoff"] = off + 16;
      m[base + "color"] = off + 32;
      m[base + "intensity"] = off + 44;
      m[base + "diffuse"] = off + 48;
      m[base + "specular"] = off + 52;
      m[base + "position"] = off + 64;
      m[base + "direction"] = off + 80;
    }
    // The five texSlot ints (1488..1508) are read by Vulkan only; GL samples
    // through named samplers. GL does read the flag + light-count + shadow
    // indices from the UBO. std140 offsets after lights[MAX_LIGHTS] (ends 1488).
    m["useNormalMap"] = 1508;
    m["lightCount"] = 1512;
    m["shadowDirIndex"] = 1516;
    m["shadowPointIndex"] = 1520;
    // PBR scalars, appended after the trailing ints; each std140 scalar is
    // 4-byte aligned. The block ends at 1536, still a multiple of 16 so
    // kBlockSize is unchanged.
    m["material.metallic"] = 1524;
    m["material.roughness"] = 1528;
    m["material.ao"] = 1532;
    return m;
  }();
  return map;
}

// slangc disambiguates identifiers with a trailing "_<n>" (e.g. ourTexture_0);
// strip it to recover the logical name the engine uses.
static std::string stripSuffix(const std::string &name) {
  size_t us = name.rfind('_');
  if (us == std::string::npos || us + 1 >= name.size())
    return name;
  for (size_t i = us + 1; i < name.size(); i++)
    if (!std::isdigit(static_cast<unsigned char>(name[i])))
      return name;
  return name.substr(0, us);
}

GLShader::GLShader(GLBackend &b, const std::string &vertPath,
                   const std::string &fragPath, const std::string &geoPath)
    : backend(b) {
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

  // Bind the program's uniform block(s) to binding point 0 and back them with
  // a dynamic UBO. There is exactly one block (the shared Uniforms).
  GLint numBlocks = 0;
  glGetProgramiv(id, GL_ACTIVE_UNIFORM_BLOCKS, &numBlocks);
  for (GLint i = 0; i < numBlocks; i++)
    glUniformBlockBinding(id, i, 0);

  glGenBuffers(1, &ubo);
  glBindBuffer(GL_UNIFORM_BUFFER, ubo);
  glBufferData(GL_UNIFORM_BUFFER, kBlockSize, nullptr, GL_DYNAMIC_DRAW);
  glBindBuffer(GL_UNIFORM_BUFFER, 0);

  // Record sampler uniform locations under their logical names.
  GLint count = 0;
  glGetProgramiv(id, GL_ACTIVE_UNIFORMS, &count);
  for (GLint i = 0; i < count; i++) {
    char nameBuf[128];
    GLsizei len = 0;
    GLint size = 0;
    GLenum type = 0;
    glGetActiveUniform(id, i, sizeof(nameBuf), &len, &size, &type, nameBuf);
    if (type == GL_SAMPLER_2D || type == GL_SAMPLER_CUBE) {
      GLint loc = glGetUniformLocation(id, nameBuf);
      samplerLocations[stripSuffix(nameBuf)] = loc;
    }
  }
}

GLShader::~GLShader() {
  if (ubo)
    glDeleteBuffers(1, &ubo);
  if (id)
    glDeleteProgram(id);
}

void GLShader::use() const {
  glUseProgram(id);
  glBindBufferBase(GL_UNIFORM_BUFFER, 0, ubo);
  backend.setCurrentShader(const_cast<GLShader *>(this));
}

void GLShader::flushUniforms() const {
  if (!dirty)
    return;
  glBindBuffer(GL_UNIFORM_BUFFER, ubo);
  glBufferSubData(GL_UNIFORM_BUFFER, 0, kBlockSize, mirror.data());
  glBindBuffer(GL_UNIFORM_BUFFER, 0);
  dirty = false;
}

void GLShader::write(const std::string &name, const void *data,
                     size_t size) const {
  const auto &offsets = glUniformOffsets();
  auto it = offsets.find(name);
  if (it == offsets.end()) {
    static std::set<std::string> warned;
    if (warned.insert(name).second)
      std::cerr << "GLShader: unknown uniform \"" << name << "\"\n";
    return;
  }
  std::memcpy(mirror.data() + it->second, data, size);
  dirty = true;
}

void GLShader::setInt(const std::string &name, int value) const {
  auto it = samplerLocations.find(name);
  if (it != samplerLocations.end()) {
    // GL semantics: a sampler uniform holds its texture unit.
    glProgramUniform1i(id, it->second, value);
    return;
  }
  // Block int members (e.g. lights[i].type) live in the UBO. A sampler name a
  // shader doesn't declare (e.g. "ourTexture" during the depth pass) is ignored
  // silently, matching GL's no-op when a uniform location is absent.
  if (glUniformOffsets().count(name))
    write(name, &value, sizeof value);
}

void GLShader::setFloat(const std::string &name, float value) const {
  write(name, &value, sizeof value);
}

void GLShader::setVec3(const std::string &name, const glm::vec3 &v) const {
  write(name, glm::value_ptr(v), sizeof(float) * 3);
}

void GLShader::setMat4(const std::string &name, const glm::mat4 &m) const {
  write(name, glm::value_ptr(m), sizeof(float) * 16);
}

GLuint GLShader::compileShader(const std::string &path, GLenum type) {
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
