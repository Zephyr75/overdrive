#version 460
#include "common.glsl"

layout(location = 0) in vec3 TexCoords;

layout(location = 0) out vec4 FragColor;

void main()
{
    FragColor = texture(texturesCube[pc.ubo.texSkybox], TexCoords);
}
