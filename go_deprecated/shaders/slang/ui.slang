#include "common.slang"

// Fullscreen UI overlay. The engine rasterises its widget tree into an RGBA
// texture on the CPU each frame and this pass composites it over the scene.
//
// The quad is a real two-triangle vertex buffer rather than the usual
// bufferless "generate a big triangle from SV_VertexID": Slang lowers
// SV_VertexID to (VertexIndex - BaseVertex), which pulls in the SPIR-V
// DrawParameters capability, and enabling that needs the
// shaderDrawParameters device feature the go-vulkan bindings do not expose yet.
// The vertex data is the same quad the OpenGL backend has always used.
//
// Vertex positions are already in clip space, so no matrices are involved and
// no TO_VK_DEPTH remap is needed: z = 0 sits on the near plane in both
// conventions, keeping the overlay in front of the scene.

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
