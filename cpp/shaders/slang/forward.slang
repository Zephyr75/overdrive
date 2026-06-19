#include "common.slang"

struct VSOut {
    float4 position : SV_Position;
    float2 texCoord : TEXCOORD0;
    float3 normal : NORMAL;
    float3 fragPos : TEXCOORD1;
    float4 fragPosLightSpace : TEXCOORD2;
};

[shader("vertex")]
VSOut vsMain(float3 aPos, float3 aNormal, float2 aTexCoord) {
    VSOut o;
    float3 fragPos = mul(U.model, float4(aPos, 1.0)).xyz;
    o.fragPos = fragPos;
    o.texCoord = aTexCoord;
    o.normal = mul(transpose(inverse3((float3x3)U.model)), aNormal);
    o.fragPosLightSpace = mul(U.lightSpaceMatrix, float4(fragPos, 1.0));
    o.position = mul(U.projection, mul(U.view, float4(fragPos, 1.0)));
    TO_VK_DEPTH(o.position);
    return o;
}

static const float3 gridSamplingDisk[20] = {
    float3(1, 1, 1), float3(1, -1, 1), float3(-1, -1, 1), float3(-1, 1, 1),
    float3(1, 1, -1), float3(1, -1, -1), float3(-1, -1, -1), float3(-1, 1, -1),
    float3(1, 1, 0), float3(1, -1, 0), float3(-1, -1, 0), float3(-1, 1, 0),
    float3(1, 0, 1), float3(-1, 0, 1), float3(1, 0, -1), float3(-1, 0, -1),
    float3(0, 1, 1), float3(0, -1, 1), float3(0, -1, -1), float3(0, 1, -1)
};

float shadowCalculationCube(float3 fragPos, float3 lightPos) {
    float3 fragToLight = fragPos - lightPos;
    float currentDepth = length(fragToLight);
    float shadow = 0.0;
    float bias = 0.15;
    int samples = 20;
    float viewDistance = length(U.viewPos - fragPos);
    float diskRadius = (1.0 + (viewDistance / U.farPlane)) / 25.0;
    for (int i = 0; i < samples; ++i) {
        float closestDepth =
            TEX_SHADOWCUBE.Sample(fragToLight + gridSamplingDisk[i] * diskRadius).r;
        closestDepth *= U.farPlane;
        if (currentDepth - bias > closestDepth)
            shadow += 1.0;
    }
    shadow /= float(samples);
    return shadow * 5.0;
}

float shadowCalculation(float4 fragPosLightSpace, float3 normal, float3 lightDir) {
    float3 projCoords = fragPosLightSpace.xyz / fragPosLightSpace.w;
    projCoords = projCoords * 0.5 + 0.5;
    float currentDepth = projCoords.z;
    float bias = max(0.05 * (1.0 - dot(normal, lightDir)), 0.005);
    float shadow = 0.0;
    uint w, h;
    TEX_SHADOWMAP.GetDimensions(w, h);
    float2 texelSize = 1.0 / float2(w, h);
    for (int x = -1; x <= 1; ++x) {
        for (int y = -1; y <= 1; ++y) {
            float pcfDepth =
                TEX_SHADOWMAP.Sample(projCoords.xy + float2(x, y) * texelSize).r;
            shadow += currentDepth - bias > pcfDepth ? 1.0 : 0.0;
        }
    }
    shadow /= 9.0;
    if (projCoords.z > 1.0)
        shadow = 0.0;
    return shadow;
}

// ---- Cook-Torrance metallic-roughness PBR -----------------------------------
//
// Replaces the old Blinn-Phong terms with a microfacet BRDF: GGX/Trowbridge-
// Reitz normal distribution, Smith height-correlated geometry (Schlick-GGX
// with the direct-lighting k), and a Fresnel-Schlick term. matDiffuse is the
// base colour (albedo); metallic blends the F0 reflectance from the 0.04
// dielectric default toward the albedo, and kills the diffuse lobe.
static const float PI = 3.14159265358979;

// Material params hoisted out of the per-light loop. On Vulkan U is a
// buffer_reference pointer, so reads inside the loop cannot be proven
// loop-invariant and would re-fetch every iteration; locals stay in registers.
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

// Roughness-aware Fresnel for the environment ambient term (Sébastien Lagarde).
float3 fresnelSchlickRoughness(float cosTheta, float3 F0, float roughness) {
    float3 Fr = max(float3(1.0 - roughness), F0);
    return F0 + (Fr - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// Outgoing radiance from one light, given its incoming radiance at the surface.
float3 cookTorrance(MatParams mat, float3 N, float3 V, float3 L, float3 radiance) {
    float3 H = normalize(V + L);
    float NdotL = max(dot(N, L), 0.0);
    float NDF = distributionGGX(N, H, mat.roughness);
    float G = geometrySmith(N, V, L, mat.roughness);
    float3 F = fresnelSchlick(max(dot(H, V), 0.0), mat.F0);
    float3 specular = (NDF * G * F) /
                      max(4.0 * max(dot(N, V), 0.0) * NdotL, 1e-4);
    // Energy conservation: what is not reflected (kS = F) is refracted/diffused,
    // and metals have no diffuse lobe.
    float3 kD = (float3(1.0) - F) * (1.0 - mat.metallic);
    return (kD * mat.albedo / PI + specular) * radiance * NdotL;
}

float3 calcDirLight(LightData light, MatParams mat, float3 normal, float3 viewDir) {
    float3 lightDir = normalize(light.direction);
    float3 radiance = light.color * light.diffuse * light.intensity;
    return cookTorrance(mat, normal, viewDir, lightDir, radiance);
}

float3 calcPointLight(LightData light, MatParams mat, float3 normal, float3 fragPos, float3 viewDir) {
    float3 lightDir = normalize(light.position - fragPos);
    float distance = length(light.position - fragPos);
    // Inverse-square falloff (physical), kept off zero by kConstant.
    float attenuation = 1.0 / (light.kConstant + distance * distance);
    float3 radiance = light.color * light.diffuse * light.intensity * attenuation;
    return cookTorrance(mat, normal, viewDir, lightDir, radiance);
}

// Perturb the geometric normal by a tangent-space normal map without
// precomputed tangents: derive the TBN basis from screen-space derivatives of
// fragPos and uv (Schüler's cotangent frame). Avoids adding tangents to the
// vertex layout.
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
    if (U.useNormalMap != 0)
        norm = perturbNormal(norm, i.fragPos, flippedTex);
    float3 viewDir = normalize(U.viewPos - i.fragPos);

    // Albedo = sRGB base-colour texture (linearised) tinted by matDiffuse. The
    // default white texture makes untextured materials fall back to matDiffuse.
    float3 texColor = TEX_OUR.Sample(flippedTex).rgb;
    float3 albedo = pow(texColor, float3(2.2)) * U.matDiffuse;

    // Hoist loop-invariant material reads into locals so the per-light loop
    // touches registers instead of re-loading through U's buffer_reference
    // pointer (Vulkan) every iteration.
    MatParams mat;
    mat.albedo = albedo;
    mat.metallic = U.matMetallic;
    mat.roughness = clamp(U.matRoughness, 0.04, 1.0);
    // Dielectrics reflect ~4%; metals tint their reflectance with the albedo.
    mat.F0 = lerp(float3(0.04), albedo, U.matMetallic);
    float ao = U.matAo;
    int lightCount = U.lightCount;
    int shadowDirIndex = U.shadowDirIndex;
    int shadowPointIndex = U.shadowPointIndex;

    float3 Lo = float3(0.0);
    for (int l = 0; l < lightCount; l++) {
        LightData light = U.lights[l];
        float shadow = 0.0;
        float3 contrib;
        if (light.type == 0) {
            contrib = calcDirLight(light, mat, norm, viewDir);
            if (l == shadowDirIndex)
                shadow = shadowCalculation(i.fragPosLightSpace, norm,
                                           normalize(light.direction));
        } else {
            contrib = calcPointLight(light, mat, norm, i.fragPos, viewDir);
            if (l == shadowPointIndex)
                shadow = shadowCalculationCube(i.fragPos, light.position);
        }
        Lo += contrib * (1.0 - shadow);
    }

    // Ambient image-based lighting. The skybox cubemap stands in for both the
    // irradiance (diffuse) and the prefiltered specular environment — a crude
    // probe (no prefiltered mips or BRDF LUT yet; see FEATURES roadmap). The
    // split-sum is approximated by a Fresnel-weighted mix so metals mirror the
    // sky and dielectrics pick up a soft tint.
    float3 F = fresnelSchlickRoughness(max(dot(norm, viewDir), 0.0), mat.F0,
                                       mat.roughness);
    float3 kD = (float3(1.0) - F) * (1.0 - mat.metallic);
    // Damp the diffuse irradiance: a single raw cubemap tap massively
    // over-estimates true hemispherical irradiance and washes out direct
    // lighting. Specular reflection (prefiltered) stays full so metals still
    // mirror the sky. (Proper prefiltered IBL would remove the fudge factor.)
    float3 irradiance = TEX_SKYBOX.Sample(norm).rgb * 0.35;
    float3 R = reflect(-viewDir, norm);
    float3 prefiltered = TEX_SKYBOX.Sample(R).rgb;
    float3 ambient = (kD * irradiance * albedo + prefiltered * F) * ao;

    // HDR -> LDR: Reinhard tonemap + gamma. A dedicated HDR/bloom pass is on the
    // roadmap; until then this keeps the unbounded PBR radiance displayable.
    float3 color = ambient + Lo;
    color = color / (color + float3(1.0));
    color = pow(color, float3(1.0 / 2.2));
    return float4(color, 1.0);
}
