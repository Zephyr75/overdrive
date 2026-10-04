#include "common.slang"

// Bakes one cube face of a point light into an atlas tile
//
// Identical to depth.slang except for what lands in the depth buffer: this
// writes the linear radial distance to the light rather than the projected
// depth. Radial distance is face-independent, so it stays continuous across a
// face boundary and one bias tunes all six tiles instead of six.
//
// Writing SV_Depth disables early-Z, which is why the sun and spot tiles keep
// depth.slang rather than sharing this one.

struct VSOut {
  float4 position : SV_Position;
  float3 worldPos : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos) {
  VSOut o;
  float3 world = mul(DRAW.model, float4(aPos, 1.0)).xyz;
  o.worldPos = world;
  o.position = mul(BAKE.worldToTile, float4(world, 1.0));
  TO_VK_DEPTH(o.position);
  return o;
}

[shader("fragment")]
float fsMain(VSOut i) : SV_Depth {
  return length(i.worldPos - BAKE.lightPos) / BAKE.farPlane;
}
