# Rewrite slangc's GLSL output into GLSL 4.10 so the OpenGL backend can stay on
# a 4.1 core context (the macOS ceiling). Invoked as:
#   cmake -DIN=<raw.glsl> -DOUT=<final.glsl> -P downgrade.cmake
#
# slangc targets ~GLSL 4.50 and uses two GL 4.2 (GL_ARB_shading_language_420pack)
# features the 4.1 core profile lacks. The rewrites:
#   1. clamp the version directive to 410
#   2. drop explicit layout(binding=) qualifiers; the backend assigns sampler
#      units / UBO binding points by name instead
#   3. drop the SSBO-only `layout(row_major) buffer;` line (no SSBOs are used)
#   4. turn `{ ... }` aggregate array initialisers into the 410 `type[]( ... )`
#      constructor form

file(READ "${IN}" content)

# CMake uses ';' as its list separator, but GLSL statements end in ';'. Protect
# the real semicolons with a sentinel, split on newlines, restore per line.
string(REPLACE ";" "@@SEMI@@" content "${content}")
string(REGEX REPLACE "\r?\n" ";" lines "${content}")

set(out "")
foreach(line ${lines})
    string(REPLACE "@@SEMI@@" ";" line "${line}")
    string(REGEX REPLACE "^#version 450$" "#version 410" line "${line}")
    if(line MATCHES "^layout\\(binding = [0-9]+\\)$")
        continue()
    endif()
    if(line STREQUAL "layout(row_major) buffer;")
        continue()
    endif()
    string(REGEX REPLACE
        "^([ \t]*(const[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]+[A-Za-z_][A-Za-z0-9_]*\\[[0-9]+\\]) = \\{ (.*) \\};$"
        "\\1 = \\3[]( \\4 );"
        line "${line}")
    set(out "${out}${line}\n")
endforeach()
file(WRITE "${OUT}" "${out}")
