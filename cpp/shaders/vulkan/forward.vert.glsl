#version 460
#include "common.glsl"

layout(location = 0) in vec3 aPos;
layout(location = 1) in vec3 aNormal;
layout(location = 2) in vec2 aTexCoord;

layout(location = 0) out vec2 TexCoord;
layout(location = 1) out vec3 Normal;
layout(location = 2) out vec3 FragPos;
layout(location = 3) out vec4 FragPosLightSpace;

void main()
{
    mat4 model = pc.ubo.model;
    FragPos = vec3(model * vec4(aPos, 1.0));
    TexCoord = aTexCoord;
    Normal = mat3(transpose(inverse(model))) * aNormal;
    FragPosLightSpace = pc.ubo.lightSpaceMatrix * vec4(FragPos, 1.0);
    gl_Position = pc.ubo.projection * pc.ubo.view * vec4(FragPos, 1.0);
    TO_VK_DEPTH(gl_Position);
}
