#!/usr/bin/env sh
# Compiles the engine's Slang shader sources for both backends: SPIR-V for
# Vulkan, GLSL 4.10 for OpenGL. Every shader is authored once, in Slang.
#
#   ./build_shaders.sh          # shaders/slang -> shaders/gl/*.glsl + shaders/vk/*.spv
#
# Sources live in shaders/slang/; the generated output directories are
# git-ignored, so this must run before the first build and after any shader
# edit. Neither backend reads .slang at runtime.
#
# slangc is taken from PATH when present, else from the Slang SDK that the C++
# Vulkan build tree fetched at configure time (a convenience while that tree
# still exists — set SLANGC to point anywhere else).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
slang_dir=$here/shaders/slang
vk_dir=$here/shaders/vk
gl_dir=$here/shaders/gl
tmp_dir=${TMPDIR:-/tmp}/overdrive-slang.$$

slangc=${SLANGC:-$(command -v slangc || true)}
if [ ! -x "$slangc" ]; then
    echo "slangc not found. Set SLANGC=/path/to/slangc or put it on PATH." >&2
    echo "Note: Arch's 'slang' package is the unrelated S-Lang library." >&2
    echo "Get shader-slang from https://github.com/shader-slang/slang/releases" >&2
    echo "(or the AUR shader-slang package)." >&2
    exit 1
fi

mkdir -p "$vk_dir" "$gl_dir" "$tmp_dir"
trap 'rm -rf "$tmp_dir"' EXIT

# Rewrite slangc's GLSL output into GLSL 4.10, so the OpenGL backend can stay on
# a 4.1 core context (the macOS ceiling). slangc targets ~GLSL 4.50 and uses two
# GL 4.2 (GL_ARB_shading_language_420pack) features the 4.1 core profile lacks:
#
#   1. clamp the version directive to 410
#   2. drop explicit layout(binding=) qualifiers — the backend assigns sampler
#      units and the UBO binding point by name instead
#   3. drop the SSBO-only `layout(row_major) buffer;` line (no SSBOs are used)
#   4. turn `{ ... }` aggregate array initialisers into the 410 `type[]( ... )`
#      constructor form
downgrade() {
    sed -E \
        -e 's/^#version 450$/#version 410/' \
        -e '/^layout\(binding = [0-9]+\)$/d' \
        -e '/^layout\(row_major\) buffer;$/d' \
        -e 's/^([[:space:]]*(const[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]+[A-Za-z_][A-Za-z0-9_]*\[[0-9]+\]) = \{ (.*) \};$/\1 = \3[]( \4 );/' \
        "$1" > "$2"
}

# name:entry:stage:suffix — one row per shader stage.
for row in \
    forward:vsMain:vertex:vert \
    forward:fsMain:fragment:frag \
    skybox:vsMain:vertex:vert \
    skybox:fsMain:fragment:frag \
    depth:vsMain:vertex:vert \
    depth:fsMain:fragment:frag \
    depth_cube:vsMain:vertex:vert \
    depth_cube:gsMain:geometry:geo \
    depth_cube:fsMain:fragment:frag \
    ui:vsMain:vertex:vert \
    ui:fsMain:fragment:frag
do
    name=${row%%:*}; rest=${row#*:}
    entry=${rest%%:*}; rest=${rest#*:}
    stage=${rest%%:*}; suffix=${rest#*:}

    # No -preserve-params on the SPIR-V path: it crashes the direct-SPIR-V
    # emitter, and the shaders reach the block through a Uniforms* pointer whose
    # full layout slangc keeps regardless of which fields are read.
    "$slangc" "$slang_dir/$name.slang" -DTARGET_VK -target spirv -profile glsl_460 \
        -emit-spirv-directly -fvk-use-scalar-layout \
        -I "$slang_dir" -stage "$stage" -entry "$entry" \
        -o "$vk_dir/$name.$suffix.spv"
    echo "slang->spirv $name.$suffix"

    # -preserve-params keeps unread block members in the GLSL output, so the
    # std140 offsets stay fixed no matter which fields a given stage touches.
    # opengl/uniforms_test.go checks those offsets against this output.
    raw=$tmp_dir/$name.$suffix.raw.glsl
    "$slangc" "$slang_dir/$name.slang" -target glsl -profile glsl_410 -preserve-params \
        -I "$slang_dir" -stage "$stage" -entry "$entry" -o "$raw"
    downgrade "$raw" "$gl_dir/$name.$suffix.glsl"
    echo "slang->glsl410 $name.$suffix"
done
