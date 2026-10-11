#include "common.slang"

// The same two calls as surface.slang's vsMain, so the position comes out
// identical to the bit: the main pass tests EQUAL
[shader("vertex")]
float4 vsMain(float3 aPos) : SV_Position {
  return clipPosition(worldPosition(aPos));
}

// Depth only, nothing to shade
[shader("fragment")]
void fsMain() {}
