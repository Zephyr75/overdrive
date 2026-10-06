#include "common.slang"

// Composites the UI texture over the scene; a real quad, as SV_VertexID needs a missing device feature
// Positions are already in clip space, so no matrices and no TO_VK_DEPTH

struct VSOut {
  float4 position : SV_Position;
  float2 uv : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float2 aTexCoord) {
  VSOut o;
  o.uv = aTexCoord;
  o.position = float4(aPos, 1.0);
  return o;
}

[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
  return TEX_DIFFUSE.Sample(i.uv);
}
