#pragma once

namespace Settings {
constexpr int SHADOW_WIDTH = 2048;
constexpr int SHADOW_HEIGHT = 2048;

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
