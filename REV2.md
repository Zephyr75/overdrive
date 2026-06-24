# REV2 — compléments offre NERD (Rendu Temps Réel)

Ce que `REV.md` ne couvre pas et que l'offre cible : techniques de rendu temps réel concrètes, **génération procédurale**, **simulation physique**, **IA**. Descriptions très basiques — juste l'idée pour suivre une discussion. Offre : Vulkan/DX12, C++/Slang/CUDA, optim math, haute perf.

---

# 1. Rendu temps réel — techniques à connaître

- **Shadow mapping** : rendre la depth depuis la lumière → 2e passe, un fragment est dans l'ombre si sa distance à la lumière > depth stockée. *Acne* (auto-ombrage) → bias ; bords durs → **PCF** (moyenne de plusieurs samples) ; grandes scènes → **CSM** (cascaded : plusieurs shadow maps par tranche de distance).
- **Forward vs Deferred** (à comprendre en profondeur — question classique) :
  - **Forward** : pour chaque objet, le fragment shader boucle sur toutes les lumières et shade directement. Coût ≈ **#objets × #lumières × overdraw** (un pixel recouvert 5× est shadé 5×, dont 4 jetés par le depth test). Marche mal avec beaucoup de lumières. *Avantages* : **MSAA** natif, transparence facile, matériaux variés, peu de mémoire.
  - **Deferred** : **passe 1 (geometry)** = on écrit les *propriétés de surface* du fragment visible dans le **G-buffer** (plusieurs render targets : albedo, normal, depth, roughness/metallic). **Passe 2 (lighting)** = on éclaire en **screen-space**, un pixel = un fragment visible (le depth test a déjà trié), on accumule les lumières. Coût ≈ **#pixels × #lumières**, **découplé du nombre d'objets et de l'overdraw**. → des centaines de lumières.
  - **Challenges du deferred** : (1) **G-buffer lourd** = beaucoup de bande passante mémoire (souvent LE mur, surtout console/Switch) → compresser les normals (octahedral), packer les canaux. (2) **MSAA difficile** (anti-aliasing en screen-space → TAA à la place). (3) **transparence cassée** (un seul fragment par pixel dans le G-buffer) → passe forward séparée pour le transparent. (4) un seul modèle d'éclairage pour toute la scène.
  - **Forward+ / clustered** : compromis moderne. On divise l'écran en tuiles (ou le frustum en clusters 3D), une passe compute liste quelles lumières touchent chaque tuile, puis un forward ne shade que ces lumières-là. Garde les avantages du forward (MSAA, transparence, matériaux) avec le scaling en lumières du deferred. Direction actuelle.
- **Ambient occlusion** : approxime combien un point est *caché* de la lumière ambiante par la géométrie voisine → assombrit creux, coins, contacts (contact shadows). Sans AO, l'ambiant est plat. Types :
  - **SSAO** (Screen-Space AO, Crytek) : pour chaque pixel, échantillonne N points aléatoires dans une demi-sphère autour de lui, compare leur depth au G-buffer → fraction de points "enterrés" = occlusion. Pas cher mais bruité (→ blur) et limité à ce qui est à l'écran.
  - **HBAO** (Horizon-Based) : marche le long de directions depuis le pixel, trouve l'angle d'horizon (la pente max qui bloque) → plus physique que SSAO.
  - **GTAO** (Ground-Truth AO) : version qui converge vers le vrai cosine-weighted AO, intègre l'horizon correctement. Le standard qualité actuel.
  - Alternatives : **AO précalculé** (baked dans une texture pour le statique), **RTAO** (ray-traced, exact mais cher).
- **Anti-aliasing** : **MSAA** (multisample, cher), **FXAA** (post-process, flou), **TAA** (temporel, réutilise les frames précédentes + motion vectors — standard actuel). Upscaling : DLSS/FSR.
- **Culling** : ne pas dessiner l'invisible. *Frustum* (hors champ caméra), *occlusion* (caché derrière autre chose), *backface* (dos). **LOD** : maillage simplifié au loin.
- **GPU-driven rendering** : déplacer la décision *quoi dessiner* du CPU vers le GPU.
  - **Problème** : en classique, le CPU fait une boucle "pour chaque objet : cull, bind, draw" → des milliers de draw calls = overhead CPU/driver, le CPU devient le bottleneck.
  - **Idée** : toute la scène (matrices, bounding boxes, matériaux) vit dans des **buffers GPU**. Un **compute shader** fait le culling (frustum + occlusion) et **écrit lui-même la liste de draws** dans un buffer. Le CPU lance un seul `vkCmdDrawIndirectCount` qui lit ce buffer → le GPU dessine ce qu'il a décidé, le CPU ne touche plus chaque objet.
  - **Briques** : `drawIndirect` (paramètres de draw lus depuis un buffer), **bindless** (toutes les textures/buffers accessibles sans rebind), **multi-draw indirect**. **Mesh shaders** poussent plus loin (cull au niveau meshlet sur le GPU).
  - **Intérêt** : scaling à des centaines de milliers d'objets, CPU libéré. Direction Vulkan/DX12 moderne (Nanite en est une forme extrême).
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

  **Collision entre deux cubes (SAT — Separating Axis Theorem)** :
  - **Théorème** : deux formes convexes ne se touchent **pas** s'il existe **un axe** sur lequel leurs projections (ombres 1D) ne se chevauchent pas. Cet axe = un *plan séparateur*. Si aucun tel axe n'existe → elles se touchent.
  - **Quels axes tester ?** Pour des cubes : les **3 faces normales du cube A**, les **3 du cube B**, et les **9 produits vectoriels arête_A × arête_B** (= 15 axes pour des OBB orientés ; AABB alignés axes → juste 3 axes).
  - **Test par axe** : projeter les 8 sommets de chaque cube sur l'axe → on obtient un intervalle [minA,maxA] et [minB,maxB]. S'ils ne se recouvrent pas → **séparés, stop** (early-out). Si tous les axes se recouvrent → collision.
  - **Profondeur de pénétration** = le **plus petit** recouvrement parmi tous les axes ; sa direction = la **normale de collision** (le sens pour les repousser). Le point de contact vient du clipping des faces en regard.
  - **Résolution** : appliquer une **impulsion** le long de la normale aux deux cubes (échange de quantité de mouvement, pondéré par les masses + restitution pour le rebond), puis corriger la position (positional correction) pour éliminer l'enfoncement. Cas AABB simplissime : juste comparer les bornes min/max sur x,y,z.
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

**En détail : codage de Huffman** (entropy coding, sans perte).
- **Idée** : un encodage à longueur fixe (ex. 8 bits/caractère) gaspille — il donne autant de bits aux symboles rares qu'aux fréquents. Huffman donne des **codes courts aux symboles fréquents**, longs aux rares → taille totale ≈ l'**entropie** des données (la limite théorique de Shannon).
- **Construction (arbre binaire)** : (1) compter la fréquence de chaque symbole. (2) Mettre chaque symbole comme un nœud-feuille dans une file de priorité. (3) Répéter : retirer les **deux** nœuds de plus faible fréquence, créer un parent (freq = somme), le réinsérer. (4) Jusqu'à un seul arbre. Le **chemin** racine→feuille (gauche=0, droite=1) donne le code de chaque symbole.
- **Exemple** : `AAAABBC` → A fréquent → `0` (1 bit), B → `10`, C → `11`. Au lieu de 7×8=56 bits, on a 4×1+2×2+1×2=10 bits.
- **Préfixe libre** : aucun code n'est le préfixe d'un autre (propriété de l'arbre) → décodage **non ambigu** sans séparateurs : on descend l'arbre bit par bit, à chaque feuille on émet le symbole et on repart de la racine.
- **Limite** : optimal seulement pour des longueurs entières de bits. Le **codage arithmétique/range** fait mieux (fractions de bit) et est utilisé dans les codecs modernes. Huffman reste partout (zlib/DEFLATE, JPEG) car simple et rapide.

---

# 6. Optimisation haute perf

**Le mur, c'est la mémoire, pas le calcul.** Un accès RAM coûte ~200-300 cycles, un cache L1 ~4. Le CPU passe son temps à *attendre* les données. Toute l'optim moderne vise à nourrir le CPU/GPU sans famine.

- **Data-oriented design (DOD)** : organiser les données pour le cache, pas pour la logique. **AoS** (`struct{pos,vel,hp}[]` = array of structs) : pour additionner toutes les positions, on charge aussi vel+hp inutiles → cache pollué. **SoA** (structure of arrays : un array de pos, un de vel…) : boucler sur les positions ne charge que des positions → **lignes de cache pleines d'utile**, et le compilo peut **vectoriser**. Base des ECS (entity-component-system) et des systèmes de particules.
- **Localité** : *spatiale* (données voisines utilisées ensemble — parcours linéaire > liste chaînée/pointer chasing) + *temporelle* (réutiliser ce qui est déjà en cache). Préférer des `vector` contigus aux structures à pointeurs.
- **SIMD** : une instruction sur 4/8/16 valeurs en parallèle (SSE/AVX sur x86, **NEON** sur ARM/Switch). Idéal pour math vectorielle, particules, traitement d'image. Suppose des données packées (→ SoA).
- **Multithreading** : **job system** = un pool de threads qui piochent des petites tâches dans une file (work-stealing), plutôt qu'un thread fixe par système. Découpe une grosse boucle en jobs indépendants. Attention au **false sharing** (deux threads écrivent la même ligne de cache → ping-pong).
- **GPU** : minimiser les draw calls (instancing, GPU-driven), la bande passante (compresser G-buffer/textures), les changements d'état (trier par pipeline/matériau). L'occupancy = assez de warps pour cacher la latence mémoire.
- **Profiler AVANT d'optimiser** : mesurer le vrai bottleneck (souvent mémoire/bande passante, pas l'ALU). Loi d'Amdahl : optimiser ce qui ne compte pas ne sert à rien. Outils : perf, VTune, **Nsight** (GPU), RenderDoc.
- **Algorithmique d'abord** : passer de O(n²) à O(n log n) bat n'importe quel micro-tuning. Bon algo + bon layout > assembleur malin.

---

# 7. Comment marche un émulateur (conceptuel)

L'offre mentionne l'**émulation** (Switch 2). Un émulateur fait croire à un programme console qu'il tourne sur sa machine d'origine, alors qu'il s'exécute sur un PC.

- **Le problème** : le jeu est du code machine compilé pour le **CPU de la console** (ex. ARM) + il parle à un **hardware spécifique** (GPU, audio, contrôleurs, mémoire). Le PC a un CPU x86 différent et un autre GPU → il faut tout **traduire**.
- **CPU — deux approches** :
  - **Interprétation** : lire une instruction console, la décoder, exécuter l'équivalent, passer à la suivante. Simple, exact, mais **lent** (overhead par instruction).
  - **Recompilation dynamique (JIT)** : traduire des **blocs** d'instructions console en code natif x86 **à la volée**, mettre en cache le résultat → un bloc chaud n'est traduit qu'une fois puis réexécuté nativement. Bien plus rapide, c'est ce qu'utilisent les émulateurs perfs.
- **Mémoire** : émuler l'espace d'adressage de la console (souvent une grosse allocation côté hôte + traduction d'adresses). Gérer l'endianness si elle diffère.
- **GPU** : le plus dur. Le jeu envoie des commandes à un GPU console → l'émulateur les **traduit en appels Vulkan/DX12** sur le GPU du PC, et **recompile les shaders** du format console vers SPIR-V/DXIL. Source des saccades : la compilation de shaders à la volée (→ caches de shaders).
- **Le reste du système** : émuler/réimplémenter les **appels système et le BIOS/OS** de la console (les "syscalls" : allouer mémoire, lire un fichier, lire la manette) → **HLE** (High-Level Emulation : remplacer une fonction OS par une impl native, rapide) vs **LLE** (Low-Level : émuler le vrai firmware, exact mais lent). Plus l'audio, les timers, l'I/O.
- **Synchronisation** : faire avancer CPU, GPU, audio au bon rythme relatif (timing) sinon glitches. Tout ça à ≥ vitesse réelle = le défi de perf (d'où JIT + multithread + cache de shaders).
</content>
