#version 460
#include "common.glsl"

layout(location = 0) in vec3 aPos;

layout(location = 0) out vec3 TexCoords;

void main()
{
    TexCoords = aPos;
    vec4 pos = pc.ubo.projection * pc.ubo.view * vec4(aPos, 1.0);
    // z = w puts the skybox at maximum depth in both GL and Vulkan ranges
    gl_Position = pos.xyww;
}
