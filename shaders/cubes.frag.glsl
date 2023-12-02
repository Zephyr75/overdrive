#version 330 core
out vec4 FragColor;

struct Material {
    vec3 ambient;
    vec3 diffuse;
    vec3 specular;    
    float shininess;
}; 

struct Light {
    int type; // 0 = directional, 1 = point, 2 = spot
    vec3 position;
    vec3 direction;

    float intensity;
    vec3 color;
    float diffuse;
    float specular;

    float constant;
    float linear;
    float quadratic;

    float cutoff;
    float outerCutoff;
};

#define NR_LIGHTS 1  
uniform Light lights[NR_LIGHTS];
uniform Material material;
  
in vec3 FragPos; 
in vec2 TexCoord;
in vec3 Normal;
in vec4 FragPosLightSpace;

uniform samplerCube shadowCubeMap;
uniform sampler2D shadowMap;
uniform sampler2D ourTexture;
uniform vec3 viewPos;
uniform float far_plane;


// function prototypes
vec3 CalcDirLight(Light light, vec3 normal, vec3 viewDir);
vec3 CalcPointLight(Light light, vec3 normal, vec3 fragPos, vec3 viewDir);
vec3 CalcSpotLight(Light light, vec3 normal, vec3 fragPos, vec3 viewDir);

float ShadowCalculation(vec4 fragPosLightSpace);

float ShadowCalculationCube(vec3 fragPos);

void main()
{
  // float distance = length(light.position - FragPos);
  // float attenuation = 1.0 / (light.constant + light.linear * distance + light.quadratic * (distance * distance));

  // // ambient
  // vec3 ambient = light.color * material.ambient;
  // 
  // // diffuse 
  // vec3 norm = normalize(Normal);
  // vec3 lightDir = light.direction;
  // if (light.type == 1) {
  //   lightDir = normalize(light.position - FragPos);
  // }
  // float diff = max(dot(norm, lightDir), 0.0);
  // vec3 diffuse = light.color * light.diffuse * (diff * material.diffuse);

  // // specular
  // vec3 viewDir = normalize(viewPos - FragPos);
  // vec3 reflectDir = reflect(-lightDir, norm);  
  // float spec = pow(max(dot(viewDir, reflectDir), 0.0), material.shininess/1000 + 0.001);
  // vec3 specular = light.color * light.specular * (spec * material.specular);
  //         
  // vec2 flipped_tex = vec2(TexCoord.x, 1.0 - TexCoord.y);

  // ambient *= attenuation;
  // diffuse *= attenuation;
  // specular *= attenuation;

  // vec4 result = vec4(0);
  // 
  // float theta = dot(lightDir, normalize(-light.direction));
  // float epsilon = light.cutoff - light.cutoff * 1.1;
  // float intensity = clamp((theta - light.cutoff * 1.1) / epsilon, 0.0, 1.0);
  // diffuse *= intensity;
  // specular *= intensity;
  // if (theta > light.cutoff) {
  //   result = vec4(ambient/3 + diffuse + specular, 1.0) * texture(ourTexture, flipped_tex);
  // }
  // else {
  //   result = vec4(ambient/3, 1.0) * texture(ourTexture, flipped_tex);
  // }

  // FragColor = result;



  // properties
  vec3 norm = normalize(Normal);
  vec3 viewDir = normalize(viewPos - FragPos);

  vec3 result = vec3(0.2);
  // phase 1: Directional lighting
  // vec3 result = CalcDirLight(dirLight, norm, viewDir);
  // // phase 2: Point lights
  // for(int i = 0; i < NR_POINT_LIGHTS; i++)
  //     result += CalcPointLight(pointLights[i], norm, FragPos, viewDir);    
  // phase 3: Spot light
  //result += CalcSpotLight(spotLight, norm, FragPos, viewDir); 

  for(int i = 0; i < NR_LIGHTS; i++){
    switch (lights[i].type) {
      case 0:
        // result += CalcDirLight(lights[i], norm, viewDir) * (1.0 - ShadowCalculation(FragPosLightSpace));
        result += CalcDirLight(lights[i], norm, viewDir) * (1.0 - ShadowCalculationCube(FragPos));
        break;
      case 1:
        result += CalcPointLight(lights[i], norm, FragPos, viewDir) * (1.0 - ShadowCalculationCube(FragPos));

        // result += CalcDirLight(lights[i], norm, viewDir) * (1.0 - ShadowCalculationCube(FragPos));
        break;
      case 2:
        result += CalcSpotLight(lights[i], norm, FragPos, viewDir);
        break;
    }
    
  }

  vec2 flipped_tex = vec2(TexCoord.x, 1.0 - TexCoord.y);


  vec3 fragToLight = FragPos - lights[0].position;
  float closestDepth = texture(shadowCubeMap, fragToLight).r;
  // FragColor = vec4(vec3(5 * closestDepth / far_plane), 1.0); 
  FragColor = vec4(result, 1.0) * texture(ourTexture, flipped_tex);

  // FragColor = texture(ourTexture, TexCoord);
  // FragColor = vec4(result, 1.0);
  // FragColor = vec4(Normal, 1.0);
  // FragColor = texture(ourTexture, TexCoord); // * vec4(lightColor, 1.0);

}

float ShadowCalculationCube(vec3 fragPos)
{
    // get vector between fragment position and light position
    vec3 fragToLight = fragPos - lights[0].position;
    // use the light to fragment vector to sample from the depth map    
    float closestDepth = texture(shadowCubeMap, fragToLight).r;
    // it is currently in linear range between [0,1]. Re-transform back to original value
    closestDepth *= far_plane;
    // now get current linear depth as the length between the fragment and light position
    float currentDepth = length(fragToLight);
    // now test for shadows
    float bias = 0.05; 
    float shadow = currentDepth -  bias > closestDepth ? 1.0 : 0.0;

    return shadow;
}  

float ShadowCalculation(vec4 fragPosLightSpace)
{
    // perform perspective divide
    vec3 projCoords = fragPosLightSpace.xyz / fragPosLightSpace.w;
    // transform to [0,1] range
    projCoords = projCoords * 0.5 + 0.5;
    // get closest depth value from light's perspective (using [0,1] range fragPosLight as coords)
    float closestDepth = texture(shadowMap, projCoords.xy).r; 
    // get depth of current fragment from light's perspective
    float currentDepth = projCoords.z;
    // check whether current frag pos is in shadow
    float bias = max(0.05 * (1.0 - dot(Normal, lights[0].direction)), 0.005);  
    // float shadow = currentDepth - bias > closestDepth  ? 1.0 : 0.0;

    float shadow = 0.0;
    vec2 texelSize = 1.0 / textureSize(shadowMap, 0);
    for(int x = -1; x <= 1; ++x)
    {
        for(int y = -1; y <= 1; ++y)
        {
            float pcfDepth = texture(shadowMap, projCoords.xy + vec2(x, y) * texelSize).r; 
            shadow += currentDepth - bias > pcfDepth ? 1.0 : 0.0;        
        }    
    }
    shadow /= 9.0;

    if (projCoords.z > 1.0) {
      shadow = 0.0;
    }

    return shadow;
}  


// calculates the color when using a directional light.
vec3 CalcDirLight(Light light, vec3 normal, vec3 viewDir)
{
    vec3 lightDir = normalize(light.direction);
    // vec3 lightDir = normalize(light.position - fragPos);
    // vec3 lightDir = normalize(light.position - vec3(0, 0, 0));
    
    // diffuse shading
    float diff = max(dot(normal, lightDir), 0.0);
    // specular shading
    vec3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), material.shininess + 0.0001); 
    // vec3 reflectDir = reflect(-lightDir, normal);
    // float spec = pow(max(dot(viewDir, reflectDir), 0.0), material.shininess + 0.0001);
    // combine results
    vec3 ambient  = light.color  * material.ambient;
    vec3 diffuse  = light.color * light.diffuse  * diff * material.diffuse;
    vec3 specular = light.color * light.specular * spec * material.specular;
    // return (ambient + diffuse + specular); // * light.intensity / 1000;
    return diffuse * light.intensity / 7;
}

// calculates the color when using a point light.
vec3 CalcPointLight(Light light, vec3 normal, vec3 fragPos, vec3 viewDir)
{
    vec3 lightDir = normalize(light.position - fragPos);
    // diffuse shading
    float diff = max(dot(normal, lightDir), 0.0);
    // specular shading
    vec3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), material.shininess + 0.0001); 
    // vec3 reflectDir = reflect(-lightDir, normal);
    // float spec = pow(max(dot(viewDir, reflectDir), 0.0), material.shininess + 0.0001);
    // attenuation
    float distance    = length(light.position - fragPos);
    float attenuation = 1.0 / (light.constant + light.linear * distance + 
  			     light.quadratic * (distance * distance));    
    // combine results
    vec3 ambient  = light.color * material.ambient;
    vec3 diffuse  = light.color * light.diffuse  * diff * material.diffuse;
    vec3 specular = light.color * light.specular * spec * material.specular;
    ambient  *= attenuation;
    diffuse  *= attenuation;
    specular *= attenuation;
    return (ambient + diffuse + specular) * light.intensity / 50;
} 

// calculates the color when using a spot light.
vec3 CalcSpotLight(Light light, vec3 normal, vec3 fragPos, vec3 viewDir)
{
    vec3 lightDir = normalize(light.position - fragPos);
    // diffuse shading
    float diff = max(dot(normal, lightDir), 0.0);
    // specular shading
    vec3 reflectDir = reflect(-lightDir, normal);
    float spec = pow(max(dot(viewDir, reflectDir), 0.0), material.shininess);
    // attenuation
    float distance = length(light.position - fragPos);
    float attenuation = 1.0 / (light.constant + light.linear * distance + light.quadratic * (distance * distance));    
    // spotlight intensity
    float theta = dot(lightDir, normalize(-light.direction)); 
    float epsilon = light.cutoff - light.outerCutoff;
    float intensity = clamp((theta - light.outerCutoff) / epsilon, 0.0, 1.0);
    // combine results
    vec3 ambient = light.color * material.ambient;
    vec3 diffuse = light.diffuse * diff * material.diffuse;
    vec3 specular = light.specular * spec * material.specular;
    ambient *= attenuation * intensity;
    diffuse *= attenuation * intensity;
    specular *= attenuation * intensity;
    return (ambient + diffuse + specular) * light.intensity;
}
