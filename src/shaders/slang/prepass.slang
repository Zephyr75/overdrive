#include "common.slang"

// The same two calls as surface.slang's vsMain, so the position comes out
// identical to the bit: the main pass tests EQUAL
#ifdef CUTOUT
// prepass_cutout.slang: the uv too, for the alpha map
struct VSOut {
  float4 position : SV_Position;
  float2 texCoord : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float2 aTexCoord) {
  VSOut o;
  o.position = clipPosition(worldPosition(aPos));
  o.texCoord = aTexCoord;
  return o;
}

// Depth only, minus the fragments the transparent pass draws
[shader("fragment")]
void fsMain(VSOut i) {
  clipAlpha(i.texCoord);
}
#else
[shader("vertex")]
float4 vsMain(float3 aPos) : SV_Position {
  return clipPosition(worldPosition(aPos));
}

// Depth only, nothing to shade
[shader("fragment")]
void fsMain() {}
#endif
