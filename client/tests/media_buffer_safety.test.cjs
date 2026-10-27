"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// Execute actual production-QML methods, including the handler used by
// MediaPlayer.onMediaStatusChanged. No copied version of the algorithm.
const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extract(name) {
    const found = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(qml);
    assert.ok(found, "missing shipping QML function: " + name);
    const start = qml.indexOf("{", found.index);
    let depth = 0;
    for (let i = start; i < qml.length; i++) {
        if (qml[i] === "{") depth++;
        else if (qml[i] === "}" && --depth === 0)
            return qml.slice(found.index, i + 1);
    }
    throw Error("unclosed function " + name);
}
const methods = [
    "send", "handleMediaStatusChanged", "mediaLoaded", "requestWakeSnapshot",
    "acceptWakeSnapshot", "maybeReady", "reconcile", "guardClockContinuity",
    "safeguardAuthority", "authorityFresh", "pendingUsable", "armPending",
    "deferPending", "acknowledgeArming", "sameRecording", "ownReady",
    "predictedPosition"
].map(extract).join("\n");
function setup() {
    const hash = "a".repeat(64);
    const signal = {plays:0, pauses:0, sent:[], stops:0, starts:0, now:2000, logs:[]};
    const player = {mediaStatus:1, playbackState:2, duration:16000, position:1000,
        play(){signal.plays++;this.playbackState=2;},
        pause(){signal.pauses++;this.playbackState=0;}};
    const playbackTimer={running:true,interval:200,
        stop(){signal.stops++;this.running=false;},
        start(){signal.starts++;this.running=true;}};
    const ctx={
        selectedHash:hash,memberId:"guest",authenticated:true,clockReady:true,
        mediaFaulted:false,
        clockEpoch:"epoch",lastClockSampleMs:1990,lastAuthorityReplyMs:1990,
        authorityStaleAfterMs:2500,clockOffsetMs:0,
        wakeSnapshotId:"",wakeSnapshotLastSentMs:-1,
        readySentKey:hash+":1",scheduledKey:"1:7",deferredPendingKey:"1:7",
        finishedPendingKey:"",armSentKey:"1:7",cancellationAckKey:"",
        clockSamples:3,bestRttMs:5,pendingProbes:{},
        roomState:{roomId:"room",roomEpoch:"room-epoch",stateRevision:7,
            playback:{trackId:hash,trackGeneration:1,durationMs:16000,
                playState:"playing",anchorServerMs:1000,anchorMediaMs:0,
                pending:{id:7,phase:"committed",playState:"playing",
                    executeServerMs:2300,mediaMs:0}},
            members:[{id:"guest",ready:true}]},
        player,playbackTimer,
        MediaPlayer:{LoadedMedia:1,BufferedMedia:2,PlayingState:2},
        socket:{status:1,sendTextMessage(x){signal.sent.push(JSON.parse(x));}},
        WebSocket:{Open:1},logText:"",
        syncTools:{monotonicMs:()=>signal.now,clockContinuityOk:()=>true,
            newOperationId:()=>"buffer-recovery-123",
            logEvent:e=>signal.logs.push(e),logEventLevel:e=>signal.logs.push(e)}
    };
    vm.createContext(ctx);
    vm.runInContext(methods,ctx,{filename:"client/qml/Main.qml"});
    return {ctx,signal,player,playbackTimer,hash};
}
function sent(signal,type){return signal.sent.filter(v=>v.type===type);}

// 1) StalledMedia can coexist with PlayingState. The local audio must be
// forced paused before a server round-trip and all old appointments removed.
{
    const {ctx,signal,player,playbackTimer}=setup();
    player.mediaStatus=3; // e.g. StalledMedia: not Loaded/Buffered
    ctx.handleMediaStatusChanged();
    assert.equal(player.playbackState,0,"buffer stall left local audio playing");
    assert.ok(signal.pauses>0);
    assert.equal(playbackTimer.running,false,"stalled media left COMMIT timer armed");
    assert.equal(ctx.scheduledKey,"");
    assert.equal(ctx.deferredPendingKey,"");
    assert.equal(ctx.armSentKey,"");
    assert.equal(sent(signal,"ready").length,1);
    assert.equal(sent(signal,"ready")[0].ready,false);
    assert.equal(sent(signal,"snapshot").length,1);
    assert.equal(sent(signal,"snapshot")[0].operationId,ctx.wakeSnapshotId);
    assert.equal(signal.plays,0);

    // The file may finish buffering BEFORE the server processes Ready(false).
    // A healthy stale local roomState is not a fresh playback authorization.
    player.mediaStatus=1;
    ctx.handleMediaStatusChanged();
    assert.equal(sent(signal,"ready").length,1,"buffering recovered before resync");
    assert.equal(signal.plays,0,"buffer recovery autoplayed a stale COMMIT");
    ctx.reconcile();
    assert.equal(signal.plays,0,"reconcile ignored buffer recovery fence");
    assert.ok(ctx.wakeSnapshotId);

    // A correctly correlated reply that reports playing is NOT permission
    // to join late, and it causes renewed explicit Ready(false).
    const request=ctx.wakeSnapshotId;
    const playing={...ctx.roomState,playback:{...ctx.roomState.playback,
        pending:null,playState:"playing"}};
    assert.equal(ctx.acceptWakeSnapshot({operationId:request,state:playing}),false);
    assert.equal(ctx.wakeSnapshotId,request);
    assert.equal(sent(signal,"ready").length,2);
    assert.equal(sent(signal,"ready")[1].ready,false);
    assert.equal(signal.plays,0);

    // Once the server has actually paused and answered this same request,
    // readiness can be reannounced without restarting playback.
    const paused={...playing,members:[{id:"guest",ready:false}],
        playback:{...playing.playback,playState:"paused"}};
    assert.equal(ctx.acceptWakeSnapshot({operationId:request,state:paused}),true);
    ctx.roomState=paused;
    ctx.maybeReady();
    ctx.reconcile();
    assert.equal(ctx.wakeSnapshotId,"");
    assert.equal(sent(signal,"ready").length,3);
    assert.equal(sent(signal,"ready")[2].ready,true);
    assert.equal(signal.plays,0);
}

// A fatal Qt Multimedia error may leave mediaStatus == BufferedMedia.
// Retaining the prior Ready or relying only on mediaStatus is unsafe.
{
    const {ctx,signal,player}=setup();
    player.mediaStatus=2;
    ctx.mediaFaulted=true;
    ctx.handleMediaStatusChanged();
    assert.equal(player.playbackState,0);
    assert.equal(ctx.mediaLoaded(),false);
    assert.equal(sent(signal,"ready").length,1);
    assert.equal(sent(signal,"ready")[0].ready,false);
    assert.equal(sent(signal,"snapshot").length,1);
    ctx.mediaFaulted=true;
    ctx.maybeReady();
    assert.equal(sent(signal,"ready").length,1,"faulted media became Ready again");
}

// 2) Media invalidation when no READY key remains must still revoke the
// server-side membership, because the previous receipt may have been lost.
{
    const {ctx,signal,player}=setup();
    ctx.readySentKey="";
    player.mediaStatus=0;
    ctx.handleMediaStatusChanged();
    assert.equal(sent(signal,"ready").length,1);
    assert.equal(sent(signal,"ready")[0].ready,false);
    assert.equal(sent(signal,"snapshot").length,1);
}

// 3) The initial loading of an unconnected file is not a network room event.
{
    const {ctx,signal,player}=setup();
    ctx.authenticated=false;
    ctx.roomState={};
    player.mediaStatus=0;
    ctx.handleMediaStatusChanged();
    assert.equal(player.playbackState,0);
    assert.equal(sent(signal,"ready").length,0);
    assert.equal(sent(signal,"snapshot").length,0);
}
assert.match(qml,/onMediaStatusChanged:\s*window\.handleMediaStatusChanged\(\)/);
console.log("QML media buffer-loss fail-closed regressions passed");
