#version 330 core
out vec4 FragColor;
  
// in vec3 ourColor;
in vec2 TexCoord;

uniform sampler2D ourTexture;
uniform vec3 lightColor;

void main()
{
  // FragColor = vec4(ourColor, 1.0);

  FragColor = texture(ourTexture, TexCoord) * vec4(lightColor, 1.0);

}
