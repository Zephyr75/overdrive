#include "common.slang"

// Screen-space ambient occlusion from the prepass depth alone: positions and
// normals are rebuilt from it, so the prepass needs no colour attachment.
// Writes the noisy occlusion and the view distance, which forward.slang blurs by

// Samples per pixel; the blur hides the noise a per-pixel rotation leaves
#define AO_SAMPLES 16

struct VSOut {
  float4 position : SV_Position;
};

// A triangle that overhangs the screen, so every pixel is covered once
[shader("vertex")]
VSOut vsMain(float3 aPos) {
  VSOut o;
  o.position = float4(aPos.xy, 0.0, 1.0);
  return o;
}

// View-space position under a uv, v = 0 at the top as the flipped screen passes draw it
float3 viewPosition(float2 uv) {
  float depth = TEX_DEPTH.SampleLevel(uv, 0.0).r;
  // Undo TO_VK_DEPTH: the projections are OpenGL style, z in [-1, 1]
  float4 ndc = float4(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, depth * 2.0 - 1.0, 1.0);
  float4 view = mul(pushConstants.frame.invProjection, ndc);
  return view.xyz / view.w;
}

// Screen uv of a view-space point, the inverse of viewPosition
float2 screenUV(float3 view) {
  float4 clip = mul(pushConstants.frame.projection, float4(view, 1.0));
  float2 ndc = clip.xy / clip.w;
  return float2(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);
}

// A rotation in [0, 1) that repeats every 4x4 pixels, so any 4x4 window of the
// blur holds each of the 16 exactly once and averages the noise out entirely.
// Bayer order keeps neighbouring pixels far apart in angle
float tiledNoise(float2 pixel) {
  static const uint bayer[16] = { 0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5 };
  uint2 p = uint2(pixel) % 4;
  return (float(bayer[p.y * 4 + p.x]) + 0.5) / 16.0;
}

[shader("fragment")]
float2 fsMain(VSOut i) : SV_Target {
  uint width, height;
  TEX_DEPTH.GetDimensions(width, height);
  float2 texel = 1.0 / float2(width, height);
  float2 uv = i.position.xy * texel;

  // The far plane is sky, which nothing occludes; its distance, the largest a
  // half holds, keeps it out of every blur
  if (TEX_DEPTH.SampleLevel(uv, 0.0).r >= 1.0)
    return float2(1.0, 65504.0);

  // The normal from the neighbour on each axis whose depth is closer, so a
  // silhouette does not bend it toward the background behind
  float3 p = viewPosition(uv);
  float3 right = viewPosition(uv + float2(texel.x, 0.0));
  float3 left = viewPosition(uv - float2(texel.x, 0.0));
  float3 down = viewPosition(uv + float2(0.0, texel.y));
  float3 up = viewPosition(uv - float2(0.0, texel.y));
  float3 dx = abs(right.z - p.z) < abs(p.z - left.z) ? right - p : p - left;
  float3 dy = abs(down.z - p.z) < abs(p.z - up.z) ? down - p : p - up;
  float3 n = normalize(cross(dx, dy));
  // Toward the camera, which sits at the view-space origin
  if (dot(n, p) > 0.0)
    n = -n;

  // An orthonormal frame around n (Duff et al.), so samples are a hemisphere
  float side = n.z >= 0.0 ? 1.0 : -1.0;
  float a = -1.0 / (side + n.z);
  float b = n.x * n.y * a;
  float3 t = float3(1.0 + side * n.x * n.x * a, side * b, -side * n.x);
  float3 bt = float3(b, side + n.y * n.y * a, -n.y);

  float radius = pushConstants.frame.aoRadius;
  // Keeps a flat surface from occluding itself through depth precision
  float bias = 0.02 * radius;
  float rotation = tiledNoise(i.position.xy) * 2.0 * PI;
  float occlusion = 0.0;
  for (int s = 0; s < AO_SAMPLES; s++) {
    // Cosine-weighted directions on a golden-angle spiral, lengths biased
    // toward the centre where occlusion matters most
    float u = (float(s) + 0.5) / AO_SAMPLES;
    float phi = float(s) * 2.39996323 + rotation;
    float sinTheta = sqrt(u);
    float3 h = float3(cos(phi) * sinTheta, sin(phi) * sinTheta, sqrt(1.0 - u));
    float scale = lerp(0.1, 1.0, frac(float(s) * 0.618034 + rotation * 0.159155));
    scale *= scale;
    float3 samplePos = p + (t * h.x + bt * h.y + n * h.z) * radius * scale;

    // samplePos is the 3d coordinate of the test point in space,
    // sampleUV is the screen pixel this point is located at
    float2 sampleUV = screenUV(samplePos);
    // if sampleUV is out of bounds, we ignore it
    if (any(sampleUV < 0.0) || any(sampleUV > 1.0))
      continue;
    float sceneZ = viewPosition(sampleUV).z;
    // We check if test point is further than stored value for this pixel,
    // `range` makes this edge towards either fully occluded or fully open
    // taking into consideration that a far away occlusion should be ignored
    float range = smoothstep(0.0, 1.0, radius / abs(p.z - sceneZ));
    occlusion += (sceneZ >= samplePos.z + bias ? 1.0 : 0.0) * range;
  }
  // View z is negative in front of the camera
  return float2(1.0 - occlusion / AO_SAMPLES, -p.z);
}
