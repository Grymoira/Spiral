#include "synctools.h"

#include <QCryptographicHash>
#include <QFile>

SyncTools::SyncTools(QObject *parent) : QObject(parent) { clock_.start(); }

qint64 SyncTools::monotonicMs() const { return clock_.elapsed(); }

QString SyncTools::sha256File(const QUrl &url) const {
    if (!url.isLocalFile()) return {};
    QFile f(url.toLocalFile());
    if (!f.open(QIODevice::ReadOnly)) return {};
    QCryptographicHash digest(QCryptographicHash::Sha256);
    if (!digest.addData(&f)) return {};
    return QString::fromLatin1(digest.result().toHex());
}
