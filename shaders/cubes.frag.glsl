#version 330 core
out vec4 FragColor;

struct Material {
    vec3 ambient;
    vec3 diffuse;
    vec3 specular;    
    float shininess;
}; 

struct Light {
    int type; // 0 = directional, 1 = point
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

#define NR_LIGHTS 2  
uniform Light lights[NR_LIGHTS];
uniform Material material;
  
in vec3 FragPos; 
in vec2 TexCoord;
in vec3 Normal;
in vec4 FragPosLightSpace;

uniform samplerCube shadowCubeMap;
uniform samplerCube skybox;
uniform sampler2D shadowMap;
uniform sampler2D ourTexture;
uniform vec3 viewPos;
uniform float farPlane;


vec3 CalcDirLight(Light light, vec3 normal, vec3 viewDir);
vec3 CalcPointLight(Light light, vec3 normal, vec3 fragPos, vec3 viewDir);

float ShadowCalculation(vec4 fragPosLightSpace);

float ShadowCalculationCube(vec3 fragPos);

void main()
{
  // properties
  vec3 norm = normalize(Normal);
  vec3 viewDir = normalize(viewPos - FragPos);

  vec3 result = vec3(0.2);

  for(int i = 0; i < NR_LIGHTS; i++){
    switch (lights[i].type) {
      case 0:
        result += CalcDirLight(lights[i], norm, viewDir) * (1.0 - ShadowCalculation(FragPosLightSpace));
        break;
      case 1:

        // result += CalcDirLight(lights[i], norm, viewDir) * (1.0 - ShadowCalculation(FragPosLightSpace));
        result += CalcPointLight(lights[i], norm, FragPos, viewDir) * (1.0 - ShadowCalculationCube(FragPos));
        break;
    }
    
  }

  vec2 flipped_tex = vec2(TexCoord.x, 1.0 - TexCoord.y);


  vec3 fragToLight = FragPos - lights[0].position;
  float closestDepth = texture(shadowCubeMap, fragToLight).r;
  // Debug cubemap depth
  // FragColor = vec4(vec3(20 * closestDepth / farPlane), 1.0); 
  
  // Skybox reflection
  vec3 I = normalize(FragPos - viewPos);
  vec3 R = reflect(I, norm);
  // FragColor = vec4(texture(skybox, R).rgb, 1.0);
  vec4 skyboxColor = vec4(texture(skybox, R).rgb, 1.0);
  // multiply skybox by diffuse coefficient
  // TODO: adapt formula to diffuse and specular
  skyboxColor *= vec4(vec3(1 - material.diffuse), 1.0);
  skyboxColor /= vec4(vec3(5), 1.0);
  // skyboxColor += vec4(vec3(material.diffuse), 1.0);
  FragColor = vec4(result, 1.0) * texture(ourTexture, flipped_tex) + skyboxColor;

  // Normal
  // FragColor = vec4(result, 1.0) * texture(ourTexture, flipped_tex);

  // FragColor = texture(ourTexture, TexCoord);
  // FragColor = vec4(result, 1.0);
  // FragColor = vec4(Normal, 1.0);
  // FragColor = texture(ourTexture, TexCoord); // * vec4(lightColor, 1.0);

}

// array of offset direction for sampling
vec3 gridSamplingDisk[20] = vec3[]
(
   vec3(1, 1,  1), vec3( 1, -1,  1), vec3(-1, -1,  1), vec3(-1, 1,  1), 
   vec3(1, 1, -1), vec3( 1, -1, -1), vec3(-1, -1, -1), vec3(-1, 1, -1),
   vec3(1, 1,  0), vec3( 1, -1,  0), vec3(-1, -1,  0), vec3(-1, 1,  0),
   vec3(1, 0,  1), vec3(-1,  0,  1), vec3( 1,  0, -1), vec3(-1, 0, -1),
   vec3(0, 1,  1), vec3( 0, -1,  1), vec3( 0, -1, -1), vec3( 0, 1, -1)
);

float ShadowCalculationCube(vec3 fragPos)
{
    // get vector between fragment position and light position
    vec3 fragToLight = fragPos - lights[0].position;
    float currentDepth = length(fragToLight);
    // shadow /= (samples * samples * samples);
    float shadow = 0.0;
    float bias = 0.15;
    int samples = 20;
    float viewDistance = length(viewPos - fragPos);
    float diskRadius = (1.0 + (viewDistance / farPlane)) / 25.0;
    for(int i = 0; i < samples; ++i)
    {
        float closestDepth = texture(shadowCubeMap, fragToLight + gridSamplingDisk[i] * diskRadius).r;
        closestDepth *= farPlane;   // undo mapping [0;1]
        if(currentDepth - bias > closestDepth)
            shadow += 1.0;
    }
    shadow /= float(samples);
        
    // display closestDepth as debug (to visualize depth cubemap)
    // FragColor = vec4(vec3(closestDepth / farPlane), 1.0);    
        
    return shadow * 5;
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
    // diffuse shading
    float diff = max(dot(normal, lightDir), 0.0);
    // specular shading
    vec3 halfwayDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfwayDir), 0.0), material.shininess + 0.0001); 
    // combine results
    vec3 ambient  = light.color  * material.ambient;
    vec3 diffuse  = light.color * light.diffuse  * diff * material.diffuse;
    vec3 specular = light.color * light.specular * spec * material.specular;
    return (ambient + diffuse + specular) * light.intensity / 5;
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
    // attenuation
    float distance    = length(light.position - fragPos);
    float attenuation = 1.0 / (light.constant + light.linear * distance + light.quadratic * (distance * distance));    
    // combine results
    vec3 ambient  = light.color * material.ambient;
    vec3 diffuse  = light.color * light.diffuse  * diff * material.diffuse;
    vec3 specular = light.color * light.specular * spec * material.specular;
    ambient  *= attenuation;
    diffuse  *= attenuation;
    specular *= attenuation;
    return (ambient + diffuse + specular) * light.intensity / 5;
} 

