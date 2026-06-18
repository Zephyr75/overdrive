#!/usr/bin/env bash
# Build the Overdrive C++ engine for both backends (OpenGL + Vulkan).
# Run after code/shader changes, then use ./overdrive.sh to launch.
set -euo pipefail

cd "$(dirname "$0")/cpp"

JOBS="$(nproc)"

echo ">> Configuring OpenGL build (build-gl)"
cmake -B build-gl -DUSE_VULKAN=OFF
echo ">> Building OpenGL backend"
cmake --build build-gl -j "$JOBS"

echo ">> Configuring Vulkan build (build-vk)"
cmake -B build-vk -DUSE_VULKAN=ON
echo ">> Building Vulkan backend"
cmake --build build-vk -j "$JOBS"

echo ">> Done. OpenGL: cpp/build-gl/overdrive  Vulkan: cpp/build-vk/overdrive"
