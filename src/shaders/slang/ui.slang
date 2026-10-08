#include "common.slang"

// One gutter Cmd: a unit quad the model matrix stretches onto the Cmd's rect,
// already in clip space, so no camera matrices and no TO_VK_DEPTH. The tint is
// the material colour and metallic slots, as DrawUniforms has no field for it

struct VSOut {
  float4 position : SV_Position;
  float2 uv : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float2 aTexCoord) {
  VSOut o;
  o.uv = aTexCoord;
  o.position = mul(pushConstants.draw.model, float4(aPos, 1.0));
  return o;
}

[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
  // Both straight alpha, so tinting is a plain multiply
  float4 tint = float4(pushConstants.draw.matDiffuse, pushConstants.draw.matMetallic);
  return TEX_DIFFUSE.Sample(i.uv) * tint;
}
