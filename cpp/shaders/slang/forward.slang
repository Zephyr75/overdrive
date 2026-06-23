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
    float3 fragPos = mul(U.model, float4(aPos, 1.0)).xyz;
    o.fragPos = fragPos;
    o.texCoord = aTexCoord;
    o.normal = mul(transpose(inverse3((float3x3)U.model)), aNormal);
    // The light-space position used to be precomputed here, but the shadow
    // test now applies a normal-offset bias to the world position first, so it
    // re-projects in the fragment shader instead.
    o.position = mul(U.projection, mul(U.view, float4(fragPos, 1.0)));
    TO_VK_DEPTH(o.position);
    return o;
}

// Normal-offset shadow bias: rather than offsetting the stored/compared *depth*
// (which detaches the contact shadow = peter-panning), we push the receiver
// sample point along its surface normal, in world space, before projecting it
// into the shadow map. The offset grows toward grazing light angles where acne
// is worst. This keeps contact shadows attached while still escaping self-
// shadow acne, so the leftover constant depth bias can be tiny.
// Tuned for the showcase's ~10-unit scene scale; see FEATURES.md (Shadow bias).
static const float NORMAL_OFFSET_2D = 0.08;   // ortho 2D map, world units
static const float NORMAL_OFFSET_CUBE = 0.10; // point cube map, world units

static const float3 gridSamplingDisk[20] = {
    float3(1, 1, 1), float3(1, -1, 1), float3(-1, -1, 1), float3(-1, 1, 1),
    float3(1, 1, -1), float3(1, -1, -1), float3(-1, -1, -1), float3(-1, 1, -1),
    float3(1, 1, 0), float3(1, -1, 0), float3(-1, -1, 0), float3(-1, 1, 0),
    float3(1, 0, 1), float3(-1, 0, 1), float3(1, 0, -1), float3(-1, 0, -1),
    float3(0, 1, 1), float3(0, -1, 1), float3(0, -1, -1), float3(0, 1, -1)
};

float shadowCalculationCube(float3 fragPos, float3 normal, float3 lightPos,
                            int slot) {
    // Normal-offset bias: nudge the sample off the surface along its normal so
    // self-shadow acne is escaped geometrically, letting the depth bias shrink.
    float3 offsetPos = fragPos + normal * NORMAL_OFFSET_CUBE;
    float3 fragToLight = offsetPos - lightPos;
    float currentDepth = length(fragToLight);
    float shadow = 0.0;
    float bias = 0.04;
    int samples = 20;
    // Hoist the bindless cubemap and the loop-invariant uniforms into locals.
    // On Vulkan TEX_SHADOWCUBE expands to a dynamically-indexed descriptor
    // (texturesCube[U.texShadowCubeMap]); resolving it once instead of inside
    // the 20-tap loop stops Intel's driver re-fetching the descriptor (and
    // re-reading the BDA index) on every tap — the main GL/Vulkan perf gap.
    SamplerCube shadowCube = shadowCubeMap[slot];
    float farPlane = U.farPlane;
    float viewDistance = length(U.viewPos - fragPos);
    float diskRadius = (1.0 + (viewDistance / farPlane)) / 25.0;

    // Early-bail PCF: 4 spread taps first. If they unanimously agree (fully lit
    // or fully shadowed — true for almost every fragment outside a penumbra),
    // skip the full 20-tap kernel. Only soft edges pay the full price. This is
    // the main shadow-sampling cost saver; it disproportionately helps the
    // Vulkan/Intel path, where these taps dominated the frame.
    float early = 0.0;
    // No [unroll]: slangc would emit GL_EXT_control_flow_attributes, which the
    // GL 4.10 fragment profile (Mesa) rejects. The early-bail itself is the win.
    for (int j = 0; j < 4; ++j) {
        float cd = shadowCube.Sample(fragToLight + gridSamplingDisk[j * 5] *
                                                       diskRadius).r * farPlane;
        if (currentDepth - bias > cd)
            early += 1.0;
    }
    if (early == 0.0)
        return 0.0; // unanimously lit
    if (early == 4.0)
        return 5.0; // unanimously shadowed (shadow = 1.0, scaled by 5)

    // Penumbra: full kernel for a smooth edge.
    for (int i = 0; i < samples; ++i) {
        float closestDepth =
            shadowCube.Sample(fragToLight + gridSamplingDisk[i] * diskRadius).r;
        closestDepth *= farPlane;
        if (currentDepth - bias > closestDepth)
            shadow += 1.0;
    }
    shadow /= float(samples);
    return shadow * 5.0;
}

float shadowCalculation(float3 fragPos, float3 normal, float3 lightDir) {
    // Normal-offset bias: offset the world-space sample along its normal, more
    // at grazing angles, then re-project into light space. Replaces the old
    // slope-scaled *depth* bias that caused peter-panning at contact points.
    float ndl = clamp(dot(normal, lightDir), 0.0, 1.0);
    float offset = NORMAL_OFFSET_2D * (1.0 - ndl);
    float4 fragPosLightSpace =
        mul(U.lightSpaceMatrix, float4(fragPos + normal * offset, 1.0));
    float3 projCoords = fragPosLightSpace.xyz / fragPosLightSpace.w;
    projCoords = projCoords * 0.5 + 0.5;
    float currentDepth = projCoords.z;
    // Small residual depth bias; the normal offset does the heavy lifting.
    float bias = 0.0015;
    float shadow = 0.0;
    // Resolve the bindless 2D shadow map once (see shadowCalculationCube).
    Sampler2D shadowMap = TEX_SHADOWMAP;
    uint w, h;
    shadowMap.GetDimensions(w, h);
    float2 texelSize = 1.0 / float2(w, h);

    // Early-bail: test the 4 corners; skip the 3x3 kernel if they agree (same
    // idea as the cube path).
    float2 corners[4] = {float2(-1, -1), float2(1, -1), float2(-1, 1),
                         float2(1, 1)};
    float early = 0.0;
    // No [unroll] (see shadowCalculationCube): avoids GL_EXT_control_flow_attributes.
    for (int c = 0; c < 4; ++c) {
        float pcfDepth =
            shadowMap.Sample(projCoords.xy + corners[c] * texelSize).r;
        early += currentDepth - bias > pcfDepth ? 1.0 : 0.0;
    }
    if (early == 0.0)
        return 0.0; // fully lit
    if (early == 4.0)
        return projCoords.z > 1.0 ? 0.0 : 1.0; // fully shadowed (unless past far)

    for (int x = -1; x <= 1; ++x) {
        for (int y = -1; y <= 1; ++y) {
            float pcfDepth =
                shadowMap.Sample(projCoords.xy + float2(x, y) * texelSize).r;
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

    float3 Lo = float3(0.0);
    for (int l = 0; l < lightCount; l++) {
        LightData light = U.lights[l];
        float shadow = 0.0;
        float3 contrib;
        if (light.type == 0) {
            contrib = calcDirLight(light, mat, norm, viewDir);
            if (l == shadowDirIndex)
                shadow = shadowCalculation(i.fragPos, norm,
                                           normalize(light.direction));
        } else {
            contrib = calcPointLight(light, mat, norm, i.fragPos, viewDir);
            // Does this point light own a cube shadow slot? Up to
            // MAX_SHADOW_CUBES casters; the rest light unshadowed. (No [unroll]:
            // it makes slangc emit GL_EXT_control_flow_attributes, which the
            // GL 4.10 fragment profile rejects.)
            for (int s = 0; s < MAX_SHADOW_CUBES; s++)
                if (U.pointShadowLights[s] == l)
                    shadow = shadowCalculationCube(i.fragPos, norm,
                                                   light.position, s);
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
