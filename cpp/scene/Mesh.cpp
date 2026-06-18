#define TINYOBJLOADER_IMPLEMENTATION
#include <tiny_obj_loader.h>

#include "Mesh.hpp"
#include "Scene.hpp"
#include "renderer/Backend.hpp"
#include "renderer/Shader.hpp"

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

  // Blender bakes machine-specific paths into the .mtl (often absolute). Keep
  // only the basename and resolve against the project-local textures/ dir, so
  // the project is portable across machines and folders.
  auto resolveTex = [](const std::string &name) {
    auto slash = name.find_last_of("/\\");
    std::string base = slash == std::string::npos ? name : name.substr(slash + 1);
    return "textures/" + base;
  };

  // Load materials — store paths; GPU upload happens in setup()
  for (auto &m : mats) {
    Material mat;
    mat.ambient = {m.ambient[0], m.ambient[1], m.ambient[2]};
    mat.diffuse = {m.diffuse[0], m.diffuse[1], m.diffuse[2]};
    mat.specular = {m.specular[0], m.specular[1], m.specular[2]};
    mat.shininess = m.shininess > 0.0f ? m.shininess : 32.0f;
    mat.alpha = m.dissolve;
    if (!m.diffuse_texname.empty())
      mat.texturePath = resolveTex(m.diffuse_texname);
    if (!m.bump_texname.empty())
      mat.normalMapPath = resolveTex(m.bump_texname);
    materials.push_back(mat);
  }

  // Expand vertices and group by material ID
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

void Mesh::setup(Backend &b) {
  backend = &b;
  if (rawVertices.empty())
    return;

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

  sharedVbo = b.createBuffer(buf.data(), buf.size() * sizeof(float), true);

  for (auto &sm : submeshes)
    b.createMesh(sharedVbo, sm.indices.data(), sm.indices.size(), sm.vao,
                 sm.ebo);

  for (auto &mat : materials) {
    if (!mat.texturePath.empty())
      mat.texture = b.loadTexture(mat.texturePath);
    if (!mat.normalMapPath.empty())
      mat.normalMap = b.loadTexture(mat.normalMapPath);
  }
}

// ---- cleanup ----------------------------------------------------------------

void Mesh::destroy() {
  if (!backend)
    return;
  for (auto &sm : submeshes) {
    if (sm.vao)
      backend->destroyMesh(sm.vao, sm.ebo);
    sm.vao = sm.ebo = 0;
  }
  if (sharedVbo) {
    backend->destroyBuffer(sharedVbo);
    sharedVbo = 0;
  }
  for (auto &mat : materials) {
    if (mat.texture) {
      backend->destroyTexture(mat.texture);
      mat.texture = 0;
    }
    if (mat.normalMap) {
      backend->destroyTexture(mat.normalMap);
      mat.normalMap = 0;
    }
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
  backend->updateBuffer(sharedVbo, buf.data(), buf.size() * sizeof(float));
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

  shader.setInt("shadowMap", 0);
  shader.setInt("ourTexture", 1);
  shader.setInt("shadowCubeMap", 2);
  shader.setInt("skybox", 3);
  shader.setInt("normalMap", 4);

  // lights[1] = directional → 2D shadow map
  if (scene.lights.size() > 1)
    backend->bindTexture2D(0, scene.lights[1].depthMap);
  // lights[0] = point → cubemap shadow
  if (!scene.lights.empty())
    backend->bindCubemap(2, scene.lights[0].depthCubeMap);
  // Skybox cubemap
  backend->bindCubemap(3, scene.skybox.texture);

  for (auto &sm : submeshes) {
    int mi = sm.materialIndex;
    const Material &mat =
        (mi >= 0 && mi < (int)materials.size()) ? materials[mi] : Material{};

    shader.setVec3("material.ambient", mat.ambient);
    shader.setVec3("material.diffuse", mat.diffuse);
    shader.setVec3("material.specular", mat.specular);
    shader.setFloat("material.shininess", mat.shininess);

    backend->bindTexture2D(1, mat.texture ? mat.texture : backend->whiteTexture());
    backend->bindTexture2D(4, mat.normalMap ? mat.normalMap : backend->whiteTexture());
    shader.setInt("useNormalMap", mat.normalMap ? 1 : 0);
    backend->drawMesh(sm.vao, sm.indices.size());
  }
}
