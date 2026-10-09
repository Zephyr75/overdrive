#include "common.slang"

struct VSOut {
  float4 position : SV_Position;
  float3 texCoords : TEXCOORD0;
};

[shader("vertex")]
VSOut vsMain(float3 aPos) {
  VSOut o;
  o.texCoords = aPos;
  float4 pos = mul(pushConstants.frame.projection, mul(pushConstants.frame.view, float4(aPos, 1.0)));
  // z = w puts the sky on the far plane, so no TO_VK_DEPTH
  o.position = pos.xyww;
  return o;
}

[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
  return toDisplay(envSample(TEX_SKY, normalize(i.texCoords), 0.0));
}
