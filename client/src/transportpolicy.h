#pragma once

#include <QHostAddress>
#include <QNetworkAccessManager>
#include <QNetworkRequest>
#include <QQmlNetworkAccessManagerFactory>
#include <QString>
#include <QUrl>

namespace SpiralTransport {

// Strictly accept only an origin (scheme + host + optional port). Never allow
// credentials, path prefixes, queries or fragments in a server configuration.
inline QUrl serverOrigin(const QString &input) {
    const QUrl url(input.trimmed(), QUrl::StrictMode);
    if (!url.isValid() || url.host().isEmpty()
        || (url.scheme() != QStringLiteral("https") && url.scheme() != QStringLiteral("http"))
        || url.hasQuery() || url.hasFragment()
        || (!url.path().isEmpty() && url.path() != QStringLiteral("/"))
        || url.authority(QUrl::FullyEncoded).contains(QLatin1Char('@'))
        || url.authority(QUrl::FullyEncoded).endsWith(QLatin1Char(':'))
        || url.port(-1) == 0) {
        return {};
    }

    if (url.scheme() == QStringLiteral("http")) {
        const QString host = url.host();
        QHostAddress address;
        // "localhost" is a development convenience, not a wildcard for LAN
        // addresses. Numeric IPs must be verified as loopback by Qt.
        if (host.compare(QStringLiteral("localhost"), Qt::CaseInsensitive) != 0
            && (!address.setAddress(host) || !address.isLoopback())) {
            return {};
        }
    }

    return url.adjusted(QUrl::RemovePath | QUrl::RemoveQuery | QUrl::RemoveFragment);
}

inline QString baseUrl(const QString &input) {
    const QUrl url = serverOrigin(input);
    return url.isValid() && !url.isEmpty() ? url.toString(QUrl::FullyEncoded) : QString{};
}

inline QString webSocketUrl(const QString &input) {
    QUrl url = serverOrigin(input);
    if (!url.isValid() || url.isEmpty()) return {};
    url.setScheme(url.scheme() == QStringLiteral("https") ? QStringLiteral("wss")
                                                          : QStringLiteral("ws"));
    url.setPath(QStringLiteral("/v1/ws"));
    return url.toString(QUrl::FullyEncoded);
}

// QML XMLHttpRequest uses the engine's network manager. Prevent redirects to
// a different origin, including a local-HTTP -> remote-HTTP credential leak.
class SameOriginNetworkAccessFactory final : public QQmlNetworkAccessManagerFactory {
public:
    QNetworkAccessManager *create(QObject *parent) override {
        auto *manager = new QNetworkAccessManager(parent);
        manager->setRedirectPolicy(QNetworkRequest::SameOriginRedirectPolicy);
        return manager;
    }
};

} // namespace SpiralTransport
