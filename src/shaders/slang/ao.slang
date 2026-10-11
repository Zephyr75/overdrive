#pragma once
#include "surface.slang"

// ssao's occlusion blurred over the 4x4 window its noise repeats in, skipping
// neighbours at another distance so an edge keeps its contact shadow. Slot 0 is
// the white pixel, every tap equal and the result 1, which is AO off
float screenOcclusion(float2 pixel) {
  uint width, height;
  TEX_AO.GetDimensions(width, height);
  float2 texel = 1.0 / float2(width, height);
  float2 uv = pixel * texel;
  float centre = TEX_AO.SampleLevel(uv, 0.0).g;

  float sum = 0.0;
  float weight = 0.0;
  for (int y = -2; y < 2; y++) {
    for (int x = -2; x < 2; x++) {
      float2 tap = TEX_AO.SampleLevel(uv + float2(x, y) * texel, 0.0).rg;
      // Full weight within 5% of the centre's distance, none past 10%
      float w = saturate(2.0 - abs(tap.y - centre) / (0.05 * centre));
      sum += tap.x * w;
      weight += w;
    }
  }
  // The centre tap always weighs 1, so weight is never 0
  return sum / weight;
}

// The material's occlusion times the screen-space one, raised to aoIntensity:
// what a style multiplies its environment term by. A scene light has its own
// shadow, so it takes neither
float ambientOcclusion(Surface s, float2 pixel) {
  return s.ao * pow(screenOcclusion(pixel), pushConstants.frame.aoIntensity);
}
