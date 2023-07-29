#version 330 core
out vec4 FragColor;

struct Material {
    vec3 ambient;
    vec3 diffuse;
    vec3 specular;    
    float shininess;
}; 

struct Light {
    vec3 position;
    vec3 direction;
    int type; // 0 = directional, 1 = point

    vec3 color;
    float diffuse;
    float specular;
};
  
in vec3 FragPos; 
in vec2 TexCoord;
in vec3 Normal;

uniform sampler2D ourTexture;
uniform vec3 viewPos;
uniform Material material;
uniform Light light;



void main()
{
  // ambient
  vec3 ambient = light.color * material.ambient;
  
  // diffuse 
  vec3 norm = normalize(Normal);
  vec3 lightDir = light.direction;
  if (light.type == 1) {
    lightDir = normalize(light.position - FragPos);
  }
  float diff = max(dot(norm, lightDir), 0.0);
  vec3 diffuse = light.color * light.diffuse * (diff * material.diffuse);

  // specular
  vec3 viewDir = normalize(viewPos - FragPos);
  vec3 reflectDir = reflect(-lightDir, norm);  
  float spec = pow(max(dot(viewDir, reflectDir), 0.0), material.shininess/1000 + 0.001);
  vec3 specular = light.color * light.specular * (spec * material.specular);
          
  vec2 flipped_tex = vec2(TexCoord.x, 1.0 - TexCoord.y);

  vec4 result = vec4(ambient/3 + diffuse + specular, 1.0) * texture(ourTexture, flipped_tex);

  FragColor = result;


  // FragColor = vec4(Normal, 1.0);
  // FragColor = texture(ourTexture, TexCoord); // * vec4(lightColor, 1.0);


}
