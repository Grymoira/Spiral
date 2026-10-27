#include "transportpolicy.h"

#include <QCoreApplication>
#include <memory>
#include <cstdio>

struct Case {
    const char *input;
    const char *base;
    const char *ws;
};

int main(int argc, char **argv) {
    QCoreApplication app(argc, argv);
    const Case cases[] = {
        {"http://127.0.0.1:8787", "http://127.0.0.1:8787", "ws://127.0.0.1:8787/v1/ws"},
        {"HTTP://LOCALHOST:8787/", "http://localhost:8787", "ws://localhost:8787/v1/ws"},
        {"http://[::1]:8787", "http://[::1]:8787", "ws://[::1]:8787/v1/ws"},
        {"http://127.10.20.30", "http://127.10.20.30", "ws://127.10.20.30/v1/ws"},
        {"HTTPS://Example.org:443/", "https://example.org:443", "wss://example.org:443/v1/ws"},
        {"https://example.org", "https://example.org", "wss://example.org/v1/ws"},
        {"https://192.0.2.1:8443", "https://192.0.2.1:8443", "wss://192.0.2.1:8443/v1/ws"},
        {"http://example.org", "", ""},
        {"http://192.0.2.1", "", ""},
        {"http://10.0.0.1", "", ""},
        {"http://localhost.evil.example", "", ""},
        {"http://127.0.0.1.evil.example", "", ""},
        {"http://[2001:db8::1]", "", ""},
        {"http://[::ffff:192.0.2.1]", "", ""},
        {"ws://example.org", "", ""},
        {"wss://example.org", "", ""},
        {"ftp://127.0.0.1", "", ""},
        {"javascript:alert(1)", "", ""},
        {"https://user:password@example.org", "", ""},
        {"https://@example.org", "", ""},
        {"https://example.org?token=abc", "", ""},
        {"https://example.org#fragment", "", ""},
        {"https://example.org/subpath", "", ""},
        {"https://example.org:0", "", ""},
        {"https://example.org:65536", "", ""},
        {"http://127.0.0.1:bad", "", ""},
        {"http://localhost:broken", "", ""},
        {"https://", "", ""},
        {"not-a-url", "", ""},
        {"", "", ""},
    };
    for (const auto &test : cases) {
        const QString base = SpiralTransport::baseUrl(QString::fromUtf8(test.input));
        const QString ws = SpiralTransport::webSocketUrl(QString::fromUtf8(test.input));
        if (base != QString::fromUtf8(test.base) || ws != QString::fromUtf8(test.ws)) {
            std::fprintf(stderr, "policy mismatch: %s\n  actual base: %s\n  actual WS: %s\n",
                         test.input, qPrintable(base), qPrintable(ws));
            return 1;
        }
    }

    SpiralTransport::SameOriginNetworkAccessFactory factory;
    std::unique_ptr<QNetworkAccessManager> manager(factory.create(nullptr));
    if (manager->redirectPolicy() != QNetworkRequest::SameOriginRedirectPolicy) {
        std::fputs("QML HTTP redirect policy must be same-origin\n", stderr);
        return 1;
    }
    std::puts("Transport URL and HTTP redirect policy regressions passed");
    return 0;
}
