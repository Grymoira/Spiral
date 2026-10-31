#include "synctools.h"
#include "transportpolicy.h"

#include <QCryptographicHash>
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSet>
#include <QStandardPaths>
#include <QUuid>

namespace {
constexpr qint64 MaxLogBytes = 2 * 1024 * 1024;

// Never accept caller-supplied text, URLs, tokens, paths or error messages.
// Event names are a fixed vocabulary maintained alongside the protocol.
const QSet<QString> &approvedEvents() {
    static const QSet<QString> events = {
        QStringLiteral("client.started"), QStringLiteral("client.stopped"),
        QStringLiteral("client.file_selected"), QStringLiteral("client.file_rejected"),
        QStringLiteral("client.media_error"),
        QStringLiteral("client.ready_withdraw_wake"),
        QStringLiteral("client.ready_withdraw_clock"),
        QStringLiteral("client.ready_withdraw_media"),
        QStringLiteral("client.ready_withdraw_explicit"),
        QStringLiteral("client.ready_withdraw_authority"),
        QStringLiteral("client.ready_withdraw_clock_expired"),
        QStringLiteral("client.play_request_timer"),
        QStringLiteral("client.play_request_snapshot"),
        QStringLiteral("client.player_state_playing"),
        QStringLiteral("client.player_state_not_playing"),
        QStringLiteral("client.player_position_advanced"),
        QStringLiteral("client.media_state_buffering"),
        QStringLiteral("client.media_state_stalled"),
        QStringLiteral("client.media_state_loaded"),
        QStringLiteral("client.media_state_other"),
        QStringLiteral("client.room_created"),
        QStringLiteral("client.room_joined"), QStringLiteral("client.room_left"),
        QStringLiteral("client.member_removed"),
        QStringLiteral("client.websocket_open"), QStringLiteral("client.websocket_closed"),
        QStringLiteral("client.authenticated"), QStringLiteral("client.clock_calibrated"),
        QStringLiteral("client.clock_expired"), QStringLiteral("client.track_shared"),
        QStringLiteral("client.scheduled_command"), QStringLiteral("client.playback_executed"),
        QStringLiteral("client.playback_deferred"), QStringLiteral("client.playback_skipped"),
        QStringLiteral("client.authority_stale"),
        QStringLiteral("client.clock_discontinuity"),
        QStringLiteral("client.protocol_error"), QStringLiteral("client.room_error")
    };
    return events;
}
}

SyncTools::SyncTools(QObject *parent) : QObject(parent) {
    clock_.start();
    clockContinuity_.observe(clock_.elapsed(), QDateTime::currentMSecsSinceEpoch());
    sessionId_ = QUuid::createUuid().toString(QUuid::WithoutBraces);
    logEvent(QStringLiteral("client.started"));
}

qint64 SyncTools::monotonicMs() const { return clock_.elapsed(); }

bool SyncTools::clockContinuityOk() {
    return clockContinuity_.observe(clock_.elapsed(), QDateTime::currentMSecsSinceEpoch());
}

QString SyncTools::newOperationId() const {
    return QUuid::createUuid().toString(QUuid::WithoutBraces);
}

QString SyncTools::validatedServerBase(const QString &input) const {
    return SpiralTransport::baseUrl(input);
}

QString SyncTools::validatedWebSocketUrl(const QString &input) const {
    return SpiralTransport::webSocketUrl(input);
}

QString SyncTools::sha256File(const QUrl &url) const {
    if (!url.isLocalFile()) return {};
    QFile f(url.toLocalFile());
    if (!f.open(QIODevice::ReadOnly)) return {};
    QCryptographicHash digest(QCryptographicHash::Sha256);
    if (!digest.addData(&f)) return {};
    return QString::fromLatin1(digest.result().toHex());
}

void SyncTools::logEvent(const QString &event) const {
    logEventLevel(event, QStringLiteral("INFO"));
}

void SyncTools::logEventLevel(const QString &event, const QString &level) const {
    if (!approvedEvents().contains(event)) return;
    if (level != QStringLiteral("INFO") && level != QStringLiteral("WARN") && level != QStringLiteral("ERROR")) return;
    const QString directory = QStandardPaths::writableLocation(QStandardPaths::AppLocalDataLocation);
    if (directory.isEmpty() || !QDir().mkpath(directory)) return;
    const QString path = QDir(directory).filePath(QStringLiteral("events.jsonl"));
    QFile previous(path);
    if (previous.size() >= MaxLogBytes) {
        QFile::remove(path + QStringLiteral(".1"));
        if (!previous.rename(path + QStringLiteral(".1"))) return;
    }
    QFile file(path);
    if (!file.open(QIODevice::WriteOnly | QIODevice::Append)) return;
    const QJsonObject record {
        {QStringLiteral("timestamp"), QDateTime::currentDateTimeUtc().toString(Qt::ISODateWithMs)},
        {QStringLiteral("level"), level},
        {QStringLiteral("event"), event},
        {QStringLiteral("component"), QStringLiteral("desktop")},
        {QStringLiteral("service"), QStringLiteral("spiral-client")},
        {QStringLiteral("schema_version"), 1},
        {QStringLiteral("session_id"), sessionId_}
    };
    file.write(QJsonDocument(record).toJson(QJsonDocument::Compact));
    file.write("\n");
}
