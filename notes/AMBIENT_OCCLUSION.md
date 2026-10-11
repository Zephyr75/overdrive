# Ambient occlusion: how it works

> **Scope** the SSAO added on 2026-10-11, walked through change by change, with the code inline.
> **Not here** why AO is limited to the environment term beyond one line; see `FEATURES.md` for the summary.

## The idea

A point in a crease or under an object sees less of the sky than a point in the open, so it should get less of the environment light. Screen-space AO estimates "how much is around me" from the depth buffer alone:

1. For each pixel, scatter 16 points in a small half-sphere (radius 0.25 m) above the surface.
2. For each point, ask the depth buffer: *is there geometry in front of it?* If so, the point is buried.
3. The fraction of buried points is the occlusion. 4 buried out of 16 means the pixel keeps 75% of its sky light.

16 points per pixel is too few to be smooth, so each pixel rotates its pattern differently, and a 4×4 blur averages the 16 rotations back into a clean result.

```
depthPrepass ──► ssao pass ──────────────► main pass (forward.slang)
 writes depth    reads depth                reads the ao image,
                 writes ao image (RG16F):   blurs 4×4, darkens the
                 R = occlusion (noisy)      environment light only
                 G = view distance
```

---

## 1. Getting a depth the shader can read

The prepass already writes depth, but under MSAA each pixel holds 4 depth samples, and a normal `Sampler2D` cannot read a multisampled image. So the prepass now also **copies sample 0** into a plain single-sample image, `sceneDepth`.

`core/targets.go`: the prepass attachment gains a resolve target. Without MSAA there is nothing to resolve, and `sceneDepthView` is `NoView`.

```go
func (targets *screenTargets) prepassDepth(clear *[4]float32) *renderer.Attachment {
	return &renderer.Attachment{View: targets.depthView, Resolve: targets.sceneDepthView, Clear: clear, Store: true}
}
```

`vulkan/frame.go`: the backend turns `Resolve` on a depth attachment into Vulkan's depth resolve. It uses sample 0 rather than an average, because an averaged depth at an edge is a surface that does not exist.

```go
if spec.Depth.Resolve != renderer.NoView {
	resolveView, resolvedImage, _, _, _ := backend.view(spec.Depth.Resolve)
	if resolveView != 0 {
		backend.recordUseImage(commandBuffer, resolvedImage, useDepthResolve)
		att.ResolveImageView = resolveView
		att.ResolveImageLayout = vk.ImageLayoutDepthAttachmentOptimal
		att.ResolveMode = vk.ResolveModeSampleZero
	}
}
```

`vulkan/barrier.go` gets a matching `useDepthResolve` entry. A resolve writes in the colour-output stage even for depth, and the barrier has to wait on that stage.

---

## 2. The ssao pass (`shaders/slang/ssao.slang`)

A full-screen triangle runs the fragment shader once per pixel. It does five things.

**a. Rebuild the 3D position from depth.** Depth plus the pixel's screen position, pushed back through the inverse projection, gives the point in view space (camera at the origin, looking down −z).

```hlsl
float3 viewPosition(float2 uv) {
  float depth = TEX_DEPTH.SampleLevel(uv, 0.0).r;
  float4 ndc = float4(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, depth * 2.0 - 1.0, 1.0);
  float4 view = mul(pushConstants.frame.invProjection, ndc);
  return view.xyz / view.w;
}
```

`invProjection` is new in `FrameUniforms`, computed once a frame in `scene.go` as `Projection.Inv()`.

**b. Rebuild the normal.** Take the neighbours left/right and up/down. The cross product of the two slopes is the surface normal. At an object's outline, one neighbour is on the background, so it uses the neighbour whose depth is closer on each axis.

```hlsl
float3 dx = abs(right.z - p.z) < abs(p.z - left.z) ? right - p : p - left;
float3 dy = abs(down.z - p.z) < abs(p.z - up.z) ? down - p : p - up;
float3 n = normalize(cross(dx, dy));
```

**c. Pick this pixel's rotation.** The rotation repeats every 4×4 pixels, in Bayer order so neighbours differ a lot. Because the pattern repeats exactly, the 4×4 blur later contains each of the 16 rotations exactly once, and the noise cancels completely.

```hlsl
float tiledNoise(float2 pixel) {
  static const uint bayer[16] = { 0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5 };
  uint2 p = uint2(pixel) % 4;
  return (float(bayer[p.y * 4 + p.x]) + 0.5) / 16.0;
}
```

**d. Test 16 points in the half-sphere.** Each point lies on a spiral (the golden angle spreads them evenly), more of them near the top of the half-sphere and close to the surface, where occlusion matters most. Each point is projected back to the screen, and the depth buffer says whether something sits in front of it.

```hlsl
float3 samplePos = p + (t * h.x + bt * h.y + n * h.z) * radius * scale;
float2 sampleUV = screenUV(samplePos);
float sceneZ = viewPosition(sampleUV).z;
float range = smoothstep(0.0, 1.0, radius / abs(p.z - sceneZ));
occlusion += (sceneZ >= samplePos.z + bias ? 1.0 : 0.0) * range;
```

- `bias` (2% of the radius) stops a flat floor from occluding itself through depth rounding.
- `range` is the important guard. A ball floating 3 m in front of a wall also "hides" the wall's sample points on screen, but it is far away and should cast no AO. The occlusion fades out once the hiding surface is further than the radius.

**e. Output two values.** The occlusion goes in R, and the view distance in G, which the blur needs. The sky gets distance 65504, the largest a half float holds, so it never blends into anything.

```hlsl
return float2(1.0 - occlusion / AO_SAMPLES, -p.z);
```

---

## 3. Blur and apply, in the forward pass (`shaders/slang/ao.slang`)

`screenOcclusion`, included by `forward.slang`, averages the 4×4 window around the pixel. Neighbours at a different distance get no weight, so the AO on an object does not bleed onto the wall behind it and contact shadows keep their edge.

```hlsl
for (int y = -2; y < 2; y++) {
  for (int x = -2; x < 2; x++) {
    float2 tap = TEX_AO.SampleLevel(uv + float2(x, y) * texel, 0.0).rg;
    // Full weight within 5% of the centre's distance, none past 10%
    float w = saturate(2.0 - abs(tap.y - centre) / (0.05 * centre));
    sum += tap.x * w;
    weight += w;
  }
}
return sum / weight;
```

The result darkens **only the environment term**. A scene light already has a shadow map, and AO on top would darken it twice. `ambientOcclusion` combines it with the material's own AO, and `forward.slang` multiplies the environment light by it:

```hlsl
float ambientOcclusion(Surface s, float2 pixel) {
  return s.ao * pow(screenOcclusion(pixel), pushConstants.frame.aoIntensity);
}
...
return toDisplay(pbrAmbient(s) * ambientOcclusion(s, i.position.xy) + Lo);
```

Why the blur lives here rather than in a pass of its own:

- **It costs no extra pass.** Thanks to the prepass, the forward shader runs once per pixel, so it can blur as it reads, with no extra image.
- **It explains the G channel.** The blur needs depth. But without MSAA, the depth image *is* the main pass's own depth attachment, and a pass cannot sample what it is drawing into. So ssao stores the distance next to the occlusion instead.

**AO off costs no branch.** `TexAO = 0` points at the white pixel. Every tap then reads (1, 1), all weights are equal, and the result is exactly 1.

---

## 4. Wiring

`core/ao.go` builds the full-screen triangle (one triangle overhanging the screen, so there is no diagonal seam) and the `ssao` pipeline, which writes RG16F with no depth test and no culling. `record` adds the pass:

```go
frame.Pass(renderer.PassSpec{
	Name:  "ssao",
	Color: []renderer.Attachment{{View: targets.aoView, Clear: &unoccluded, Store: true}},
	Reads: []renderer.Handle{targets.sceneDepth},
}, func(pass renderer.Pass) {
	pass.Draw(renderer.DrawCall{Pipeline: passes.occlusion, Mesh: passes.triangle, Push: scene.FrameOnlyAddressArray(frameAddr)})
})
```

`Reads` is what makes the barrier table wait for the prepass's depth before ssao samples it.

`core/app.go` places it between the prepass and the main pass. It runs only with the prepass, since otherwise there is no depth yet. `targets.ao` joins the main pass's reads.

```go
runAO := prepass && ao.Enabled
occlusion.fillUniforms(backend, &targets, &frameUniforms, runAO, ao.Radius, ao.Intensity)
...
if runAO {
	occlusion.record(frame, &targets, frameAddr)
	reads = append(reads, targets.ao)
}
```

`core/targets.go` creates the window-sized images and rebuilds them on resize:

- `sceneDepth`, under MSAA only. Without MSAA the depth buffer itself is made sampleable.
- `ao`, RG16F. R8 would be enough for the occlusion, but not for the distance.
- A point sampler, because AO reads exact texels and linear filtering of a depth format is optional in Vulkan.

`renderer/uniforms.go` and `common.slang` grow by the same five fields, keeping the two in step: `InvProjection`, `TexDepth`, `TexAO`, `AORadius` and `AOIntensity`. `FrameUniforms` goes from 4776 to 4856 bytes.

---

## 5. Settings

```toml
[ambientOcclusion]
enabled = true
radius = 0.25         # metres sampled around each pixel
intensity = 1.5       # exponent on the result: above 1 is darker
```

- **`radius`:** how far a crease can be and still count. Larger catches bigger shapes but loses small contact detail.
- **`intensity`:** a power curve on the result. 1 is as computed; 3–4 is clearly visible in the showcase.

`settings/config.go` rejects a radius or intensity that is not positive. `low.toml` turns AO off.

## Limits worth knowing

- **Screen space only.** Something off screen or hidden behind another object casts no AO, so occlusion can pop at the screen edge.
- **The blur shifts the result.** It covers offsets −2 to +1, so the result sits half a pixel up and left, which is invisible.
- **Edges under MSAA.** AO comes from sample 0 only, so its edges are not antialiased; at normal intensities this does not show.
