#pragma once
#include <string>
#include <vector>
#include <glad/glad.h>

namespace Texture {
    // Load 2D texture from file. Returns 0 on failure.
    GLuint load(const std::string& path);

    // Load cubemap from 6 face files: right, left, top, bottom, front, back.
    GLuint loadCubemap(const std::vector<std::string>& faces);

    // 1x1 white texture (lazy-initialized)
    GLuint white();
}
