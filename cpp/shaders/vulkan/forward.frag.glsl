#version 460
#include "common.glsl"

layout(location = 0) in vec2 TexCoord;
layout(location = 1) in vec3 Normal;
layout(location = 2) in vec3 FragPos;
layout(location = 3) in vec4 FragPosLightSpace;

layout(location = 0) out vec4 FragColor;

#define NR_LIGHTS 2

#define shadowMap textures2D[pc.ubo.texShadowMap]
#define ourTexture textures2D[pc.ubo.texOurTexture]
#define shadowCubeMap texturesCube[pc.ubo.texShadowCubeMap]
#define skybox texturesCube[pc.ubo.texSkybox]

vec3 CalcDirLight(LightData light, vec3 normal, vec3 viewDir);
vec3 CalcPointLight(LightData light, vec3 normal, vec3 fragPos, vec3 viewDir);
float ShadowCalculation(vec4 fragPosLightSpace);
float ShadowCalculationCube(vec3 fragPos);

void main()
{
    vec3 norm = normalize(Normal);
    vec3 viewDir = normalize(pc.ubo.viewPos - FragPos);
    vec3 result = vec3(0.2);

    for (int i = 0; i < NR_LIGHTS; i++) {
        switch (pc.ubo.lights[i].type) {
            case 0:
            result += CalcDirLight(pc.ubo.lights[i], norm, viewDir) * (1.0 - ShadowCalculation(FragPosLightSpace));
            break;
            case 1:
            result += CalcPointLight(pc.ubo.lights[i], norm, FragPos, viewDir) * (1.0 - ShadowCalculationCube(FragPos));
            break;
        }
    }

    vec2 flipped_tex = vec2(TexCoord.x, 1.0 - TexCoord.y);

    vec3 I = normalize(FragPos - pc.ubo.viewPos);
    vec3 R = reflect(I, norm);
    vec4 skyboxColor = vec4(texture(skybox, R).rgb, 1.0);
    skyboxColor *= vec4(vec3(1.0 - pc.ubo.matDiffuse), 1.0);
    skyboxColor /= vec4(vec3(5.0), 1.0);

    // DEBUG (no result contamination): red = lights[1].intensity (sun, expect 1.0),
    // green = lights[0].intensity/10 (point, expect 1.0), blue = lights[0].kConstant (expect 1.0)
    FragColor = vec4(result, 1.0) * texture(ourTexture, flipped_tex) + skyboxColor;
}

vec3 gridSamplingDisk[20] = vec3[](
        vec3(1, 1, 1), vec3(1, -1, 1), vec3(-1, -1, 1), vec3(-1, 1, 1),
        vec3(1, 1, -1), vec3(1, -1, -1), vec3(-1, -1, -1), vec3(-1, 1, -1),
        vec3(1, 1, 0), vec3(1, -1, 0), vec3(-1, -1, 0), vec3(-1, 1, 0),
        vec3(1, 0, 1), vec3(-1, 0, 1), vec3(1, 0, -1), vec3(-1, 0, -1),
        vec3(0, 1, 1), vec3(0, -1, 1), vec3(0, -1, -1), vec3(0, 1, -1)
    );

float ShadowCalculationCube(vec3 fragPos)
{
    vec3 fragToLight = fragPos - pc.ubo.lights[0].position;
    float currentDepth = length(fragToLight);
    float shadow = 0.0;
    float bias = 0.15;
    int samples = 20;
    float viewDistance = length(pc.ubo.viewPos - fragPos);
    float diskRadius = (1.0 + (viewDistance / pc.ubo.farPlane)) / 25.0;
    for (int i = 0; i < samples; ++i) {
        float closestDepth = texture(shadowCubeMap, fragToLight + gridSamplingDisk[i] * diskRadius).r;
        closestDepth *= pc.ubo.farPlane;
        if (currentDepth - bias > closestDepth)
            shadow += 1.0;
    }
    shadow /= float(samples);
    return shadow * 5.0;
}

float ShadowCalculation(vec4 fragPosLightSpace)
{
    vec3 projCoords = fragPosLightSpace.xyz / fragPosLightSpace.w;
    projCoords = projCoords * 0.5 + 0.5;
    float closestDepth = texture(shadowMap, projCoords.xy).r;
    float currentDepth = projCoords.z;
    float bias = max(0.05 * (1.0 - dot(Normal, pc.ubo.lights[1].direction)), 0.005);
    float shadow = 0.0;
    vec2 texelSize = 1.0 / textureSize(shadowMap, 0);
    for (int x = -1; x <= 1; ++x) {
        for (int y = -1; y <= 1; ++y) {
            float pcfDepth = texture(shadowMap, projCoords.xy + vec2(x, y) * texelSize).r;
            shadow += currentDepth - bias > pcfDepth ? 1.0 : 0.0;
        }
    }
    shadow /= 9.0;
    if (projCoords.z > 1.0)
        shadow = 0.0;
    return shadow;
}

vec3 CalcDirLight(LightData light, vec3 normal, vec3 viewDir)
{
    vec3 lightDir = normalize(light.direction);
    float diff = max(dot(normal, lightDir), 0.0);
    vec3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), pc.ubo.matShininess + 0.0001);
    vec3 ambient = light.color * pc.ubo.matAmbient;
    vec3 diffuse = light.color * light.diffuse * diff * pc.ubo.matDiffuse;
    vec3 specular = light.color * light.specular * spec * pc.ubo.matSpecular;
    return (ambient + diffuse + specular) * light.intensity / 5.0;
}

vec3 CalcPointLight(LightData light, vec3 normal, vec3 fragPos, vec3 viewDir)
{
    vec3 lightDir = normalize(light.position - fragPos);
    float diff = max(dot(normal, lightDir), 0.0);
    vec3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), pc.ubo.matShininess + 0.0001);
    float distance = length(light.position - fragPos);
    float attenuation = 1.0 / (light.kConstant + light.kLinear * distance + light.kQuadratic * (distance * distance));
    vec3 ambient = light.color * pc.ubo.matAmbient;
    vec3 diffuse = light.color * light.diffuse * diff * pc.ubo.matDiffuse;
    vec3 specular = light.color * light.specular * spec * pc.ubo.matSpecular;
    ambient *= attenuation;
    diffuse *= attenuation;
    specular *= attenuation;
    return (ambient + diffuse + specular) * light.intensity / 5.0;
}
