#pragma once

#include <QElapsedTimer>
#include <QObject>
#include <QUrl>

class SyncTools final : public QObject {
    Q_OBJECT
public:
    explicit SyncTools(QObject *parent = nullptr);
    Q_INVOKABLE qint64 monotonicMs() const;
    Q_INVOKABLE QString sha256File(const QUrl &url) const;
private:
    QElapsedTimer clock_;
};
