#include "surface.slang"
#include "lighting.slang"
#include "pbr.slang"

// A material's translucent fragments: forward.slang's shading, drawn sorted
// after everything opaque and blended over it. No ambientOcclusion, whose
// buffer holds the opaque surface behind this one rather than this one
[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
  // Opaque ones were drawn by the opaque passes, and 0 is nothing to blend
  float alpha = surfaceAlpha(i.texCoord);
  if (alpha >= OPAQUE_ALPHA || alpha <= 0.0)
    discard;

  Surface s = loadSurface(i);

  float3 Lo = float3(0.0);
  for (int l = 0; l < pushConstants.frame.lightCount; l++) {
    LightSample light;
    if (sampleLight(pushConstants.frame.lights[l], s, light))
      Lo += cookTorrance(s, light) * (1.0 - light.shadow);
  }

  return float4(toDisplay(pbrAmbient(s) + Lo).rgb, alpha);
}
