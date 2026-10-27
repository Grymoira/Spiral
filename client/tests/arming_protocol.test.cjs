"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extract(name) {
    const match = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(match, "missing shipped QML function " + name);
    const start = qml.indexOf("{", match.index);
    let depth = 0;
    for (let i = start; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0) return qml.slice(match.index, i + 1);
    }
    throw Error("function never closes: " + name);
}
const code = ["acknowledgeArming", "armPending", "pendingUsable", "deferPending",
    "executePending", "reconcile", "sameRecording", "mediaLoaded",
    "ownReady", "predictedPosition"].map(extract).join("\n");

function scene() {
    const hash = "a".repeat(64), sent = [], clock = {now: 1000, plays: 0};
    const timer = {running:false, interval:0,
        stop() {this.running = false;},
        start() {this.running = true;}
    };
    const player = {mediaStatus:1, playbackState:0, position:0,
        pause() {this.playbackState = 0;},
        play() {this.playbackState = 2; clock.plays++;}
    };
    const context = {
        authenticated:true, clockReady:true, clockOffsetMs:0, selectedHash:hash,
        memberId:"guest", armSentKey:"", scheduledKey:"", deferredPendingKey:"",
        finishedPendingKey:"", maxLatePlayMs:250, lastCorrectionMs:-100000,
        socket:{status:1}, WebSocket:{Open:1}, send:m=>sent.push(m),
        roomState: {playback: {trackId:hash, trackGeneration:1,
            durationMs:10000, playState:"paused", anchorMediaMs:0, anchorServerMs:0,
            pending:{id:7, phase:"arming", executeServerMs:0,
                armExpiresServerMs:6000, playState:"playing", mediaMs:0}},
            members:[{id:"guest", ready:true}]},
        player, playbackTimer:timer,
        MediaPlayer:{LoadedMedia:1, BufferedMedia:2, PlayingState:2},
        syncTools:{monotonicMs:()=>clock.now, logEvent(){}}
    };
    vm.createContext(context);
    vm.runInContext(code, context, {filename:"client/qml/Main.qml"});
    return {context,clock,timer,player,sent,hash};
}
{
    const {context,clock,timer,sent}=scene();
    context.reconcile();
    assert.equal(sent.length,1);
    assert.equal(sent[0].type,"arm_ack");
    assert.equal(sent[0].eventId,7);
    assert.equal(timer.running,false,"ARM must never start a play timer");
    clock.now=7000;
    context.executePending();
    assert.equal(clock.plays,0,"ARM must not become audible");
    context.reconcile();
    assert.equal(sent.length,1,"cannot resend duplicate arming ACK");
    context.roomState.playback.pending.phase="committed";
    context.roomState.playback.pending.executeServerMs=9500;
    context.reconcile();
    assert.equal(timer.interval,2500);
    assert.equal(timer.running,true);
    timer.running=false;
    clock.now=9500;
    context.executePending();
    assert.equal(clock.plays,1);
    context.executePending();
    assert.equal(clock.plays,1,"no replay of COMMIT");
    context.roomState.playback.pending=null;
    context.reconcile();
    assert.equal(context.armSentKey,"");
}
{
    const {context,player,timer,sent}=scene();
    player.mediaStatus=0;
    context.reconcile();
    assert.equal(sent.length,0,"unloaded media cannot ACK");
    player.mediaStatus=1;
    context.clockReady=false;
    context.reconcile();
    assert.equal(sent.length,0,"uncalibrated clock cannot ACK");
    context.clockReady=true;
    context.roomState.members[0].ready=false;
    context.reconcile();
    assert.equal(sent.length,0,"unready participant cannot ACK");
    context.roomState.members[0].ready=true;
    context.reconcile();
    assert.equal(sent.length,1);
    assert.equal(timer.running,false);
    context.roomState.playback.pending=null;
    context.reconcile();
    assert.equal(context.armSentKey,"");
}
{
    const {context,sent,hash}=scene();
    context.selectedHash="b".repeat(64);
    context.reconcile();
    assert.equal(sent.length,0,"wrong track cannot ACK");
    context.selectedHash=hash;
    context.socket.status=0;
    context.reconcile();
    assert.equal(sent.length,0,"closed socket cannot ACK");
    context.socket.status=1;
    context.reconcile();
    assert.equal(sent.length,1);
    context.roomState.playback.pending={id:8,phase:"arming",
        executeServerMs:0,playState:"playing",mediaMs:0};
    context.reconcile();
    assert.equal(sent.length,2,"replacement event gets a fresh ACK");
    assert.equal(sent[1].eventId,8);
}
console.log("QML event arming and commitment regressions passed");
