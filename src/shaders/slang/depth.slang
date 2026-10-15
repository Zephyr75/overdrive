#include "common.slang"

struct VSOut {
  float4 position : SV_Position;
#ifdef CUTOUT
  float2 texCoord : TEXCOORD0;
#endif
};

// depth_cutout.slang reads the uv too, for the alpha map
#ifdef CUTOUT
[shader("vertex")]
VSOut vsMain(float3 aPos, float2 aTexCoord) {
#else
[shader("vertex")]
VSOut vsMain(float3 aPos) {
#endif
  VSOut o;
  o.position = mul(pushConstants.bake.worldToTile, mul(pushConstants.draw.model, float4(aPos, 1.0)));
  TO_VK_DEPTH(o.position);
#ifdef CUTOUT
  o.texCoord = aTexCoord;
#endif
  return o;
}

// Depth only, minus the fragments that are not opaque, which cast no shadow
[shader("fragment")]
void fsMain(VSOut i) {
#ifdef CUTOUT
  clipAlpha(i.texCoord);
#endif
}
