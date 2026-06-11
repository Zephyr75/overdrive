#version 460
#include "common.glsl"

layout(triangles) in;
layout(triangle_strip, max_vertices = 18) out;

layout(location = 0) out vec4 FragPos;

void main()
{
    for (int face = 0; face < 6; ++face) {
        gl_Layer = face;
        for (int i = 0; i < 3; ++i) {
            FragPos = gl_in[i].gl_Position;
            gl_Position = pc.ubo.shadowMatrices[face] * FragPos;
            TO_VK_DEPTH(gl_Position);
            EmitVertex();
        }
        EndPrimitive();
    }
}
