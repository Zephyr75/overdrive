#version 460
#include "common.glsl"

layout(location = 0) in vec3 aPos;

void main()
{
    gl_Position = pc.ubo.lightSpaceMatrix * pc.ubo.model * vec4(aPos, 1.0);
    TO_VK_DEPTH(gl_Position);
}
