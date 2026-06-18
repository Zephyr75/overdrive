#!/usr/bin/env bash
# Pick a scene + backend with gum, then launch the engine.
# Build first with ./overdrive_build.sh.
set -euo pipefail

if ! command -v gum >/dev/null; then
  echo "gum not found. Install: sudo pacman -S gum" >&2
  exit 1
fi

cd "$(dirname "$0")/cpp"

# Scene: list assets/*.xml, show basenames, pass full path to the engine.
mapfile -t scenes < <(cd assets && ls *.xml 2>/dev/null)
if [ "${#scenes[@]}" -eq 0 ]; then
  echo "No scenes found in cpp/assets/*.xml" >&2
  exit 1
fi
scene="$(gum choose --header "Scene" "${scenes[@]}")"
[ -n "$scene" ] || exit 0

backend="$(gum choose --header "Backend" "OpenGL" "Vulkan")"
[ -n "$backend" ] || exit 0

case "$backend" in
  OpenGL) bin="build-gl/overdrive" ;;
  Vulkan) bin="build-vk/overdrive" ;;
esac

if [ ! -x "$bin" ]; then
  echo "$bin not built. Run ./overdrive_build.sh first." >&2
  exit 1
fi

echo ">> $backend  |  assets/$scene"
exec "./$bin" "assets/$scene"
