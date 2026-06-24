# REV2 — compléments offre NERD (Rendu Temps Réel)

Ce que `REV.md` ne couvre pas et que l'offre cible : techniques de rendu temps réel concrètes, **génération procédurale**, **simulation physique**, **IA**. Descriptions très basiques — juste l'idée pour suivre une discussion. Offre : Vulkan/DX12, C++/Slang/CUDA, optim math, haute perf.

---

# 1. Rendu temps réel — techniques à connaître

- **Shadow mapping** : rendre la depth depuis la lumière → 2e passe, un fragment est dans l'ombre si sa distance à la lumière > depth stockée. *Acne* (auto-ombrage) → bias ; bords durs → **PCF** (moyenne de plusieurs samples) ; grandes scènes → **CSM** (cascaded : plusieurs shadow maps par tranche de distance).
- **Forward vs Deferred** : *forward* = shade chaque objet directement (cher si beaucoup de lumières). *Deferred* = passe 1 écrit la géométrie dans un **G-buffer** (albedo, normal, depth…), passe 2 éclaire en screen-space (découple #lumières de #objets). *Forward+/clustered* = forward + lumières triées par tuile/cluster.
- **Ambient occlusion** : assombrir les creux/contacts. **SSAO** (screen-space : échantillonne la depth autour du pixel), **HBAO**, **GTAO** (plus exact).
- **Anti-aliasing** : **MSAA** (multisample, cher), **FXAA** (post-process, flou), **TAA** (temporel, réutilise les frames précédentes + motion vectors — standard actuel). Upscaling : DLSS/FSR.
- **Culling** : ne pas dessiner l'invisible. *Frustum* (hors champ caméra), *occlusion* (caché derrière autre chose), *backface* (dos). **LOD** : maillage simplifié au loin.
- **GPU-driven rendering** : le GPU décide quoi dessiner (compute cull + `drawIndirect`), CPU ne pilote plus chaque draw. Direction Vulkan/DX12.
- **HDR pipeline** : rendre en float linéaire → **tone mapping** (ACES, Reinhard) compresse en [0,1] → gamma/sRGB. **Bloom** : flou des zones très lumineuses.

---

# 2. Génération procédurale de contenu

- **Bruit (la brique de base)** : **Perlin** / **Simplex** (gradient noise, lisse, sans direction privilégiée), **value noise**. **fBm** (fractal Brownian motion) = somme d'octaves de bruit à fréquences/amplitudes croissantes → détail multi-échelle.
- **Terrain** : heightmap = fBm 2D. **Diamond-square** (subdivision récursive d'une grille). **Érosion hydraulique** (simuler l'eau qui creuse) pour du réalisme. Marching cubes pour terrain volumétrique (grottes 3D).
- **Herbe** : **GPU instancing** (un mesh, N copies en un draw), placement par **compute shader** (densité depuis une texture), vent par animation de vertex (sin + bruit). **Billboards** + LOD au loin (cartes au lieu de géométrie).
- **Végétation / arbres** : **L-systems** (grammaire de réécriture : règles récursives `F→F[+F]F[-F]` → branches). Distribution naturelle par **Poisson-disk sampling** (points aléatoires mais espacés, pas de paquets).
- **Niveaux / donjons** : **BSP** (découpage binaire récursif en salles), **automates cellulaires** (grottes organiques), **Wave Function Collapse** (assemble des tuiles selon des contraintes de voisinage — très en vogue).
- **Textures** : domain warping (bruit qui déforme les coords d'un autre bruit).

---

# 3. Simulation physique

- **Intégration** : **Euler** (simple, instable), **Verlet** (stocke pos actuelle + précédente, stable, pas de vitesse explicite — utilisé dans ton moteur Go), **RK4** (précis, cher).
- **Particules** : chaque point = pos + vitesse + durée de vie ; update + spawn/kill. Base des effets (feu, étincelles, fumée).
- **Tissu / soft body** : **mass-spring** (masses reliées par ressorts) intégré en Verlet, ou **PBD/XPBD** (Position-Based Dynamics : on corrige directement les positions pour satisfaire des contraintes de distance — stable, rapide, standard jeu).
- **Corps rigides** : **broad phase** (trouver les paires qui *pourraient* se toucher : grille, **sweep & prune**, **BVH**) → **narrow phase** (test exact : **SAT** pour convexes, **GJK/EPA** pour la distance/pénétration) → **résolution par impulsions** (corriger vitesses au point de contact).
- **Eau / fluides** :
  - **SPH** (Smoothed Particle Hydrodynamics) : fluide = particules, chaque particule moyenne ses voisines (pression, viscosité). Splashs, fluide libre.
  - **Shallow water equations** : eau = heightfield 2D (lacs, rivières). Pas de déferlement mais rapide.
  - **FFT ocean (Tessendorf)** : océan = somme de vagues dans le domaine fréquentiel, inverse FFT → heightmap qui boucle. Le standard des océans (films + jeux).
  - **Gerstner / sine waves** : quelques vagues analytiques additionnées (les points font des cercles → crêtes pointues). Cheap, pour eau simple.
  - **Stable Fluids (Stam)** : grille + Navier-Stokes inconditionnellement stable → fumée/feu.

---

# 4. IA d'ennemis / agents

- **FSM** (machine à états finis) : états (patrol, chase, attack) + transitions sur événements. Simple, lisible, le défaut historique.
- **Behavior Tree** : arbre de nœuds (sequence, selector, condition, action) parcouru chaque frame → décisions modulaires et réutilisables. Standard moderne.
- **Pathfinding** : **A\*** (Dijkstra guidé par une heuristique distance-au-but) sur grille/graphe. **NavMesh** = maillage des zones marchables (au lieu d'une grille). **Flow field** = un champ de directions partagé pour des foules (calcul une fois, suivi par tous).
- **Steering / Boids** (Reynolds) : comportement de groupe émergent depuis 3 règles locales — **séparation** (éviter les voisins), **alignement** (même direction), **cohésion** (rester groupé). Bancs de poissons, nuées.
- **GOAP** (Goal-Oriented Action Planning) : l'IA planifie une séquence d'actions (avec préconditions/effets) pour atteindre un but — comme un mini A* sur les actions. (F.E.A.R.)
- **Utility AI** : chaque action reçoit un score (faim, danger…) → choisir le meilleur. Comportements nuancés.
- **Influence maps** : grille où chaque case accumule danger/contrôle → décisions tactiques (où attaquer/fuir).
- **Jeux à tour** : **Minimax + élagage alpha-bêta** (échecs), **MCTS** (Monte Carlo Tree Search : simule des parties aléatoires — Go/AlphaGo).

---

# 5. Compression (mentionnée dans l'offre)

- **Textures** : **BCn** (block compression, DXT) / **ASTC** : compression par blocs décodée par le GPU → moins de VRAM, bande passante. **Basis Universal** : transcode vers le format de chaque GPU.
- **Maillages** : **quantization** (positions en int16 au lieu de float32), **meshoptimizer**, **Draco**.
- **Général** : **entropy coding** (Huffman, arithmétique/range — code court pour symboles fréquents), **LZ** (références à des motifs déjà vus), **delta encoding** (stocker les différences).

---

# 6. Optimisation haute perf

- **Data-oriented design** : **SoA** (structure of arrays) plutôt qu'AoS → meilleure localité cache, vectorisable. Le cache, pas le CPU, est souvent le mur.
- **SIMD** : une instruction sur 4/8 valeurs (SSE/AVX x86, NEON ARM/Switch). Math vectorielle, particules.
- **Multithreading** : **job system** (pool de tâches volées entre threads) plutôt qu'un thread par système.
- **Profiler avant d'optimiser** : mesurer le vrai bottleneck (souvent mémoire/bande passante, pas le calcul). Nsight côté GPU.
</content>
