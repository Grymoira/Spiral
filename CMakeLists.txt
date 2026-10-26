cmake_minimum_required(VERSION 3.24)
project(Spiral VERSION 0.1.0 LANGUAGES NONE)

include(CTest)
option(SPIRAL_BUILD_CLIENT "Build the Qt Quick audio experiment" ON)
option(SPIRAL_BUILD_SERVER "Build the Go development room server" ON)

if(NOT SPIRAL_BUILD_CLIENT AND NOT SPIRAL_BUILD_SERVER)
    message(FATAL_ERROR "At least one Spiral component must be enabled")
endif()

if(SPIRAL_BUILD_SERVER)
    find_program(SPIRAL_GO_EXECUTABLE NAMES go REQUIRED)
    if(WIN32)
        set(SPIRAL_SERVER_SUFFIX ".exe")
    else()
        set(SPIRAL_SERVER_SUFFIX "")
    endif()

    add_custom_target(spiral-server ALL
        COMMAND "${SPIRAL_GO_EXECUTABLE}" build -o "${CMAKE_CURRENT_BINARY_DIR}/spiral-server${SPIRAL_SERVER_SUFFIX}" ./cmd/spiral-server
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
endif()

if(SPIRAL_BUILD_CLIENT)
    enable_language(CXX)
    find_package(Qt6 6.8 REQUIRED COMPONENTS Quick QuickControls2 QuickDialogs2 Multimedia)
    qt_standard_project_setup(REQUIRES 6.8)
    add_subdirectory(client)
endif()
