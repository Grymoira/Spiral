qt_add_executable(spiral-client
    src/main.cpp
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
)
set_target_properties(spiral-client PROPERTIES
    WIN32_EXECUTABLE TRUE
    MACOSX_BUNDLE TRUE
)
