"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// Execute the functions from the shipped QML rather than a copy of the algorithm.
// The substitutes control monotonic time, the one-shot timer and the media player.
const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extractFunction(name) {
    const pattern = new RegExp("\\bfunction\\s+" + name + "\\s*\\(");
    const match = pattern.exec(qml);
    assert.ok(match, "missing production QML function: " + name);
    const begin = match.index;
    const open = qml.indexOf("{", begin);
    let depth = 0;
    for (let i = open; i < qml.length; ++i) {
        if (qml[i] === "{") ++depth;
        else if (qml[i] === "}" && --depth === 0)
            return qml.slice(begin, i + 1);
    }
    throw new Error("unterminated production function: " + name);
}
const functions = [
    "receiveClock", "armPending", "pendingUsable", "authorityFresh", "safeguardAuthority", "deferPending",
    "acknowledgeArming", "executePending", "reconcile",
    "sameRecording", "mediaLoaded", "ownReady", "predictedPosition"
].map(extractFunction).join("\n");

function scenario() {
    const mock = {now: 1000, plays: 0, pauses: 0, starts: 0, stops: 0};
    const timer = {
        running: false, interval: 0,
        stop() { this.running = false; mock.stops++; },
        start() { this.running = true; mock.starts++; }
    };
    const player = {
        mediaStatus: 1, playbackState: 0, position: 0,
        play() { mock.plays++; this.playbackState = 2; },
        pause() { mock.pauses++; this.playbackState = 0; }
    };
    const hash = "a".repeat(64);
    const context = {
        authenticated: true, clockReady: true, clockSamples: 3,
        clockOffsetMs: 0, lastAuthorityReplyMs: 0, authorityStaleAfterMs: 1000000, bestRttMs: 1000000, lastClockSampleMs: 0,
        clockEpoch: "epoch", pendingProbes: {},
        scheduledKey: "", deferredPendingKey: "", finishedPendingKey: "", armSentKey: "",
        maxLatePlayMs: 250, selectedHash: hash, memberId: "owner",
        lastCorrectionMs: -100000, logText: "",
        roomState: {
            playback: {
                trackId: hash, trackGeneration: 1, durationMs: 10000,
                playState: "paused", anchorMediaMs: 0, anchorServerMs: 0,
                pending: {id: 7, phase: "committed", executeServerMs: 5000, mediaMs: 0, playState: "playing"}
            },
            members: [{id: "owner", ready: true}]
        },
        playbackTimer: timer, player,
        MediaPlayer: {LoadedMedia: 1, BufferedMedia: 2, PlayingState: 2},
        syncTools: {
            monotonicMs: () => mock.now,
            logEvent: () => {}
        },
        maybeReady: () => {}
    };
    vm.createContext(context);
    vm.runInContext(functions, context, {filename: "client/qml/Main.qml"});
    function deliverClock(offset, probeId) {
        context.pendingProbes[probeId] = mock.now;
        context.receiveClock({
            clockEpoch: "epoch", probeId, c0: mock.now,
            s1: mock.now + offset, s2: mock.now + offset
        });
    }
    function fireTimer() {
        assert.equal(timer.running, true, "timer should be armed");
        timer.running = false;
        context.executePending();
    }
    return {mock, context, timer, deliverClock, fireTimer};
}

{
    const {mock, context, timer, deliverClock, fireTimer} = scenario();
    context.reconcile();
    assert.equal(timer.interval, 4000);
    const starts = mock.starts;
    context.reconcile();
    assert.equal(mock.starts, starts, "same event should not rearm without a clock change");
    deliverClock(-200, "negative");
    assert.equal(context.clockOffsetMs, -200);
    assert.equal(context.scheduledKey, "1:7");
    assert.equal(timer.interval, 4200, "negative offset must defer local deadline");
    mock.now = 5000; // A callback from the old clock estimate is too early.
    fireTimer();
    assert.equal(mock.plays, 0, "never play ahead of the corrected server deadline");
    assert.equal(timer.interval, 200, "early callback must rearm itself");
    mock.now = 5200;
    fireTimer();
    assert.equal(mock.plays, 1, "play once at the corrected deadline");
    // Force an accepted clock-model update after the timer has fired.
    context.bestRttMs = 1000000;
    deliverClock(-100, "after-execution");
    assert.equal(context.clockOffsetMs, -100);
    assert.equal(timer.running, false, "new sample must not replay a completed event");
}
{
    const {mock, context, timer, deliverClock, fireTimer} = scenario();
    context.reconcile();
    deliverClock(200, "positive");
    assert.equal(timer.interval, 3800, "positive offset must advance local deadline");
    mock.now = 4800;
    fireTimer();
    assert.equal(mock.plays, 1);
}
{
    const {mock, context, timer, fireTimer} = scenario();
    context.reconcile();
    context.roomState.members[0].ready = false;
    mock.now = 5000;
    fireTimer();
    assert.equal(mock.plays, 0, "a revoked Ready must prevent playback");
    context.roomState.playback.pending = null;
    context.reconcile();
    assert.equal(context.scheduledKey, "", "canceled event key must be cleared");
    assert.equal(timer.running, false, "canceled event timer must stop");
    context.executePending();
    assert.equal(mock.plays, 0, "canceled event must not execute");
}
{
    const {mock, context, timer} = scenario();
    context.reconcile();
    context.clockReady = false; // Simulate clock estimate expiry.
    mock.now = 5000;
    timer.running = false;
    context.executePending();
    assert.equal(mock.plays, 0, "an expired clock must not start playback");
    context.scheduledKey = "";
    context.clockReady = true;
    mock.now = 5100;
    context.reconcile();
    assert.equal(timer.interval, 1, "fresh calibration must rearm overdue events");
}

console.log("QML clock scheduling regression tests passed");
