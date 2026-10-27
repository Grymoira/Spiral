#pragma once

#include "clockcontinuity.h"

#include <QElapsedTimer>
#include <QObject>
#include <QUrl>
#include <QString>

class SyncTools final : public QObject {
    Q_OBJECT
public:
    explicit SyncTools(QObject *parent = nullptr);
    Q_INVOKABLE qint64 monotonicMs() const;
    // A false value means suspension, a clock reset or an unsafe wall-clock
    // jump may have invalidated the currently buffered media appointment.
    Q_INVOKABLE bool clockContinuityOk();
    Q_INVOKABLE QString sha256File(const QUrl &url) const;
    Q_INVOKABLE QString newOperationId() const;
    Q_INVOKABLE QString validatedServerBase(const QString &input) const;
    Q_INVOKABLE QString validatedWebSocketUrl(const QString &input) const;
    Q_INVOKABLE void logEvent(const QString &event) const;
    Q_INVOKABLE void logEventLevel(const QString &event, const QString &level) const;
private:
    QElapsedTimer clock_;
    ClockContinuityGuard clockContinuity_;
    QString sessionId_;
};
