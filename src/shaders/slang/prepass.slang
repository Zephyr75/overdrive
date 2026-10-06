#include "common.slang"

// Must match forward.slang's vsMain operation for operation: the main pass tests EQUAL
[shader("vertex")]
float4 vsMain(float3 aPos) : SV_Position {
  float3 fragPos = mul(pushConstants.draw.model, float4(aPos, 1.0)).xyz;
  float4 pos = mul(pushConstants.frame.projection, mul(pushConstants.frame.view, float4(fragPos, 1.0)));
  TO_VK_DEPTH(pos);
  return pos;
}

// Depth only, nothing to shade
[shader("fragment")]
void fsMain() {}
