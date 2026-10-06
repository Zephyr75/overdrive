#include "common.slang"

struct VSOut {
  float4 position : SV_Position;
  float2 texCoord : TEXCOORD0;
  float3 normal : NORMAL;
  float3 fragPos : TEXCOORD1;
};

// prepass.slang must compute position with exactly these operations
[shader("vertex")]
VSOut vsMain(float3 aPos, float3 aNormal, float2 aTexCoord) {
  VSOut o;
  float3 fragPos = mul(pushConstants.draw.model, float4(aPos, 1.0)).xyz;
  o.fragPos = fragPos;
  o.texCoord = aTexCoord;
  o.normal = mul(transpose(inverse3((float3x3)pushConstants.draw.model)), aNormal);
  o.position = mul(pushConstants.frame.projection, mul(pushConstants.frame.view, float4(fragPos, 1.0)));
  TO_VK_DEPTH(o.position);
  return o;
}

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

// ---- Cook-Torrance metallic-roughness PBR -----------------------------------
static const float PI = 3.14159265358979;

// Copied out of pushConstants.draw once, so the light loop reads registers rather than memory
struct MatParams {
  float3 albedo;
  float metallic;
  float roughness;
  float3 F0;
};

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

// Fresnel for the ambient term, which has no single half vector
float3 fresnelSchlickRoughness(float cosTheta, float3 F0, float roughness) {
  float3 Fr = max(float3(1.0 - roughness), F0);
  return F0 + (Fr - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// Outgoing radiance from one light
float3 cookTorrance(MatParams mat, float3 N, float3 V, float3 L,
                    float3 radiance) {
  float3 H = normalize(V + L);
  float NdotL = max(dot(N, L), 0.0);
  float NDF = distributionGGX(N, H, mat.roughness);
  float G = geometrySmith(N, V, L, mat.roughness);
  float3 F = fresnelSchlick(max(dot(H, V), 0.0), mat.F0);
  float3 specular =
      (NDF * G * F) / max(4.0 * max(dot(N, V), 0.0) * NdotL, 1e-4);
  // What is not reflected is diffused; metals diffuse nothing
  float3 kD = (float3(1.0) - F) * (1.0 - mat.metallic);
  return (kD * mat.albedo / PI + specular) * radiance * NdotL;
}

float3 calcDirLight(LightData light, MatParams mat, float3 normal,
                    float3 viewDir) {
  float3 lightDir = normalize(light.direction);
  float3 radiance = light.color * light.diffuse * light.intensity;
  return cookTorrance(mat, normal, viewDir, lightDir, radiance);
}

float3 calcPointLight(LightData light, MatParams mat, float3 normal,
                      float3 fragPos, float3 viewDir) {
  float3 lightDir = normalize(light.position - fragPos);
  float distance = length(light.position - fragPos);
  float attenuation = 1.0 / (light.kConstant + distance * distance);
  float3 radiance = light.color * light.diffuse * light.intensity * attenuation;
  return cookTorrance(mat, normal, viewDir, lightDir, radiance);
}

// A point light inside a cone; direction points toward the light, like a sun's
float3 calcSpotLight(LightData light, MatParams mat, float3 normal,
                     float3 fragPos, float3 viewDir) {
  float3 lightDir = normalize(light.position - fragPos);
  float theta = dot(lightDir, normalize(light.direction));
  float cone = smoothstep(light.outerCutoff, light.cutoff, theta);
  if (cone <= 0.0)
    return float3(0.0);
  return calcPointLight(light, mat, normal, fragPos, viewDir) * cone;
}

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

[shader("fragment")]
float4 fsMain(VSOut i) : SV_Target {
  float2 flippedTex = float2(i.texCoord.x, 1.0 - i.texCoord.y);
  float3 norm = normalize(i.normal);
  if (pushConstants.draw.useNormalMap != 0)
    norm = perturbNormal(norm, i.fragPos, flippedTex);
  float3 viewDir = normalize(pushConstants.frame.viewPos - i.fragPos);

  // sRGB texture linearised, tinted by matDiffuse
  float3 texColor = TEX_DIFFUSE.Sample(flippedTex).rgb;
  float3 albedo = pow(texColor, float3(2.2)) * pushConstants.draw.matDiffuse;

  MatParams mat;
  mat.albedo = albedo;
  mat.metallic = pushConstants.draw.matMetallic;
  mat.roughness = clamp(pushConstants.draw.matRoughness, 0.04, 1.0);
  // Dielectrics reflect ~4%, metals reflect their albedo
  mat.F0 = lerp(float3(0.04), albedo, pushConstants.draw.matMetallic);
  float ao = pushConstants.draw.matAo;
  int lightCount = pushConstants.frame.lightCount;

  float3 Lo = float3(0.0);
  for (int l = 0; l < lightCount; l++) {
    LightData light = pushConstants.frame.lights[l];
    // Skip lights out of range before any work; a sun has radius 0 and never skips
    if (light.type != LIGHT_SUN) {
      float3 d = light.position - i.fragPos;
      if (dot(d, d) > light.radius * light.radius)
        continue;
    }
    float shadow = 0.0;
    float3 contrib;
    if (light.type == LIGHT_SUN) {
      contrib = calcDirLight(light, mat, norm, viewDir);
    } else if (light.type == LIGHT_SPOT) {
      contrib = calcSpotLight(light, mat, norm, i.fragPos, viewDir);
    } else {
      contrib = calcPointLight(light, mat, norm, i.fragPos, viewDir);
    }
    if (light.shadowIndex >= 0) {
      int rec = light.shadowIndex;
      if (light.shadowCount == 6)
        rec += cubeFace(i.fragPos - light.position);
      float3 sdir = light.type == LIGHT_SUN
                        ? normalize(light.direction)
                        : normalize(light.position - i.fragPos);
      shadow = shadowLookup(rec, i.fragPos, norm, sdir, light.position);
    }
    Lo += contrib * (1.0 - shadow);
  }

  // Ambient from the skybox, a crude stand-in for prefiltered IBL
  float3 F = fresnelSchlickRoughness(max(dot(norm, viewDir), 0.0), mat.F0,
                                     mat.roughness);
  float3 kD = (float3(1.0) - F) * (1.0 - mat.metallic);
  // One raw cubemap tap overestimates irradiance, so it is damped
  float3 irradiance = TEX_SKYBOX.Sample(norm).rgb * 0.35;
  float3 R = reflect(-viewDir, norm);
  float3 prefiltered = TEX_SKYBOX.Sample(R).rgb;
  float3 ambient = (kD * irradiance * albedo + prefiltered * F) * ao;

  // Reinhard tonemap, then gamma
  float3 color = ambient + Lo;
  color = color / (color + float3(1.0));
  color = pow(color, float3(1.0 / 2.2));
  return float4(color, 1.0);
}
