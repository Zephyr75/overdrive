#version 460
#include "common.glsl"

layout(location = 0) in vec4 FragPos;

void main()
{
    float lightDistance = length(FragPos.xyz - pc.ubo.lightPos);
    lightDistance = lightDistance / pc.ubo.farPlane;
    gl_FragDepth = lightDistance;
}
