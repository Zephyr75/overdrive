#pragma once
#include "surface.slang"

// What arrives at a surface from each scene light, shadow included, apart from
// how the surface responds: that is the shading style's part

struct LightSample {
  float3 L;        // toward the light
  float3 radiance; // arriving, after attenuation and the spot cone
  float shadow;    // 0 lit, 1 fully shadowed
};

// How far the receiver is pushed along its normal before the shadow test, in world units
static const float NORMAL_OFFSET_2D = 0.08;
static const float NORMAL_OFFSET_CUBE = 0.10;

// One texel of a tile, clamped one texel inside it: past the edge is another light's shadow
float tileSample(Sampler2D atlas, ShadowRecord rec, float2 local) {
  float2 inset = clamp(local, rec.texelSize, 1.0 - rec.texelSize);
  return atlas.Sample(rec.atlasRect.xy + inset * rec.atlasRect.zw).r;
}

// 4 corner taps, then the full 3x3 only where they disagree
float pcfTile(Sampler2D atlas, ShadowRecord rec, float2 local, float depth,
              float bias) {
  float2 corners[4] = { float2(-1, -1), float2(1, -1), float2(-1, 1),
                        float2(1, 1) };
  float early = 0.0;
  // No [unroll], slangc would emit GL_EXT_control_flow_attributes
  for (int c = 0; c < 4; ++c) {
    float d = tileSample(atlas, rec, local + corners[c] * rec.texelSize);
    early += depth - bias > d ? 1.0 : 0.0;
  }
  if (early == 0.0)
    return 0.0;
  if (early == 4.0)
    return 1.0;
  if ((rec.flags & 2) != 0)
    return early * 0.25;

  float shadow = 0.0;
  for (int x = -1; x <= 1; ++x) {
    for (int y = -1; y <= 1; ++y) {
      float d = tileSample(atlas, rec, local + float2(x, y) * rec.texelSize);
      shadow += depth - bias > d ? 1.0 : 0.0;
    }
  }
  return shadow / 9.0;
}

// +X, -X, +Y, -Y, +Z, -Z: must match the face order in scene/shadowatlas.go
int cubeFace(float3 d) {
  float3 a = abs(d);
  if (a.x >= a.y && a.x >= a.z)
    return d.x > 0.0 ? 0 : 1;
  if (a.y >= a.z)
    return d.y > 0.0 ? 2 : 3;
  return d.z > 0.0 ? 4 : 5;
}

// Occlusion in [0, 1] from one tile; a cube face stores radial distance / farPlane
float shadowLookup(int recordIndex, float3 fragPos, float3 normal,
                   float3 lightDir, float3 lightPos) {
  ShadowRecord rec = pushConstants.records[recordIndex];

  // 2D tiles grow the offset toward grazing angles, where acne is worst
  float offset = NORMAL_OFFSET_CUBE;
  if (rec.face < 0) {
    float ndl = clamp(dot(normal, lightDir), 0.0, 1.0);
    offset = NORMAL_OFFSET_2D * (1.0 - ndl);
  }
  offset *= pushConstants.frame.shadowNormalScale;
  float3 offsetPos = fragPos + normal * offset;

  float4 clip = mul(rec.lightSpace, float4(offsetPos, 1.0));
  if (clip.w <= 0.0)
    return 0.0;
  float3 ndc = clip.xyz / clip.w;
  float2 local = ndc.xy * 0.5 + 0.5;
  float z = ndc.z * 0.5 + 0.5;
  if (local.x < 0.0 || local.x > 1.0 || local.y < 0.0 || local.y > 1.0 ||
      z > 1.0)
    return 0.0;

  float depth = z;
  float bias = 0.0015;
  if (rec.face >= 0) {
    depth = length(offsetPos - lightPos) / rec.farPlane;
    bias = 0.04 / rec.farPlane;
  }

  if ((rec.flags & 1) != 0)
    return pcfTile(HOT_SHADOW_DYNAMIC, rec, local, depth, bias);
  return pcfTile(HOT_SHADOW_STATIC, rec, local, depth, bias);
}

// False when the light cannot reach the surface: out of its radius, or outside
// a spot's cone. A sun has radius 0 and always reaches
bool sampleLight(LightData light, Surface s, out LightSample arriving) {
  arriving = (LightSample)0;
  float3 radiance = light.color * light.diffuse * light.intensity;
  if (light.type == LIGHT_SUN) {
    arriving.L = normalize(light.direction);
  } else {
    float3 d = light.position - s.position;
    if (dot(d, d) > light.radius * light.radius)
      return false;
    arriving.L = normalize(d);
    float distance = length(d);
    radiance *= 1.0 / (light.kConstant + distance * distance);
    // A spot's direction points toward the light, like a sun's
    if (light.type == LIGHT_SPOT) {
      float theta = dot(arriving.L, normalize(light.direction));
      float cone = smoothstep(light.outerCutoff, light.cutoff, theta);
      if (cone <= 0.0)
        return false;
      radiance *= cone;
    }
  }
  arriving.radiance = radiance;

  if (light.shadowIndex >= 0) {
    int rec = light.shadowIndex;
    if (light.shadowCount == 6)
      rec += cubeFace(s.position - light.position);
    arriving.shadow = shadowLookup(rec, s.position, s.normal, arriving.L, light.position);
  }
  return true;
}
