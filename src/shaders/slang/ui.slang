#include "common.slang"

// Fullscreen UI overlay
// The engine rasterises its widget tree into an RGBA texture each frame
// This pass composites that texture over the scene
//
// The quad is a real two-triangle vertex buffer, not the bufferless trick
// Slang lowers SV_VertexID to (VertexIndex - BaseVertex)
// That pulls in the SPIR-V DrawParameters capability
// Enabling it needs shaderDrawParameters, which the go-vulkan bindings lack
// So we send the same quad the OpenGL backend has always used
//
// Positions are already in clip space, so no matrices and no TO_VK_DEPTH
// z = 0 sits on the near plane both ways, keeping the overlay in front

struct VSOut {
    float4 position : SV_Position;
    float2 uv : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float2 aTexCoord) {
    VSOut o;
    o.uv = aTexCoord;
    o.position = float4(aPos, 1.0);
    return o;
}

[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
    return TEX_OUR.Sample(i.uv);
}
