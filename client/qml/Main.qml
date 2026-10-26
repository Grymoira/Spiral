import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtMultimedia
import QtWebSockets

ApplicationWindow {
    id: window
    visible: true
    width: 940
    height: 730
    minimumWidth: 690
    minimumHeight: 560
    title: "Spiral · synchronized local audio (experimental)"
    color: "#121720"
    readonly property color textColor: "#eff2f7"
    readonly property color secondary: "#b7c4d7"

    property string serverUrl: "http://127.0.0.1:8787"
    property string roomId: ""
    property string inviteCode: ""
    property string memberToken: ""
    property string memberRole: ""
    property string memberId: ""
    property string selectedHash: ""
    property var roomState: ({})
    property string logText: "Select a local test audio file, then create or join a room."
    property bool authenticated: false
    property bool clockReady: false
    property real clockOffsetMs: 0
    property real bestRttMs: 1000000
    property int clockSamples: 0
    property int lastClockSampleMs: -1
    property string clockEpoch: ""
    property int probeSequence: 0
    property var pendingProbes: ({})
    property string readySentKey: ""
    property string scheduledKey: ""
    property int lastCorrectionMs: -100000
    property bool userJoining: false

    function endpoint(path) { return serverUrl.replace(/\/+$/, "") + path }
    function send(message) {
        if (socket.status === WebSocket.Open && authenticated)
            socket.sendTextMessage(JSON.stringify(message))
    }
    function request(method, path, payload, done) {
        var req = new XMLHttpRequest()
        req.onreadystatechange = function() {
            if (req.readyState !== XMLHttpRequest.DONE) return
            var body = {}
            try { body = JSON.parse(req.responseText) } catch (e) {}
            if (req.status >= 200 && req.status < 300) done(body, "")
            else done(null, body.error || ("HTTP " + req.status + " · check network / TLS"))
        }
        req.open(method, endpoint(path))
        req.setRequestHeader("Content-Type", "application/json")
        req.send(JSON.stringify(payload))
    }
    function connectRoom(credentials) {
        roomId = credentials.roomId
        memberId = credentials.memberId
        memberToken = credentials.sessionToken
        inviteCode = credentials.inviteCode || ""
        authenticated = false
        clockReady = false
        clockSamples = 0
        bestRttMs = 1000000
        clockEpoch = ""
        scheduledKey = ""
        readySentKey = ""
        roomState = ({})
        socket.url = serverUrl.replace(/^http/, "ws").replace(/\/+$/, "") + "/v1/ws"
        socket.active = true
        logText = "Connecting real-time room…"
    }
    function createRoom() {
        if (roomId !== "") return
        serverUrl = addressField.text
        request("POST", "/v1/rooms", {}, function(body, err) {
            if (err) { logText = "Create failed: " + err; return }
            connectRoom(body)
        })
    }
    function joinRoom() {
        if (roomId !== "") return
        serverUrl = addressField.text
        var target = roomField.text.trim()
        var invitation = inviteField.text.trim()
        if (!target || !invitation) { logText = "Room ID and invitation required"; return }
        request("POST", "/v1/rooms/" + encodeURIComponent(target) + "/join",
                {inviteCode: invitation}, function(body, err) {
            if (err) { logText = "Join failed: " + err; return }
            connectRoom(body)
        })
    }
    function leaveRoom() {
        var originalRoom = roomId
        var token = memberToken
        socket.active = false
        authenticated = false
        roomId = ""
        memberToken = ""
        roomState = ({})
        playbackTimer.stop()
        // Explicit leave: server may destroy the room if we were the host.
        if (originalRoom !== "" && token !== "") {
            var req = new XMLHttpRequest()
            req.open("DELETE", endpoint("/v1/rooms/" + encodeURIComponent(originalRoom) + "/members/me"))
            req.setRequestHeader("Authorization", "Bearer " + token)
            req.send()
        }
        player.pause()
        logText = "Left room"
    }
    function probeClock() {
        if (!authenticated) return
        probeSequence++
        var id = String(probeSequence)
        var sent = syncTools.monotonicMs()
        pendingProbes[id] = sent
        send({type: "clock_probe", probeId: id, c0: sent})
    }
    function receiveClock(msg) {
        if (msg.clockEpoch !== clockEpoch || pendingProbes[msg.probeId] === undefined) return
        var c0 = pendingProbes[msg.probeId]
        delete pendingProbes[msg.probeId]
        var c3 = syncTools.monotonicMs()
        if (msg.c0 !== c0 || msg.s1 > msg.s2 || c3 < c0) return
        var rtt = (c3 - c0) - (msg.s2 - msg.s1)
        if (rtt < 0 || rtt > 500) return
        var offset = ((msg.s1 - c0) + (msg.s2 - c3)) / 2
        clockSamples++
        lastClockSampleMs = syncTools.monotonicMs()
        if (rtt < bestRttMs || clockSamples % 10 === 0) {
            bestRttMs = rtt
            clockOffsetMs = offset
        }
        if (clockSamples >= 3 && !clockReady) {
            clockReady = true
            logText = "Clock calibrated (best RTT " + Math.round(bestRttMs) + " ms)"
            maybeReady()
            reconcile()
        }
    }
    function sameRecording() {
        return selectedHash.length === 64 && roomState.playback
                && roomState.playback.trackId === selectedHash
    }
    function mediaLoaded() {
        return player.mediaStatus === MediaPlayer.LoadedMedia
                || player.mediaStatus === MediaPlayer.BufferedMedia
    }
    function maybeReady() {
        if (!authenticated || !clockReady || !roomState.playback || !sameRecording()) return
        if (!mediaLoaded()) return
        var generation = roomState.playback.trackGeneration
        var key = selectedHash + ":" + generation
        if (readySentKey === key) return
        readySentKey = key
        send({type:"ready", trackId:selectedHash, trackGeneration:generation, ready:true})
    }
    function revokeReady() {
        if (!authenticated || !readySentKey || !roomState.playback) return
        send({type:"ready", trackId:roomState.playback.trackId,
              trackGeneration:roomState.playback.trackGeneration, ready:false})
        readySentKey = ""
    }
    function selectFile(url) {
        revokeReady()
        player.pause()
        selectedHash = syncTools.sha256File(url)
        if (!selectedHash) { logText = "Could not hash selected file"; return }
        player.source = url
        logText = "Local file selected (SHA-256 " + selectedHash.substring(0, 12) + "…)"
    }
    function shareTrack() {
        if (memberRole !== "host" || !authenticated || !selectedHash || player.duration <= 0) {
            logText = "Host must select and load a test audio file first"
            return
        }
        send({type:"track", sha256:selectedHash, durationMs:Math.round(player.duration)})
    }
    function roomCommand(action, ms) {
        if (memberRole !== "host" || !authenticated || !clockReady) {
            logText = "Host connection and a calibrated clock are required"
            return
        }
        send({type:"schedule", action:action, mediaMs:Math.round(ms || 0)})
    }
    function predictedPosition(p) {
        var v = Number(p.anchorMediaMs)
        if (p.playState === "playing")
            v += Math.max(0, syncTools.monotonicMs() + clockOffsetMs - Number(p.anchorServerMs))
        return Math.max(0, Math.min(Number(p.durationMs), v))
    }
    function executePending() {
        if (!clockReady || !sameRecording() || !mediaLoaded()) return
        var p = roomState.playback
        if (!p || !p.pending || scheduledKey !== String(p.trackGeneration) + ":" + p.pending.id) return
        if (p.pending.playState === "playing") {
            player.position = Math.min(Number(p.durationMs), Number(p.pending.mediaMs) +
                    Math.max(0, syncTools.monotonicMs() + clockOffsetMs - Number(p.pending.executeServerMs)))
            player.play()
        } else {
            player.pause()
            player.position = Number(p.pending.mediaMs)
        }
    }
    function reconcile() {
        if (!authenticated || !clockReady || !roomState.playback || !sameRecording() || !mediaLoaded()) return
        var p = roomState.playback
        if (p.pending) {
            var key = String(p.trackGeneration) + ":" + p.pending.id
            if (scheduledKey !== key) {
                scheduledKey = key
                playbackTimer.stop()
                playbackTimer.interval = Math.max(1, Math.floor(Number(p.pending.executeServerMs) -
                                                       syncTools.monotonicMs() - clockOffsetMs))
                playbackTimer.start()
            }
            return
        }
        playbackTimer.stop()
        scheduledKey = ""
        var position = predictedPosition(p)
        if (p.playState === "playing") {
            if (player.playbackState !== MediaPlayer.PlayingState) {
                player.position = position
                player.play()
                lastCorrectionMs = syncTools.monotonicMs()
            } else if (Math.abs(player.position - position) > 300 &&
                       syncTools.monotonicMs() - lastCorrectionMs > 4000) {
                player.position = position
                lastCorrectionMs = syncTools.monotonicMs()
            }
        } else if (player.playbackState === MediaPlayer.PlayingState) {
            player.pause()
            player.position = position
        } else if (Math.abs(player.position - position) > 300) {
            player.position = position
        }
    }

    MediaPlayer {
        id: player
        audioOutput: AudioOutput { volume: 0.65 }
        onErrorOccurred: function(error, errorString) { window.logText = "Audio error: " + errorString }
        onMediaStatusChanged: { if (!window.mediaLoaded()) window.revokeReady(); window.maybeReady(); window.reconcile() }
        onDurationChanged: window.maybeReady()
    }
    FileDialog {
        id: picker
        title: "Choose identical audio file on each device"
        nameFilters: ["Audio files (*.wav *.flac *.mp3 *.ogg *.m4a)", "All files (*)"]
        onAccepted: window.selectFile(selectedFile)
    }
    WebSocket {
        id: socket
        active: false
        onStatusChanged: {
            if (status === WebSocket.Open) {
                window.authenticated = false
                socket.sendTextMessage(JSON.stringify({type:"auth", roomId:window.roomId,
                                                      sessionToken:window.memberToken}))
            } else if (status === WebSocket.Error || status === WebSocket.Closed) {
                window.authenticated = false
                window.clockReady = false
                window.readySentKey = ""
                window.scheduledKey = ""
                playbackTimer.stop()
                player.pause() // Conservative policy: do not free-run while disconnected.
                if (window.roomId !== "") window.logText = "Connection lost; retrying"
            }
        }
        onTextMessageReceived: function(text) {
            var msg
            try { msg = JSON.parse(text) } catch (e) { return }
            if (msg.type === "auth_ok") {
                window.memberRole = msg.role
                window.authenticated = true
                window.clockEpoch = msg.clockEpoch
                window.bestRttMs = 1000000
                window.clockSamples = 0
                window.pendingProbes = ({})
                window.probeClock()
                window.logText = "Room connected. Calibrating server clock…"
            } else if (msg.type === "clock_reply") window.receiveClock(msg)
            else if (msg.type === "snapshot") {
                if (msg.state.roomId !== window.roomId) return
                if (window.clockEpoch !== "" && msg.state.clockEpoch !== window.clockEpoch) {
                    window.clockReady = false
                    window.clockSamples = 0
                    window.bestRttMs = 1000000
                    window.clockEpoch = msg.state.clockEpoch
                    window.readySentKey = ""
                    window.probeClock()
                }
                if (!window.roomState.stateRevision || msg.state.stateRevision >= window.roomState.stateRevision) {
                    window.roomState = msg.state
                    if (msg.state.playback && msg.state.playback.trackId && !window.sameRecording()) {
                        player.pause()
                        playbackTimer.stop()
                        window.scheduledKey = ""
                        window.logText = "Different recording: select the same local file as the room host"
                    }
                    window.maybeReady()
                    window.reconcile()
                }
            } else if (msg.type === "error") window.logText = "Room error: " + msg.code
        }
    }
    Timer { id: playbackTimer; repeat: false; onTriggered: window.executePending() }
    Timer { interval: 500; running: true; repeat: true; onTriggered: {
        if (window.clockReady && window.lastClockSampleMs >= 0 && syncTools.monotonicMs() - window.lastClockSampleMs > 60000) {
            window.revokeReady()
            window.clockReady = false
            window.clockSamples = 0
            window.bestRttMs = 1000000
            player.pause()
            window.logText = "Clock estimate expired; playback paused pending recalibration"
        }
        window.reconcile()
    }}
    Timer { interval: 15000; running: window.authenticated; repeat: true; onTriggered: window.probeClock() }
    Timer { interval: 600; running: window.authenticated && !window.clockReady; repeat: true; onTriggered: window.probeClock() }
    Timer {
        interval: 3500
        running: window.roomId !== "" && !window.authenticated
        repeat: true
        onTriggered: {
            if (socket.status === WebSocket.Closed || socket.status === WebSocket.Error) {
                socket.active = false
                socket.active = true
            }
        }
    }

    ScrollView {
        anchors.fill: parent
        anchors.margins: 22
        contentWidth: availableWidth
        ColumnLayout {
            width: parent.width
            spacing: 14
            Label { text: "SPIRAL"; font.pixelSize: 32; font.bold: true; font.letterSpacing: 5; color: window.textColor }
            Label { text: "Live room sync · identical local audio files · no audio relay"; color: window.secondary }
            Frame {
                Layout.fillWidth: true
                background: Rectangle { color:"#1c2635"; radius: 12 }
                ColumnLayout {
                    anchors.fill: parent
                    spacing: 9
                    Label { text:"Connection & room"; font.bold:true; color:window.textColor }
                    TextField { id: addressField; Layout.fillWidth: true; text: window.serverUrl; placeholderText:"https://your-server.example:8787"; enabled: window.roomId === "" }
                    RowLayout {
                        Layout.fillWidth:true
                        TextField { id:roomField; Layout.fillWidth:true; placeholderText:"Room ID (for joining)"; enabled:window.roomId === "" }
                        TextField { id:inviteField; Layout.fillWidth:true; placeholderText:"Invitation code"; enabled:window.roomId === "" }
                    }
                    RowLayout {
                        Button { text:"Create room"; enabled:window.roomId === ""; onClicked:window.createRoom() }
                        Button { text:"Join room"; enabled:window.roomId === ""; onClicked:window.joinRoom() }
                        Button { text:"Leave"; enabled:window.roomId !== ""; onClicked:window.leaveRoom() }
                    }
                    Label { Layout.fillWidth:true; color:window.secondary; wrapMode:Text.WrapAnywhere;
                        text:window.roomId ? "Room ID: " + window.roomId + "    Role: " + window.memberRole : "Not in room" }
                    Label { Layout.fillWidth:true; color:window.secondary; wrapMode:Text.WrapAnywhere;
                        text:window.inviteCode ? "Share invitation code securely: " + window.inviteCode : "Invitation is shown to host only" }
                    Label { color:window.secondary; text:"Members: " + (window.roomState.members ? window.roomState.members.length : 0) + "/5 · Clock: " + (window.clockReady ? "ready (RTT " + Math.round(window.bestRttMs) + " ms)" : "calibrating") }
                    Repeater { model:window.roomState.members || []
                        delegate: Label { color:window.secondary; text:modelData.role + " · " + modelData.id.substring(0,10) + " · " + (modelData.ready ? "Ready" : "Not ready") }
                    }
                }
            }
            Frame {
                Layout.fillWidth:true
                background: Rectangle { color:"#1c2635"; radius:12 }
                ColumnLayout {
                    anchors.fill:parent
                    spacing:10
                    Label { text:"Local recording"; color:window.textColor; font.bold:true }
                    Label { Layout.fillWidth:true; wrapMode:Text.WrapAnywhere; color:window.secondary; text:player.source.toString() || "No file selected" }
                    RowLayout {
                        Button { text:"Select audio file"; onClicked:picker.open() }
                        Button { text:"Host: share track"; enabled:window.authenticated && window.memberRole==="host" && window.selectedHash!=="" && player.duration>0; onClicked:window.shareTrack() }
                    }
                    Label { Layout.fillWidth:true; color:window.secondary; wrapMode:Text.WrapAnywhere; text:"Room track SHA-256: " + (window.roomState.playback && window.roomState.playback.trackId ? window.roomState.playback.trackId : "not selected") }
                    Label { color:window.sameRecording() ? "#83d9ae" : window.secondary; text: window.sameRecording() ? "Local file matches room recording" : "Local file not yet matched" }
                    Slider { id:progress; Layout.fillWidth:true; from:0; to:Math.max(1,player.duration); value:player.position; enabled:player.seekable
                        onMoved: { if (window.roomId !== "") window.roomCommand("seek",value); else player.position=value }
                    }
                    RowLayout {
                        Label { color:window.secondary; text:Math.floor(player.position/1000) + "s / " + Math.floor(player.duration/1000) + "s" }
                        Item { Layout.fillWidth:true }
                        Button { text:"Play together"; enabled:window.memberRole==="host" && window.clockReady && window.sameRecording(); onClicked:window.roomCommand("play",0) }
                        Button { text:"Pause together"; enabled:window.memberRole==="host" && window.clockReady; onClicked:window.roomCommand("pause",0) }
                        Button { text:"Local play"; enabled:window.roomId==="" && window.selectedHash!==""; onClicked:player.play() }
                    }
                }
            }
            Label { Layout.fillWidth:true; color:window.secondary; wrapMode:Text.Wrap; text:window.logText }
            Label { Layout.fillWidth:true; color:window.secondary; wrapMode:Text.Wrap; font.pixelSize:12;
                text:"Experimental synchronization: scheduled commands and player-reported positions are not acoustic output measurements. Use WSS/HTTPS for any non-local server." }
        }
    }
}
