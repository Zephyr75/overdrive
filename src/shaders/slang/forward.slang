#include "surface.slang"
#include "lighting.slang"
#include "ao.slang"
#include "pbr.slang"

// The shading style: Cook-Torrance under every scene light, plus the image-based
// light. Everything it reads is built by the includes, so another style is
// another file with its own fsMain, keeping shadows, lights and AO as they are
[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
  Surface s = loadSurface(i);

  float3 Lo = float3(0.0);
  for (int l = 0; l < pushConstants.frame.lightCount; l++) {
    LightSample light;
    if (sampleLight(pushConstants.frame.lights[l], s, light))
      Lo += cookTorrance(s, light) * (1.0 - light.shadow);
  }

  return toDisplay(pbrAmbient(s) * ambientOcclusion(s, i.position.xy) + Lo);
}
