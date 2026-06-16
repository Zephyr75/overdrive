cmake_minimum_required(VERSION 3.20)
project(overdrive)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

option(USE_VULKAN "Use the Vulkan rendering backend" OFF)

find_package(glfw3 REQUIRED)
find_package(glm REQUIRED)
find_package(pugixml REQUIRED)

file(GLOB SOURCES
    main.cpp
    core/*.cpp
    input/*.cpp
    renderer/*.cpp
    scene/*.cpp
    settings/*.cpp
)

# ---- Slang toolchain ---------------------------------------------------------
# Every shader is authored once in Slang (shaders/slang/*.slang) and compiled
# per backend: GLSL 4.10 for OpenGL, SPIR-V for Vulkan. slangc is used if on
# PATH, otherwise the SDK release is fetched automatically.
set(SLANG_VERSION "2026.10.2")
find_program(SLANGC slangc)
if(NOT SLANGC)
    include(FetchContent)
    if(CMAKE_SYSTEM_NAME STREQUAL "Darwin")
        if(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64|aarch64")
            set(SLANG_ASSET "slang-${SLANG_VERSION}-macos-aarch64.zip")
        else()
            set(SLANG_ASSET "slang-${SLANG_VERSION}-macos-x86_64.zip")
        endif()
    elseif(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64|aarch64")
        set(SLANG_ASSET "slang-${SLANG_VERSION}-linux-aarch64.tar.gz")
    else()
        set(SLANG_ASSET "slang-${SLANG_VERSION}-linux-x86_64.tar.gz")
    endif()
    message(STATUS "slangc not found; fetching Slang ${SLANG_VERSION} SDK (${SLANG_ASSET})")
    FetchContent_Declare(slang
        URL "https://github.com/shader-slang/slang/releases/download/v${SLANG_VERSION}/${SLANG_ASSET}"
        DOWNLOAD_EXTRACT_TIMESTAMP TRUE)
    FetchContent_MakeAvailable(slang)
    set(SLANGC "${slang_SOURCE_DIR}/bin/slangc")
endif()
message(STATUS "Using slangc: ${SLANGC}")

set(SLANG_DIR ${CMAKE_CURRENT_SOURCE_DIR}/shaders/slang)
# name entry stage suffix  (one shader stage per row)
set(SHADER_STAGES
    "forward:vsMain:vertex:vert"
    "forward:fsMain:fragment:frag"
    "skybox:vsMain:vertex:vert"
    "skybox:fsMain:fragment:frag"
    "depth:vsMain:vertex:vert"
    "depth:fsMain:fragment:frag"
    "depth_cube:vsMain:vertex:vert"
    "depth_cube:gsMain:geometry:geo"
    "depth_cube:fsMain:fragment:frag"
)

set(SHADER_OUTPUTS "")
foreach(row ${SHADER_STAGES})
    string(REPLACE ":" ";" parts ${row})
    list(GET parts 0 name)
    list(GET parts 1 entry)
    list(GET parts 2 stage)
    list(GET parts 3 suffix)
    set(src ${SLANG_DIR}/${name}.slang)
    if(USE_VULKAN)
        set(out ${CMAKE_CURRENT_SOURCE_DIR}/shaders/vk/${name}.${suffix}.spv)
        add_custom_command(OUTPUT ${out}
            COMMAND ${CMAKE_COMMAND} -E make_directory ${CMAKE_CURRENT_SOURCE_DIR}/shaders/vk
            # No -preserve-params here: it crashes the direct-SPIR-V path, and
            # the VK shaders reach the UBO through a Uniforms* pointer whose
            # full struct layout slangc keeps regardless of field usage.
            COMMAND ${SLANGC} ${src} -DTARGET_VK -target spirv -profile glsl_460
                    -emit-spirv-directly -fvk-use-scalar-layout
                    -I ${SLANG_DIR} -stage ${stage} -entry ${entry} -o ${out}
            DEPENDS ${src} ${SLANG_DIR}/common.slang
            COMMENT "slang->spirv ${name}.${suffix}")
    else()
        set(raw ${CMAKE_CURRENT_BINARY_DIR}/${name}.${suffix}.raw.glsl)
        set(out ${CMAKE_CURRENT_SOURCE_DIR}/shaders/gl/${name}.${suffix}.glsl)
        add_custom_command(OUTPUT ${out}
            COMMAND ${CMAKE_COMMAND} -E make_directory ${CMAKE_CURRENT_SOURCE_DIR}/shaders/gl
            COMMAND ${SLANGC} ${src} -target glsl -profile glsl_410 -preserve-params
                    -I ${SLANG_DIR} -stage ${stage} -entry ${entry} -o ${raw}
            # slangc emits #version 450 + GL 4.2 features; rewrite to 4.10 core
            COMMAND ${CMAKE_COMMAND} -DIN=${raw} -DOUT=${out}
                    -P ${SLANG_DIR}/downgrade.cmake
            DEPENDS ${src} ${SLANG_DIR}/common.slang ${SLANG_DIR}/downgrade.cmake
            COMMENT "slang->glsl410 ${name}.${suffix}")
    endif()
    list(APPEND SHADER_OUTPUTS ${out})
endforeach()
add_custom_target(shaders ALL DEPENDS ${SHADER_OUTPUTS})

if(USE_VULKAN)
    find_package(Vulkan REQUIRED)
    file(GLOB BACKEND_SOURCES vulkan/*.cpp)
    add_executable(overdrive ${SOURCES} ${BACKEND_SOURCES})
    target_link_libraries(overdrive PRIVATE Vulkan::Vulkan)
else()
    find_package(OpenGL REQUIRED)
    file(GLOB BACKEND_SOURCES opengl/*.cpp)
    add_executable(overdrive ${SOURCES} ${BACKEND_SOURCES} src/glad.c)
    target_link_libraries(overdrive PRIVATE OpenGL::GL)
endif()
add_dependencies(overdrive shaders)

target_include_directories(overdrive PRIVATE . include)

target_link_libraries(overdrive PRIVATE
    glfw
    glm::glm
    pugixml::pugixml
    ${CMAKE_DL_LIBS}
)
