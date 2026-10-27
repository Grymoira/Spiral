import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material
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
    // The window and custom frames are dark: force Material controls to use
    // a matching palette regardless of the operating-system color scheme.
    Material.theme: Material.Dark
    Material.foreground: window.textColor
    Material.background: "#2b394d"
    Material.accent: "#8bd2c4"
    readonly property color textColor: "#eff2f7"
    readonly property color secondary: "#b7c4d7"

    property string serverUrl: "http://127.0.0.1:8787"
    property string roomId: ""
    property string inviteCode: ""
    property string memberToken: ""
    property string memberRole: ""
    property string memberId: ""
    property string selectedHash: ""
    // A Qt Multimedia decoding/output error must not be interpreted as
    // temporary buffering; require explicit file selection to retry it.
    property bool mediaFaulted: false
    property var roomState: ({})
    property string logText: "Select a local test audio file, then create or join a room."
    property bool authenticated: false
    property bool clockReady: false
    property real clockOffsetMs: 0
    property real bestRttMs: 1000000
    property int clockSamples: 0
    property int lastClockSampleMs: -1
    // Monotonic timestamps need QML real (double), never int32.
    property real lastAuthorityReplyMs: -1
    readonly property int authorityStaleAfterMs: 2500
    property string clockEpoch: ""
    property int probeSequence: 0
    property var pendingProbes: ({})
    property string readySentKey: ""
    property string scheduledKey: ""
    property string deferredPendingKey: ""
    property string finishedPendingKey: ""
    property string armSentKey: ""
    // Cancellation acknowledgements are client claims, not acoustic proof.
    property string cancellationAckKey: ""
    property string cancellationAckConfirmedKey: ""
    property real cancellationAckLastSentMs: -1
    // A sleeping application must fetch a correlated new snapshot before
    // recalibration can restore Ready or a previously scheduled event.
    property string wakeSnapshotId: ""
    property real wakeSnapshotLastSentMs: -1
    // A late playing-state appointment must wait for the authoritative
    // committed snapshot rather than starting from an expired local timer.
    readonly property int maxLatePlayMs: 250
    property int lastCorrectionMs: -100000
    property bool userJoining: false
    property var pendingControl: null

    function endpoint(path) { return serverUrl.replace(/\/+$/, "") + path }
    function send(message) {
        if (socket.status === WebSocket.Open && authenticated)
            socket.sendTextMessage(JSON.stringify(message))
    }
    function submitControl(command) {
        if (pendingControl || !roomState || !roomState.stateRevision) {
            logText = "Waiting for room state or previous command acknowledgement"
            return
        }
        command.operationId = syncTools.newOperationId()
        command.expectedRevision = Number(roomState.stateRevision)
        pendingControl = command
        send(command)
    }
    function request(method, path, payload, done) {
        var base = syncTools.validatedServerBase(serverUrl)
        if (!base) { done(null, "Server address rejected: use HTTPS remotely or local loopback HTTP"); return }
        var req = new XMLHttpRequest()
        req.onreadystatechange = function() {
            if (req.readyState !== XMLHttpRequest.DONE) return
            var body = {}
            try { body = JSON.parse(req.responseText) } catch (e) {}
            if (req.status >= 200 && req.status < 300) done(body, "")
            else done(null, body.error || ("HTTP " + req.status + " · check network / TLS"))
        }
        req.open(method, base + path)
        req.setRequestHeader("Content-Type", "application/json")
        req.send(JSON.stringify(payload))
    }
    function connectRoom(credentials) {
        var wsUrl = syncTools.validatedWebSocketUrl(serverUrl)
        if (!wsUrl) {
            logText = "Server address rejected: use HTTPS remotely or local loopback HTTP"
            return
        }
        roomId = credentials.roomId
        memberId = credentials.memberId
        memberToken = credentials.sessionToken
        inviteCode = credentials.inviteCode || ""
        authenticated = false
        clockReady = false
        clockSamples = 0
        bestRttMs = 1000000
        clockEpoch = ""
        lastAuthorityReplyMs = -1
        scheduledKey = ""
        deferredPendingKey = ""
        finishedPendingKey = ""
        armSentKey = ""
        cancellationAckKey = ""
        cancellationAckConfirmedKey = ""
        cancellationAckLastSentMs = -1
        wakeSnapshotId = ""
        wakeSnapshotLastSentMs = -1
        readySentKey = ""
        roomState = ({})
        pendingControl = null
        socket.url = wsUrl
        socket.active = true
        logText = "Connecting real-time room…"
    }
    function createRoom() {
        if (roomId !== "") return
        var base = syncTools.validatedServerBase(addressField.text)
        if (!base) { logText = "Server address rejected: use HTTPS remotely or local loopback HTTP"; return }
        serverUrl = base
        request("POST", "/v1/rooms", {}, function(body, err) {
            if (err) { syncTools.logEventLevel("client.protocol_error", "WARN"); logText = "Create failed: " + err; return }
            syncTools.logEvent("client.room_created")
            connectRoom(body)
        })
    }
    function joinRoom() {
        if (roomId !== "") return
        var base = syncTools.validatedServerBase(addressField.text)
        if (!base) { logText = "Server address rejected: use HTTPS remotely or local loopback HTTP"; return }
        serverUrl = base
        var target = roomField.text.trim()
        var invitation = inviteField.text.trim()
        if (!target || !invitation) { logText = "Room ID and invitation required"; return }
        request("POST", "/v1/rooms/" + encodeURIComponent(target) + "/join",
                {inviteCode: invitation}, function(body, err) {
            if (err) { syncTools.logEventLevel("client.protocol_error", "WARN"); logText = "Join failed: " + err; return }
            syncTools.logEvent("client.room_joined")
            connectRoom(body)
        })
    }
    // Host-controlled fail-closed recovery for a member that cannot ACK a
    // cancellation. Server authorization is authoritative; the target's old
    // socket is revoked, and the room is paused before a fresh ARM is possible.
    function removeGuest(targetId) {
        if (!authenticated || memberRole !== "host" || !roomId ||
                !targetId || targetId === memberId) return
        var base = syncTools.validatedServerBase(serverUrl)
        if (!base) { logText = "Server address rejected"; return }
        var originalRoom = roomId
        var token = memberToken
        var req = new XMLHttpRequest()
        req.onreadystatechange = function() {
            if (req.readyState !== XMLHttpRequest.DONE ||
                    roomId !== originalRoom || memberToken !== token) return
            if (req.status === 204) {
                syncTools.logEvent("client.member_removed")
                logText = "Member removed. Playback remains paused until every remaining member is Ready"
            } else {
                syncTools.logEventLevel("client.room_error", "WARN")
                logText = "Unable to remove member (HTTP " + req.status + ")"
            }
        }
        req.open("DELETE", base + "/v1/rooms/" + encodeURIComponent(originalRoom) +
                 "/members/" + encodeURIComponent(targetId))
        req.setRequestHeader("Authorization", "Bearer " + token)
        req.send()
    }
    function leaveRoom() {
        var originalRoom = roomId
        var token = memberToken
        socket.active = false
        authenticated = false
        roomId = ""
        memberToken = ""
        roomState = ({})
        pendingControl = null
        scheduledKey = ""
        deferredPendingKey = ""
        finishedPendingKey = ""
        armSentKey = ""
        cancellationAckKey = ""
        cancellationAckConfirmedKey = ""
        cancellationAckLastSentMs = -1
        wakeSnapshotId = ""
        wakeSnapshotLastSentMs = -1
        lastAuthorityReplyMs = -1
        playbackTimer.stop()
        // Explicit leave: server may destroy the room if we were the host.
        var base = syncTools.validatedServerBase(serverUrl)
        if (originalRoom !== "" && token !== "" && base) {
            var req = new XMLHttpRequest()
            req.open("DELETE", base + "/v1/rooms/" + encodeURIComponent(originalRoom) + "/members/me")
            req.setRequestHeader("Authorization", "Bearer " + token)
            req.send()
        }
        player.pause()
        syncTools.logEvent("client.room_left")
        logText = "Left room"
    }
    function acceptWakeSnapshot(message) {
        if (!wakeSnapshotId || message.operationId !== wakeSnapshotId ||
                !message.state || !message.state.playback) return false
        var p = message.state.playback
        // A delayed COMMIT may have made the room play while this client was
        // suspended. Never treat that state as permission to join late.
        if (p.playState === "playing" ||
                (p.pending && p.pending.playState === "playing")) {
            if (authenticated && p.trackId) {
                send({type:"ready", trackId:p.trackId,
                      trackGeneration:p.trackGeneration, ready:false})
            }
            return false
        }
        wakeSnapshotId = ""
        wakeSnapshotLastSentMs = -1
        return true
    }
    function requestWakeSnapshot() {
        if (!wakeSnapshotId || !authenticated || socket.status !== WebSocket.Open) return
        var now = syncTools.monotonicMs()
        if (wakeSnapshotLastSentMs >= 0 && now - wakeSnapshotLastSentMs < 1200) return
        wakeSnapshotLastSentMs = now
        send({type:"snapshot", operationId:wakeSnapshotId})
    }
    // Some OS monotonic clocks exclude time spent suspended. In that case the
    // usual 2.5s server lease can look recent after hours of system sleep.
    // A divergence with wall-clock elapsed time is an unsafe time discontinuity:
    // stop locally before interpreting any queued timer or probe response.
    function guardClockContinuity() {
        if (syncTools.clockContinuityOk()) return true
        // Send Ready(false) even if the old readySentKey has been cleared.
        // The server may still hold Ready after an unseen COMMIT.
        if (authenticated && roomState.playback && roomState.playback.trackId) {
            send({type:"ready", trackId:roomState.playback.trackId,
                  trackGeneration:roomState.playback.trackGeneration, ready:false})
        }
        clockReady = false
        clockSamples = 0
        bestRttMs = 1000000
        lastClockSampleMs = -1
        lastAuthorityReplyMs = -1
        pendingProbes = ({})
        readySentKey = ""
        scheduledKey = ""
        deferredPendingKey = ""
        finishedPendingKey = ""
        armSentKey = ""
        cancellationAckKey = ""
        cancellationAckConfirmedKey = ""
        cancellationAckLastSentMs = -1
        playbackTimer.stop()
        player.pause()
        wakeSnapshotId = ""
        wakeSnapshotLastSentMs = -1
        if (authenticated && socket.status === WebSocket.Open) {
            wakeSnapshotId = syncTools.newOperationId()
            requestWakeSnapshot()
            probeClock()
        }
        syncTools.logEventLevel("client.clock_discontinuity", "WARN")
        logText = "Clock changed during sleep/wake; playback paused until a fresh room snapshot and recalibration"
        return false
    }
    function probeClock() {
        if (!authenticated) return
        // Bound unanswered samples across packet loss / intermittent networks.
        if (Object.keys(pendingProbes).length >= 8)
            pendingProbes = ({})
        probeSequence++
        var id = String(probeSequence)
        var sent = syncTools.monotonicMs()
        pendingProbes[id] = sent
        send({type: "clock_probe", probeId: id, c0: sent})
    }
    function receiveClock(msg) {
        if (!guardClockContinuity()) return
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
        // A validated reply proves the socket has delivered earlier,
        // TCP-ordered server frames (including room cancellations).
        lastAuthorityReplyMs = c3
        if (rtt < bestRttMs || clockSamples % 10 === 0) {
            bestRttMs = rtt
            if (clockOffsetMs !== offset) {
                clockOffsetMs = offset
                // A new clock model changes the local deadline of an active event.
                // Do not replay a timer that already finished.
                var p = roomState.playback
                if (clockReady && playbackTimer.running && p && p.pending &&
                        scheduledKey === String(p.trackGeneration) + ":" + p.pending.id)
                    armPending(p)
            }
        }
        if (clockSamples >= 3 && !clockReady) {
            clockReady = true
            syncTools.logEvent("client.clock_calibrated")
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
        return !mediaFaulted &&
                (player.mediaStatus === MediaPlayer.LoadedMedia ||
                 player.mediaStatus === MediaPlayer.BufferedMedia)
    }
    // Qt may report StalledMedia/LoadingMedia while playbackState is still
    // PlayingState. Readiness withdrawal alone is insufficient: stop the
    // audio pipeline immediately and fence any old scheduled callback.
    function handleMediaStatusChanged() {
        if (!mediaLoaded()) {
            player.pause()
            playbackTimer.stop()
            scheduledKey = ""
            deferredPendingKey = ""
            finishedPendingKey = ""
            armSentKey = ""
            if (authenticated && roomState.playback && roomState.playback.trackId) {
                // The server may still have Ready=true even after a previous
                // local key was cleared. A fresh authoritative snapshot after
                // this Ready(false) must precede any resumed playback.
                send({type:"ready", trackId:roomState.playback.trackId,
                      trackGeneration:roomState.playback.trackGeneration, ready:false})
                readySentKey = ""
                if (!wakeSnapshotId && socket.status === WebSocket.Open) {
                    wakeSnapshotId = syncTools.newOperationId()
                    wakeSnapshotLastSentMs = -1
                    requestWakeSnapshot()
                }
                syncTools.logEventLevel("client.media_unavailable", "WARN")
            }
        }
        maybeReady()
        reconcile()
    }
    function ownReady() {
        var members = roomState.members || []
        for (var i = 0; i < members.length; ++i) {
            if (members[i].id === memberId)
                return members[i].ready === true
        }
        return false
    }
    function maybeReady() {
        if (!guardClockContinuity() || !!wakeSnapshotId) return
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
        mediaFaulted = false
        selectedHash = syncTools.sha256File(url)
        if (!selectedHash) { syncTools.logEventLevel("client.file_rejected", "WARN"); logText = "Could not hash selected file"; return }
        player.source = url
        syncTools.logEvent("client.file_selected")
        logText = "Local file selected (SHA-256 " + selectedHash.substring(0, 12) + "…)"
    }
    function shareTrack() {
        if (memberRole !== "host" || !authenticated || !selectedHash || player.duration <= 0) {
            logText = "Host must select and load a test audio file first"
            return
        }
        if (roomState.playback && roomState.playback.trackId === selectedHash &&
                Number(roomState.playback.durationMs) === Math.round(player.duration)) {
            logText = "This recording is already shared; use an explicit reload to reset readiness"
            return
        }
        submitControl({type:"track", sha256:selectedHash, durationMs:Math.round(player.duration)})
        syncTools.logEvent("client.track_shared")
    }
    function reloadTrack() {
        if (memberRole !== "host" || !authenticated || !selectedHash || player.duration <= 0) return
        submitControl({type:"track", sha256:selectedHash, durationMs:Math.round(player.duration),
                       reload:true})
    }
    function roomCommand(action, ms) {
        if (!guardClockContinuity() || !!wakeSnapshotId) {
            logText = "Waiting for a fresh clock and room state"
            return
        }
        if (memberRole !== "host" || !authenticated || !clockReady) {
            logText = "Host connection and a calibrated clock are required"
            return
        }
        submitControl({type:"schedule", action:action, mediaMs:Math.round(ms || 0)})
        syncTools.logEvent("client.scheduled_command")
    }
    // The server's latest cancellation is a cumulative revision watermark:
    // acknowledging it means this client has reconciled ALL earlier retired
    // appointments, not only the single event referenced by the tombstone.
    // Never acknowledge while an obsolete local timer may still execute.
    function cancellationKey(state) {
        if (!state || !state.cancellation || !state.roomEpoch) return ""
        var c = state.cancellation
        return state.roomEpoch + ":" + String(c.trackGeneration) + ":" +
                String(c.eventId) + ":" + String(c.stateRevision)
    }
    function applyCancellation(state) {
        if (!state || !state.cancellation || !state.playback) return
        var c = state.cancellation
        if (Number(c.stateRevision) > Number(state.stateRevision)) return
        // Snapshot reconciliation is cumulative: a rapid sequence of
        // replacements can retire multiple events before we observe any of
        // their individual notices. Keep only the pending event present in
        // this latest authoritative state.
        var live = state.playback.pending ?
                String(state.playback.trackGeneration) + ":" +
                String(state.playback.pending.id) : ""
        if (scheduledKey !== "" && scheduledKey !== live) {
            playbackTimer.stop()
            scheduledKey = ""
        }
        if (deferredPendingKey !== "" && deferredPendingKey !== live)
            deferredPendingKey = ""
        if (armSentKey !== "" && armSentKey !== live)
            armSentKey = ""
        if (state.playback.playState === "paused")
            player.pause()
    }
    function transmitCancellationReceipt() {
        if (!!wakeSnapshotId || !authenticated || socket.status !== WebSocket.Open) return
        var state = roomState
        if (!state || !state.cancellation || !state.playback) return
        var c = state.cancellation
        if (Number(c.stateRevision) > Number(state.stateRevision)) return
        var key = cancellationKey(state)
        if (key === "" || cancellationAckConfirmedKey === key) return
        // Never claim the cumulative watermark while an older callback
        // or deferred/ARM bookkeeping still belongs to another appointment.
        var live = state.playback.pending ?
                String(state.playback.trackGeneration) + ":" +
                String(state.playback.pending.id) : ""
        if (scheduledKey !== "" && scheduledKey !== live) return
        if (deferredPendingKey !== "" && deferredPendingKey !== live) return
        if (armSentKey !== "" && armSentKey !== live) return
        if (state.playback.playState === "paused" &&
                player.playbackState === MediaPlayer.PlayingState) return
        var now = syncTools.monotonicMs()
        if (cancellationAckKey === key && cancellationAckLastSentMs >= 0 &&
                now - cancellationAckLastSentMs < 1200) return
        cancellationAckKey = key
        cancellationAckLastSentMs = now
        send({type:"cancel_ack", roomEpoch:state.roomEpoch,
              trackGeneration:c.trackGeneration, eventId:c.eventId,
              stateRevision:c.stateRevision})
    }
    function predictedPosition(p) {
        var v = Number(p.anchorMediaMs)
        if (p.playState === "playing")
            v += Math.max(0, syncTools.monotonicMs() + clockOffsetMs - Number(p.anchorServerMs))
        return Math.max(0, Math.min(Number(p.durationMs), v))
    }
    // Keep the deadline in server time; convert to a local delay at each arm.
    function armPending(p) {
        playbackTimer.stop()
        playbackTimer.interval = Math.max(1, Math.ceil(Number(p.pending.executeServerMs) -
                                                     syncTools.monotonicMs() - clockOffsetMs))
        playbackTimer.start()
    }
    // Keep the readiness barrier on BOTH arming and firing the timer.
    // On temporary media loss, resume only after a current authoritative
    // snapshot again confirms this member is ready to play the same track.
    function authorityFresh() {
        return authenticated && clockReady && lastAuthorityReplyMs >= 0 &&
                syncTools.monotonicMs() - lastAuthorityReplyMs <= authorityStaleAfterMs
    }
    // An apparently Open WebSocket does not prove that its old playing
    // snapshot is still authoritative after the server stops responding.
    // Clock probes alone cannot release this fence: require a correlated,
    // freshly read nonplaying snapshot just as for sleep/media recovery.
    function safeguardAuthority() {
        if (!guardClockContinuity()) return
        if (!authenticated || !roomState.playback) return
        if (!!wakeSnapshotId) return // Existing recovery gate owns the retries.
        if (!clockReady) return
        var p = roomState.playback
        var audibleOrQueued = p.playState === "playing" ||
                (p.pending && p.pending.phase === "committed" && p.pending.playState === "playing") ||
                player.playbackState === MediaPlayer.PlayingState
        if (!audibleOrQueued || authorityFresh()) return
        // Ready(false) must be sent even if the local cached key was already
        // cleared: the server may still believe that this member is Ready.
        if (p.trackId) {
            send({type:"ready", trackId:p.trackId,
                  trackGeneration:p.trackGeneration, ready:false})
        }
        readySentKey = ""
        clockReady = false
        clockSamples = 0
        bestRttMs = 1000000
        lastClockSampleMs = -1
        lastAuthorityReplyMs = -1
        pendingProbes = ({})
        playbackTimer.stop()
        scheduledKey = ""
        deferredPendingKey = ""
        finishedPendingKey = ""
        armSentKey = ""
        player.pause()
        if (socket.status === WebSocket.Open) {
            wakeSnapshotId = syncTools.newOperationId()
            wakeSnapshotLastSentMs = -1
            requestWakeSnapshot()
        }
        syncTools.logEventLevel("client.authority_stale", "WARN")
        logText = "Server confirmation stalled; playback paused until a correlated fresh room snapshot"
    }
    function pendingUsable(p) {
        return authenticated && clockReady && sameRecording() && mediaLoaded() &&
                (p.pending.playState !== "playing" || (ownReady() && authorityFresh()))
    }
    function deferPending(key) {
        if (scheduledKey !== "" || playbackTimer.running)
            playbackTimer.stop()
        scheduledKey = ""
        if (deferredPendingKey !== key) {
            deferredPendingKey = key
            syncTools.logEvent("client.playback_deferred")
        }
    }
    // An ARM snapshot has no executable deadline. Acknowledge a concrete
    // event only after the current local recording and clock are usable.
    function acknowledgeArming(p) {
        if (!p.pending || p.pending.phase !== "arming") return
        var key = String(p.trackGeneration) + ":" + p.pending.id
        if (!authenticated || !clockReady || !authorityFresh() ||
                !sameRecording() || !mediaLoaded() || !ownReady()) return
        if (armSentKey === key || socket.status !== WebSocket.Open) return
        armSentKey = key
        send({type:"arm_ack", trackId:p.trackId,
              trackGeneration:p.trackGeneration, eventId:p.pending.id})
    }
    function executePending() {
        if (!guardClockContinuity() || !!wakeSnapshotId) return
        var p = roomState.playback
        if (!p || !p.pending || p.pending.phase !== "committed") return
        var key = String(p.trackGeneration) + ":" + p.pending.id
        // A stopped timer or a completed event must never execute again.
        if (scheduledKey !== key || finishedPendingKey === key) return
        if (!pendingUsable(p)) {
            safeguardAuthority()
            deferPending(key)
            return
        }
        var serverNow = syncTools.monotonicMs() + clockOffsetMs
        var lateness = serverNow - Number(p.pending.executeServerMs)
        // Timer callbacks may be early after a clock correction or rounding.
        if (lateness < 0) {
            armPending(p)
            return
        }
        // Starting audible playback well after the authoritative deadline
        // would produce an unsynchronized late start. Stay paused and wait
        // for the committed server snapshot to reconcile the current position.
        if (p.pending.playState === "playing" && lateness > maxLatePlayMs) {
            finishedPendingKey = key
            scheduledKey = ""
            deferredPendingKey = ""
            player.pause()
            syncTools.logEvent("client.playback_skipped")
            return
        }
        finishedPendingKey = key
        deferredPendingKey = ""
        syncTools.logEvent("client.playback_executed")
        if (p.pending.playState === "playing") {
            player.position = Math.min(Number(p.durationMs), Number(p.pending.mediaMs) +
                    Math.max(0, lateness))
            player.play()
        } else {
            // Delayed pause is safer than free-running audio when the media
            // finally becomes available, even if its deadline has passed.
            player.pause()
            player.position = Number(p.pending.mediaMs)
        }
    }
    function reconcile() {
        if (!authenticated || !roomState.playback) return
        if (!guardClockContinuity() || !!wakeSnapshotId) return
        var p = roomState.playback
        if (p.playState === "playing" && !authorityFresh()) {
            safeguardAuthority()
            return
        }
        if (p.pending) {
            var key = String(p.trackGeneration) + ":" + p.pending.id
            if (p.pending.phase === "arming") {
                if (scheduledKey !== "") playbackTimer.stop()
                scheduledKey = ""
                deferredPendingKey = ""
                acknowledgeArming(p)
                return
            }
            if (p.pending.phase !== "committed") {
                playbackTimer.stop()
                scheduledKey = ""
                return
            }
            if (finishedPendingKey === key) return
            if (p.pending.playState === "playing" && !authorityFresh()) {
                safeguardAuthority()
                return
            }
            if (!pendingUsable(p)) {
                deferPending(key)
                return
            }
            if (scheduledKey !== key) {
                scheduledKey = key
                deferredPendingKey = ""
                armPending(p)
            }
            return
        }
        // A cancellation, replacement without a pending event, or a commit
        // clears all one-shot bookkeeping before normal timeline correction.
        playbackTimer.stop()
        scheduledKey = ""
        deferredPendingKey = ""
        finishedPendingKey = ""
        armSentKey = ""
        if (!clockReady || !sameRecording() || !mediaLoaded()) return
        var position = predictedPosition(p)
        if (p.playState === "playing") {
            if (!ownReady()) {
                player.pause()
                return
            }
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
        onErrorOccurred: function(error, errorString) {
            // Some backends emit an error without leaving BufferedMedia.
            // A faulted pipeline must stop even if mediaStatus looks usable.
            window.mediaFaulted = true
            window.handleMediaStatusChanged()
            syncTools.logEventLevel("client.media_error", "ERROR")
            window.logText = "Audio error: select the local file again to retry"
        }
        onMediaStatusChanged: window.handleMediaStatusChanged()
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
                syncTools.logEvent("client.websocket_open")
                window.authenticated = false
                socket.sendTextMessage(JSON.stringify({type:"auth", roomId:window.roomId,
                                                      sessionToken:window.memberToken}))
            } else if (status === WebSocket.Error || status === WebSocket.Closed) {
                syncTools.logEventLevel("client.websocket_closed", "WARN")
                window.authenticated = false
                window.clockReady = false
                window.lastAuthorityReplyMs = -1
                window.readySentKey = ""
                window.scheduledKey = ""
                window.armSentKey = ""
                window.cancellationAckKey = ""
                window.cancellationAckConfirmedKey = ""
                window.cancellationAckLastSentMs = -1
                window.wakeSnapshotId = ""
                window.wakeSnapshotLastSentMs = -1
                playbackTimer.stop()
                player.pause() // Conservative policy: do not free-run while disconnected.
                if (window.roomId !== "") window.logText = "Connection lost; retrying"
            }
        }
        onTextMessageReceived: function(text) {
            var msg
            try { msg = JSON.parse(text) } catch (e) { return }
            if (msg.type === "auth_ok") {
                syncTools.logEvent("client.authenticated")
                window.memberRole = msg.role
                window.authenticated = true
                window.clockEpoch = msg.clockEpoch
                window.bestRttMs = 1000000
                window.clockSamples = 0
                window.pendingProbes = ({})
                window.lastAuthorityReplyMs = -1
                window.armSentKey = ""
                window.cancellationAckKey = ""
                window.cancellationAckConfirmedKey = ""
                window.cancellationAckLastSentMs = -1
                window.wakeSnapshotId = ""
                window.wakeSnapshotLastSentMs = -1
                window.probeClock()
                // Resend the same operation ID after reconnect; never create a
                // second event if the first command was already accepted.
                if (window.pendingControl) window.send(window.pendingControl)
                window.logText = "Room connected. Calibrating server clock…"
            } else if (msg.type === "clock_reply") window.receiveClock(msg)
            else if (msg.type === "snapshot") {
                if (msg.state.roomId !== window.roomId) return
                if (window.clockEpoch !== "" && msg.state.clockEpoch !== window.clockEpoch) {
                    window.clockReady = false
                    window.clockSamples = 0
                    window.bestRttMs = 1000000
                    window.lastAuthorityReplyMs = -1
                    window.clockEpoch = msg.state.clockEpoch
                    window.readySentKey = ""
                    window.scheduledKey = ""
                    window.deferredPendingKey = ""
                    window.finishedPendingKey = ""
                    window.armSentKey = ""
                    window.cancellationAckKey = ""
                    window.cancellationAckConfirmedKey = ""
                    window.cancellationAckLastSentMs = -1
                    // A clock-epoch reset must not release a wake barrier
                    // unless the correlated server snapshot has arrived.
                    playbackTimer.stop()
                    player.pause()
                    window.probeClock()
                }
                if (!window.roomState.stateRevision || msg.state.stateRevision >= window.roomState.stateRevision) {
                    window.lastAuthorityReplyMs = syncTools.monotonicMs()
                    window.roomState = msg.state
                    window.acceptWakeSnapshot(msg)
                    window.applyCancellation(msg.state)
                    if (msg.state.playback && msg.state.playback.trackId && !window.sameRecording()) {
                        player.pause()
                        playbackTimer.stop()
                        window.scheduledKey = ""
                        window.logText = "Different recording: select the same local file as the room host"
                    }
                    window.maybeReady()
                    window.reconcile()
                    window.transmitCancellationReceipt()
                }
            } else if (msg.type === "cancel_ack_ok") {
                var ack = msg.roomEpoch + ":" + String(msg.trackGeneration) +
                        ":" + String(msg.eventId) + ":" + String(msg.stateRevision)
                if (window.cancellationAckKey === ack)
                    window.cancellationAckConfirmedKey = ack
            } else if (msg.type === "command_ack") {
                if (window.pendingControl && msg.operationId === window.pendingControl.operationId) {
                    window.pendingControl = null
                }
            } else if (msg.type === "error") {
                if (window.pendingControl && msg.operationId === window.pendingControl.operationId) {
                    window.pendingControl = null
                }
                syncTools.logEventLevel("client.room_error", "WARN")
                window.logText = msg.code === "cancellation_pending"
                        ? "Waiting for room members to confirm previous cancellation"
                        : "Room error: " + msg.code
            }
        }
    }
    Timer { id: playbackTimer; repeat: false; onTriggered: window.executePending() }
    Timer { interval: 500; running: true; repeat: true; onTriggered: {
        window.safeguardAuthority()
        window.requestWakeSnapshot()
        if (window.clockReady && window.lastClockSampleMs >= 0 && syncTools.monotonicMs() - window.lastClockSampleMs > 60000) {
            window.revokeReady()
            window.clockReady = false
            window.clockSamples = 0
            window.bestRttMs = 1000000
            window.scheduledKey = ""
            window.armSentKey = ""
            playbackTimer.stop()
            player.pause()
            syncTools.logEventLevel("client.clock_expired", "WARN")
            window.logText = "Clock estimate expired; playback paused pending recalibration"
        }
        window.reconcile()
        window.transmitCancellationReceipt()
    }}
    // Probe during active playback/pending events; do not change the
    // idle-state 15-second probe interval.
    Timer {
        interval: 700
        running: window.authenticated && window.clockReady &&
                 !!window.roomState.playback &&
                 (window.roomState.playback.playState === "playing" ||
                  !!window.roomState.playback.pending)
        repeat: true
        onTriggered: window.probeClock()
    }
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
                    TextField { id: addressField; Layout.fillWidth: true; text: window.serverUrl; placeholderText:"https://your-server.example:8787"; placeholderTextColor: window.secondary; enabled: window.roomId === "" }
                    RowLayout {
                        Layout.fillWidth:true
                        TextField { id:roomField; Layout.fillWidth:true; placeholderText:"Room ID (for joining)"; placeholderTextColor: window.secondary; enabled:window.roomId === "" }
                        TextField { id:inviteField; Layout.fillWidth:true; placeholderText:"Invitation code"; placeholderTextColor: window.secondary; enabled:window.roomId === "" }
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
                        delegate: RowLayout {
                            Label { color:window.secondary; text:modelData.role + " · " + modelData.id.substring(0,10) + " · " + (modelData.ready ? "Ready" : "Not ready") }
                            Button {
                                text:"Remove member"
                                visible: window.memberRole === "host" && modelData.role !== "host"
                                enabled: window.authenticated
                                onClicked:window.removeGuest(modelData.id)
                            }
                        }
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
                        Button { text:"Host: share track"; enabled:window.authenticated && !window.pendingControl && window.memberRole==="host" && window.selectedHash!=="" && player.duration>0; onClicked:window.shareTrack() }
                        Button { text:"Host: reload"; enabled:window.authenticated && !window.pendingControl && window.memberRole==="host" && window.selectedHash!=="" && player.duration>0; onClicked:window.reloadTrack() }
                    }
                    Label { Layout.fillWidth:true; color:window.secondary; wrapMode:Text.WrapAnywhere; text:"Room track SHA-256: " + (window.roomState.playback && window.roomState.playback.trackId ? window.roomState.playback.trackId : "not selected") }
                    Label { color:window.sameRecording() ? "#83d9ae" : window.secondary; text: window.sameRecording() ? "Local file matches room recording" : "Local file not yet matched" }
                    Slider { id:progress; Layout.fillWidth:true; from:0; to:Math.max(1,player.duration); value:player.position; enabled:player.seekable && !window.pendingControl
                        onMoved: { if (window.roomId !== "") window.roomCommand("seek",value); else player.position=value }
                    }
                    RowLayout {
                        Label { color:window.secondary; text:Math.floor(player.position/1000) + "s / " + Math.floor(player.duration/1000) + "s" }
                        Item { Layout.fillWidth:true }
                        Button { text:"Play together"; enabled:!window.pendingControl && window.memberRole==="host" && window.clockReady && window.sameRecording(); onClicked:window.roomCommand("play",0) }
                        Button { text:"Pause together"; enabled:!window.pendingControl && window.memberRole==="host" && window.clockReady; onClicked:window.roomCommand("pause",0) }
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
