#include "common.slang"

[shader("vertex")]
float4 vsMain(float3 aPos) : SV_Position {
  float4 pos = mul(pushConstants.bake.worldToTile, mul(pushConstants.draw.model, float4(aPos, 1.0)));
  TO_VK_DEPTH(pos);
  return pos;
}

// Depth only, nothing to shade
[shader("fragment")]
void fsMain() {}
