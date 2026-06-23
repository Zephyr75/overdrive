#pragma once

namespace Settings {
constexpr int SHADOW_WIDTH = 2048;
constexpr int SHADOW_HEIGHT = 2048;

// Max point lights that can cast a cube shadow at once. Must match
// MAX_SHADOW_CUBES in shaders/slang/common.slang and the C++ uniform mirrors.
constexpr int MAX_SHADOW_CUBES = 4;

// Texture-unit assignment shared by Mesh::draw (binds + sampler uniforms) and
// the Vulkan backend (unit -> dedicated descriptor mapping). The cube shadow
// maps occupy a contiguous block of MAX_SHADOW_CUBES units starting at the base.
constexpr int UNIT_SHADOW_2D = 0;
constexpr int UNIT_DIFFUSE = 1;
constexpr int UNIT_SKYBOX = 2;
constexpr int UNIT_NORMAL = 3;
constexpr int SHADOW_CUBE_UNIT_BASE = 4;

// mutable — updated on framebuffer resize
extern int windowWidth;
extern int windowHeight;

inline float aspectRatio() {
  return static_cast<float>(windowWidth) / static_cast<float>(windowHeight);
}

inline float shadowAspectRatio() {
  return static_cast<float>(SHADOW_WIDTH) / static_cast<float>(SHADOW_HEIGHT);
}
} // namespace Settings
