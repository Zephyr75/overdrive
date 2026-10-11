#pragma once
#include "lighting.slang"

// Cook-Torrance metallic-roughness: the shading style forward.slang uses

// Dielectrics reflect ~4%, metals reflect their albedo
float3 baseReflectance(Surface s) {
  return lerp(float3(0.04), s.albedo, s.metallic);
}

float distributionGGX(float3 N, float3 H, float roughness) {
  float a = roughness * roughness;
  float a2 = a * a;
  float NdotH = max(dot(N, H), 0.0);
  float d = NdotH * NdotH * (a2 - 1.0) + 1.0;
  return a2 / max(PI * d * d, 1e-7);
}

float geometrySchlickGGX(float NdotV, float roughness) {
  float r = roughness + 1.0;
  float k = (r * r) / 8.0;
  return NdotV / (NdotV * (1.0 - k) + k);
}

float geometrySmith(float3 N, float3 V, float3 L, float roughness) {
  return geometrySchlickGGX(max(dot(N, V), 0.0), roughness) *
         geometrySchlickGGX(max(dot(N, L), 0.0), roughness);
}

float3 fresnelSchlick(float cosTheta, float3 F0) {
  return F0 + (1.0 - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// Karis's fit of the split-sum BRDF integral, scale and bias on F0, in place of a LUT
float2 envBRDFApprox(float roughness, float NdotV) {
  const float4 c0 = float4(-1.0, -0.0275, -0.572, 0.022);
  const float4 c1 = float4(1.0, 0.0425, 1.04, -0.04);
  float4 r = roughness * c0 + c1;
  float a004 = min(r.x * r.x, exp2(-9.28 * NdotV)) * r.x + r.y;
  return float2(-1.04, 1.04) * a004 + r.zw;
}

// Fresnel for the ambient term, which has no single half vector
float3 fresnelSchlickRoughness(float cosTheta, float3 F0, float roughness) {
  float3 Fr = max(float3(1.0 - roughness), F0);
  return F0 + (Fr - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// Outgoing radiance from one light
float3 cookTorrance(Surface s, LightSample light) {
  float3 N = s.normal, V = s.viewDir, L = light.L;
  float3 H = normalize(V + L);
  float NdotL = max(dot(N, L), 0.0);
  float NDF = distributionGGX(N, H, s.roughness);
  float G = geometrySmith(N, V, L, s.roughness);
  float3 F = fresnelSchlick(max(dot(H, V), 0.0), baseReflectance(s));
  float3 specular =
      (NDF * G * F) / max(4.0 * max(dot(N, V), 0.0) * NdotL, 1e-4);
  // What is not reflected is diffused; metals diffuse nothing
  float3 kD = (float3(1.0) - F) * (1.0 - s.metallic);
  return (kD * s.albedo / PI + specular) * light.radiance * NdotL;
}

// Image-based light, split-sum: the specular map was prefiltered per roughness
// by cmgen at export, the BRDF integral is fitted analytically. Unoccluded:
// the caller multiplies by ambientOcclusion
float3 pbrAmbient(Surface s) {
  float3 F0 = baseReflectance(s);
  float NdotV = max(dot(s.normal, s.viewDir), 0.0);
  float3 F = fresnelSchlickRoughness(NdotV, F0, s.roughness);
  float3 kD = (float3(1.0) - F) * (1.0 - s.metallic);
  float3 irradiance = envSample(TEX_IRRADIANCE, s.normal, 0.0);
  float3 R = reflect(-s.viewDir, s.normal);
  // cmgen spaces its levels by roughness * (2 - roughness), not linearly
  float3 prefiltered = envSample(
      TEX_SPECULAR, R, s.roughness * (2.0 - s.roughness) * (SPECULAR_LEVELS - 1));
  float2 brdf = envBRDFApprox(s.roughness, NdotV);
  return kD * irradiance * s.albedo + prefiltered * (F0 * brdf.x + brdf.y);
}
