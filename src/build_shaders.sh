#!/usr/bin/env sh
# Compiles the engine's Slang shader sources to SPIR-V. Shaders are authored
# once, in Slang; the backend never reads .slang at runtime.
#
#   ./build_shaders.sh          # shaders/slang -> shaders/vk/*.spv
#
# Sources live in shaders/slang/; the generated output directory is git-ignored,
# so this must run before the first build and after any shader edit.
#
# slangc is taken from PATH when present. The AUR shader-slang-bin package
# installs it to /opt/shader-slang-bin/bin/slangc without adding that to PATH,
# so set SLANGC to point there.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
slang_dir=$here/shaders/slang
vk_dir=$here/shaders/vk

slangc=${SLANGC:-$(command -v slangc || true)}
if [ ! -x "$slangc" ]; then
    echo "slangc not found. Set SLANGC=/path/to/slangc or put it on PATH." >&2
    echo "Note: Arch's 'slang' package is the unrelated S-Lang library." >&2
    echo "Get shader-slang from https://github.com/shader-slang/slang/releases" >&2
    echo "(or the AUR shader-slang package)." >&2
    exit 1
fi

mkdir -p "$vk_dir"

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

    # No -preserve-params: it crashes the direct-SPIR-V emitter, and the shaders
    # reach the block through a Uniforms* pointer whose full layout slangc keeps
    # regardless of which fields are read.
    "$slangc" "$slang_dir/$name.slang" -target spirv -profile glsl_460 \
        -emit-spirv-directly -fvk-use-scalar-layout \
        -I "$slang_dir" -stage "$stage" -entry "$entry" \
        -o "$vk_dir/$name.$suffix.spv"
    echo "slang->spirv $name.$suffix"
done
