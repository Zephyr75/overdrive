"""Writes the box meshes of the breakout scene into scene/meshes/

Run from anywhere: python3 make_meshes.py. Coordinates are the engine's, y up,
the camera at +z looking down -z. The game reads every size back from these
vertices, so this file is the one place a brick or a wall is resized.
"""

import os

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "scene", "meshes")

# Each face: its normal, and the two in-face axes u, v with u x v = normal, so
# the corners below wind counter-clockwise seen from outside
FACES = [
    ((1, 0, 0), (0, 0, -1), (0, 1, 0)),
    ((-1, 0, 0), (0, 0, 1), (0, 1, 0)),
    ((0, 1, 0), (1, 0, 0), (0, 0, -1)),
    ((0, -1, 0), (1, 0, 0), (0, 0, 1)),
    ((0, 0, 1), (1, 0, 0), (0, 1, 0)),
    ((0, 0, -1), (-1, 0, 0), (0, 1, 0)),
]


def dot(a, b):
    return sum(x * y for x, y in zip(a, b))


def box(name, material, lo, hi, tile):
    """An axis-aligned box from corner lo to hi, its UVs in metres / tile"""
    center = [(a + b) / 2 for a, b in zip(lo, hi)]
    half = [(b - a) / 2 for a, b in zip(lo, hi)]
    lines = [f"mtllib {name}.mtl", f"o {name}"]
    vts, faces = [], []
    for face, (n, u, v) in enumerate(FACES):
        for su, sv in ((-1, -1), (1, -1), (1, 1), (-1, 1)):
            p = [center[k] + half[k] * (n[k] + su * u[k] + sv * v[k]) for k in range(3)]
            lines.append("v %.6f %.6f %.6f" % tuple(p))
            vts.append("vt %.6f %.6f" % (dot(p, u) / tile, dot(p, v) / tile))
        base = face * 4 + 1
        faces += [(base, base + 1, base + 2), (base, base + 2, base + 3)]
    lines += vts
    lines += ["vn %d %d %d" % n for n, _, _ in FACES]
    lines.append(f"usemtl {material}")
    for i, tri in enumerate(faces):
        normal = i // 2 + 1
        lines.append("f " + " ".join(f"{c}/{c}/{normal}" for c in tri))
    with open(os.path.join(OUT, name + ".obj"), "w") as f:
        f.write("\n".join(lines) + "\n")


# Static: placed where they stand, as the Blender exporter would
box("Floor", "japanese_cedar_planks", (-3, -0.1, -3.5), (3, 0, 3), 1.5)
box("WallLeft", "stacked_brick_wall", (-1.45, 0, -2.2), (-1.25, 0.25, 1.6), 1.0)
box("WallRight", "stacked_brick_wall", (1.25, 0, -2.2), (1.45, 0.25, 1.6), 1.0)
box("WallBack", "stacked_brick_wall", (-1.45, 0, -2.2), (1.45, 0.25, -2.0), 1.0)

# Moved by the game: built about the origin, so Place puts their centre at its argument
box("Brick", "brick", (-0.19, -0.07, -0.08), (0.19, 0.07, 0.08), 1.0)
box("Paddle", "paddle", (-0.3, -0.06, -0.07), (0.3, 0.06, 0.07), 1.0)
