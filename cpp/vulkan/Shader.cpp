#include "Shader.hpp"
#include "Backend.hpp"

#include <cstring>
#include <fstream>
#include <iostream>
#include <set>
#include <vector>

// ---- uniform block reflection ------------------------------------------------

const std::unordered_map<std::string, VKUniformField> &vkUniformFields() {
  static const auto map = [] {
    std::unordered_map<std::string, VKUniformField> m;
    auto add = [&](const std::string &n, size_t off, size_t sz) {
      m[n] = {off, sz};
    };

    add("view", offsetof(VKUniformBlock, view), 64);
    add("projection", offsetof(VKUniformBlock, projection), 64);
    add("model", offsetof(VKUniformBlock, model), 64);
    add("lightSpaceMatrix", offsetof(VKUniformBlock, lightSpaceMatrix), 64);
    for (int i = 0; i < 6; i++)
      add("shadowMatrices[" + std::to_string(i) + "]",
          offsetof(VKUniformBlock, shadowMatrices) + i * 64, 64);

    add("viewPos", offsetof(VKUniformBlock, viewPos), 12);
    add("farPlane", offsetof(VKUniformBlock, farPlane), 4);
    add("lightPos", offsetof(VKUniformBlock, lightPos), 4 * 3);

    add("material.ambient", offsetof(VKUniformBlock, matAmbient), 12);
    add("material.diffuse", offsetof(VKUniformBlock, matDiffuse), 12);
    add("material.specular", offsetof(VKUniformBlock, matSpecular), 12);
    add("material.shininess", offsetof(VKUniformBlock, matShininess), 4);

    for (int i = 0; i < MAX_LIGHTS; i++) {
      const std::string base = "lights[" + std::to_string(i) + "].";
      const size_t off = offsetof(VKUniformBlock, lights) + i * sizeof(VKLightData);
      add(base + "type", off + offsetof(VKLightData, type), 4);
      add(base + "constant", off + offsetof(VKLightData, constant), 4);
      add(base + "linear", off + offsetof(VKLightData, linear), 4);
      add(base + "quadratic", off + offsetof(VKLightData, quadratic), 4);
      add(base + "cutoff", off + offsetof(VKLightData, cutoff), 4);
      add(base + "color", off + offsetof(VKLightData, color), 12);
      add(base + "intensity", off + offsetof(VKLightData, intensity), 4);
      add(base + "diffuse", off + offsetof(VKLightData, diffuse), 4);
      add(base + "specular", off + offsetof(VKLightData, specular), 4);
      add(base + "position", off + offsetof(VKLightData, position), 12);
      add(base + "direction", off + offsetof(VKLightData, direction), 12);
    }

    add("useNormalMap", offsetof(VKUniformBlock, useNormalMap), 4);
    add("lightCount", offsetof(VKUniformBlock, lightCount), 4);
    add("shadowDirIndex", offsetof(VKUniformBlock, shadowDirIndex), 4);
    add("shadowPointIndex", offsetof(VKUniformBlock, shadowPointIndex), 4);

    add("material.metallic", offsetof(VKUniformBlock, matMetallic), 4);
    add("material.roughness", offsetof(VKUniformBlock, matRoughness), 4);
    add("material.ao", offsetof(VKUniformBlock, matAo), 4);
    return m;
  }();
  return map;
}

const std::unordered_map<std::string, VKSamplerSlot> &vkSamplerSlots() {
  static const std::unordered_map<std::string, VKSamplerSlot> map = {
      {"shadowMap", {offsetof(VKUniformBlock, texShadowMap), false}},
      {"ourTexture", {offsetof(VKUniformBlock, texOurTexture), false}},
      {"shadowCubeMap", {offsetof(VKUniformBlock, texShadowCubeMap), true}},
      {"skybox", {offsetof(VKUniformBlock, texSkybox), true}},
      {"normalMap", {offsetof(VKUniformBlock, texNormalMap), false}},
  };
  return map;
}

// ---- module loading ----------------------------------------------------------

static VkShaderModule loadModule(VkDevice device, const std::string &path) {
  std::ifstream file(path, std::ios::binary | std::ios::ate);
  if (!file) {
    std::cerr << "Failed to open SPIR-V file: " << path << "\n";
    return VK_NULL_HANDLE;
  }
  const auto size = static_cast<size_t>(file.tellg());
  std::vector<char> code(size);
  file.seekg(0);
  file.read(code.data(), static_cast<std::streamsize>(size));

  VkShaderModuleCreateInfo ci{VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO};
  ci.codeSize = size;
  ci.pCode = reinterpret_cast<const uint32_t *>(code.data());

  VkShaderModule module = VK_NULL_HANDLE;
  if (vkCreateShaderModule(device, &ci, nullptr, &module) != VK_SUCCESS)
    std::cerr << "vkCreateShaderModule failed: " << path << "\n";
  return module;
}

// ---- VKShader ----------------------------------------------------------------

VKShader::VKShader(VKBackend &b, const std::string &vert,
                   const std::string &frag, const std::string &geo)
    : backend(b) {
  vertModule = loadModule(backend.vkDevice(), vert);
  fragModule = loadModule(backend.vkDevice(), frag);
  if (!geo.empty())
    geoModule = loadModule(backend.vkDevice(), geo);
}

VKShader::~VKShader() {
  VkDevice device = backend.vkDevice();
  if (!device)
    return;
  vkDeviceWaitIdle(device);
  for (auto &perPass : pipelines)
    for (auto &p : perPass)
      if (p)
        vkDestroyPipeline(device, p, nullptr);
  if (vertModule)
    vkDestroyShaderModule(device, vertModule, nullptr);
  if (fragModule)
    vkDestroyShaderModule(device, fragModule, nullptr);
  if (geoModule)
    vkDestroyShaderModule(device, geoModule, nullptr);
}

void VKShader::use() const {
  backend.setCurrentShader(const_cast<VKShader *>(this));
}

void VKShader::write(const std::string &name, const void *data,
                     size_t size) const {
  const auto &fields = vkUniformFields();
  auto it = fields.find(name);
  if (it == fields.end()) {
    static std::set<std::string> warned;
    if (warned.insert(name).second)
      std::cerr << "VKShader: unknown uniform \"" << name << "\"\n";
    return;
  }
  std::memcpy(reinterpret_cast<char *>(&block) + it->second.offset, data,
              std::min(size, it->second.size));
}

void VKShader::setInt(const std::string &name, int value) const {
  const auto &samplers = vkSamplerSlots();
  if (samplers.count(name)) {
    samplerUnits[name] = value; // GL semantics: sampler uniform = texture unit
    return;
  }
  int32_t v = value;
  write(name, &v, sizeof v);
}

void VKShader::setFloat(const std::string &name, float value) const {
  write(name, &value, sizeof value);
}

void VKShader::setVec3(const std::string &name, const glm::vec3 &v) const {
  write(name, &v, sizeof v);
}

void VKShader::setMat4(const std::string &name, const glm::mat4 &m) const {
  write(name, &m, sizeof m);
}
