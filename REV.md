# NERD — révision entretien rendu temps réel

Le minimum à maîtriser. Détails complets dans `notes/` (PBR, OPENGL, VULKAN, RAYTRACING, ALGEBRA) et `NERDShaderExercises_GameRendering2026/.../README.md` (heat) + `CUDA_Refresher/REPORT.md`.

---

# 1. BRDF / PBR

## Équation de réflexion (le cœur)

$$L_o(\omega_o) = \int_{\mathcal{H}^2} f(\omega_o, \omega_i)\, L_i(\omega_i)\, |\cos\theta_i|\, d\omega_i$$

Tout le rendu (raster, ray tracing, path tracing) n'est qu'une façon d'**approximer cette intégrale**. La rendering equation (Kajiya 1986) y ajoute l'émission $L_e$ et rend $L_i$ récursif.

**Radiance** $L$ = W/(m²·sr) : la grandeur reine, **constante le long d'un rayon dans le vide** (c'est ce qui rend le ray tracing possible). Le `NdotL` final vient du $\cos\theta_i$ (loi de Lambert : un faisceau en biais étale son énergie).

## Les 3 propriétés d'une BRDF plausible

1. **Positivité** : $f \geq 0$.
2. **Réciprocité de Helmholtz** : $f(\omega_o,\omega_i) = f(\omega_i,\omega_o)$ (échanger source ↔ caméra).
3. **Conservation d'énergie** : $\int_{\mathcal{H}^2} f \cos\theta_i\, d\omega_i \leq 1$.

## Diffus (Lambert)

$$f_\text{diff} = \frac{\rho}{\pi}$$

Le $\pi$ vient de la conservation : $\int_{\mathcal{H}^2} k\cos\theta\, d\omega = k\pi$, donc $k=\rho/\pi$. (En shading direct on l'absorbe souvent dans l'intensité de la lumière.)

## Cook-Torrance (spéculaire) = F · D · G

$$f_\text{spec} = \frac{D(\omega_m)\, F(\omega_o,\omega_m)\, G(\omega_o,\omega_i)}{4\,(\mathbf{n}\cdot\omega_o)(\mathbf{n}\cdot\omega_i)}, \qquad \omega_m = \frac{\omega_o+\omega_i}{\lVert\omega_o+\omega_i\rVert} \text{ (half-vector)}$$

Multiplication car les 3 conditions sont indépendantes et **toutes nécessaires** : un photon revient s'il frappe un miroir *bien orienté* ($D$) **et** que ce miroir *réfléchit* ($F$) **et** qu'il est *dégagé* ($G$).

| Terme | Dépend de | Contrôle |
|---|---|---|
| **D** (GGX) | rugosité + géométrie ($\omega_m$ vs $\mathbf{n}$) | **forme/taille** du highlight |
| **F** (Fresnel) | matériau ($F_0$) + angle | **couleur/force** du reflet |
| **G** (Smith) | rugosité + angles rasants | **énergie** perdue (masking/shadowing) |

- **D = GGX** : densité de microfacettes orientées selon $\omega_m$. Lisse → pic haut/étroit (highlight petit, vif) ; rugueux → étalé. Longues queues = signature GGX.
- **F = Fresnel-Schlick** (la formule à connaître par cœur), sur la **micronormale $\omega_m$**, pas $\mathbf{n}$ :

$$F(\theta) = F_0 + (1-F_0)(1-\cos\theta)^5$$

- **G = Smith** : fraction de microfacettes ni masquées (côté $\omega_o$) ni ombrées (côté $\omega_i$). Mord aux angles rasants, empêche le gain d'énergie non physique. État de l'art : **height-correlated** (Heitz 2014) au lieu du séparable.

```glsl
D(ωm) = α² / (π·((n·ωm)²·(α²-1)+1)²)            // GGX isotrope
G = 1 / (1 + Λ(ωo) + Λ(ωi))                      // Smith height-correlated
```

## Workflow metallic-roughness (standard glTF/UE4)

```
F0     = lerp(vec3(0.04), baseColor, metallic)
albedo = baseColor * (1.0 - metallic)   // les métaux n'ont pas de diffus
```

**Insight clé métal vs diélectrique** :
- **Diélectrique** : $F_0 \approx 0.04$ gris **+ diffus coloré** (lumière réfractée ressort).
- **Métal** : $F_0$ **coloré** (sa couleur de reflet), **aucun diffus** (lumière réfractée absorbée).

BRDF complète : $f = \underbrace{(1-F)(1-m)\frac{\rho}{\pi}}_\text{diffus} + \underbrace{\frac{DFG}{4(\mathbf{n}\cdot\omega_o)(\mathbf{n}\cdot\omega_i)}}_\text{spéculaire}$. Le $(1-F)$ = conservation (ce qui part en spéculaire ne va pas au diffus).

## IBL — split-sum (Karis 2013, temps réel)

On scinde l'intégrale env-map en deux précalculs :

```
specular = prefiltered * (F0 * envBRDF.x + envBRDF.y)
```
1. **Pre-filtered environment map** : on préfiltre la HDRI en convoluant avec GGX pour plusieurs rugosités, stockées dans les **mips** d'une cubemap. Rugosité faible → mip net (réflexion miroir). Rugosité forte → mip flou.
2. **BRDF LUT** : texture 2D indexée par $(\text{NdotV}, \text{roughness})$, donne $(A,B)$ → un *scale+bias* sur $F_0$.

## Importance sampling

Estimateur Monte Carlo : $L_o \approx \frac{1}{N}\sum \frac{f\,L_i\,|\cos\theta_k|}{p(\omega_k)}$. On choisit $p$ proche de $f\cos\theta$ pour réduire la variance. État de l'art : **VNDF** (Heitz 2018, n'échantillonne que les microfacettes *visibles*). Bruit en $O(1/\sqrt{N})$ → denoising.

## Pièges entretien
- Fresnel sur la **micronormale** $\omega_m$, pas $\mathbf{n}$.
- Le facteur $\pi$ qui apparaît/disparaît selon les conventions.
- Roughness perceptuel vs $\alpha$ (UE4/Disney : $\alpha = \text{roughness}^2$).

---

# 2. Ray tracing vs Path tracing

| | Whitted Ray Tracing (1980) | Path Tracing (1986) |
|---|---|---|
| Rebonds | déterministes (miroir/réfraction) | aléatoires (Monte Carlo, selon la BRDF) |
| Global illumination | non | oui (color bleeding, soft shadows, caustics) |
| Image | propre mais incomplète | réaliste mais bruitée ($1/\sqrt{N}$) |

Path tracing **est** du ray tracing (résout la rendering equation complète), mais l'inverse est faux. "Ray tracing" en jeu (RTX) = abus de langage : path tracing partiel + denoising agressif.

---

# 3. Heat equation exercise (sphere & torus)

Équation de la chaleur stationnaire $\Delta u = 0$ (équation de Laplace) résolue en temps réel sur Shadertoy. Contraintes = régions chaudes ($u=1$) / froides ($u=0$). 3 shaders : `Common` (types), `BufferA` (solveur), `Image` (ray tracing + couleur).

## Principe
**Laplace-Beltrami** = le Laplacien sur une surface courbe (dérivée seconde : le point est-il au-dessus ou en dessous de ses voisins ?). À $\Delta u = 0$, chaque point = moyenne pondérée de ses voisins.

### D'où vient la formule
Une surface est décrite par une **paramétrisation** $X(\phi,\theta)$ (2 coords → point 3D). La métrique $g$ mesure comment les distances se déforment : $g_{ab} = \partial_a X \cdot \partial_b X$ (produits scalaires des vecteurs tangents). Le Laplacien généralisé à n'importe quelle métrique est la **formule de Laplace-Beltrami** :

$$\Delta u = \frac{1}{\sqrt{|g|}}\,\partial_a\!\left(\sqrt{|g|}\;g^{ab}\,\partial_b u\right)$$

C'est la divergence du gradient, mais pondérée par l'élément d'aire $\sqrt{|g|}$ pour rester correct quand la surface s'étire/se resserre.

**Sphère** : $X(\phi,\theta)=(\sin\theta\cos\phi,\,\sin\theta\sin\phi,\,\cos\theta)$. Les tangentes sont orthogonales → métrique diagonale : $g_{\phi\phi}=\sin^2\theta$, $g_{\theta\theta}=1$, donc $\sqrt{|g|}=\sin\theta$. On injecte dans la formule :

$$\Delta u = \frac{1}{\sin^2\theta}\frac{\partial^2 u}{\partial\phi^2} + \frac{1}{\sin\theta}\frac{\partial}{\partial\theta}\!\left(\sin\theta\frac{\partial u}{\partial\theta}\right)$$

Les deux pièces : le $1/\sin^2\theta$ devant $\partial_\phi^2$ vient de $g^{\phi\phi}=1/\sin^2\theta$ (les méridiens se resserrent aux pôles) ; le facteur $\sin\theta$ **dans** la dérivée $\theta$ est l'élément d'aire qui rend l'opérateur conservatif. On garde cette forme conservative car elle se discrétise avec des poids positifs partout et gère bien les pôles. **Torus** : même formule, $\sin\theta$ remplacé par $D(\theta)=R+r\cos\theta$ (distance à l'axe).

**Solveur = itération de Jacobi** sur une grille $256\times128$ en $(\phi,\theta)$ : chaque frame, une passe où chaque cellule devient la moyenne pondérée de ses 4 voisins (cellules contraintes restées figées). Self-feedback de BufferA → la relaxation accumule. Stencil 5 points :

$$u_{i,j} = \frac{w_\phi(u_E+u_W) + w_N u_N + w_S u_S}{2w_\phi + w_N + w_S}$$

Dérivation (4 étapes) : multiplier par le facteur métrique pour virer le $1/\sin\theta$ → différences centrales sur $\phi$ → divergence de flux (différences finies-volumes) sur $\theta$ → assembler, poser $=0$, résoudre pour la cellule centrale. Le torus suit la même recette avec $D(\theta)=R+r\cos\theta$ à la place de $\sin\theta$ (métrique bornée → bien conditionnée, périodique sur les 2 axes).

## Intersection rayon-surface
- **Sphère** : forme close. $\|P-C\|^2=r^2$ → quadratique en $t$, $t=-b\pm\sqrt{b^2-c}$ (half-b), plus petite racine positive. Normale $=(P-C)/r$.
- **Torus** : SDF + sphere tracing (le quartique exact est fragile en float → artefacts gris).

$$\text{SDF}(P) = \sqrt{\bigl(\sqrt{P_x^2+P_y^2}-R\bigr)^2 + P_z^2} - r$$

Sphere tracing : on avance de $\text{SDF}(P)$ à chaque pas (ne peut pas dépasser). Bounding sphere $R+r$ pour early-out.

## Choix / alternatives
- **Bilinéaire manuelle** dans Image : besoin de wrap par axe ($\phi$ wrap, $\theta$ clamp aux pôles).
- **Walk on Spheres** (Monte Carlo, exact, sans maillage) : abandonné, trop bruité en temps réel.
- **Multigrid** : accélérerait la convergence (résoudre en basse résolution d'abord), skippé pour la complexité.

---

# 4. OpenGL — l'essentiel

OpenGL = **machine à états géante** + driver qui gère mémoire/sync/état en cachette.

## Pipeline
```
Vertex data → Vertex shader (transform position) → [Geometry shader]
→ assemblage primitives + clipping → Rasterization (→ fragments)
→ Fragment shader (couleur) → stencil → depth → blending → Framebuffer
```

## Objets buffers
- **VBO** (Vertex Buffer Object) : stocke les vertices.
- **EBO** (Element Buffer Object) : stocke les indices (réutilise les vertices partagés).
- **VAO** (Vertex Array Object) : stocke la config (`VertexAttribPointer` + bindings VBO/EBO) → un seul bind au draw.
- `glVertexAttribPointer(loc, size, type, normalized, stride, offset)` dit comment lire le buffer.

## Pipeline coordonnées (MVP)
$$V_\text{clip} = M_\text{proj} \cdot M_\text{view} \cdot M_\text{model} \cdot V_\text{local}$$
local → world (model) → view (view) → clip (projection) → NDC ([-1,1], perspective divide) → screen. Pas de caméra en OpenGL : la view matrix bouge **tout le monde** dans l'autre sens. **Matrices lues de droite à gauche** (la dernière écrite s'applique en premier). Normales : `mat3(transpose(inverse(model)))` (correct sous scaling non uniforme).

## Concepts clés
- **Uniforms** : variables globales constantes pour un draw call. **UBO** (std140) les partage entre programmes.
- **Textures** : `ActiveTexture(unit)` + `BindTexture`. Wrapping (REPEAT/CLAMP), filtering (NEAREST/LINEAR), **mipmaps** (versions /2 /4… pour objets lointains : évite artefacts + cache misses).
- **Depth test** : z-buffer, jette les fragments cachés. Précision non linéaire → **z-fighting**.
- **Stencil** : masque 8-bit par pixel (outlines, mirrors, portals).
- **Blending** : transparence, $C = \alpha_\text{src}C_\text{src}+(1-\alpha_\text{src})C_\text{dst}$. Dessiner opaque d'abord, transparent **trié far→near**.
- **Face culling** : skip les triangles de dos (~50% de fragments en moins). Winding CCW = front par défaut.
- **Framebuffer (FBO)** : render-to-texture → post-processing, mirrors, shadow maps. Renderbuffer = attachement write-only (plus rapide qu'une texture si jamais samplé).
- **Cubemap** : 6 faces, samplée par une direction 3D → skybox, réflexions d'environnement.
- **Instancing** : `DrawArraysInstanced` dessine le même mesh N fois en un call (supprime l'overhead CPU→GPU par draw).
- **MSAA** : depth/stencil testés à N points par pixel, fragment shader 1× → bords lisses.

## Phong (avant PBR)
`ambient` (base constante) + `diffuse` ($\propto$ angle normale/lumière) + `specular` (highlight, dépend de la vue). Light casters : directional (soleil, pas d'atténuation), point (atténuation $1/(K_c+K_l d+K_q d^2)$), spotlight (cône).

---

# 5. Vulkan — l'essentiel

> Partout où Vulkan est verbeux, il **expose ce qu'OpenGL faisait en cachette** : perf prévisible, command generation multithread, ~1000 lignes pour un triangle.

## Baseline Vulkan 1.3 (4 features à activer, chacune tue du boilerplate)
- **dynamicRendering** : plus de render pass / framebuffer objects, on décrit les attachments au draw.
- **bufferDeviceAddress (BDA)** : les buffers deviennent des pointeurs 64-bit dans le shader, pas de descriptors pour les buffers.
- **descriptorIndexing** : un gros tableau bindless de textures, pas de descriptor set par matériau.
- **synchronization2** : API de barrières plus propre.

## Hiérarchie
`Instance` (connexion au loader) → `PhysicalDevice` (un GPU) → `Device` (ton "contexte") → `Queue` (où on soumet le travail), `Swapchain` (ring d'images que le compositeur lit), `CommandPool`/`CommandBuffer`, `Pipeline` (état figé), sync objects.

## Étapes d'initialisation (one-time setup, dans l'ordre)
1. **Instance** `vkCreateInstance` : app info + extensions (surface, debug) + validation layers.
2. **Surface** `SDL_Vulkan_CreateSurface` : lien fenêtre ↔ Vulkan (par-OS, géré par SDL).
3. **PhysicalDevice** : `vkEnumeratePhysicalDevices` → choisir un GPU (discrete, supporte 1.3 + les queues voulues).
4. **Queue families** : trouver une famille graphics+present (souvent family 0 sur desktop).
5. **Device (logique)** `vkCreateDevice` : queues voulues + extensions (`VK_KHR_swapchain`) + **chaîne de features 1.2/1.3** (dynamicRendering, BDA, descriptorIndexing, sync2, scalarBlockLayout).
6. **VMA allocator** : créé sur le device, gère toute la mémoire.
7. **Swapchain** `vkCreateSwapchainKHR` : ring d'images presentables (present mode FIFO pour démarrer) + leurs `ImageView`.
8. **Depth image** + view (via VMA, DEVICE_LOCAL).
9. **Descriptor set layout + pool + set** : décrit l'interface (le gros tableau bindless de textures), rempli une fois.
10. **Pipeline layout** : descriptor set layouts + ranges de push constants.
11. **Shaders** : charger les blobs SPIR-V → `vkCreateShaderModule`.
12. **Graphics pipeline** `vkCreateGraphicsPipelines` : fige tout l'état (voir ci-dessous).
13. **Per-frame** (×2) : command buffers, uniform buffers (HOST_VISIBLE mappés), fences, semaphores.
14. **Upload** vertex/index buffers + textures via **staging buffer** (HOST_VISIBLE → copy → DEVICE_LOCAL).

## Le graphics pipeline fige (état immuable)
Vertex input layout + input assembly (topology) · stages shaders · rasterization (cull, polygon mode) · multisample · depth/stencil · blend · pipeline layout · **formats d'attachments** (remplace le render pass via dynamic rendering). Blend ou format différent = **autre pipeline** → un vrai moteur en a des centaines (d'où le pipeline cache). Seuls viewport + scissor sont dynamiques sans recréer.

## Concepts clés
- **Mémoire (VMA)** : `DEVICE_LOCAL` (VRAM, meshes/textures via staging buffer) vs `HOST_VISIBLE|HOST_COHERENT` (uniforms par frame, CPU mappe et memcpy).
- **Image layouts** : chaque `VkImage` a un layout (UNDEFINED, ATTACHMENT_OPTIMAL, SHADER_READ_ONLY_OPTIMAL, TRANSFER_DST, PRESENT_SRC). Les transitions disent au driver de réorganiser les texels. **Oubli de transition = bug #1** "marche sur mon GPU, casse sur le tien".
- **Synchronisation**, 3 primitives :
  - **Fence** : GPU signale CPU ("frame N-2 finie ?"). Throttle du render loop.
  - **Semaphore** : GPU signale GPU (gate la présentation).
  - **Pipeline barrier** : ordonne le travail *dans* un command buffer + fait les transitions de layout (srcStage/srcAccess → dstStage/dstAccess).
- **Buffers + BDA** : `vkGetBufferDeviceAddress` → adresse passée en push constant, déréférencée comme un pointeur C. ⚠ layouts CPU/GPU doivent matcher → `scalarBlockLayout`.
- **Descriptors** (interface des ressources shader) : DescriptorSetLayout / Pool / Set. Avec BDA, n'utiles que pour les **textures** (bindless : `textures[NonUniformResourceIndex(idx)]`).
- **Pipeline** : objet immuable figeant tout (shaders, blend, depth, cull, formats). Blend différent = pipeline différent → des centaines (pipeline cache). Dynamique sans recréer : viewport, scissor.
- **Command buffers** : `vkCmd*` **enregistre**, n'exécute pas. Exécution après `vkQueueSubmit`. Jamais ré-enregistrer un buffer Pending (fence le garantit).
- **Shaders** : SPIR-V (binaire), généré depuis **Slang** (toutes les stages dans un fichier, pointeurs first-class pour BDA, emit SPIR-V/GLSL/Metal/CUDA).
- **Frames in flight** : pendant que le GPU rend la frame N, le CPU enregistre N+1. `maxFramesInFlight = 2` (sweet spot). Dupliquer par frame : command buffers, uniform buffers, fences, present semaphores. Pas dupliquer : depth, textures, vertex/index buffers, pipelines.
- **imageIndex ≠ frameIndex** : nombre d'images swapchain (choix du driver) ≠ frames in flight (ton choix). Indexer les ressources per-image par imageIndex, per-frame par frameIndex.

## Render loop
```
wait fence[frame] → acquire image (signal presentSem) → update/memcpy uniforms
→ record CB (barrier UNDEFINED→ATTACHMENT, beginRendering, draw, barrier →PRESENT_SRC)
→ submit (wait presentSem, signal renderSem + fence) → present (wait renderSem)
```
**RenderDoc** quand validation clean mais rendu faux (bug logique : mauvaise matrice, offset d'attribut).

---

# 6. GPGPU / CUDA

CPU = peu de cœurs latence-faible/branchy ; GPU = milliers de cœurs throughput sur du **data-parallel** (même op sur plein d'éléments).

## Modèle
- **Kernel** : fonction `__global__`, lancée depuis le host (CPU), exécutée par plein de threads sur le device (GPU). Syntaxe `kernel<<<gridDim, blockDim>>>(args)`.
- **Hiérarchie threads** :
  - **Thread** : une instance du kernel.
  - **Block** : threads sur le *même* SM, coopèrent via shared memory + `__syncthreads()`. Max 1024.
  - **Grid** : tous les blocks d'un lancement (les blocks ne communiquent pas entre eux).
  - **Warp** : 32 threads en lockstep (SIMT) — l'unité réelle du scheduler. Block size = multiple de 32.
- Index d'un thread : `int i = blockIdx.x * blockDim.x + threadIdx.x;`

## Mémoire
| Mémoire | Scope | Vitesse |
|---|---|---|
| Registers | thread | + rapide |
| Shared (`__shared__`) | block | très rapide (on-chip) |
| Global (`cudaMalloc`) | device | lente (DRAM) |
| Constant/texture | device, cachée | rapide si réutilisée |

Host/device = **espaces séparés**, on copie explicitement avec `cudaMemcpy(dst, src, bytes, dir)`. Ce transfert PCIe est souvent **le** bottleneck → CUDA paye sur grosses données réutilisées, pas sur petits arrays one-off.

Cycle : `cudaMalloc` → memcpy H→D → kernel → memcpy D→H → `cudaFree`.

## Exemple complet : vector add (C = A + B)
```cuda
// KERNEL : tourne sur le GPU, un thread par élément (grid-stride loop)
__global__ void vecAdd(const float* A, const float* B, float* C, int n) {
    int i = blockIdx.x * blockDim.x + threadIdx.x;   // index global du thread
    for (; i < n; i += blockDim.x * gridDim.x)        // stride : couvre n même si threads < n
        C[i] = A[i] + B[i];
}

int main() {
    int n = 1 << 20;  size_t bytes = n * sizeof(float);
    float *hA = ..., *hB = ..., *hC = malloc(bytes);   // buffers host (CPU)

    float *dA, *dB, *dC;                               // 1. allouer sur le device
    cudaMalloc(&dA, bytes); cudaMalloc(&dB, bytes); cudaMalloc(&dC, bytes);

    cudaMemcpy(dA, hA, bytes, cudaMemcpyHostToDevice); // 2. copier H→D
    cudaMemcpy(dB, hB, bytes, cudaMemcpyHostToDevice);

    int threads = 256;                                 // multiple de 32 (warp)
    int blocks  = (n + threads - 1) / threads;         // ceil(n/threads)
    vecAdd<<<blocks, threads>>>(dA, dB, dC, n);        // 3. lancer le kernel
    cudaDeviceSynchronize();                           //    attendre la fin (async sinon)

    cudaMemcpy(hC, dC, bytes, cudaMemcpyDeviceToHost); // 4. copier D→H
    cudaFree(dA); cudaFree(dB); cudaFree(dC);          // 5. libérer
}
```

## Reduction tree (somme par block en shared memory)
```cuda
__global__ void reduce(const float* in, float* out, int n) {
    __shared__ float s[256];                           // une copie par block
    int tid = threadIdx.x;
    int i   = blockIdx.x * blockDim.x + tid;
    s[tid] = (i < n) ? in[i] : 0.0f;
    __syncthreads();                                   // tous ont écrit avant de lire

    for (int stride = blockDim.x / 2; stride > 0; stride >>= 1) {
        if (tid < stride) s[tid] += s[tid + stride];   // arbre : moitié actifs à chaque étape
        __syncthreads();                               // barrière entre niveaux (sinon race)
    }
    if (tid == 0) atomicAdd(out, s[0]);                // thread 0 ajoute le partiel du block
}
```
`log2(blockDim)` étapes. `__syncthreads()` obligatoire entre niveaux sinon un thread lit avant qu'un autre ait écrit. Brique de base des reductions/histogrammes/dot products.

---

# 7. Algèbre linéaire (rappel express)

- **Matrice = transformation de l'espace**, pas une grille de nombres. Ses **colonnes = où atterrissent les vecteurs de base**. Produit matrice-vecteur = combinaison linéaire des colonnes.
- **Produit de matrices = composition**, appliquée **droite→gauche** ($M_2M_1$ = $M_1$ puis $M_2$). Non commutatif.
- **Déterminant** = facteur d'échelle des aires/volumes. $\det=0$ → espace écrasé en dimension inférieure (colonnes dépendantes, non inversible). $\det<0$ → orientation inversée.
- **Inverse** $A^{-1}$ annule $A$ ; existe ssi $\det\neq 0$. Résout $A\vec{x}=\vec{v}$.
- **Rank** = dimension du column space = nb de colonnes indépendantes. **Kernel** = vecteurs envoyés sur $\vec{0}$.
- **Dot product** : $\vec{v}\cdot\vec{w}=|\vec{v}||\vec{w}|\cos\theta$. $=0$ → orthogonaux. (Dotter avec $\vec{v}$ = appliquer la matrice ligne $[v_x\,v_y]$ : dualité.)
- **Cross product** : perpendiculaire aux deux, longueur = aire du parallélogramme. Antisymétrique.
- **Change of basis** : $B^{-1}AB$ = la même transformation vue depuis une autre base.
- **Eigenvector** : reste sur sa propre droite ($A\vec{v}=\lambda\vec{v}$) ; **eigenvalue** $\lambda$ = facteur d'étirement. Résoudre $\det(A-\lambda I)=0$. (Axe d'une rotation 3D = eigenvector avec $\lambda=1$.)
- **SVD** : $A=U\Sigma V^T$ = rotate → scale → rotate. Tronquer = meilleure approximation rank-$k$ (compression).

---

# 8. C++ — rappel express

## `.hpp` (déclaration) vs `.cpp` (définition)
Le header dit **ce qui existe** (l'interface, inclus par les autres fichiers) ; le `.cpp` dit **comment ça marche** (l'implémentation, compilé une fois). Séparer = compilation plus rapide (changer un `.cpp` ne recompile pas les fichiers qui incluent le `.hpp`) et pas de définition dupliquée à l'édition de liens.

```cpp
// mesh.hpp — l'interface
#pragma once                 // évite la double-inclusion
#include <vector>

class Mesh {
public:
    Mesh(std::vector<float> verts);  // déclaré seulement
    void draw() const;
    int  vertexCount() const { return count_; }  // petit corps inline OK dans le header
private:
    std::vector<float> verts_;
    int count_;
};
```
```cpp
// mesh.cpp — l'implémentation
#include "mesh.hpp"

Mesh::Mesh(std::vector<float> verts)   // Mesh:: = "ce membre de la classe Mesh"
    : verts_(std::move(verts)),        // liste d'initialisation (construit les membres)
      count_(verts_.size() / 3) {}

void Mesh::draw() const { /* ... */ }
```

## Structures disponibles
- **`struct`** : membres **public** par défaut. Convention : données simples (POD), pas de logique.
- **`class`** : membres **private** par défaut. Convention : invariants + méthodes. (Seule vraie différence avec struct = défaut d'accès.)
- **`enum class`** : énumération typée, scopée (`Backend::Vulkan`, pas de collision de noms).
- **`union`** : un seul champ actif à la fois (rare, bas niveau).
- **`namespace`** : groupe des noms (`overdrive::Mesh`) pour éviter les collisions.

```cpp
struct Vertex { glm::vec3 pos, normal; glm::vec2 uv; };  // POD, tout public
enum class Backend { OpenGL, Vulkan };
```

## Mémoire & ownership (l'essentiel moderne)
- **Stack** (auto) : `Mesh m(...)` — détruit en sortie de scope (RAII). À préférer.
- **Heap** : jamais de `new`/`delete` brut → **smart pointers** :
  - `std::unique_ptr<T>` : ownership **unique**, libéré automatiquement. `auto b = std::make_unique<VKBackend>();`
  - `std::shared_ptr<T>` : ownership **partagé** (compteur de références).
- **RAII** : la ressource (mémoire, fichier, handle GPU) est libérée par le destructeur → pas de fuite même en cas d'exception.
- **Références `T&`** : alias non-null, pas de copie. `const T&` pour passer un gros objet en lecture sans le copier.

## Polymorphisme (utilisé par le renderer abstrait)
```cpp
class Backend {                          // interface abstraite (.hpp)
public:
    virtual ~Backend() = default;
    virtual void beginFrame() = 0;       // = 0 → méthode pure, pas d'impl
};
class VKBackend : public Backend {       // implémentation concrète
    void beginFrame() override { /* Vulkan */ }
};
std::unique_ptr<Backend> b = std::make_unique<VKBackend>();
b->beginFrame();                          // appel résolu à l'exécution (vtable)
```
C'est exactement le pattern du moteur : `scene/` parle à `Backend`/`Shader` abstraits, `opengl/` et `vulkan/` les implémentent.
</content>
</invoke>
