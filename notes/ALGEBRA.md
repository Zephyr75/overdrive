<!-- TODO: tout -->

Inverse matrix

Eigenvectors 

Eigenvalues

Cross-product

Determinant

Kernel

SVD

# Change of basis

To change to basis 
$\begin{bmatrix}
   3 \\ 0 
\end{bmatrix}$,
$\begin{bmatrix}
   1 \\ 2 
\end{bmatrix}$,
the transformation matrix is 
$\begin{bmatrix}
   3 & 1 \\ 0 & 2 
\end{bmatrix}$,

# Eigenvalues and eigenvectors

`Eigenvectors` are the vectors that remain on the same line when going through a linear transformation

`Eigenvalues` are the factors by which these vectors' length is multiplied

## Computation

$A\vec{v} = \lambda\vec{v}$ with $A$ the **transformation matrix**, $\vec{v}$ an **eigenvector** and $\lambda$ the associated **eigenvalue**

=> $(A - \lambda I)\vec{v} = \vec{0}$

> If $\vec{v}$ is the zero vector, we have a trivial solution

To get the non-zero eigenvectors, we need to solve the equation when $(A - \lambda I)$ is `singular` (does not have a multiplicative inverse), which means $det(A - \lambda I) = 0$

> If $(A - \lambda I)$ was invertible, we would have $(A - \lambda I)^{-1}(A - \lambda I)\vec{v} = 0 \rightarrow I\vec{v} = 0$ 

# Kernel

The `kernel` of a linear map is the linear subspace of the domain of the map which is mapped to the zero vector

# Definitions

A set of vectors is linearly `dependent` if there is a nontrivial linear combination of the vectors that equals 0

# Properties

    The inverse of a matrix exists iff the determinant is non-zero

    If A is a square matrix with linearly dependent columns, then A is not invertible


# Linear Algebra

Determinant defines the area spanned by the transformation vectors
Eigenvectors define axis that remain in place after applying transformation
Eigenvalues define how much stretching is applied to a given vector on this axis during the transformation
Compute eigenvalues :
$(A - \lambda I) v = 0$
<=> $det(A - \lambda I) = 0$
since we want the vector to stay on the same line and thus to generate an area of 0
Solve $A v = \lambda v$ to get the corresponding eigenvectors

Compute determinant :
2x2 => ad - bc with matrix ((a b)(c d))

> 2x2 =>
(a b c d) =>   a det ((f g h)(j k l)(n o p))
(e f g h)	 - b det ((e g h)(i k l)(m o p))
(i j k l)	 + c det ((e f h)(i j l)(m n p))
(m n o p)	 - d det ((e f g)(i j k)(m n o))
> 

Rank of A = maximal number of linearly independent columns of A

Gaussian elimination =

- Swapping two rows,
- Multiplying a row by a nonzero number,
- Adding a multiple of one row to another row. (subtraction can be achieved by multiplying one row with -1 and adding the result to another row)

Computing inverse of a matrix :
Start with A | I and convert the left part to I, the right part becomes I^{-1}

Compute matrix rank :
Reduce matrix with Gaussian elimination
Count independent columns

Permutation matrix = square binary matrix with exactly one entry of 1 in each row and each column and 0s elsewhere, it results in permuting the rows when mutilplied with another matrix

Degenerate matrix example :
Every element in a row is the same => turns plane to line

SVD : Data-Driven Generalization of the Fourier Transform
Sum of rank 1 matrices => incomplete sum = approximation = compression
U = Each column is Ui
E = Each diagonal element is singular value Ei
V = Each row is Vi
Sum all Ei*UixVi
UTU = I / VTV = I
U = eigenvectors of $AA^T$
V = eigenvectors of $A^TA$
E = square roots of eigenvalues of $AA^T$ or $A^TA$ = singular values of A

Orthogonal = making a 90 degrees angle