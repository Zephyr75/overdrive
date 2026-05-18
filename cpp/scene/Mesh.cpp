#define TINYOBJLOADER_IMPLEMENTATION
#include <tiny_obj_loader.h>

#include "Mesh.hpp"
#include "Scene.hpp"
#include "opengl/Shader.hpp"
#include "opengl/Texture.hpp"
#include "settings/Settings.hpp"

#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>
#include <iostream>
#include <unordered_map>

// ---- loading ----------------------------------------------------------------

void Mesh::load(const std::string &objPath, const std::string &mtlDir,
                glm::vec3 pos) {
  position = pos;
  initialPosition = pos;

  tinyobj::ObjReaderConfig cfg;
  cfg.mtl_search_path = mtlDir;
  cfg.triangulate = true;

  tinyobj::ObjReader reader;
  if (!reader.ParseFromFile(objPath, cfg)) {
    std::cerr << "TinyOBJ error (" << objPath << "): " << reader.Error()
              << "\n";
    return;
  }
  if (!reader.Warning().empty())
    std::cerr << "TinyOBJ warning: " << reader.Warning() << "\n";

  auto &attrib = reader.GetAttrib();
  auto &shapes = reader.GetShapes();
  auto &mats = reader.GetMaterials();

  // Load materials
  for (auto &m : mats) {
    Material mat;
    mat.ambient = {m.ambient[0], m.ambient[1], m.ambient[2]};
    mat.diffuse = {m.diffuse[0], m.diffuse[1], m.diffuse[2]};
    mat.specular = {m.specular[0], m.specular[1], m.specular[2]};
    mat.shininess = m.shininess > 0.0f ? m.shininess : 32.0f;
    mat.alpha = m.dissolve;
    if (!m.diffuse_texname.empty())
      mat.texture = Texture::load(mtlDir + m.diffuse_texname);
    if (!m.bump_texname.empty())
      mat.normalMap = Texture::load(mtlDir + m.bump_texname);
    materials.push_back(mat);
  }

  // Expand vertices and group by material ID
  // matVertices[matId] = flat list of RawVertex
  std::unordered_map<int, std::vector<RawVertex>> matVertices;

  for (auto &shape : shapes) {
    size_t indexOffset = 0;
    for (size_t f = 0; f < shape.mesh.num_face_vertices.size(); f++) {
      int fv = shape.mesh.num_face_vertices[f];
      int matId = shape.mesh.material_ids[f];
      if (matId < 0)
        matId = 0;

      for (int v = 0; v < fv; v++) {
        tinyobj::index_t idx = shape.mesh.indices[indexOffset + v];

        RawVertex rv;
        rv.basePos = {attrib.vertices[3 * idx.vertex_index + 0],
                      attrib.vertices[3 * idx.vertex_index + 1],
                      attrib.vertices[3 * idx.vertex_index + 2]};

        if (idx.normal_index >= 0) {
          rv.normal = {attrib.normals[3 * idx.normal_index + 0],
                       attrib.normals[3 * idx.normal_index + 1],
                       attrib.normals[3 * idx.normal_index + 2]};
        }

        if (idx.texcoord_index >= 0) {
          rv.texcoord = {attrib.texcoords[2 * idx.texcoord_index + 0],
                         attrib.texcoords[2 * idx.texcoord_index + 1]};
        }

        matVertices[matId].push_back(rv);
      }
      indexOffset += fv;
    }
  }

  // Build rawVertices (all, shared) and per-material submesh index ranges
  uint32_t globalIndex = 0;
  for (auto &[matId, verts] : matVertices) {
    SubMesh sm;
    sm.materialIndex = matId;
    for (auto &rv : verts) {
      rawVertices.push_back(rv);
      sm.indices.push_back(globalIndex++);
    }
    submeshes.push_back(std::move(sm));
  }
}

// ---- GPU setup --------------------------------------------------------------

void Mesh::setup() {
  if (rawVertices.empty())
    return;

  // Build interleaved float buffer with position offset applied
  glm::vec3 offset = position - initialPosition;
  std::vector<float> buf;
  buf.reserve(rawVertices.size() * 8);
  for (auto &rv : rawVertices) {
    glm::vec3 p = rv.basePos + offset;
    buf.push_back(p.x);
    buf.push_back(p.y);
    buf.push_back(p.z);
    buf.push_back(rv.normal.x);
    buf.push_back(rv.normal.y);
    buf.push_back(rv.normal.z);
    buf.push_back(rv.texcoord.x);
    buf.push_back(rv.texcoord.y);
  }

  // Shared VBO
  glGenBuffers(1, &sharedVbo);
  glBindBuffer(GL_ARRAY_BUFFER, sharedVbo);
  glBufferData(GL_ARRAY_BUFFER, buf.size() * sizeof(float), buf.data(),
               GL_DYNAMIC_DRAW);

  // Per-submesh: VAO + EBO
  for (auto &sm : submeshes) {
    glGenVertexArrays(1, &sm.vao);
    glGenBuffers(1, &sm.ebo);

    glBindVertexArray(sm.vao);
    glBindBuffer(GL_ARRAY_BUFFER, sharedVbo);

    glBindBuffer(GL_ELEMENT_ARRAY_BUFFER, sm.ebo);
    glBufferData(GL_ELEMENT_ARRAY_BUFFER, sm.indices.size() * sizeof(uint32_t),
                 sm.indices.data(), GL_STATIC_DRAW);

    // pos(3) + normal(3) + texcoord(2) = 8 floats stride
    glVertexAttribPointer(0, 3, GL_FLOAT, GL_FALSE, 8 * sizeof(float),
                          (void *)0);
    glEnableVertexAttribArray(0);
    glVertexAttribPointer(1, 3, GL_FLOAT, GL_FALSE, 8 * sizeof(float),
                          (void *)(3 * sizeof(float)));
    glEnableVertexAttribArray(1);
    glVertexAttribPointer(2, 2, GL_FLOAT, GL_FALSE, 8 * sizeof(float),
                          (void *)(6 * sizeof(float)));
    glEnableVertexAttribArray(2);

    glBindVertexArray(0);
  }
}

// ---- vertex update (on move) ------------------------------------------------

void Mesh::rebuildAndUpload() {
  glm::vec3 offset = position - initialPosition;
  std::vector<float> buf;
  buf.reserve(rawVertices.size() * 8);
  for (auto &rv : rawVertices) {
    glm::vec3 p = rv.basePos + offset;
    buf.push_back(p.x);
    buf.push_back(p.y);
    buf.push_back(p.z);
    buf.push_back(rv.normal.x);
    buf.push_back(rv.normal.y);
    buf.push_back(rv.normal.z);
    buf.push_back(rv.texcoord.x);
    buf.push_back(rv.texcoord.y);
  }
  glBindBuffer(GL_ARRAY_BUFFER, sharedVbo);
  glBufferSubData(GL_ARRAY_BUFFER, 0, buf.size() * sizeof(float), buf.data());
}

void Mesh::updateVertices() {
  if (!needsUpdate)
    return;
  rebuildAndUpload();
  needsUpdate = false;
}

void Mesh::moveTo(glm::vec3 dest) {
  position = dest;
  needsUpdate = true;
}

void Mesh::moveBy(glm::vec3 delta) {
  position += delta;
  needsUpdate = true;
}

// ---- draw -------------------------------------------------------------------

void Mesh::draw(const Shader &shader, const Scene &scene) const {
  // Set light uniforms
  for (int i = 0; i < (int)scene.lights.size(); i++) {
    auto &l = scene.lights[i];
    std::string base = "lights[" + std::to_string(i) + "].";
    shader.setInt(base + "type", static_cast<int>(l.type));
    shader.setFloat(base + "constant", 1.0f);
    shader.setFloat(base + "linear", 0.09f);
    shader.setFloat(base + "quadratic", 0.032f);
    shader.setFloat(base + "cutoff", glm::cos(glm::radians(45.0f)));
    shader.setVec3(base + "color", l.color);
    shader.setFloat(base + "intensity", l.intensity);
    shader.setFloat(base + "diffuse", l.diffuse);
    shader.setFloat(base + "specular", l.specular);
    shader.setVec3(base + "position", l.pos);
    shader.setVec3(base + "direction", l.dir);
  }

  shader.setVec3("viewPos", scene.camera.pos);

  // Shadow textures
  shader.setInt("shadowMap", 0);
  shader.setInt("ourTexture", 1);
  shader.setInt("shadowCubeMap", 2);
  shader.setInt("skybox", 3);

  // lights[1] = directional → 2D shadow map
  if (scene.lights.size() > 1) {
    glActiveTexture(GL_TEXTURE0);
    glBindTexture(GL_TEXTURE_2D, scene.lights[1].depthMap);
  }
  // lights[0] = point → cubemap shadow
  if (!scene.lights.empty()) {
    glActiveTexture(GL_TEXTURE2);
    glBindTexture(GL_TEXTURE_CUBE_MAP, scene.lights[0].depthCubeMap);
  }
  // Skybox cubemap
  glActiveTexture(GL_TEXTURE3);
  glBindTexture(GL_TEXTURE_CUBE_MAP, scene.skybox.texture);

  for (auto &sm : submeshes) {
    int mi = sm.materialIndex;
    const Material &mat =
        (mi >= 0 && mi < (int)materials.size()) ? materials[mi] : Material{};

    shader.setVec3("material.ambient", mat.ambient);
    shader.setVec3("material.diffuse", mat.diffuse);
    shader.setVec3("material.specular", mat.specular);
    shader.setFloat("material.shininess", mat.shininess);

    glActiveTexture(GL_TEXTURE1);
    glBindTexture(GL_TEXTURE_2D, mat.texture ? mat.texture : Texture::white());

    glBindVertexArray(sm.vao);
    glDrawElements(GL_TRIANGLES, static_cast<GLsizei>(sm.indices.size()),
                   GL_UNSIGNED_INT, 0);
    glBindVertexArray(0);
  }
}
