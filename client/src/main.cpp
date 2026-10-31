#include <QGuiApplication>
#include <QFont>
#include <QFontDatabase>
#include <QDebug>
#include <QStringList>
#include <QQmlApplicationEngine>
#include <QQuickStyle>
#include <QQmlContext>
#include "synctools.h"
#include "transportpolicy.h"

int main(int argc, char* argv[]) {
    QQuickStyle::setStyle("Material");
    QGuiApplication app(argc, argv);
    app.setOrganizationName("Grymoira");
    app.setApplicationName("Spiral");
    // Four unmodified Source Code Pro faces are embedded in Qt resources.
    // Fail visibly if the fonts are missing rather than silently shipping a
    // default-family UI that differs from the reviewed design.
    const QStringList bundledFaces = {
        QStringLiteral(":/fonts/SourceCodePro-Regular.ttf"),
        QStringLiteral(":/fonts/SourceCodePro-Medium.ttf"),
        QStringLiteral(":/fonts/SourceCodePro-Semibold.ttf"),
        QStringLiteral(":/fonts/SourceCodePro-Bold.ttf")
    };
    for (const auto &resource : bundledFaces) {
        if (QFontDatabase::addApplicationFont(resource) < 0) {
            qCritical("Failed to load bundled Source Code Pro UI font");
            return 1;
        }
    }
    QFont uiFont;
    // Qt uses the first available face for Latin; preserve Windows/Linux
    // CJK + OS Unicode/emoji fallback instead of attempting to fake glyphs.
    uiFont.setFamilies({
        QStringLiteral("Source Code Pro"),
        QStringLiteral("Microsoft YaHei UI"),
        QStringLiteral("Microsoft JhengHei UI"),
        QStringLiteral("Yu Gothic UI"),
        QStringLiteral("Noto Sans CJK SC"),
        QStringLiteral("Noto Sans CJK TC"),
        QStringLiteral("Noto Sans CJK JP")
    });
    app.setFont(uiFont);
    SpiralTransport::SameOriginNetworkAccessFactory networkFactory;
    QQmlApplicationEngine engine;
    engine.setNetworkAccessManagerFactory(&networkFactory);
    SyncTools syncTools;
    engine.rootContext()->setContextProperty("syncTools", &syncTools);
    engine.rootContext()->setContextProperty("spiralUiFont", uiFont);
    engine.loadFromModule("Spiral", "Main");
    if (engine.rootObjects().isEmpty()) {
        return 1;
    }
    return app.exec();
}
