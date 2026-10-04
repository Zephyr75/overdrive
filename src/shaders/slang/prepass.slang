#include "common.slang"

// The depth prepass: position only, no shading
//
// The clip position is computed by exactly the same expression as
// forward.slang's vsMain, operation for operation. The main pass then tests
// EQUAL against what this wrote, and EQUAL is unforgiving: reassociating the
// three multiplies here would round differently and z-fight the whole scene.
[shader("vertex")]
float4 vsMain(float3 aPos) : SV_Position {
  float3 fragPos = mul(DRAW.model, float4(aPos, 1.0)).xyz;
  float4 pos = mul(FRAME.projection, mul(FRAME.view, float4(fragPos, 1.0)));
  TO_VK_DEPTH(pos);
  return pos;
}

// Depth-only, so depth is written automatically and there is nothing to shade
[shader("fragment")]
void fsMain() {}
