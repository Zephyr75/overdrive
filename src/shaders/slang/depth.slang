#include "common.slang"

[shader("vertex")]
float4 vsMain(float3 aPos) : SV_Position {
    float4 pos = mul(U.lightSpaceMatrix, mul(U.model, float4(aPos, 1.0)));
    TO_VK_DEPTH(pos);
    return pos;
}

// Depth-only pass: depth is written automatically, the fragment stage is empty.
[shader("fragment")]
void fsMain() {}
