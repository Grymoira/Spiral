#pragma once

#include <QElapsedTimer>
#include <QObject>
#include <QUrl>
#include <QString>

class SyncTools final : public QObject {
    Q_OBJECT
public:
    explicit SyncTools(QObject *parent = nullptr);
    Q_INVOKABLE qint64 monotonicMs() const;
    Q_INVOKABLE QString sha256File(const QUrl &url) const;
    Q_INVOKABLE QString validatedServerBase(const QString &input) const;
    Q_INVOKABLE QString validatedWebSocketUrl(const QString &input) const;
    Q_INVOKABLE void logEvent(const QString &event) const;
    Q_INVOKABLE void logEventLevel(const QString &event, const QString &level) const;
private:
    QElapsedTimer clock_;
    QString sessionId_;
};
