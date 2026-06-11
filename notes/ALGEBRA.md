# Vectors

A vector is an arrow rooted at the origin, fully described by its coordinates. The coordinates are *instructions*: how far to walk along each axis to get from the origin to the tip

$\vec{v} = \begin{bmatrix} x \\ y \end{bmatrix}$ means: walk $x$ along $\hat{i}$, then $y$ along $\hat{j}$

In 3D, add a $z$-axis perpendicular to both and a third unit vector $\hat{k}$: every ordered triplet picks out exactly one arrow in space, and every arrow has exactly one triplet

`Vector addition` chain the arrows tip to tail

`Scalar multiplication` stretch ($|s| > 1$), squish ($|s| < 1$) or flip ($s < 0$) the arrow

> "Scalar" and "scaling" are the same idea: numbers exist to stretch arrows

# Span, basis, linear independence

`Linear combination` $a\vec{v} + b\vec{w}$ with $a, b$ scalars — scale each vector, then add

`Span` set of all linear combinations of a set of vectors = everything you can *reach* using only addition and scaling

`Basis` set of linearly independent vectors that span the full space

A set of vectors is linearly `dependent` if there is a nontrivial linear combination of the vectors that equals $\vec{0}$ — one vector is redundant, it already lives in the span of the others and adds no new dimension

`Independent` each vector genuinely adds a dimension to the span

> Two vectors in 2D span the whole plane unless they are aligned (dependent), then they span a line. In 3D: two independent vectors span a plane through the origin; a third adds the full space only if it leaves that plane

# Linear transformations

A transformation is a function on vectors: input arrow in, output arrow out. It is `linear` if all lines remain lines (nothing curves), grid lines stay parallel and evenly spaced, and the origin stays fixed

The whole transformation is fully described by where it sends the basis vectors — every other vector is just a linear combination of them, and linearity preserves that combination. Those landing spots are the **columns of the matrix**

$\begin{bmatrix} a & b \\ c & d \end{bmatrix}$ means $\hat{i} \rightarrow \begin{bmatrix} a \\ c \end{bmatrix}$ and $\hat{j} \rightarrow \begin{bmatrix} b \\ d \end{bmatrix}$

Matrix-vector product = applying the transformation:

$\begin{bmatrix} a & b \\ c & d \end{bmatrix} \begin{bmatrix} x \\ y \end{bmatrix} = x \begin{bmatrix} a \\ c \end{bmatrix} + y \begin{bmatrix} b \\ d \end{bmatrix}$

> Read it as: the output is a linear combination of the columns, weighted by the input coordinates. A matrix is not a grid of numbers — it *is* a transformation of space; multiplying by it is performing the transformation

Exactly the same in 3D: three basis vectors $\hat{i}, \hat{j}, \hat{k}$, a 3×3 matrix whose three columns are their landing spots, and the product is the three-term combination

## Common 2D transformations

$\begin{bmatrix} 0 & -1 \\ 1 & 0 \end{bmatrix}$ rotation 90° counterclockwise ($\hat{i} \rightarrow \begin{bmatrix} 0 \\ 1 \end{bmatrix}$, $\hat{j} \rightarrow \begin{bmatrix} -1 \\ 0 \end{bmatrix}$)

$\begin{bmatrix} 1 & 1 \\ 0 & 1 \end{bmatrix}$ shear ($\hat{j}$ tilts right, $\hat{i}$ stays) — turns the unit square into a parallelogram of the *same area*

$\begin{bmatrix} s & 0 \\ 0 & s \end{bmatrix}$ uniform scale by $s$

# Matrix multiplication

`Matrix product` = composition of transformations, **applied right to left** (like function notation: $f(g(x))$)

$M_2 M_1$ means: first apply $M_1$, then apply $M_2$ — and the composition is itself one linear transformation

> Order matters: $M_2 M_1 \neq M_1 M_2$ in general (rotate-then-shear ≠ shear-then-rotate). Matrix multiplication is **not commutative**

Composition is associative: $(AB)C = A(BC)$ — both sides apply the same three transformations in the same order, the parentheses change nothing

Computation: column $i$ of $M_2 M_1$ = $M_2$ applied to column $i$ of $M_1$ — i.e. track where each basis vector lands after the first transformation, then feed that landing spot through the second

# Determinant

`Determinant` factor by which the transformation scales areas (2D) or volumes (3D)

> Scale by 2 along x and 3 along y → every area gets multiplied by 6 → $det = 6$. A shear distorts shapes but preserves area → $det = 1$

$det(A) = 0$ the transformation squishes space into a lower dimension (plane → line or point), columns are linearly dependent

$det(A) < 0$ orientation is flipped; the absolute value still scales area

- 2D: the plane is mirrored ($\hat{j}$ ends up *clockwise* from $\hat{i}$ instead of counterclockwise)
- 3D: right-handed becomes left-handed. Right-hand test: forefinger along $\hat{i}$, middle finger along $\hat{j}$, thumb along $\hat{k}$ — if after the transformation you need your *left* hand to do this, $det < 0$

$det(AB) = det(A)\,det(B)$

> Obvious once read geometrically: scaling areas by $det(B)$ and then by $det(A)$ scales them by the product. The algebraic proof is painful; the geometric one is one sentence

## Computation

2×2: $det\begin{bmatrix} a & b \\ c & d \end{bmatrix} = ad - bc$

> $ad$ = scaled unit square area, $bc$ = how much it gets sheared away

n×n: expand along a row, alternate signs, recurse on minors:

$det\begin{bmatrix} a & b & c \\ d & e & f \\ g & h & i \end{bmatrix} = a\,det\begin{bmatrix} e & f \\ h & i \end{bmatrix} - b\,det\begin{bmatrix} d & f \\ g & i \end{bmatrix} + c\,det\begin{bmatrix} d & e \\ g & h \end{bmatrix}$

# Linear systems, inverse matrix

A system of linear equations — variables on the left, constants on the right — is one matrix equation:

$A\vec{x} = \vec{v}$

> Geometric reading: find the vector $\vec{x}$ that *lands on* $\vec{v}$ after the transformation $A$. Solving a system = playing a transformation backwards

`Inverse` $A^{-1}$ the transformation that undoes $A$: $A^{-1}A = I$ (the do-nothing transformation, $\hat{i}$ and $\hat{j}$ stay put)

Rotation⁻¹ = rotate the other way; shear⁻¹ = shear the other way

Solve $A\vec{x} = \vec{v}$ by $\vec{x} = A^{-1}\vec{v}$

    The inverse exists iff det(A) ≠ 0

> If $det(A) = 0$, space was squished into a lower dimension: you cannot un-squish a line back into a plane with a *function* — a function must send each input to a single output, but un-squishing would send one point to a whole line of candidates

Even when $det(A) = 0$, solutions can still exist — exactly when $\vec{v}$ happens to lie in the column space (you're lucky: the squished space passes through your target)

## Computation

Start with the augmented matrix $[A \mid I]$, reduce the left part to $I$ with Gaussian elimination, the right part becomes $A^{-1}$

`Gaussian elimination` allowed row operations (each is reversible and preserves the solution set):
- Swap two rows
- Multiply a row by a nonzero number
- Add a multiple of one row to another row

`Row echelon form` the staircase shape elimination aims for:
- all-zero rows at the bottom
- each row's leading nonzero entry (`pivot`) is strictly to the right of the pivot above
- everything below a pivot is zero

# Rank, column space, kernel

`Column space` span of the columns = set of all possible outputs $A\vec{v}$

> The columns are where the basis vectors land; their span is everywhere *anything* can land

`Rank` dimension of the column space = number of linearly independent columns

> Full rank = rank equals input dimension = nothing gets squished = invertible (if square)

`Kernel` (null space) set of all vectors mapped to $\vec{0}$

> When the transformation squishes dimensions, a whole line/plane of vectors lands on the origin: that is the kernel. Full rank ⇒ kernel = $\{\vec{0}\}$

Connection to systems: for $A\vec{x} = \vec{0}$, the kernel *is* the solution set. For $A\vec{x} = \vec{v}$ with $\vec{v}$ in the column space, the solutions are one particular solution plus anything in the kernel

Compute rank: reduce with Gaussian elimination to row echelon form, count pivots

## Nonsquare matrices

Columns = input dimensions, rows = output dimensions:

A 3×2 matrix maps 2D → 3D (2 columns = 2 input basis vectors, 3 rows = 3 output coordinates). Its column space is a 2D plane slicing through the origin of 3D space — still "full rank", since rank equals the *input* dimension

A 2×3 matrix maps 3D → 2D (projection-like, necessarily has a nontrivial kernel)

A 1×2 matrix $[a\ \ b]$ maps 2D → the number line: $\hat{i} \rightarrow a$, $\hat{j} \rightarrow b$. This is the bridge to the dot product

# Dot product

$\vec{v} \cdot \vec{w} = v_x w_x + v_y w_y + \dots = |\vec{v}|\,|\vec{w}|\cos\theta$

- $> 0$ pointing the same general direction
- $= 0$ perpendicular (`orthogonal` = 90° angle)
- $< 0$ pointing away from each other

Geometric reading: length of the projection of $\vec{w}$ onto $\vec{v}$, times $|\vec{v}|$

Properties: symmetric ($\vec{v} \cdot \vec{w} = \vec{w} \cdot \vec{v}$ — not obvious from the projection picture, but the projection can be read either way), and linear in each argument ($(k\vec{v}) \cdot \vec{w} = k(\vec{v} \cdot \vec{w})$)

> `Duality`: dotting with $\vec{v}$ is the same as applying the 1×n matrix $[v_x\ v_y]$ — a linear map to the number line. Every linear transformation to the number line secretly *is* some vector $\vec{v}$ lying down as a matrix row, and conversely every vector defines one. Vector ⟷ "measure along me"

# Cross product

$\vec{v} \times \vec{w}$ vector perpendicular to both, length = area of the parallelogram they span

Direction given by the right-hand rule ($\hat{i} \times \hat{j} = \hat{k}$)

$\vec{v} \times \vec{w} = -(\vec{w} \times \vec{v})$ antisymmetric

## Computation

$\begin{bmatrix} v_1 \\ v_2 \\ v_3 \end{bmatrix} \times \begin{bmatrix} w_1 \\ w_2 \\ w_3 \end{bmatrix} = \begin{bmatrix} v_2 w_3 - v_3 w_2 \\ v_3 w_1 - v_1 w_3 \\ v_1 w_2 - v_2 w_1 \end{bmatrix}$

Mnemonic: $det\begin{bmatrix} \hat{i} & v_1 & w_1 \\ \hat{j} & v_2 & w_2 \\ \hat{k} & v_3 & w_3 \end{bmatrix}$

> The determinant connection is no accident: the cross product's component along any $\vec{p}$ measures the volume of the parallelepiped spanned by $\vec{p}, \vec{v}, \vec{w}$ — duality again: "volume with these two vectors" is a linear map to the number line, so it must *be* some vector. That vector is the cross product

# Change of basis

A basis is a choice of language for describing vectors; the same vector has different coordinates in different bases

To translate **from** basis $\vec{b_1} = \begin{bmatrix} 3 \\ 0 \end{bmatrix}$, $\vec{b_2} = \begin{bmatrix} 1 \\ 2 \end{bmatrix}$ **to** our standard basis, multiply by the matrix whose columns are the new basis vectors:

$B = \begin{bmatrix} 3 & 1 \\ 0 & 2 \end{bmatrix}$

$B^{-1}$ translates the other way (our language → their language)

Apply a transformation $A$ (expressed in our basis) to a vector expressed in their basis:

$B^{-1} A B$

> Read right to left: translate to our language, transform, translate back. An expression like $B^{-1}AB$ is "the same transformation, seen from another basis"

# Eigenvectors and eigenvalues

`Eigenvectors` vectors that stay on their own span (same line through origin) through a transformation

`Eigenvalues` factor by which the eigenvector is stretched ($\lambda < 0$ = flipped)

> Example: a 3D rotation's eigenvector with $\lambda = 1$ is its rotation axis

## Computation

$A\vec{v} = \lambda\vec{v}$ with $A$ the **transformation matrix**, $\vec{v}$ an **eigenvector**, $\lambda$ the associated **eigenvalue**

$\Rightarrow (A - \lambda I)\vec{v} = \vec{0}$

A nonzero $\vec{v}$ mapped to $\vec{0}$ means $(A - \lambda I)$ squishes space: it must be `singular` (no inverse)

$\Rightarrow det(A - \lambda I) = 0$ solve this polynomial for $\lambda$

> If $(A - \lambda I)$ were invertible, then $(A - \lambda I)^{-1}(A - \lambda I)\vec{v} = \vec{0} \Rightarrow \vec{v} = \vec{0}$: only the trivial solution

Then for each $\lambda$, solve $(A - \lambda I)\vec{v} = \vec{0}$ for the eigenvectors (= kernel of $A - \lambda I$)

> Not every real matrix has real eigenvalues: a 90° rotation has none ($\lambda = \pm i$, no vector stays on its span)

## Diagonalization (eigenbasis)

If the eigenvectors span the space, change basis to them: $E^{-1} A E$ is **diagonal**, with eigenvalues on the diagonal

> Diagonal matrices are trivial to work with: $D^n$ = raise each diagonal entry to the $n$. To compute $A^{100}$: $A^{100} = E D^{100} E^{-1}$

# SVD

Singular Value Decomposition: any matrix $A = U \Sigma V^T$

- $U$ orthogonal, columns $\vec{u_i}$ = eigenvectors of $AA^T$ ($U^TU = I$)
- $V$ orthogonal, columns $\vec{v_i}$ = eigenvectors of $A^TA$ ($V^TV = I$)
- $\Sigma$ diagonal, entries $\sigma_i$ = `singular values` = square roots of the eigenvalues of $AA^T$ (or $A^TA$)

> Geometric reading: every transformation, however messy, is rotate → scale along axes → rotate

Equivalent reading: sum of rank-1 matrices, ordered by importance:

$A = \sum_i \sigma_i\, \vec{u_i} \vec{v_i}^T$

> Truncating the sum after $k$ terms gives the best rank-$k$ approximation of $A$ = lossy compression. "Data-driven generalization of the Fourier transform"

# Misc

`Permutation matrix` square binary matrix with exactly one 1 in each row and each column; multiplying with it permutes rows

`Degenerate (singular) matrix` example: every element in a row is the same → squishes plane to a line, $det = 0$

    If A is a square matrix with linearly dependent columns, then A is not invertible
