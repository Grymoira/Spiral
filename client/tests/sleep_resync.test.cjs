"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// Run the actual functions embedded in the distributed QML component.
const source = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extract(name) {
    const match = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(source);
    assert.ok(match, "missing QML function " + name);
    const start = source.indexOf("{", match.index);
    let depth = 0;
    for (let i = start; i < source.length; i++) {
        if (source[i] === "{") depth++;
        else if (source[i] === "}" && --depth === 0)
            return source.slice(match.index, i + 1);
    }
    throw Error("unterminated " + name);
}
const methods = [
    "send", "requestWakeSnapshot", "acceptWakeSnapshot", "guardClockContinuity",
    "revokeReady", "probeClock", "receiveClock", "maybeReady",
    "safeguardAuthority", "authorityFresh", "pendingUsable",
    "deferPending", "armPending", "acknowledgeArming", "executePending",
    "reconcile", "sameRecording", "mediaLoaded", "ownReady",
    "predictedPosition"
].map(extract).join("\n");

function createScenario() {
    const hash = "a".repeat(64);
    const mock = {now: 2000, plays: 0, pauses: 0, failContinuity: false,
        sent: [], logs: [], requests: 0};
    const player = {mediaStatus: 1, playbackState: 0, position: 0,
        play() {mock.plays++; this.playbackState=2;},
        pause() {mock.pauses++; this.playbackState=0;}};
    const playbackTimer = {running: true, interval: 50,
        start(){this.running=true;}, stop(){this.running=false;}};
    const state = {roomId: "room", roomEpoch:"room-epoch", stateRevision:12,
        clockEpoch:"server-clock",
        playback:{trackId:hash,trackGeneration:1,durationMs:10000,playState:"paused",
            anchorMediaMs:0,anchorServerMs:0,
            pending:{id:7,phase:"committed",playState:"playing",
                executeServerMs:2010,mediaMs:0}},
        members:[{id:"guest", ready:true}]};
    const context = {
        authenticated:true, clockReady:true, roomId:"room", memberId:"guest",
        clockEpoch:"server-clock", clockOffsetMs:0, bestRttMs:10,
        clockSamples:3, lastClockSampleMs:1900, lastAuthorityReplyMs:1950,
        authorityStaleAfterMs:2500, probeSequence:10, pendingProbes:{old:1800},
        readySentKey:hash+":1", scheduledKey:"1:7",
        deferredPendingKey:"", finishedPendingKey:"", armSentKey:"",
        cancellationAckKey:"", cancellationAckConfirmedKey:"", cancellationAckLastSentMs:-1,
        wakeSnapshotId:"", wakeSnapshotLastSentMs:-1,
        selectedHash:hash, maxLatePlayMs:250, lastCorrectionMs:-100000,
        roomState:state, logText:"",
        socket:{status:1,sendTextMessage(data){mock.sent.push(JSON.parse(data));}},
        WebSocket:{Open:1},
        player, playbackTimer, MediaPlayer:{LoadedMedia:1,BufferedMedia:2,PlayingState:2},
        syncTools:{
            monotonicMs:()=>mock.now,
            newOperationId:()=>{mock.requests++; return "wake-request-"+mock.requests;},
            clockContinuityOk:()=>{
                if (mock.failContinuity) {mock.failContinuity=false; return false;}
                return true;
            },
            logEvent:event=>mock.logs.push(event),
            logEventLevel:event=>mock.logs.push(event)
        }
    };
    vm.createContext(context);
    vm.runInContext(methods,context,{filename:"client/qml/Main.qml"});
    return {context,mock,player,playbackTimer,state,hash};
}

function count(sent,type) {return sent.filter(m=>m.type===type).length;}

// Detect system suspend BEFORE a queued play timer can execute; a stale
// monotonic clock must not be interpreted as a fresh 2.5-second lease.
{
    const {context,mock,player,playbackTimer,state}=createScenario();
    mock.now=2010;
    mock.failContinuity=true;
    context.executePending();
    assert.equal(mock.plays,0,"resume from sleep executed a pre-suspend play");
    assert.equal(context.clockReady,false);
    assert.equal(context.lastAuthorityReplyMs,-1);
    assert.equal(player.playbackState,0);
    assert.equal(playbackTimer.running,false);
    assert.equal(context.scheduledKey,"");
    assert.equal(context.finishedPendingKey,"");
    assert.equal(context.pendingProbes.old,undefined,"old probes survived suspend");
    assert.equal(count(mock.sent,"ready"),1);
    assert.equal(mock.sent[0].ready,false,"Ready was not explicitly withdrawn");
    assert.equal(count(mock.sent,"snapshot"),1,"fresh snapshot not requested");
    assert.equal(count(mock.sent,"clock_probe"),1,"clock recalibration not restarted");
    assert.equal(mock.sent[1].operationId,context.wakeSnapshotId);
    assert.ok(mock.logs.includes("client.clock_discontinuity"));

    // Messages buffered before the snapshot request must not lift the fence.
    context.clockReady=true;
    context.lastAuthorityReplyMs=mock.now;
    context.reconcile();
    assert.equal(mock.plays,0,"old state restarted while resync outstanding");
    context.maybeReady();
    assert.equal(count(mock.sent,"ready"),1,"Ready restored from stale state");

    assert.equal(context.acceptWakeSnapshot({operationId:"older-request"}),false);
    assert.ok(context.wakeSnapshotId,"incorrect snapshot released wake fence");
    mock.now=3200;
    context.requestWakeSnapshot();
    assert.equal(count(mock.sent,"snapshot"),1,"snapshot retry bypassed throttle");
    mock.now=3300;
    context.requestWakeSnapshot();
    assert.equal(count(mock.sent,"snapshot"),2,"unacknowledged snapshot was not retried");
    assert.equal(mock.sent.at(-1).operationId,context.wakeSnapshotId);

    // An authorized server handles Ready(false) before our snapshot request.
    // Its latest reply therefore represents paused state, not obsolete COMMIT.
    state.playback.pending=null;
    state.playback.playState="paused";
    state.members[0].ready=false;
    const request=context.wakeSnapshotId;
    assert.equal(context.acceptWakeSnapshot({operationId:request,state}),true);
    assert.equal(context.wakeSnapshotId,"");
    context.reconcile();
    assert.equal(mock.plays,0,"paused authoritative snapshot restarted audio");
    context.maybeReady();
    assert.equal(count(mock.sent,"ready"),2,"fresh snapshot did not allow readiness recovery");
}

// An authoritative reply that reports *already playing* must never lift the
// wake fence. It may represent a COMMIT missed while the process was asleep.
{
    const {context,mock,player}=createScenario();
    mock.failContinuity=true;
    context.safeguardAuthority();
    const id=context.wakeSnapshotId;
    assert.ok(id);
    const playing={
        roomId:"room", roomEpoch:"room-epoch", stateRevision:13,
        playback:{trackId:"b".repeat(64),trackGeneration:2,
            playState:"playing",pending:null}
    };
    const before=count(mock.sent,"ready");
    assert.equal(context.acceptWakeSnapshot({operationId:id,state:playing}),false);
    assert.equal(context.wakeSnapshotId,id,"missed COMMIT released the wake gate");
    assert.equal(count(mock.sent,"ready"),before+1);
    const latest=mock.sent.filter(m=>m.type==="ready").at(-1);
    assert.equal(latest.ready,false);
    assert.equal(latest.trackGeneration,2,"revoke must use server's NEW generation");
    assert.equal(player.playbackState,0);
    context.clockReady=true;
    context.lastAuthorityReplyMs=mock.now;
    context.reconcile();
    assert.equal(mock.plays,0);
    const paused={...playing,playback:{...playing.playback,playState:"paused"}};
    assert.equal(context.acceptWakeSnapshot({operationId:id,state:paused}),true);
    assert.equal(context.wakeSnapshotId,"");
}

// A delayed old clock reply cannot recalibrate a resumed client before the
// new snapshot is correlated and a fresh three-probe model is established.
{
    const {context,mock}=createScenario();
    mock.failContinuity=true;
    context.receiveClock({clockEpoch:"server-clock",probeId:"old",
        c0:1800,s1:1800,s2:1800});
    assert.equal(context.clockReady,false);
    assert.equal(context.clockSamples,0);
    assert.equal(context.lastAuthorityReplyMs,-1);
    assert.equal(count(mock.sent,"snapshot"),1);
    const sentAt=mock.sent.length;
    context.receiveClock({clockEpoch:"server-clock",probeId:"old",
        c0:1800,s1:1800,s2:1800});
    assert.equal(context.clockSamples,0,"old clock reply was admitted");
    assert.equal(mock.sent.length,sentAt);
    assert.equal(count(mock.sent,"ready"),1);
}

// The handler can only release the wake fence from the actual correlated
// authoritative snapshot and does so before Ready/reconciliation calls.
assert.match(source,/window\.acceptWakeSnapshot\(msg\)/);
assert.match(source,/window\.roomState = msg\.state\s*window\.acceptWakeSnapshot\(msg\)/);
assert.match(source,/window\.requestWakeSnapshot\(\)/);
console.log("QML suspend/resume, missed timer and correlated snapshot regressions passed");
