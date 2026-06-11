#pragma once
#include "Uniforms.hpp"
#include "renderer/Shader.hpp"

#include <string>
#include <unordered_map>
#include <vulkan/vulkan.h>

class VKBackend;

// One pipeline per (pass type, vertex layout) combination, built lazily by
// the backend on first draw.
enum VKPass { VKPassMain = 0, VKPassShadow2D = 1, VKPassShadowCube = 2 };
enum VKVertexLayout { VKLayoutMesh = 0, VKLayoutSkybox = 1 };

class VKShader final : public Shader {
public:
  VKShader(VKBackend &backend, const std::string &vert,
           const std::string &frag, const std::string &geo);
  ~VKShader() override;

  void use() const override;
  void setInt(const std::string &name, int value) const override;
  void setFloat(const std::string &name, float value) const override;
  void setVec3(const std::string &name, const glm::vec3 &v) const override;
  void setMat4(const std::string &name, const glm::mat4 &m) const override;

  // Backend-facing state
  VkShaderModule vertModule = VK_NULL_HANDLE;
  VkShaderModule fragModule = VK_NULL_HANDLE;
  VkShaderModule geoModule = VK_NULL_HANDLE;
  mutable VKUniformBlock block{};
  // sampler uniform name -> texture unit (set via setInt, GL semantics)
  mutable std::unordered_map<std::string, int> samplerUnits;
  VkPipeline pipelines[3][2] = {};

private:
  void write(const std::string &name, const void *data, size_t size) const;

  VKBackend &backend;
};
