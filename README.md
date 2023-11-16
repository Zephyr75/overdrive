# overdrive

An OpenGL game engine written in Go. Uses a custom Blender add-on to convert a full Blender scene directly as a game ready scene with camera and lighting system included.
UI library can be found [here](https://github.com/zephyr75/gutter).

Using OBJ conventions: Y is Up

    Blender to OBJ:
    pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}


