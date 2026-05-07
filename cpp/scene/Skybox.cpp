#include "Skybox.hpp"
#include "Camera.hpp"
#include "opengl/Shader.hpp"
#include "opengl/Texture.hpp"
#include "settings/Settings.hpp"

#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>

constexpr float Skybox::vertices[];

void Skybox::setup() {
    glGenVertexArrays(1, &vao);
    glGenBuffers(1, &vbo);

    glBindVertexArray(vao);
    glBindBuffer(GL_ARRAY_BUFFER, vbo);
    glBufferData(GL_ARRAY_BUFFER, sizeof(vertices), vertices, GL_STATIC_DRAW);

    glVertexAttribPointer(0, 3, GL_FLOAT, GL_FALSE, 3 * sizeof(float), (void*)0);
    glEnableVertexAttribArray(0);
    glBindVertexArray(0);

    texture = Texture::loadCubemap({
        "textures/skybox/right.png",
        "textures/skybox/left.png",
        "textures/skybox/top.png",
        "textures/skybox/bottom.png",
        "textures/skybox/front.png",
        "textures/skybox/back.png",
    });
}

void Skybox::destroy() {
    if (vao)     glDeleteVertexArrays(1, &vao);
    if (vbo)     glDeleteBuffers(1, &vbo);
    if (texture) glDeleteTextures(1, &texture);
}

void Skybox::render(const Shader& shader, const Camera& cam) const {
    glDepthFunc(GL_LEQUAL);
    shader.use();

    // Strip translation from view matrix so skybox stays at infinity
    glm::mat4 view = glm::mat4(glm::mat3(
        glm::lookAt(cam.pos, cam.pos + cam.front, cam.up)));
    glm::mat4 proj = glm::perspective(
        glm::radians(cam.fov),
        Settings::aspectRatio(),
        0.1f, 100.0f);

    shader.setMat4("view",       view);
    shader.setMat4("projection", proj);
    shader.setInt  ("skybox",    0);

    glBindVertexArray(vao);
    glActiveTexture(GL_TEXTURE0);
    glBindTexture(GL_TEXTURE_CUBE_MAP, texture);
    glDrawArrays(GL_TRIANGLES, 0, 36);
    glBindVertexArray(0);

    glDepthFunc(GL_LESS);
}
