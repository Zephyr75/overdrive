#version 330 core

out vec4 FragColor;
in vec3 FragPos;
in vec3 Normal;
in vec4 FragPosLightSpace;

uniform samplerCube shadowCubeMap;
uniform samplerCube skybox;
uniform sampler2D shadowMap;
uniform sampler2D ourTexture;
uniform vec3 viewPos;
uniform float farPlane;
uniform float time;

#define TAU 6.28318530718
#define MAX_ITER 5

void main()
{
    vec2 fragCoord = FragPos.xz;
    // vec2 uv = (fragCoord * 2.0 - iResolution.xy) / iResolution.y;
    vec2 uv = fragCoord / 10;

// #ifdef SHOW_TILING
// 	vec2 p = mod(uv*TAU*2.0, TAU)-250.0;
// #else
    vec2 p = mod(uv*TAU, TAU)-250.0;
// #endif
	vec2 i = vec2(p);
	float c = 1.0;
	float inten = .005;

	for (int n = 0; n < MAX_ITER; n++) 
	{
		float t = time * (1.0 - (3.5 / float(n+1)));
		i = p + vec2(cos(t - i.x) + sin(t + i.y), sin(t - i.y) + cos(t + i.x));
		c += 1.0/length(vec2(p.x / (sin(i.x+t)/inten),p.y / (cos(i.y+t)/inten)));
	}
	c /= float(MAX_ITER);
	c = 1.17-pow(c, 1.4);
	vec3 colour = vec3(pow(abs(c), 8.0));
    colour = clamp(colour + vec3(0.0, 0.35, 0.5), 0.0, 1.0);

	// #ifdef SHOW_TILING
	// // Flash tile borders...
	// vec2 pixel = 2.0 / iResolution.xy;
	// uv *= 2.0;
	// float f = floor(mod(iTime*.5, 2.0)); 	// Flash value.
	// vec2 first = step(pixel, uv) * f;		   	// Rule out first screen pixels and flash.
	// uv  = step(fract(uv), pixel);				// Add one line of pixels per tile.
	// colour = mix(colour, vec3(1.0, 1.0, 0.0), (uv.x + uv.y) * first.x * first.y); // Yellow line
	// #endif
    
	FragColor = vec4(colour, 0.8);
    
    
}

