#pragma once
#include <string>
#include <glad/glad.h>
#include <glm/glm.hpp>

class Shader {
public:
    GLuint id = 0;

    Shader() = default;
    Shader(const std::string& vertPath, const std::string& fragPath,
           const std::string& geoPath = "");
    ~Shader();

    Shader(const Shader&) = delete;
    Shader& operator=(const Shader&) = delete;

    void use() const;

    void setInt  (const std::string& name, int value)          const;
    void setFloat(const std::string& name, float value)        const;
    void setVec3 (const std::string& name, const glm::vec3& v) const;
    void setMat4 (const std::string& name, const glm::mat4& m) const;

private:
    static GLuint compileShader(const std::string& path, GLenum type);
};
