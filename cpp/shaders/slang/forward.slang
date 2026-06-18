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

float shadowCalculationCube(float3 fragPos) {
    float3 fragToLight = fragPos - U.lights[0].position;
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

float shadowCalculation(float4 fragPosLightSpace, float3 normal) {
    float3 projCoords = fragPosLightSpace.xyz / fragPosLightSpace.w;
    projCoords = projCoords * 0.5 + 0.5;
    float currentDepth = projCoords.z;
    float bias = max(0.05 * (1.0 - dot(normal, U.lights[1].direction)), 0.005);
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

float3 calcDirLight(LightData light, float3 normal, float3 viewDir) {
    float3 lightDir = normalize(light.direction);
    float diff = max(dot(normal, lightDir), 0.0);
    float3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), U.matShininess + 0.0001);
    float3 ambient = light.color * U.matAmbient;
    float3 diffuse = light.color * light.diffuse * diff * U.matDiffuse;
    float3 specular = light.color * light.specular * spec * U.matSpecular;
    return (ambient + diffuse + specular) * light.intensity / 5.0;
}

float3 calcPointLight(LightData light, float3 normal, float3 fragPos, float3 viewDir) {
    float3 lightDir = normalize(light.position - fragPos);
    float diff = max(dot(normal, lightDir), 0.0);
    float3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), U.matShininess + 0.0001);
    float distance = length(light.position - fragPos);
    float attenuation =
        1.0 / (light.kConstant + light.kLinear * distance +
               light.kQuadratic * (distance * distance));
    float3 ambient = light.color * U.matAmbient * attenuation;
    float3 diffuse = light.color * light.diffuse * diff * U.matDiffuse * attenuation;
    float3 specular = light.color * light.specular * spec * U.matSpecular * attenuation;
    return (ambient + diffuse + specular) * light.intensity / 5.0;
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
    float3 result = float3(0.2);

    for (int l = 0; l < 2; l++) {
        if (U.lights[l].type == 0)
            result += calcDirLight(U.lights[l], norm, viewDir) *
                      (1.0 - shadowCalculation(i.fragPosLightSpace, norm));
        else
            result += calcPointLight(U.lights[l], norm, i.fragPos, viewDir) *
                      (1.0 - shadowCalculationCube(i.fragPos));
    }

    float3 I = normalize(i.fragPos - U.viewPos);
    float3 R = reflect(I, norm);
    float4 skyboxColor = float4(TEX_SKYBOX.Sample(R).rgb, 1.0);
    skyboxColor *= float4(float3(1.0 - U.matDiffuse), 1.0);
    skyboxColor /= float4(float3(5.0), 1.0);

    return float4(result, 1.0) * TEX_OUR.Sample(flippedTex) + skyboxColor;
}
