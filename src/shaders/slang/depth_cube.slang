#include "common.slang"

// The vertex stage emits world-space positions; the geometry stage fans each
// triangle out to all six cubemap faces, applying that face's view-projection.

struct VSOut {
    float4 worldPos : SV_Position;
};

[shader("vertex")]
VSOut vsMain(float3 aPos) {
    VSOut o;
    o.worldPos = mul(U.model, float4(aPos, 1.0));
    return o;
}

struct GSOut {
    // Named clipPos, not position: on the GLSL target slangc would otherwise
    // disambiguate this against LightData.position by renaming the *block's*
    // field (position_0 -> position_1) in this stage only, and GL rejects a
    // program whose uniform-block struct members differ by name across stages.
    float4 clipPos : SV_Position;
    float4 fragPos : TEXCOORD0;
    uint layer : SV_RenderTargetArrayIndex;
};

[shader("geometry")]
[maxvertexcount(18)]
void gsMain(triangle VSOut input[3], inout TriangleStream<GSOut> stream) {
    for (int face = 0; face < 6; ++face) {
        for (int i = 0; i < 3; ++i) {
            GSOut o;
            o.layer = (uint)face;
            o.fragPos = input[i].worldPos;
            o.clipPos = mul(U.shadowMatrices[face], input[i].worldPos);
            TO_VK_DEPTH(o.clipPos);
            stream.Append(o);
        }
        stream.RestartStrip();
    }
}

[shader("fragment")]
float fsMain(GSOut i) : SV_Depth {
    float lightDistance = length(i.fragPos.xyz - U.lightPos);
    return lightDistance / U.farPlane;
}
