Specular is the reflection. Light bounces off the surface like off a mirror, so the surface shows the sky behind you, mirrored about its normal.
- What you see depends on where you stand: move the camera and the reflection slides across the surface.
- On a smooth surface it's sharp (m0). On a rough one the tiny bumps scatter the bounce, so it's a blur of a wider patch of sky (m5).
- That's why there are six levels, one per amount of blur.
- In the scene: the shine and sky reflection on the metal plate and the football.

Irradiance is the soft base lighting. Light enters the surface, scatters around inside, and comes out in every direction.
- A point on the surface is lit by the whole half of the sky it faces, averaged together.
- It doesn't depend on where you stand, only on which way the surface faces. A floor facing up gets the average of the whole sky above it; a wall facing the sunset gets mostly warm orange.
- Averaging half the sky leaves almost no detail, so one small, very blurry map is enough.
- In the scene: the overall colour and brightness of the bricks, the pebbles and the wood.

Every material is a mix of both. The shader adds irradiance × the base colour to specular × the reflectance. Metalness and roughness decide how much of each you see: a rough brick is nearly all irradiance, and polished metal is nearly all specular.

---

- Sky: yes, it's only what you see when you look at the background.
- Specular: yes, it's the reflections. But every surface reflects a little, not only shiny ones. Roughness picks which blur level is read (m0 sharp, m5 very blurry), and the Fresnel term decides how strong the reflection is. That's why even brick reflects some sky at grazing angles.
- Irradiance: it's the diffuse lighting the sky gives every surface, the soft base colour and brightness. A floor facing up gets the bright sky average, and a wall facing the sunset gets orange. Without it, anything outside the direct lights would be black.