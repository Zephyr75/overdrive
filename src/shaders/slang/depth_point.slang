#include "common.slang"

// One point-light cube face: writes radial distance, so all six faces share one bias
// Writing SV_Depth disables early-Z, which is why suns and spots keep depth.slang

struct VSOut {
  float4 position : SV_Position;
  float3 worldPos : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos) {
  VSOut o;
  float3 world = mul(pushConstants.draw.model, float4(aPos, 1.0)).xyz;
  o.worldPos = world;
  o.position = mul(pushConstants.bake.worldToTile, float4(world, 1.0));
  TO_VK_DEPTH(o.position);
  return o;
}

[shader("fragment")]
float fsMain(VSOut i) : SV_Depth {
  return length(i.worldPos - pushConstants.bake.lightPos) / pushConstants.bake.farPlane;
}
