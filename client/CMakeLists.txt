qt_add_executable(spiral-client
    src/main.cpp
    src/synctools.cpp
    src/synctools.h
    src/transportpolicy.h
)

set_source_files_properties(qml/Main.qml PROPERTIES QT_RESOURCE_ALIAS Main.qml)
qt_add_qml_module(spiral-client
    URI Spiral
    VERSION 1.0
    QML_FILES qml/Main.qml
)

target_compile_features(spiral-client PRIVATE cxx_std_20)
target_link_libraries(spiral-client PRIVATE
    Qt6::Quick
    Qt6::QuickControls2
    Qt6::QuickDialogs2
    Qt6::Multimedia
    Qt6::WebSockets
    Qt6::Network
)
set_target_properties(spiral-client PROPERTIES
    WIN32_EXECUTABLE TRUE
    MACOSX_BUNDLE TRUE
)

if(BUILD_TESTING)
    add_executable(spiral-transport-policy-test tests/transport_policy_test.cpp)
    target_include_directories(spiral-transport-policy-test PRIVATE src)
    target_link_libraries(spiral-transport-policy-test PRIVATE Qt6::Core Qt6::Network Qt6::Qml)
    add_test(NAME spiral-transport-policy COMMAND spiral-transport-policy-test)
endif()
