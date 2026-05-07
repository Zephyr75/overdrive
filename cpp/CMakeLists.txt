cmake_minimum_required(VERSION 3.20)
project(Overdrive VERSION 1.0 LANGUAGES CXX)

# glad's cmake/CMakeLists.txt uses cmake_minimum_required(VERSION < 3.5)
# which CMake 3.29+ rejects. This tells CMake to treat any min < 3.5 as 3.5.
set(CMAKE_POLICY_VERSION_MINIMUM "3.5")

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

include(FetchContent)

# GLFW
FetchContent_Declare(
    glfw
    GIT_REPOSITORY https://github.com/glfw/glfw.git
    GIT_TAG        3.4
)
set(GLFW_BUILD_DOCS     OFF CACHE BOOL "" FORCE)
set(GLFW_BUILD_TESTS    OFF CACHE BOOL "" FORCE)
set(GLFW_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
FetchContent_MakeAvailable(glfw)

# glad v2 (generates OpenGL 4.1 core loader)
FetchContent_Declare(
    glad
    GIT_REPOSITORY https://github.com/Dav1dde/glad.git
    GIT_TAG        v2.0.4
    SOURCE_SUBDIR  cmake
)
FetchContent_MakeAvailable(glad)
glad_add_library(glad_gl STATIC REPRODUCIBLE API gl:core=4.1)

# GLM (header-only math)
FetchContent_Declare(
    glm
    GIT_REPOSITORY https://github.com/g-truc/glm.git
    GIT_TAG        1.0.1
)
FetchContent_MakeAvailable(glm)

# stb (header-only image loading)
FetchContent_Declare(
    stb
    GIT_REPOSITORY https://github.com/nothings/stb.git
    GIT_TAG        master
)
FetchContent_MakeAvailable(stb)

# tinyobjloader (header-only OBJ loading)
FetchContent_Declare(
    tinyobjloader
    GIT_REPOSITORY https://github.com/tinyobjloader/tinyobjloader.git
    GIT_TAG        v2.0.0rc13
)
FetchContent_MakeAvailable(tinyobjloader)

# pugixml (XML parsing)
FetchContent_Declare(
    pugixml
    GIT_REPOSITORY https://github.com/zeux/pugixml.git
    GIT_TAG        v1.14
)
FetchContent_MakeAvailable(pugixml)

# Explicit source list — GLOB_RECURSE would pick up build/_deps examples
set(SOURCES
    main.cpp
    core/App.cpp
    opengl/Shader.cpp
    opengl/Texture.cpp
    scene/Light.cpp
    scene/Mesh.cpp
    scene/Scene.cpp
    scene/Skybox.cpp
    input/Input.cpp
    settings/Settings.cpp
)

add_executable(overdrive ${SOURCES})

target_include_directories(overdrive PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}
    ${stb_SOURCE_DIR}
    ${tinyobjloader_SOURCE_DIR}
)

target_link_libraries(overdrive PRIVATE
    glfw
    glad_gl
    glm::glm
    pugixml
)

if(WIN32)
    target_link_libraries(overdrive PRIVATE opengl32)
elseif(UNIX AND NOT APPLE)
    target_link_libraries(overdrive PRIVATE GL)
endif()

# Copy shaders to build directory alongside executable
add_custom_command(TARGET overdrive POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_directory
        ${CMAKE_CURRENT_SOURCE_DIR}/shaders
        $<TARGET_FILE_DIR:overdrive>/shaders
    COMMENT "Copying shaders to build directory"
)

# Copy assets to build directory
add_custom_command(TARGET overdrive POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_directory
        ${CMAKE_SOURCE_DIR}/assets
        $<TARGET_FILE_DIR:overdrive>/assets
    COMMENT "Copying assets to build directory"
)
