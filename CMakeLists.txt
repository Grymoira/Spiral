cmake_minimum_required(VERSION 3.24)
project(Spiral VERSION 0.1.0 LANGUAGES CXX)

include(CTest)
option(SPIRAL_BUILD_CLIENT "Build the Qt Quick audio experiment" ON)

find_program(SPIRAL_GO_EXECUTABLE go)
if(SPIRAL_GO_EXECUTABLE)
    add_custom_target(spiral-server ALL
        COMMAND "${SPIRAL_GO_EXECUTABLE}" build -o "${CMAKE_CURRENT_BINARY_DIR}/spiral-server${CMAKE_EXECUTABLE_SUFFIX}" ./cmd/spiral-server
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/server"
        COMMENT "Building the Go development room server"
        VERBATIM
    )
    if(BUILD_TESTING)
        add_test(NAME spiral-go-tests COMMAND "${SPIRAL_GO_EXECUTABLE}" test ./...)
        set_tests_properties(spiral-go-tests PROPERTIES
            WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/server"
            TIMEOUT 90
        )
    endif()
else()
    message(WARNING "Go executable not found; Go server and tests cannot be built")
endif()

if(SPIRAL_BUILD_CLIENT)
    find_package(Qt6 6.8 REQUIRED COMPONENTS Quick QuickControls2 Multimedia)
    qt_standard_project_setup(REQUIRES 6.8)
    add_subdirectory(client)
endif()
