#version 460
#include "common.glsl"

layout(location = 0) in vec3 aPos;

void main()
{
    // World-space position; projection happens per cube face in the
    // geometry stage.
    gl_Position = pc.ubo.model * vec4(aPos, 1.0);
}
