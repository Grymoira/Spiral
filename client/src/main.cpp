#include <QGuiApplication>
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
    SpiralTransport::SameOriginNetworkAccessFactory networkFactory;
    QQmlApplicationEngine engine;
    engine.setNetworkAccessManagerFactory(&networkFactory);
    SyncTools syncTools;
    engine.rootContext()->setContextProperty("syncTools", &syncTools);
    engine.loadFromModule("Spiral", "Main");
    if (engine.rootObjects().isEmpty()) {
        return 1;
    }
    return app.exec();
}
