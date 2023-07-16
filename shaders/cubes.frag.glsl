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

    vec3 ambient;
    vec3 diffuse;
    vec3 specular;
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
  vec3 ambient = light.ambient * material.ambient;
  
  // diffuse 
  vec3 norm = normalize(Normal);
  vec3 lightDir = normalize(light.position - FragPos);
  float diff = max(dot(norm, lightDir), 0.0);
  vec3 diffuse = light.diffuse * (diff * material.diffuse);

  // specular
  vec3 viewDir = normalize(viewPos - FragPos);
  vec3 reflectDir = reflect(-lightDir, norm);  
  float spec = pow(max(dot(viewDir, reflectDir), 0.0), material.shininess+0.01);
  vec3 specular = light.specular * (spec * material.specular);
          
  vec4 result = vec4(ambient/3 + diffuse + specular, 1.0) * texture(ourTexture, TexCoord);

  // only multiply texture if it is provided
  // if (ourTexture != 0) {
  //   result = result * texture(ourTexture, TexCoord);
  // }

  // vec4 result = vec4(ambient/3 + diffuse + specular, 1.0); // * texture(ourTexture, TexCoord);
  // vec4 result = vec4(ambient + diffuse + specular, 1.0); // * texture(ourTexture, TexCoord);
  FragColor = result;


  // FragColor = vec4(Normal, 1.0);
  // FragColor = texture(ourTexture, TexCoord); // * vec4(lightColor, 1.0);


}
