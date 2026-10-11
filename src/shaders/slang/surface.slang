#pragma once
#include "common.slang"

// The geometry and material inputs every shading style starts from: the vertex
// stage, and the per-fragment Surface built from the textures and the draw block

struct VSOut {
  float4 position : SV_Position;
  float2 texCoord : TEXCOORD0;
  float3 normal : NORMAL;
  float3 fragPos : TEXCOORD1;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float3 aNormal, float2 aTexCoord) {
  VSOut o;
  o.fragPos = worldPosition(aPos);
  o.texCoord = aTexCoord;
  o.normal = mul(transpose(inverse3((float3x3)pushConstants.draw.model)), aNormal);
  o.position = clipPosition(o.fragPos);
  return o;
}

// Read out of pushConstants.draw and the textures once, so the light loop reads
// registers rather than memory
struct Surface {
  float3 position; // world space
  float3 normal;   // after normal mapping
  float3 viewDir;  // toward the camera
  float3 albedo;   // linear
  float metallic;
  float roughness;
  float ao; // the material's own occlusion, before the screen-space term
};

// Normal mapping with a tangent frame built from screen derivatives, so meshes need no tangents
float3 perturbNormal(float3 N, float3 fragPos, float2 uv) {
  float3 tangentNormal = TEX_NORMAL.Sample(uv).xyz * 2.0 - 1.0;

  float3 dp1 = ddx(fragPos);
  float3 dp2 = ddy(fragPos);
  float2 duv1 = ddx(uv);
  float2 duv2 = ddy(uv);

  float3 dp2perp = cross(dp2, N);
  float3 dp1perp = cross(N, dp1);
  float3 T = dp2perp * duv1.x + dp1perp * duv2.x;
  float3 B = dp2perp * duv1.y + dp1perp * duv2.y;

  float invmax = rsqrt(max(dot(T, T), dot(B, B)));
  float3x3 TBN = float3x3(T * invmax, B * invmax, N);
  return normalize(mul(tangentNormal, TBN));
}

Surface loadSurface(VSOut i) {
  float2 flippedTex = float2(i.texCoord.x, 1.0 - i.texCoord.y);
  Surface s;
  s.position = i.fragPos;
  s.normal = normalize(i.normal);
  if (pushConstants.draw.useNormalMap != 0)
    s.normal = perturbNormal(s.normal, i.fragPos, flippedTex);
  s.viewDir = normalize(pushConstants.frame.viewPos - i.fragPos);

  // sRGB texture linearised, tinted by matDiffuse
  float3 texColor = TEX_DIFFUSE.Sample(flippedTex).rgb;
  s.albedo = pow(texColor, float3(2.2)) * pushConstants.draw.matDiffuse;
  s.metallic = pushConstants.draw.matMetallic;
  // The map is the white pixel when there is none, so the scalar stands alone
  float roughness = pushConstants.draw.matRoughness * TEX_ROUGHNESS.Sample(flippedTex).r;
  s.roughness = clamp(roughness, 0.04, 1.0);
  s.ao = pushConstants.draw.matAo;
  return s;
}
