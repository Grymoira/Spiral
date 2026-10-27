"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extract(name) {
    const m = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(m, "missing production QML function " + name);
    const start = qml.indexOf("{", m.index);
    let depth = 0;
    for (let i = start; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0) return qml.slice(m.index, i + 1);
    }
    throw Error("unclosed production QML function " + name);
}
const code = ["send", "cancellationKey", "applyCancellation",
    "transmitCancellationReceipt"].map(extract).join("\n");

function setup() {
    const mock = {now: 1000, messages: [], pauses: 0, stops: 0};
    const player = {playbackState: 2, pause() {this.playbackState = 0; mock.pauses++;}};
    const timer = {running:true, stop() {this.running = false; mock.stops++;}};
    const state = {
        roomEpoch: "room-epoch", stateRevision: 11,
        cancellation: {eventId: 7, trackGeneration: 1, stateRevision: 11},
        playback: {playState: "paused", pending: null}
    };
    const c = {
        authenticated:true, socket:{status:1,sendTextMessage(text) {mock.messages.push(JSON.parse(text));}},
        WebSocket:{Open:1}, MediaPlayer:{PlayingState:2},
        roomState: state, scheduledKey:"1:7", deferredPendingKey:"1:7",
        armSentKey:"1:7", cancellationAckKey:"", cancellationAckConfirmedKey:"",
        cancellationAckLastSentMs:-1, playbackTimer:timer, player,
        syncTools:{monotonicMs:()=>mock.now}
    };
    vm.createContext(c);
    vm.runInContext(code,c,{filename:"client/qml/Main.qml"});
    return {c,mock,player,timer,state};
}

// A retired event must be removed locally before any acknowledgement.
{
    const {c,mock,timer,player,state} = setup();
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,0,"must not ACK while old event is still queued");
    c.applyCancellation(state);
    assert.equal(timer.running,false);
    assert.equal(c.scheduledKey,"");
    assert.equal(c.deferredPendingKey,"");
    assert.equal(c.armSentKey,"");
    assert.equal(player.playbackState,0,"paused authoritative state must stop local audio");
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,1);
    assert.equal(mock.messages[0].type,"cancel_ack");
    assert.equal(mock.messages[0].roomEpoch,"room-epoch");
    assert.equal(mock.messages[0].trackGeneration,1);
    assert.equal(mock.messages[0].eventId,7);
    assert.equal(mock.messages[0].stateRevision,11);
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,1,"ACK should be throttled");
    mock.now=2300;
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,2,"unconfirmed ACK must retry after timeout");
    c.cancellationAckConfirmedKey=c.cancellationKey(state);
    mock.now=10000;
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,2,"confirmed ACK should stop retrying");
}

// The server's event revision, not just a reusable event ID, is required.
{
    const {c,mock,state,timer}=setup();
    state.cancellation.stateRevision=12;
    c.applyCancellation(state);
    c.transmitCancellationReceipt();
    assert.equal(timer.running,true,"future/invalid notice cannot cancel an appointment");
    assert.equal(mock.messages.length,0,"future revision must not be acknowledged");
}

// The ACK must not precede pausing when the authoritative room is paused.
{
    const {c,mock,state,player}=setup();
    player.pause = () => {mock.pauses++;}; // simulate backend refusing local transition
    c.applyCancellation(state);
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,0,"still-playing client must not claim successful cancellation");
}

// A newer appointment must not be stopped by an older cancellation tombstone.
{
    const {c,mock,state,timer,player}=setup();
    c.scheduledKey="1:8";
    state.playback.playState="playing";
    player.playbackState=2;
    c.applyCancellation(state);
    assert.equal(c.scheduledKey,"1:8");
    assert.equal(timer.running,true);
    assert.equal(mock.pauses,0);
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,1,"old event can be ACKed without stopping new one");
}

// No receipts are sent while unauthenticated or disconnected.
{
    const {c,mock,state}=setup();
    c.applyCancellation(state);
    c.authenticated=false;
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,0);
    c.authenticated=true;
    c.socket.status=0;
    c.transmitCancellationReceipt();
    assert.equal(mock.messages.length,0);
}

// Production snapshot handling must apply cancellation before it ACKs.
assert.match(qml,/window\.applyCancellation\(msg\.state\)/);
assert.match(qml,/window\.reconcile\(\)\s*window\.transmitCancellationReceipt\(\)/);
assert.match(qml,/msg\.type === "cancel_ack_ok"/);
console.log("QML cancellation receipt regressions passed");
