Distinction qui prête souvent à confusion parce que le vocabulaire a dérivé avec le temps.

---

## Ray tracing — le terme général

C'est l'idée de base : tu lances des rayons depuis la caméra dans la scène, tu trouves ce qu'ils touchent, tu calcules une couleur. Au sens large, "ray tracing" englobe toutes les techniques qui utilisent des rayons.

Mais au sens **historique/strict**, "ray tracing" (ou *Whitted ray tracing*, 1980) désigne une méthode spécifique :
- Tu lances un rayon, il touche une surface
- Tu calcules l'éclairage direct (lumières visibles depuis ce point)
- Tu lances des rayons **secondaires déterministes** : réflexion parfaite (miroir), réfraction
- Récursion sur ces rayons

Résultat : ombres nettes, miroirs et verre parfaits. Mais **pas de global illumination réaliste** — pas de surfaces rugueuses qui rebondissent la lumière diffuse, pas de soft shadows naturelles, pas de color bleeding.

---

## Path tracing — une forme de ray tracing

C'est aussi du ray tracing, mais qui résout la **rendering equation** complète (Kajiya, 1986) par méthode Monte Carlo.

La différence clé : à chaque rebond, au lieu de suivre une direction déterministe, tu **échantillonnes aléatoirement** une direction selon la BRDF de la surface.

- Une surface diffuse rebondit le rayon dans une direction random de l'hémisphère
- Tu suis ce chemin (*path*) sur plusieurs rebonds jusqu'à une lumière
- Tu fais ça des **centaines/milliers de fois par pixel** et tu moyennes

Résultat : global illumination complète — color bleeding, soft shadows, caustics, ambient occlusion, tout émerge naturellement. Le prix : le **bruit** (noise) qui diminue en $1/\sqrt{N}$ avec le nombre d'échantillons.

---

## Le résumé

| | Whitted Ray Tracing | Path Tracing |
|---|---|---|
| Année | 1980 | 1986 |
| Rebonds | déterministes (miroir/réfraction) | aléatoires (Monte Carlo) |
| Global illumination | non | oui |
| Surfaces diffuses | éclairage direct seulement | rebonds indirects complets |
| Image | propre, mais incomplète | réaliste, mais bruitée |
| Coût | faible | élevé |

---

## La nuance qui piège

Quand on dit "ray tracing" aujourd'hui (RTX, jeux), c'est un abus de langage marketing. Les jeux font en réalité du path tracing partiel/échantillonné avec du denoising agressif. Donc :

- **Ray tracing** = la famille entière + la méthode Whitted historique
- **Path tracing** = la technique Monte Carlo qui résout la GI

Path tracing **est** du ray tracing, mais tout ray tracing n'est pas du path tracing. Pour ton entretien NERD, c'est exactement le genre de distinction de vocabulaire qu'ils peuvent vérifier que tu maîtrises.