"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");

function shipped(name) {
    const match = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(match, "missing shipping QML helper " + name);
    const brace = qml.indexOf("{", match.index);
    let depth = 0;
    for (let i = brace; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0) return qml.slice(match.index, i + 1);
    }
    throw Error("unbalanced shipping QML helper " + name);
}

const ctx = {
    roomId: "test-room", authenticated: true, memberRole: "host",
    pendingControl: null, selectedHash: "a".repeat(64), clockReady: true,
    player: {duration: 20000, playbackState: 0}, mediaFaulted: false,
    MediaPlayer: {PlayingState: 1, PausedState: 2},
    sameRecording: () => true, mediaLoaded: () => true
};
vm.createContext(ctx);
vm.runInContext(["hostControlHint", "localPlaybackStatus"].map(shipped).join("\n"), ctx);

assert.match(ctx.hostControlHint(), /all room members must still be Ready/);
ctx.roomId = "";
assert.match(ctx.hostControlHint(), /Create or join a room/);
ctx.roomId = "test-room"; ctx.authenticated = false;
assert.match(ctx.hostControlHint(), /authentication/);
ctx.authenticated = true; ctx.memberRole = "member";
assert.match(ctx.hostControlHint(), /Only the room host/);
ctx.memberRole = "host"; ctx.pendingControl = {operationId: "pending"};
assert.match(ctx.hostControlHint(), /previous coordinator acknowledgement/);
ctx.pendingControl = null; ctx.selectedHash = "";
assert.match(ctx.hostControlHint(), /Select a local audio file/);
ctx.selectedHash = "a".repeat(64); ctx.player.duration = 0;
assert.match(ctx.hostControlHint(), /load its duration/);
ctx.player.duration = 20000; ctx.clockReady = false;
assert.match(ctx.hostControlHint(), /Calibrating the room clock/);
ctx.clockReady = true; ctx.sameRecording = () => false;
assert.match(ctx.hostControlHint(), /matching room metadata/);

ctx.selectedHash = "";
assert.match(ctx.localPlaybackStatus(), /no recording selected/);
ctx.selectedHash = "a".repeat(64); ctx.mediaFaulted = true;
assert.match(ctx.localPlaybackStatus(), /reselect the audio file/);
ctx.mediaFaulted = false; ctx.player.playbackState = 1;
assert.match(ctx.localPlaybackStatus(), /reports Playing/);
assert.match(ctx.localPlaybackStatus(), /not been verified/);
ctx.player.playbackState = 2;
assert.match(ctx.localPlaybackStatus(), /reports Paused/);
ctx.player.playbackState = 0; ctx.mediaLoaded = () => false;
assert.match(ctx.localPlaybackStatus(), /loading or the recording is unavailable/);
ctx.mediaLoaded = () => true;
assert.match(ctx.localPlaybackStatus(), /playback is not running/);

// The hints and media-backend status must be wired into the actual UI.
assert.match(qml, /text:\s*window\.hostControlHint\(\)/);
assert.match(qml, /text:\s*window\.localPlaybackStatus\(\)/);
assert.match(qml, /Local playback requested; waiting for media backend state/);
assert.match(qml, /onClicked:\s*\{\s*window\.logText\s*=[^\n]+\s+player\.play\(\)/);
console.log("QML host button reasons and local media status regression passed");
