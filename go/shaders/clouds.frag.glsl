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

#define MAX_STEPS 100
#define MARCH_SIZE 0.08

vec4 permute(vec4 x){return mod(((x*34.0)+1.0)*x, 289.0);}
vec4 taylorInvSqrt(vec4 r){return 1.79284291400159 - 0.85373472095314 * r;}
vec3 fade(vec3 t) {return t*t*t*(t*(t*6.0-15.0)+10.0);}

float perlinNoise(vec3 P){
    // return sin(time);
  vec3 Pi0 = floor(P); // Integer part for indexing
  vec3 Pi1 = Pi0 + vec3(1.0); // Integer part + 1
  Pi0 = mod(Pi0, 289.0);
  Pi1 = mod(Pi1, 289.0);
  vec3 Pf0 = fract(P); // Fractional part for interpolation
  vec3 Pf1 = Pf0 - vec3(1.0); // Fractional part - 1.0
  vec4 ix = vec4(Pi0.x, Pi1.x, Pi0.x, Pi1.x);
  vec4 iy = vec4(Pi0.yy, Pi1.yy);
  vec4 iz0 = Pi0.zzzz;
  vec4 iz1 = Pi1.zzzz;

  vec4 ixy = permute(permute(ix) + iy);
  vec4 ixy0 = permute(ixy + iz0);
  vec4 ixy1 = permute(ixy + iz1);

  vec4 gx0 = ixy0 / 7.0;
  vec4 gy0 = fract(floor(gx0) / 7.0) - 0.5;
  gx0 = fract(gx0);
  vec4 gz0 = vec4(0.5) - abs(gx0) - abs(gy0);
  vec4 sz0 = step(gz0, vec4(0.0));
  gx0 -= sz0 * (step(0.0, gx0) - 0.5);
  gy0 -= sz0 * (step(0.0, gy0) - 0.5);

  vec4 gx1 = ixy1 / 7.0;
  vec4 gy1 = fract(floor(gx1) / 7.0) - 0.5;
  gx1 = fract(gx1);
  vec4 gz1 = vec4(0.5) - abs(gx1) - abs(gy1);
  vec4 sz1 = step(gz1, vec4(0.0));
  gx1 -= sz1 * (step(0.0, gx1) - 0.5);
  gy1 -= sz1 * (step(0.0, gy1) - 0.5);

  vec3 g000 = vec3(gx0.x,gy0.x,gz0.x);
  vec3 g100 = vec3(gx0.y,gy0.y,gz0.y);
  vec3 g010 = vec3(gx0.z,gy0.z,gz0.z);
  vec3 g110 = vec3(gx0.w,gy0.w,gz0.w);
  vec3 g001 = vec3(gx1.x,gy1.x,gz1.x);
  vec3 g101 = vec3(gx1.y,gy1.y,gz1.y);
  vec3 g011 = vec3(gx1.z,gy1.z,gz1.z);
  vec3 g111 = vec3(gx1.w,gy1.w,gz1.w);

  vec4 norm0 = taylorInvSqrt(vec4(dot(g000, g000), dot(g010, g010), dot(g100, g100), dot(g110, g110)));
  g000 *= norm0.x;
  g010 *= norm0.y;
  g100 *= norm0.z;
  g110 *= norm0.w;
  vec4 norm1 = taylorInvSqrt(vec4(dot(g001, g001), dot(g011, g011), dot(g101, g101), dot(g111, g111)));
  g001 *= norm1.x;
  g011 *= norm1.y;
  g101 *= norm1.z;
  g111 *= norm1.w;

  float n000 = dot(g000, Pf0);
  float n100 = dot(g100, vec3(Pf1.x, Pf0.yz));
  float n010 = dot(g010, vec3(Pf0.x, Pf1.y, Pf0.z));
  float n110 = dot(g110, vec3(Pf1.xy, Pf0.z));
  float n001 = dot(g001, vec3(Pf0.xy, Pf1.z));
  float n101 = dot(g101, vec3(Pf1.x, Pf0.y, Pf1.z));
  float n011 = dot(g011, vec3(Pf0.x, Pf1.yz));
  float n111 = dot(g111, Pf1);

  vec3 fade_xyz = fade(Pf0);
  vec4 n_z = mix(vec4(n000, n100, n010, n110), vec4(n001, n101, n011, n111), fade_xyz.z);
  vec2 n_yz = mix(n_z.xy, n_z.zw, fade_xyz.y);
  float n_xyz = mix(n_yz.x, n_yz.y, fade_xyz.x); 
  return 2.2 * n_xyz;
}


float SDSphere(vec3 p, float radius) {
  return length(p) - radius;
}


float Fbm(vec3 p) {
  vec3 q = p + time * 0.5 * vec3(1.0, -0.2, -1.0);
  float g = perlinNoise(q);

  float f = 0.0;
  float scale = 0.5;
  float factor = 2.02;

  for (int i = 0; i < 6; i++) {
      f += scale * perlinNoise(q);
      q *= factor;
      factor += 0.21;
      scale *= 0.5;
  }

  return f;
}

float GetDensity(vec3 p) {
  float distance = SDSphere(p, 1.0);

  float f = Fbm(p);

  return -distance + f;
}

const vec3 SUN_POSITION = vec3(1.0, 0.0, 0.0);


vec4 Raymarch(vec3 rayOrigin, vec3 rayDirection) {
  float totalDist = 0.;
  vec3 curPoint = rayOrigin + totalDist * rayDirection;
  vec3 sunDirection = normalize(SUN_POSITION);

  vec4 totalColor = vec4(0.0);

  for (int i = 0; i < MAX_STEPS; i++) {
    float density = GetDensity(curPoint);

    // We only draw the density if it's greater than 0
    if (density > 0.0) {
      // Directional derivative
      // For fast diffuse lighting
      // Could also compute normal to compute diffuse lighting as with standard raymarching
      float diffuse = clamp((GetDensity(curPoint) - GetDensity(curPoint + 0.3 * sunDirection)) / 0.3, 0.0, 1.0 );
      vec3 lin = vec3(0.60,0.60,0.75) * 1.1 + 0.8 * vec3(1.0,0.6,0.3) * diffuse;
      vec4 color = vec4(mix(vec3(1), vec3(0), density), density);
      color.rgb *= lin;
      color.rgb *= color.a;
      totalColor += color * (1.0 - totalColor.a);
    }

    totalDist += MARCH_SIZE;
    curPoint = rayOrigin + totalDist * rayDirection;
  }

  return totalColor;
}


void main()
{
    vec2 iResolution = vec2(1920, 1080);
    vec2 fragCoord = FragPos.xz;
    // vec2 uv = (fragCoord * 2.0 - iResolution.xy) / iResolution.y;
    vec2 uv = fragCoord / 10;
    
    vec3 rayOrigin = vec3(0, 0, 2);
    vec3 rayDirection = normalize(vec3(uv.x, uv.y, -1));
  
    vec3 color = vec3(1.0);

    // // Sun and Sky
    // vec3 sunDirection = normalize(SUN_POSITION);
    // float sun = clamp(dot(sunDirection, rayDirection), 0.0, 1.0 );
    // // Base sky color
    // color = vec3(0.7,0.7,0.90);
    // // Add vertical gradient
    // color -= 0.8 * vec3(0.90,0.75,0.90) * rayDirection.y;
    // // Add sun color to sky
    // color += 0.5 * vec3(1.0,0.5,0.3) * pow(sun, 10.0);

    // Cloud
    vec4 res = Raymarch(rayOrigin, rayDirection);
    color = color * (1.0 - res.a) + res.rgb;

   
    FragColor = vec4(color, res.a);
    // FragColor = vec4(uv, 0. ,1.0);
}

