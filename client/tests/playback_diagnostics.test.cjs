"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// Exercise the actual shipping QML functions, not a second implementation.
const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
const cpp = fs.readFileSync(path.join(__dirname, "../src/synctools.cpp"), "utf8");
function extract(name) {
    const match = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(match, "missing production function: " + name);
    const open = qml.indexOf("{", match.index);
    let depth = 0;
    for (let i = open; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0)
            return qml.slice(match.index, i + 1);
    }
    throw Error("unterminated function: " + name);
}
const methods = [
    "send", "guardClockContinuity", "executePending", "reconcile",
    "mediaLoaded", "sameRecording", "ownReady", "predictedPosition",
    "pendingUsable", "authorityFresh", "safeguardAuthority",
    "deferPending", "armPending", "acknowledgeArming",
    "revokeReady", "acceptWakeSnapshot", "handleMediaStatusChanged",
    "requestWakeSnapshot", "maybeReady"
].map(extract).join("\n");

function scene() {
    const hash = "a".repeat(64), events = [], messages = [];
    const player = {
        mediaStatus: 1, playbackState: 0, position: 0,
        pause() { this.playbackState = 0; },
        play() { this.playbackState = 2; }
    };
    const playbackTimer = {running: false, interval: 1,
        stop() { this.running = false; },
        start() { this.running = true; }
    };
    const ctx = {
        selectedHash: hash, memberId: "host", authenticated: true,
        clockReady: true, mediaFaulted: false,
        clockOffsetMs: 0, lastAuthorityReplyMs: 4900,
        authorityStaleAfterMs: 2500, wakeSnapshotId: "",
        wakeSnapshotLastSentMs: -1, readySentKey: hash + ":1",
        scheduledKey: "1:7", deferredPendingKey: "", finishedPendingKey: "",
        armSentKey: "", maxLatePlayMs: 250, lastCorrectionMs: -100000,
        diagnosticPositionOrigin: -1, diagnosticPositionReported: false,
        roomState: {roomEpoch: "ep", stateRevision: 2,
            playback: {trackId: hash, trackGeneration: 1, durationMs: 10000,
                playState: "paused", anchorMediaMs: 0, anchorServerMs: 0,
                pending: {id: 7, phase: "committed", executeServerMs: 5000,
                    playState: "playing", mediaMs: 0}},
            members: [{id: "host", ready: true}]},
        player, playbackTimer,
        MediaPlayer: {LoadedMedia: 1, BufferedMedia: 2, PlayingState: 2,
            StalledMedia: 3, BufferingMedia: 4},
        WebSocket: {Open: 1},
        socket: {status: 1, sendTextMessage(text) { messages.push(JSON.parse(text)); }},
        syncTools: {
            monotonicMs: () => 5000, clockContinuityOk: () => true,
            newOperationId: () => "safe-diagnostic-snapshot",
            logEvent: name => events.push(name),
            logEventLevel: name => events.push(name)
        }
    };
    vm.createContext(ctx);
    vm.runInContext(methods, ctx, {filename: "client/qml/Main.qml"});
    return {ctx, events, messages, player, playbackTimer, hash};
}

// A timer execution is a player.play() REQUEST, not proof of PlayingState.
{
    const {ctx, events, player} = scene();
    ctx.executePending();
    assert.equal(player.playbackState, 2);
    assert.ok(events.includes("client.playback_executed"));
    assert.ok(events.includes("client.play_request_timer"));
    assert.equal(ctx.diagnosticPositionOrigin, 0);
    assert.equal(ctx.diagnosticPositionReported, false);
}

// A final server snapshot can independently invoke play without any pending timer.
{
    const {ctx, events, player} = scene();
    ctx.roomState.playback.pending = null;
    ctx.roomState.playback.playState = "playing";
    ctx.reconcile();
    assert.equal(player.playbackState, 2);
    assert.ok(events.includes("client.play_request_snapshot"));
    assert.ok(!events.includes("client.play_request_timer"));
}

// Every explicit Ready withdrawal is observable but remains fail-closed.
{
    const {ctx, events, messages} = scene();
    ctx.revokeReady();
    assert.ok(events.includes("client.ready_withdraw_explicit"));
    assert.equal(messages.at(-1).ready, false);
}
{
    const {ctx, events, messages, hash} = scene();
    ctx.wakeSnapshotId = "correlated";
    assert.equal(ctx.acceptWakeSnapshot({operationId: "correlated",
        state: {playback: {trackId: hash, trackGeneration: 1,
            playState: "playing", pending: null}}}), false);
    assert.ok(events.includes("client.ready_withdraw_wake"));
    assert.equal(messages.at(-1).ready, false);
    assert.equal(ctx.wakeSnapshotId, "correlated");
}
{
    const {ctx, events, messages, player} = scene();
    player.mediaStatus = 3;
    ctx.handleMediaStatusChanged();
    assert.ok(events.includes("client.media_state_stalled"));
    assert.ok(events.includes("client.ready_withdraw_media"));
    assert.ok(events.includes("client.media_unavailable"));
    assert.equal(messages.find(m => m.type === "ready").ready, false);
    assert.equal(player.playbackState, 0);
    assert.ok(ctx.wakeSnapshotId);
}

// The signal hooks are present, and EVERY new event is in the C++ allowlist.
assert.match(qml, /onPlaybackStateChanged:\s*\{/);
assert.match(qml, /onPositionChanged:\s*\{/);
assert.match(qml, /onMediaStatusChanged:\s*window\.handleMediaStatusChanged\(\)/);
const names = [
    "client.ready_withdraw_wake", "client.ready_withdraw_clock",
    "client.ready_withdraw_media", "client.ready_withdraw_explicit",
    "client.ready_withdraw_authority", "client.ready_withdraw_clock_expired",
    "client.play_request_timer", "client.play_request_snapshot",
    "client.player_state_playing", "client.player_state_not_playing",
    "client.player_position_advanced", "client.media_state_buffering",
    "client.media_state_stalled", "client.media_state_loaded", "client.media_state_other"
];
for (const name of names) {
    assert.ok(qml.includes('"' + name + '"'), "event absent from QML: " + name);
    assert.ok(cpp.includes('QStringLiteral("' + name + '")'),
        "event absent from logger allowlist: " + name);
}
console.log("QML Issue #11 playback and readiness diagnostics regressions passed");
