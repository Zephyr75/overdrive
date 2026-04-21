# tutorial_cmake.cmake — CMake concepts in one file
# Run a real project with: cmake -S . -B build && cmake --build build

# ── Minimum version + project ─────────────────────────────────────────────────
cmake_minimum_required(VERSION 3.20)
project(MyApp VERSION 1.0 LANGUAGES CXX)

# ── Global C++ standard ───────────────────────────────────────────────────────
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)          # no GNU extensions

# ── Variables ─────────────────────────────────────────────────────────────────
set(SOURCES src/main.cpp src/engine.cpp)
set(MY_FLAG "-Wall -Wextra")

# List operations
list(APPEND SOURCES src/extra.cpp)
list(REMOVE_ITEM SOURCES src/extra.cpp)

# ── Options (user-toggleable via -DENABLE_FOO=ON) ────────────────────────────
option(ENABLE_FOO "Enable foo feature" OFF)
if(ENABLE_FOO)
    add_compile_definitions(ENABLE_FOO)
endif()

# ── Targets ───────────────────────────────────────────────────────────────────
# Executable
add_executable(app ${SOURCES})

# Static library
add_library(engine STATIC src/engine.cpp)

# Header-only (INTERFACE) library
add_library(math_utils INTERFACE)
target_include_directories(math_utils INTERFACE include/)

# ── Include directories ───────────────────────────────────────────────────────
# PRIVATE  = only for this target's compilation
# PUBLIC   = this target + anything linking it
# INTERFACE= only for things linking it (header-only)
target_include_directories(engine
    PUBLIC  include/
    PRIVATE src/internal/
)

# ── Compile options per target ────────────────────────────────────────────────
target_compile_options(app PRIVATE -Wall -Wextra -Wpedantic)

# ── Linking ───────────────────────────────────────────────────────────────────
target_link_libraries(app
    PRIVATE engine        # engine is an implementation detail of app
    PRIVATE math_utils
)

# ── Find external packages ────────────────────────────────────────────────────
find_package(OpenGL REQUIRED)
target_link_libraries(app PRIVATE OpenGL::GL)

# Optional package
find_package(fmt QUIET)
if(fmt_FOUND)
    target_link_libraries(app PRIVATE fmt::fmt)
endif()

# ── FetchContent (download deps at configure time) ───────────────────────────
include(FetchContent)
FetchContent_Declare(
    glfw
    GIT_REPOSITORY https://github.com/glfw/glfw.git
    GIT_TAG        3.4
)
FetchContent_MakeAvailable(glfw)
target_link_libraries(app PRIVATE glfw)

# ── Generator expressions (evaluated at build time, not configure) ────────────
target_compile_options(app PRIVATE
    $<$<CONFIG:Debug>:-O0 -g>
    $<$<CONFIG:Release>:-O3>
)

# ── Install rules ─────────────────────────────────────────────────────────────
install(TARGETS app DESTINATION bin)
install(DIRECTORY include/ DESTINATION include)

# ── Tests ─────────────────────────────────────────────────────────────────────
enable_testing()
add_executable(tests test/main_test.cpp)
target_link_libraries(tests PRIVATE engine)
add_test(NAME unit COMMAND tests)

# ── Custom commands ───────────────────────────────────────────────────────────
add_custom_command(
    OUTPUT  ${CMAKE_BINARY_DIR}/generated.cpp
    COMMAND python3 ${CMAKE_SOURCE_DIR}/codegen.py > ${CMAKE_BINARY_DIR}/generated.cpp
    DEPENDS ${CMAKE_SOURCE_DIR}/codegen.py
    COMMENT "Generating source"
)
add_custom_target(gen DEPENDS ${CMAKE_BINARY_DIR}/generated.cpp)

# ── Subdirectories ────────────────────────────────────────────────────────────
# Typically each module has its own CMakeLists.txt
# add_subdirectory(engine)   # processes engine/CMakeLists.txt
# add_subdirectory(tests)

# ── Useful built-in variables ────────────────────────────────────────────────
# CMAKE_SOURCE_DIR   — root of source tree
# CMAKE_BINARY_DIR   — build directory
# CMAKE_CURRENT_*    — same but relative to current CMakeLists.txt
# PROJECT_NAME       — set by project()
# CMAKE_BUILD_TYPE   — Debug | Release | RelWithDebInfo | MinSizeRel
