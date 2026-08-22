#include "common.slang"

struct VSOut {
  float4 position : SV_Position;
  float2 texCoord : TEXCOORD0;
  float3 normal : NORMAL;
  float3 fragPos : TEXCOORD1;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float3 aNormal, float2 aTexCoord) {
  VSOut o;
  float3 fragPos = mul(DRAW.model, float4(aPos, 1.0)).xyz;
  o.fragPos = fragPos;
  o.texCoord = aTexCoord;
  o.normal = mul(transpose(inverse3((float3x3)DRAW.model)), aNormal);
  // The light-space position is not precomputed here any more
  // The shadow test offsets the world position along the normal first
  // So it re-projects in the fragment shader instead
  o.position = mul(FRAME.projection, mul(FRAME.view, float4(fragPos, 1.0)));
  TO_VK_DEPTH(o.position);
  return o;
}

// Normal-offset shadow bias
// Offsetting the compared depth would detach contact shadows, aka peter-panning
// Instead push the receiver sample along its world-space normal before
// projecting The offset grows toward grazing light angles where acne is worst
// Contact shadows stay attached and the leftover constant depth bias can be
// tiny Tuned for the showcase's ~10-unit scene scale, see FEATURES.md
static const float NORMAL_OFFSET_2D = 0.08; // sun and spot tiles, world units
static const float NORMAL_OFFSET_CUBE =
    0.10; // point-light face tiles, world units

// Samples one texel of a tile, in tile-local uv
//
// The clamp is what a cubemap gave free and an atlas does not: neighbouring
// tiles are adjacent texels of one image, so an unclamped kernel reads another
// light's depth and invents a shadow from nowhere. Inset by one texel because
// the kernel is centred, and the bilinear footprint straddles the edge texel
float tileSample(Sampler2D atlas, ShadowRecord rec, float2 local) {
  float2 inset = clamp(local, rec.texelSize, 1.0 - rec.texelSize);
  return atlas.Sample(rec.atlasRect.xy + inset * rec.atlasRect.zw).r;
}

// PCF over one tile, comparing in whatever units the tile stores
//
// Early-bail as before: 4 corner taps, and the 3x3 kernel only where they
// disagree. Almost every fragment outside a penumbra agrees, so only soft edges
// pay the full kernel. This is the main shadow-sampling saver
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
    return 0.0; // unanimously lit
  if (early == 4.0)
    return 1.0; // unanimously shadowed
  // Cheap PCF stops here: the corners already straddle the edge, so their mean
  // is a penumbra quantised to quarters rather than ninths
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

// Which cube face a direction falls on, in the order the tiles were baked:
// +X, -X, +Y, -Y, +Z, -Z. Must match scene/shadowatlas.go's matrix order
int cubeFace(float3 d) {
  float3 a = abs(d);
  if (a.x >= a.y && a.x >= a.z)
    return d.x > 0.0 ? 0 : 1;
  if (a.y >= a.z)
    return d.y > 0.0 ? 2 : 3;
  return d.z > 0.0 ? 4 : 5;
}

// One shadow tile lookup, for every light type
//
// The record carries the projection, the rect and the depth encoding, so the
// only fork left is which of the two encodings the comparison is in: a cube
// face stores radial distance to the light divided by farPlane, a sun or spot
// tile stores ordinary projected depth
float shadowLookup(int recordIndex, float3 fragPos, float3 normal,
                   float3 lightDir, float3 lightPos) {
  ShadowRecord rec = RECORD(recordIndex);

  // Nudge the receiver along its normal before projecting, rather than biasing
  // the compared depth: contact shadows stay attached instead of peter-panning
  float offset = NORMAL_OFFSET_CUBE;
  if (rec.face < 0) {
    // Grows toward grazing angles, where acne is worst
    float ndl = clamp(dot(normal, lightDir), 0.0, 1.0);
    offset = NORMAL_OFFSET_2D * (1.0 - ndl);
  }
  // A smaller atlas means a smaller tile means a wider world footprint per
  // texel, which is the acne these constants were tuned against. 1.0 at 4096
  offset *= FRAME.shadowNormalScale;
  float3 offsetPos = fragPos + normal * offset;

  float4 clip = mul(rec.lightSpace, float4(offsetPos, 1.0));
  if (clip.w <= 0.0)
    return 0.0; // behind the tile's frustum
  float3 ndc = clip.xyz / clip.w;
  float2 local = ndc.xy * 0.5 + 0.5;
  float z = ndc.z * 0.5 + 0.5;
  // Outside this tile's frustum entirely: unshadowed. The clamp-to-border
  // sampler used to say this for the sun's map, which an atlas cannot do —
  // its neighbours are inside the same image
  if (local.x < 0.0 || local.x > 1.0 || local.y < 0.0 || local.y > 1.0 ||
      z > 1.0)
    return 0.0;

  float depth = z;
  float bias = 0.0015; // small residual, the normal offset does the work
  if (rec.face >= 0) {
    // Same comparison the cube path made in world units, normalised by
    // farPlane so both encodings compare in the tile's own [0,1]
    depth = length(offsetPos - lightPos) / rec.farPlane;
    bias = 0.04 / rec.farPlane;
  }

  // Every encoding returns an occlusion fraction in [0, 1]. The point-light
  // path used to multiply by 5 here, which drove the caller's (1 - shadow)
  // negative and made a shadowed fragment subtract the light other lights had
  // put there. Harmless while one point light cast; with the Part D allocator
  // handing tiles to every point light in the scene it is black blotches
  if ((rec.flags & 1) != 0)
    return pcfTile(shadowAtlasDynamic, rec, local, depth, bias);
  return pcfTile(shadowAtlasStatic, rec, local, depth, bias);
}

// ---- Cook-Torrance metallic-roughness PBR -----------------------------------
//
// A microfacet BRDF, three terms
//   GGX/Trowbridge-Reitz normal distribution
//   Smith height-correlated geometry, Schlick-GGX with the direct-lighting k
//   Fresnel-Schlick reflectance
// matDiffuse is the base colour, aka albedo
// metallic blends F0 from the 0.04 dielectric default toward the albedo
// metallic also kills the diffuse lobe
static const float PI = 3.14159265358979;

// Material params hoisted out of the per-light loop
// U is a buffer_reference pointer on Vulkan, so loop reads are never invariant
// They would re-fetch every iteration, locals stay in registers instead
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
  float k = (r * r) / 8.0; // direct-lighting remap
  return NdotV / (NdotV * (1.0 - k) + k);
}

float geometrySmith(float3 N, float3 V, float3 L, float roughness) {
  return geometrySchlickGGX(max(dot(N, V), 0.0), roughness) *
         geometrySchlickGGX(max(dot(N, L), 0.0), roughness);
}

float3 fresnelSchlick(float cosTheta, float3 F0) {
  return F0 + (1.0 - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// Roughness-aware Fresnel for the environment ambient term, from Sébastien
// Lagarde
float3 fresnelSchlickRoughness(float cosTheta, float3 F0, float roughness) {
  float3 Fr = max(float3(1.0 - roughness), F0);
  return F0 + (Fr - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// Outgoing radiance from one light, given its incoming radiance at the surface
float3 cookTorrance(MatParams mat, float3 N, float3 V, float3 L,
                    float3 radiance) {
  float3 H = normalize(V + L);
  float NdotL = max(dot(N, L), 0.0);
  float NDF = distributionGGX(N, H, mat.roughness);
  float G = geometrySmith(N, V, L, mat.roughness);
  float3 F = fresnelSchlick(max(dot(H, V), 0.0), mat.F0);
  float3 specular =
      (NDF * G * F) / max(4.0 * max(dot(N, V), 0.0) * NdotL, 1e-4);
  // Energy conservation, what is not reflected as kS = F gets diffused
  // Metals have no diffuse lobe at all
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
  // Physical inverse-square falloff, kept off zero by kConstant
  float attenuation = 1.0 / (light.kConstant + distance * distance);
  float3 radiance = light.color * light.diffuse * light.intensity * attenuation;
  return cookTorrance(mat, normal, viewDir, lightDir, radiance);
}

// A point light cut by a cone: same falloff, scaled by the angular term
//
// direction points toward the light, as calcDirLight uses it, so the cone axis
// is compared against lightDir rather than its negation. cutoff is the inner
// cosine and outerCutoff the outer one, so smoothstep runs 0 outside the cone
// to 1 inside it and the rim stays soft instead of aliasing
float3 calcSpotLight(LightData light, MatParams mat, float3 normal,
                     float3 fragPos, float3 viewDir) {
  float3 lightDir = normalize(light.position - fragPos);
  float theta = dot(lightDir, normalize(light.direction));
  float cone = smoothstep(light.outerCutoff, light.cutoff, theta);
  if (cone <= 0.0)
    return float3(0.0);
  return calcPointLight(light, mat, normal, fragPos, viewDir) * cone;
}

// Perturb the geometric normal with a tangent-space normal map
// No precomputed tangents, the TBN basis comes from derivatives of fragPos and
// uv Schüler's cotangent frame, so tangents stay out of the vertex layout
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
  if (DRAW.useNormalMap != 0)
    norm = perturbNormal(norm, i.fragPos, flippedTex);
  float3 viewDir = normalize(FRAME.viewPos - i.fragPos);

  // Albedo is the sRGB base-colour texture, linearised, tinted by matDiffuse
  // The default white texture makes untextured materials fall back to
  // matDiffuse
  float3 texColor = TEX_OUR.Sample(flippedTex).rgb;
  float3 albedo = pow(texColor, float3(2.2)) * DRAW.matDiffuse;

  // Hoist loop-invariant material reads into locals
  // Otherwise the per-light loop re-loads through U's pointer every iteration
  MatParams mat;
  mat.albedo = albedo;
  mat.metallic = DRAW.matMetallic;
  mat.roughness = clamp(DRAW.matRoughness, 0.04, 1.0);
  // Dielectrics reflect ~4%, metals tint their reflectance with the albedo
  mat.F0 = lerp(float3(0.04), albedo, DRAW.matMetallic);
  float ao = DRAW.matAo;
  int lightCount = FRAME.lightCount;

  float3 Lo = float3(0.0);
  for (int l = 0; l < lightCount; l++) {
    LightData light = FRAME.lights[l];
    // Attenuation early-out, before the BRDF and before any shadow tap
    // radius is where the falloff drops below scene.lightCutoff, so past it
    // the light contributes nothing measurable. Most fragments touch a few
    // lights, not all of them, which is what makes a plain loop over 64
    // lights affordable. A sun does not attenuate and has radius 0, so it
    // must skip the test rather than cull itself
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
    // One lookup whatever the light type: shadowIndex is -1 when the
    // allocator gave this light no tile, and a point light owns six
    // consecutive records, one per face
    if (light.shadowIndex >= 0) {
      int rec = light.shadowIndex;
      if (light.shadowCount == 6)
        rec += cubeFace(i.fragPos - light.position);
      // Only the 2D encoding reads this, to scale the normal offset by
      // the incidence angle
      float3 sdir = light.type == LIGHT_SUN
                        ? normalize(light.direction)
                        : normalize(light.position - i.fragPos);
      shadow = shadowLookup(rec, i.fragPos, norm, sdir, light.position);
    }
    Lo += contrib * (1.0 - shadow);
  }

  // Ambient image-based lighting
  // The skybox cubemap stands in for both irradiance and prefiltered specular
  // A crude probe, no prefiltered mips or BRDF LUT yet, see FEATURES.md
  // A Fresnel-weighted mix approximates the split-sum
  // Metals mirror the sky and dielectrics pick up a soft tint
  float3 F = fresnelSchlickRoughness(max(dot(norm, viewDir), 0.0), mat.F0,
                                     mat.roughness);
  float3 kD = (float3(1.0) - F) * (1.0 - mat.metallic);
  // Damp the diffuse irradiance
  // One raw cubemap tap over-estimates hemispherical irradiance badly
  // It washes out the direct lighting, so scale it down
  // Specular stays full so metals still mirror the sky
  // Proper prefiltered IBL would remove this fudge factor
  float3 irradiance = TEX_SKYBOX.Sample(norm).rgb * 0.35;
  float3 R = reflect(-viewDir, norm);
  float3 prefiltered = TEX_SKYBOX.Sample(R).rgb;
  float3 ambient = (kD * irradiance * albedo + prefiltered * F) * ao;

  // HDR to LDR, Reinhard tonemap then gamma
  // A dedicated HDR/bloom pass is on the roadmap
  // Until then this keeps the unbounded PBR radiance displayable
  float3 color = ambient + Lo;
  color = color / (color + float3(1.0));
  color = pow(color, float3(1.0 / 2.2));
  return float4(color, 1.0);
}
