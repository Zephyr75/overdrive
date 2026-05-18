cmake_minimum_required(VERSION 3.20)
project(overdrive)

set(CMAKE_CXX_STANDARD 17)

find_package(OpenGL REQUIRED)
find_package(glfw3 REQUIRED)
find_package(glm REQUIRED)
find_package(pugixml REQUIRED)

file(GLOB_RECURSE SOURCES
    main.cpp
    core/*.cpp
    input/*.cpp
    opengl/*.cpp
    scene/*.cpp
    settings/*.cpp
)

add_executable(overdrive ${SOURCES} src/glad.c)

target_include_directories(overdrive PRIVATE . include)

target_link_libraries(overdrive PRIVATE
    OpenGL::GL
    glfw
    glm::glm
    pugixml::pugixml
    ${CMAKE_DL_LIBS}
)
