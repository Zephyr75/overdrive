#include "common.slang"

struct VSOut {
    float4 position : SV_Position;
    float3 texCoords : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos) {
    VSOut o;
    o.texCoords = aPos;
    float4 pos = mul(U.projection, mul(U.view, float4(aPos, 1.0)));
    // z = w puts the skybox at maximum depth in both GL and Vulkan ranges,
    // so no TO_VK_DEPTH remap is needed here.
    o.position = pos.xyww;
    return o;
}

[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
    return TEX_SKYBOX.Sample(i.texCoords);
}
