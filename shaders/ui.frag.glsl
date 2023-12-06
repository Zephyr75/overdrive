#version 330 core
out vec4 FragColor;

in vec2 TexCoords;

uniform sampler2D depthMap;

void main()
{             
    if(texture(depthMap, TexCoords).a < 0.5) {
        discard;
    }
    FragColor = texture(depthMap, TexCoords); 
}
