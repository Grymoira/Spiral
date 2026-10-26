import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtMultimedia

ApplicationWindow {
    id: window
    width: 850
    height: 540
    minimumWidth: 620
    minimumHeight: 400
    visible: true
    title: "Spiral · Local audio experiment"
    color: "#11151e"

    readonly property color foreground: "#edf0f6"
    readonly property color muted: "#aab6c8"

    MediaPlayer {
        id: player
        audioOutput: AudioOutput { }
        onErrorOccurred: function(error, errorString) {
            statusLabel.text = errorString
        }
    }

    FileDialog {
        id: picker
        title: "Choose a local audio file"
        nameFilters: ["Audio files (*.wav *.flac *.mp3 *.ogg *.m4a)", "All files (*)"]
        onAccepted: {
            player.stop()
            player.source = selectedFile
            player.play()
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 32
        spacing: 18

        Label {
            text: "SPIRAL"
            color: window.foreground
            font.pixelSize: 34
            font.bold: true
            font.letterSpacing: 6
        }
        Label {
            text: "Local playback technology baseline"
            color: window.muted
            font.pixelSize: 14
        }
        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            radius: 16
            color: "#1b2330"
            border.color: "#303d52"

            ColumnLayout {
                anchors.centerIn: parent
                width: Math.min(parent.width - 60, 580)
                spacing: 20

                Label {
                    Layout.alignment: Qt.AlignHCenter
                    text: "Your music stays on this device"
                    color: window.foreground
                    font.pixelSize: 21
                    font.bold: true
                    wrapMode: Text.Wrap
                }
                Label {
                    Layout.fillWidth: true
                    text: player.source.toString().length > 0 ? player.source.toString() : "No audio file selected"
                    color: window.muted
                    elide: Text.ElideMiddle
                    horizontalAlignment: Text.AlignHCenter
                }
                Button {
                    Layout.alignment: Qt.AlignHCenter
                    text: "Open audio file"
                    onClicked: picker.open()
                }
                Slider {
                    id: progress
                    Layout.fillWidth: true
                    from: 0
                    to: Math.max(player.duration, 1)
                    value: player.position
                    enabled: player.seekable
                    onMoved: player.position = value
                }
                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: Math.floor(player.position / 1000) + "s"
                        color: window.muted
                    }
                    Item { Layout.fillWidth: true }
                    Label {
                        text: Math.floor(player.duration / 1000) + "s"
                        color: window.muted
                    }
                }
                RowLayout {
                    Layout.alignment: Qt.AlignHCenter
                    spacing: 14
                    Button {
                        text: player.playbackState === MediaPlayer.PlayingState ? "Pause" : "Play"
                        enabled: player.source.toString().length > 0
                        onClicked: player.playbackState === MediaPlayer.PlayingState ? player.pause() : player.play()
                    }
                    Button {
                        text: "Stop"
                        enabled: player.source.toString().length > 0
                        onClicked: player.stop()
                    }
                }
                Label {
                    id: statusLabel
                    Layout.fillWidth: true
                    text: "This player does not yet implement remote rooms or synchronized audio output."
                    color: window.muted
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                }
            }
        }
    }
}
