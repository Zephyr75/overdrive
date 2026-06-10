# Vectors

A vector is an arrow rooted at the origin, fully described by its coordinates

$\vec{v} = \begin{bmatrix} x \\ y \end{bmatrix}$ means: walk $x$ along $\hat{i}$, then $y$ along $\hat{j}$

`Vector addition` chain the arrows tip to tail

`Scalar multiplication` stretch ($|s| > 1$), squish ($|s| < 1$) or flip ($s < 0$) the arrow

# Span, basis, linear independence

`Linear combination` $a\vec{v} + b\vec{w}$ with $a, b$ scalars

`Span` set of all linear combinations of a set of vectors

`Basis` set of linearly independent vectors that span the full space

A set of vectors is linearly `dependent` if there is a nontrivial linear combination of the vectors that equals $\vec{0}$ (one vector is redundant, it lives in the span of the others)

> Two vectors in 2D span the whole plane unless they are aligned (dependent), then they span a line

# Linear transformations

A transformation is `linear` if grid lines remain parallel and evenly spaced, and the origin stays fixed

A linear transformation is fully described by where it sends the basis vectors. Those landing spots are the **columns of the matrix**

$\begin{bmatrix} a & b \\ c & d \end{bmatrix}$ means $\hat{i} \rightarrow \begin{bmatrix} a \\ c \end{bmatrix}$ and $\hat{j} \rightarrow \begin{bmatrix} b \\ d \end{bmatrix}$

Matrix-vector product = applying the transformation:

$\begin{bmatrix} a & b \\ c & d \end{bmatrix} \begin{bmatrix} x \\ y \end{bmatrix} = x \begin{bmatrix} a \\ c \end{bmatrix} + y \begin{bmatrix} b \\ d \end{bmatrix}$

> Read it as: the output is a linear combination of the columns, weighted by the input coordinates

## Common 2D transformations

$\begin{bmatrix} 0 & -1 \\ 1 & 0 \end{bmatrix}$ rotation 90° counterclockwise

$\begin{bmatrix} 1 & 1 \\ 0 & 1 \end{bmatrix}$ shear ($\hat{j}$ tilts right, $\hat{i}$ stays)

$\begin{bmatrix} s & 0 \\ 0 & s \end{bmatrix}$ uniform scale by $s$

# Matrix multiplication

`Matrix product` = composition of transformations, **applied right to left**

$M_2 M_1$ means: first apply $M_1$, then apply $M_2$

> Order matters: $M_2 M_1 \neq M_1 M_2$ in general (rotate-then-shear ≠ shear-then-rotate)

Composition is associative: $(AB)C = A(BC)$ (applying the same transformations in the same order)

Computation: column $i$ of $M_2 M_1$ = $M_2$ applied to column $i$ of $M_1$

# Determinant

`Determinant` factor by which the transformation scales areas (2D) or volumes (3D)

$det(A) = 0$ the transformation squishes space into a lower dimension (plane → line or point), columns are linearly dependent

$det(A) < 0$ orientation is flipped (2D: the plane is mirrored; 3D: right-handed becomes left-handed)

$det(AB) = det(A)\,det(B)$

## Computation

2×2: $det\begin{bmatrix} a & b \\ c & d \end{bmatrix} = ad - bc$

> $a d$ = scaled unit square area, $bc$ = how much it gets sheared away

n×n: expand along a row, alternate signs, recurse on minors:

$det\begin{bmatrix} a & b & c \\ d & e & f \\ g & h & i \end{bmatrix} = a\,det\begin{bmatrix} e & f \\ h & i \end{bmatrix} - b\,det\begin{bmatrix} d & f \\ g & i \end{bmatrix} + c\,det\begin{bmatrix} d & e \\ g & h \end{bmatrix}$

# Inverse matrix

`Inverse` $A^{-1}$ the transformation that undoes $A$: $A^{-1}A = I$

Solve $A\vec{x} = \vec{v}$ by $\vec{x} = A^{-1}\vec{v}$ (play the transformation backwards)

    The inverse exists iff det(A) ≠ 0

> If $det(A) = 0$, space was squished into a lower dimension: you cannot un-squish a line back into a plane with a function

## Computation

Start with the augmented matrix $[A \mid I]$, reduce the left part to $I$ with Gaussian elimination, the right part becomes $A^{-1}$

`Gaussian elimination` allowed row operations:
- Swap two rows
- Multiply a row by a nonzero number
- Add a multiple of one row to another row

# Rank, column space, kernel

`Column space` span of the columns = set of all possible outputs $A\vec{v}$

`Rank` dimension of the column space = number of linearly independent columns

> Full rank = rank equals input dimension = nothing gets squished = invertible (if square)

`Kernel` (null space) set of all vectors mapped to $\vec{0}$

> When the transformation squishes dimensions, a whole line/plane of vectors lands on the origin: that is the kernel. Full rank ⇒ kernel = $\{\vec{0}\}$

Compute rank: reduce with Gaussian elimination, count nonzero rows (pivots)

## Nonsquare matrices

A 3×2 matrix maps 2D → 3D (2 columns = 2 input basis vectors, 3 rows = 3 output coordinates)

A 2×3 matrix maps 3D → 2D (projection-like, necessarily has a nontrivial kernel)

# Dot product

$\vec{v} \cdot \vec{w} = v_x w_x + v_y w_y + \dots = |\vec{v}|\,|\vec{w}|\cos\theta$

- $> 0$ pointing the same general direction
- $= 0$ perpendicular (`orthogonal` = 90° angle)
- $< 0$ pointing away from each other

Geometric reading: length of the projection of $\vec{w}$ onto $\vec{v}$, times $|\vec{v}|$

> Duality: dotting with $\vec{v}$ is the same as applying the 1×n matrix $[v_x\ v_y]$, i.e. a linear map to the number line

# Cross product

$\vec{v} \times \vec{w}$ vector perpendicular to both, length = area of the parallelogram they span

Direction given by the right-hand rule ($\hat{i} \times \hat{j} = \hat{k}$)

$\vec{v} \times \vec{w} = -(\vec{w} \times \vec{v})$ antisymmetric

## Computation

$\begin{bmatrix} v_1 \\ v_2 \\ v_3 \end{bmatrix} \times \begin{bmatrix} w_1 \\ w_2 \\ w_3 \end{bmatrix} = \begin{bmatrix} v_2 w_3 - v_3 w_2 \\ v_3 w_1 - v_1 w_3 \\ v_1 w_2 - v_2 w_1 \end{bmatrix}$

Mnemonic: $det\begin{bmatrix} \hat{i} & v_1 & w_1 \\ \hat{j} & v_2 & w_2 \\ \hat{k} & v_3 & w_3 \end{bmatrix}$

> The determinant connection is no accident: the cross product's component along any $\vec{p}$ measures the volume of the parallelepiped spanned by $\vec{p}, \vec{v}, \vec{w}$

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

Equivalent reading: sum of rank-1 matrices, ordered by importance:

$A = \sum_i \sigma_i\, \vec{u_i} \vec{v_i}^T$

> Truncating the sum after $k$ terms gives the best rank-$k$ approximation of $A$ = lossy compression. "Data-driven generalization of the Fourier transform"

# Misc

`Permutation matrix` square binary matrix with exactly one 1 in each row and each column; multiplying with it permutes rows

`Degenerate (singular) matrix` example: every element in a row is the same → squishes plane to a line, $det = 0$

    If A is a square matrix with linearly dependent columns, then A is not invertible
