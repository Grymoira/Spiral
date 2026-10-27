"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function functionSource(name) {
    const m = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(m, "missing shipping QML function " + name);
    const start = qml.indexOf("{", m.index);
    let depth = 0;
    for (let i = start; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0) return qml.slice(m.index, i + 1);
    }
    throw Error("unterminated function " + name);
}
function setup() {
    let next = 0;
    const sent = [], events = [];
    const ctx = {
        WebSocket: {Open: 1},
        socket: {status: 1, sendTextMessage: payload => sent.push(JSON.parse(payload))},
        authenticated: true, roomState: {stateRevision: 7}, pendingControl: null,
        controlFeedback: "", logText: "",
        syncTools: {newOperationId: () => "control-id-" + (++next)}
    };
    vm.createContext(ctx);
    vm.runInContext(["send", "submitControl", "handleControlAck", "handleControlError"]
        .map(functionSource).join("\n"), ctx, {filename:"client/qml/Main.qml"});
    return {ctx, sent, events};
}
{
    const {ctx, sent} = setup();
    assert.equal(ctx.submitControl({type:"schedule",action:"play",mediaMs:0}), true);
    assert.equal(sent.length, 1);
    assert.match(ctx.controlFeedback, /awaiting acknowledgement/);
    assert.equal(ctx.pendingControl.operationId, "control-id-1");
    assert.equal(ctx.submitControl({type:"schedule",action:"pause",mediaMs:0}), false);
    assert.equal(sent.length, 1, "unacknowledged command must not send twice");
    ctx.handleControlAck({operationId:"older",applied:true});
    ctx.handleControlError({operationId:"older",code:"not_ready"});
    assert.equal(ctx.pendingControl.operationId, "control-id-1", "stale replies cleared live request");
    ctx.handleControlAck({operationId:"control-id-1",applied:true});
    assert.equal(ctx.pendingControl, null);
    assert.match(ctx.controlFeedback, /accepted by coordinator/);
    assert.match(ctx.controlFeedback, /not audible output/);
    assert.doesNotMatch(ctx.controlFeedback, /playing now|playback started/);
}
{
    const {ctx, sent} = setup();
    ctx.submitControl({type:"track",sha256:"a".repeat(64),durationMs:25000});
    ctx.handleControlError({operationId:sent[0].operationId,code:"not_ready"});
    assert.equal(ctx.pendingControl, null, "failed request must restore controls");
    assert.match(ctx.controlFeedback, /all room members must be ready/);
    ctx.submitControl({type:"schedule",action:"seek",mediaMs:0});
    ctx.handleControlAck({operationId:sent[1].operationId,applied:false});
    assert.match(ctx.controlFeedback, /prior operation/);
    assert.doesNotMatch(ctx.controlFeedback, /accepted by coordinator/);
}
{
    const {ctx, sent} = setup();
    ctx.socket.status = 3;
    assert.equal(ctx.submitControl({type:"schedule",action:"pause",mediaMs:0}), false);
    assert.equal(sent.length, 0);
    assert.equal(ctx.pendingControl, null);
    assert.match(ctx.controlFeedback, /not sent/);
}
{
    const {ctx, sent} = setup();
    ctx.submitControl({type:"schedule",action:"play",mediaMs:0});
    ctx.handleControlError({operationId:sent[0].operationId,code:"<img src=x>"});
    assert.doesNotMatch(ctx.controlFeedback, /<img|src=x>/, "untrusted server text leaked");
}
assert.match(qml, /BusyIndicator\s*\{[\s\S]*?window\.pendingControl !== null/);
assert.match(qml, /running:\s*window\.pendingControl !== null/);
assert.match(qml, /window\.handleControlAck\(msg\)/);
assert.match(qml, /window\.handleControlError\(msg\)/);
assert.match(qml, /Reconnected; retrying the pending command with the same operation ID/);
assert.match(qml, /Coordinator:.*window\.roomState\.playback\.playState/);
console.log("QML correlated control feedback regressions passed");