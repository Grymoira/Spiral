"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");

// Test the actual UI entry points, with a mock of the already separately
// Qt-tested origin validator. No network connection or credentials are used.
function extract(name) {
    const match = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(match, "missing QML function " + name);
    const start = match.index;
    const open = qml.indexOf("{", start);
    let depth = 0;
    for (let i = open; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0) return qml.slice(start, i + 1);
    }
    throw new Error("unclosed QML function " + name);
}

const calls = [];
class FakeXHR {
    constructor() { calls.push({type: "xhr"}); }
    open(method, url) { calls.push({type: "open", method, url}); }
    setRequestHeader() {}
    send() { calls.push({type: "send"}); }
}
const local = "http://127.0.0.1:8787";
const remote = "https://public.example:8787";
const context = {
    roomId: "", memberToken: "", serverUrl: local, memberId: "",
    roomSessionGeneration: 0, removingGuestId: "", removalFeedback: "",
    guestRemovalDialog: {close: () => {}},
    addressField: {text: "http://public.example:8787"},
    roomField: {text: "ROOM"}, inviteField: {text: "INVITE"},
    logText: "", socket: {active: false, url: ""}, XMLHttpRequest: FakeXHR,
    syncTools: {
        validatedServerBase: value => value === local || value === remote ? value : "",
        validatedWebSocketUrl: value => value === local ? "ws://127.0.0.1:8787/v1/ws"
                                            : value === remote ? "wss://public.example:8787/v1/ws" : "",
        logEvent: () => {}, logEventLevel: () => {}
    },
    player: {pause: () => {}},
    playbackTimer: {stop: () => {}}
};
vm.createContext(context);
vm.runInContext(["createRoom", "joinRoom", "request", "connectRoom", "leaveRoom"].map(extract).join("\n"), context);

context.createRoom();
context.joinRoom();
assert.equal(calls.length, 0, "remote plaintext create/join caused network I/O");
assert.match(context.logText, /rejected/);
assert.equal(context.roomId, "");

context.serverUrl = "http://public.example:8787";
context.request("POST", "/v1/rooms", {}, () => {});
assert.equal(calls.length, 0, "central REST request bypassed transport validation");
context.connectRoom({roomId: "ROOM", memberId: "MEMBER", sessionToken: "TEST", inviteCode: "CODE"});
assert.equal(context.roomId, "", "unauthorized socket configuration changed the room");
assert.equal(context.socket.active, false, "invalid origin activated WebSocket");

context.addressField.text = remote;
context.createRoom();
assert.equal(calls.filter(call => call.type === "xhr").length, 1);
assert.ok(calls.some(call => call.type === "open" && call.url === remote + "/v1/rooms"));
context.roomId = "";
context.joinRoom();
assert.equal(calls.filter(call => call.type === "xhr").length, 2);
assert.ok(calls.some(call => call.type === "open"
    && call.url === remote + "/v1/rooms/ROOM/join"));

context.connectRoom({roomId: "ROOM", memberId: "MEMBER", sessionToken: "TEST", inviteCode: "CODE"});
assert.equal(context.socket.url, "wss://public.example:8787/v1/ws");
assert.equal(context.socket.active, true);

context.serverUrl = "http://public.example:8787";
const previousCount = calls.length;
context.leaveRoom();
assert.equal(calls.length, previousCount, "invalid origin sent bearer leave request");

context.serverUrl = local;
context.connectRoom({roomId: "ROOM", memberId: "MEMBER", sessionToken: "TEST"});
assert.equal(context.socket.url, "ws://127.0.0.1:8787/v1/ws");

console.log("QML transport entry-point regressions passed");
