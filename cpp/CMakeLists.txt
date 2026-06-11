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

if(USE_VULKAN)
    find_package(Vulkan REQUIRED)
    find_program(GLSLC glslc REQUIRED)

    file(GLOB BACKEND_SOURCES vulkan/*.cpp)
    add_executable(overdrive ${SOURCES} ${BACKEND_SOURCES})
    target_link_libraries(overdrive PRIVATE Vulkan::Vulkan)

    # Compile the Vulkan GLSL shaders to SPIR-V next to their sources, where
    # the app (run from the cpp/ directory) loads them.
    set(VK_SHADERS
        forward.vert forward.frag
        depth.vert depth.frag
        depth_cube.vert depth_cube.frag depth_cube.geo
        skybox.vert skybox.frag
    )
    set(SPV_FILES "")
    foreach(shader ${VK_SHADERS})
        string(REGEX MATCH "[^.]+$" ext ${shader})
        if(ext STREQUAL "vert")
            set(stage vertex)
        elseif(ext STREQUAL "frag")
            set(stage fragment)
        else()
            set(stage geometry)
        endif()
        set(src ${CMAKE_CURRENT_SOURCE_DIR}/shaders/vulkan/${shader}.glsl)
        set(out ${CMAKE_CURRENT_SOURCE_DIR}/shaders/vulkan/${shader}.spv)
        add_custom_command(
            OUTPUT ${out}
            COMMAND ${GLSLC} --target-env=vulkan1.3 -fshader-stage=${stage}
                    -I ${CMAKE_CURRENT_SOURCE_DIR}/shaders/vulkan
                    ${src} -o ${out}
            DEPENDS ${src} ${CMAKE_CURRENT_SOURCE_DIR}/shaders/vulkan/common.glsl
            COMMENT "Compiling ${shader}.glsl to SPIR-V"
        )
        list(APPEND SPV_FILES ${out})
    endforeach()
    add_custom_target(shaders ALL DEPENDS ${SPV_FILES})
    add_dependencies(overdrive shaders)
else()
    find_package(OpenGL REQUIRED)

    file(GLOB BACKEND_SOURCES opengl/*.cpp)
    add_executable(overdrive ${SOURCES} ${BACKEND_SOURCES} src/glad.c)
    target_link_libraries(overdrive PRIVATE OpenGL::GL)
endif()

target_include_directories(overdrive PRIVATE . include)

target_link_libraries(overdrive PRIVATE
    glfw
    glm::glm
    pugixml::pugixml
    ${CMAKE_DL_LIBS}
)
