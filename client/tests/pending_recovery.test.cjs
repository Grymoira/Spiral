"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// Execute functions extracted from shipped QML, not a parallel test algorithm.
const source = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extract(name) {
    const m = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(source);
    assert.ok(m, "missing production QML function " + name);
    const open = source.indexOf("{", m.index);
    let depth = 0;
    for (let i = open; i < source.length; i++) {
        if (source[i] === "{") depth++;
        else if (source[i] === "}" && --depth === 0)
            return source.slice(m.index, i + 1);
    }
    throw new Error("unclosed function: " + name);
}
const funcs = [
    "guardClockContinuity", "armPending", "pendingUsable", "authorityFresh", "safeguardAuthority", "deferPending", "acknowledgeArming", "executePending",
    "reconcile", "sameRecording", "mediaLoaded", "ownReady", "predictedPosition"
].map(extract).join("\n");

function scenario() {
    const mock = {now: 1000, plays: 0, pauses: 0, starts: 0, events: []};
    const timer = {
        running: false, interval: 0,
        stop() { this.running = false; },
        start() { this.running = true; mock.starts++; }
    };
    const player = {
        mediaStatus: 1, playbackState: 0, position: 0,
        play() { mock.plays++; this.playbackState = 2; },
        pause() { mock.pauses++; this.playbackState = 0; }
    };
    const hash = "a".repeat(64);
    const context = {
        authenticated: true, clockReady: true, mediaFaulted: false, clockOffsetMs: 0, lastAuthorityReplyMs: 0, authorityStaleAfterMs: 1000000,
        roomState: {playback: {
            trackId: hash, trackGeneration: 1, durationMs: 10000,
            playState: "paused", anchorMediaMs: 0, anchorServerMs: 0,
            pending: {id: 7, phase: "committed", executeServerMs: 5000, mediaMs: 0, playState: "playing"}
        }, members: [{id: "owner", ready: true}]},
        selectedHash: hash, memberId: "owner",
        scheduledKey: "", deferredPendingKey: "", finishedPendingKey: "", armSentKey: "",
        wakeSnapshotId: "", wakeSnapshotLastSentMs: -1,
        maxLatePlayMs: 250, lastCorrectionMs: -100000,
        player, playbackTimer: timer,
        MediaPlayer: {LoadedMedia: 1, BufferedMedia: 2, PlayingState: 2},
        syncTools: {
            monotonicMs: () => mock.now,
            clockContinuityOk: () => true,
            logEvent: event => mock.events.push(event)
        }
    };
    vm.createContext(context);
    vm.runInContext(funcs, context, {filename:"client/qml/Main.qml"});
    function fire() {
        assert.equal(timer.running, true, "one-shot must be armed");
        timer.running = false;
        context.executePending();
    }
    return {mock, context, timer, player, fire, hash};
}

// Media unavailable on initial pending snapshot: no timer until it recovers.
{
    const {mock, context, timer, player, fire} = scenario();
    player.mediaStatus = 0;
    context.reconcile();
    assert.equal(context.deferredPendingKey, "1:7");
    assert.equal(context.scheduledKey, "");
    assert.equal(timer.running, false);
    const priorLogCount = mock.events.length;
    context.reconcile();
    assert.equal(mock.events.length, priorLogCount, "defer log cannot flood every tick");
    player.mediaStatus = 1;
    context.reconcile();
    assert.equal(timer.running, true);
    assert.equal(context.scheduledKey, "1:7");
    mock.now = 5000;
    fire();
    assert.equal(mock.plays, 1);
    assert.equal(context.finishedPendingKey, "1:7");
    const starts = mock.starts;
    context.reconcile();
    context.executePending();
    assert.equal(mock.plays, 1, "executed event must be exactly once");
    assert.equal(mock.starts, starts, "a handled pending event cannot rearm");
}

// Timer fires while media is unavailable: deferred, then recovered at deadline+100ms.
{
    const {mock, context, timer, player, fire} = scenario();
    context.reconcile();
    mock.now = 5000;
    player.mediaStatus = 0;
    fire();
    assert.equal(context.deferredPendingKey, "1:7");
    assert.equal(context.scheduledKey, "");
    assert.equal(mock.plays, 0);
    player.mediaStatus = 1;
    mock.now = 5100;
    context.reconcile();
    assert.equal(timer.interval, 1);
    fire();
    assert.equal(mock.plays, 1, "recovered appointment should execute");
    assert.equal(player.position, 100, "late recover uses elapsed media offset");
    assert.equal(mock.events.filter(x => x === "client.playback_deferred").length, 1);
}

// Readiness barrier: no playback while false, and withdrawal cancels the old ID.
{
    const {mock, context, timer, fire} = scenario();
    context.reconcile();
    context.roomState.members[0].ready = false;
    mock.now = 5000;
    fire();
    assert.equal(mock.plays, 0);
    context.reconcile();
    assert.equal(timer.running, false);
    context.roomState.playback.pending = null; // server canceled the event
    context.roomState.members[0].ready = true;
    context.reconcile();
    context.executePending(); // stale callback after authoritative cancellation
    assert.equal(mock.plays, 0);
    assert.equal(context.finishedPendingKey, "");
    assert.equal(context.deferredPendingKey, "");
}

// A new generation and event replace an old deferred ID. Never resurrect the old one.
{
    const {mock, context, player, fire} = scenario();
    context.reconcile();
    player.mediaStatus = 0;
    mock.now = 5000;
    fire();
    context.roomState.playback.trackGeneration = 2;
    context.roomState.playback.pending = {
        id: 8, phase: "committed", executeServerMs: 7000, mediaMs: 0, playState: "playing"
    };
    context.reconcile();
    player.mediaStatus = 1;
    mock.now = 6000;
    context.reconcile();
    assert.equal(context.scheduledKey, "2:8");
    assert.equal(context.deferredPendingKey, "");
    context.executePending(); // an early callback must re-arm, not play
    assert.equal(mock.plays, 0);
    mock.now = 7000;
    fire();
    assert.equal(mock.plays, 1);
    assert.equal(context.finishedPendingKey, "2:8");
}

// Expired clock and wrong local recording both defer and recover only on valid inputs.
{
    const {mock, context, timer, fire, hash} = scenario();
    context.reconcile();
    context.clockReady = false;
    mock.now = 4000;
    fire();
    assert.equal(mock.plays, 0);
    context.clockReady = true;
    context.selectedHash = "b".repeat(64);
    context.reconcile();
    assert.equal(timer.running, false);
    context.selectedHash = hash;
    context.reconcile();
    assert.equal(timer.running, true);
    mock.now = 5000;
    fire();
    assert.equal(mock.plays, 1);
}

// More than 250ms late: don't start from an obsolete appointment, wait for commit.
{
    const {mock, context, timer, fire, player} = scenario();
    context.reconcile();
    player.mediaStatus = 0;
    mock.now = 5000;
    fire();
    player.mediaStatus = 1;
    mock.now = 5700;
    context.reconcile();
    assert.equal(timer.interval, 1);
    fire();
    assert.equal(mock.plays, 0, "late playing state must be skipped");
    assert.equal(context.finishedPendingKey, "1:7");
    assert.ok(mock.events.includes("client.playback_skipped"));
    context.reconcile();
    assert.equal(timer.running, false);
    // Only the committed authoritative snapshot may now start playback.
    context.roomState.playback.pending = null;
    context.roomState.playback.playState = "playing";
    context.roomState.playback.anchorServerMs = 5000;
    context.reconcile();
    assert.equal(mock.plays, 1, "committed server state restores playback");
    assert.equal(player.position, 700);
}

// A delayed pause event remains safe even when its deadline was missed.
{
    const {mock, context, player, fire} = scenario();
    context.roomState.playback.pending.playState = "paused";
    context.roomState.playback.pending.mediaMs = 400;
    player.mediaStatus = 0;
    context.reconcile();
    mock.now = 5800;
    player.mediaStatus = 1;
    context.reconcile();
    fire();
    assert.equal(mock.plays, 0);
    assert.ok(mock.pauses > 0);
    assert.equal(player.position, 400);
}

// An unauthenticated / revoked member cannot rearm or execute a pending event.
{
    const {mock, context, timer, fire} = scenario();
    context.reconcile();
    context.authenticated = false;
    mock.now = 5000;
    fire();
    assert.equal(mock.plays, 0);
    context.reconcile();
    assert.equal(timer.running, false);
}

console.log("QML pending recovery and late-event safety regressions passed");
