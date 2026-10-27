"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
// Exercise shipped QML bodies, not a second implementation of the watchdog.
const source = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
function extract(name) {
    const m = new RegExp("\\bfunction\\s+" + name + "\\s*\\(").exec(source);
    assert.ok(m, "shipped QML missing " + name);
    const open = source.indexOf("{", m.index);
    let depth = 0;
    for (let i = open; i < source.length; ++i) {
        if (source[i] === "{") depth++;
        else if (source[i] === "}" && --depth === 0) return source.slice(m.index, i + 1);
    }
    throw new Error("unterminated " + name);
}
const code = [
    "authorityFresh", "safeguardAuthority", "pendingUsable", "revokeReady",
    "armPending", "deferPending", "acknowledgeArming",
    "executePending", "reconcile", "sameRecording", "mediaLoaded",
    "ownReady", "predictedPosition", "receiveClock"
].map(extract).join("\n");
function scenario() {
    const hash = "a".repeat(64);
    const mock = { now: 1000, plays: 0, pauses: 0, logs: [], sent: [] };
    const timer = { running: false, stop(){this.running=false;}, start(){this.running=true;}, interval:0 };
    const player = { position:0, mediaStatus:1, playbackState:2,
        play(){this.playbackState=2; mock.plays++;},
        pause(){this.playbackState=0; mock.pauses++;} };
    const context = {
        authenticated: true, clockReady: true, clockOffsetMs: 0,
        clockSamples: 3, bestRttMs: 10, lastClockSampleMs: 1000,
        lastAuthorityReplyMs: 900, authorityStaleAfterMs: 2500,
        clockEpoch: "epoch", pendingProbes: {}, scheduledKey: "", armSentKey:"",
        deferredPendingKey:"", finishedPendingKey:"", readySentKey:hash+":1",
        maxLatePlayMs:250, lastCorrectionMs:-100000,
        selectedHash:hash, memberId:"owner",
        roomState:{playback:{trackId:hash,trackGeneration:1,durationMs:10000,
            playState:"playing",anchorMediaMs:100,anchorServerMs:100,
            pending:null},members:[{id:"owner",ready:true}]},
        player, playbackTimer:timer,
        MediaPlayer:{LoadedMedia:1,BufferedMedia:2,PlayingState:2},
        syncTools:{monotonicMs:()=>mock.now,
            logEvent: event=>mock.logs.push(event),
            logEventLevel: event=>mock.logs.push(event)},
        send: m=>mock.sent.push(m), socket:{status:1},WebSocket:{Open:1},
        maybeReady(){}
    };
    vm.createContext(context);
    vm.runInContext(code, context, {filename:"client/qml/Main.qml"});
    return {mock,context,player,timer,hash};
}
{
    const {mock,context,player,timer} = scenario();
    assert.equal(context.authorityFresh(), true);
    mock.now=3500;
    assert.equal(context.authorityFresh(), false);
    timer.start();
    context.safeguardAuthority();
    assert.equal(mock.plays,0);
    assert.equal(context.clockReady,false);
    assert.equal(context.scheduledKey,"");
    assert.equal(timer.running,false);
    assert.equal(player.playbackState,0);
    assert.equal(mock.sent.length,1);
    assert.equal(mock.sent[0].type,"ready");
    assert.equal(mock.sent[0].ready,false);
    assert.ok(mock.logs.includes("client.authority_stale"));
    context.safeguardAuthority();
    assert.equal(mock.logs.filter(x=>x==="client.authority_stale").length,1,
        "watchdog must not flood log events");
}
{
    const {mock,context,player,timer} = scenario();
    context.roomState.playback.playState="paused";
    context.roomState.playback.pending={id:7,phase:"committed",
        playState:"playing",executeServerMs:4500,mediaMs:0};
    context.scheduledKey="1:7";
    timer.start();
    mock.now=4500;
    context.executePending();
    assert.equal(mock.plays,0,"partitioned client must not start from stale COMMIT");
    assert.equal(context.clockReady,false);
    assert.equal(timer.running,false);
    assert.equal(player.playbackState,0);
}
{
    const {mock,context,player} = scenario();
    mock.now=4000; // stale authority while room's last snapshot said 'playing'
    context.reconcile();
    assert.equal(mock.plays,0,"stale snapshot must not restart player");
    assert.equal(context.clockReady,false);
    // Recalibration and a recent authenticated reply allow resynchronization
    // only to the current room's authoritative state.
    context.clockReady=true;
    context.lastAuthorityReplyMs=mock.now;
    context.readySentKey=context.selectedHash+":1";
    context.reconcile();
    assert.equal(mock.plays,1);
    assert.equal(player.playbackState,2);
}
{
    const {mock,context} = scenario();
    mock.now=4000;
    context.pendingProbes.p1=3980;
    context.receiveClock({clockEpoch:"wrong",probeId:"p1",c0:3980,s1:100,s2:110});
    assert.equal(context.lastAuthorityReplyMs,900,"wrong epoch refreshed lease");
    context.receiveClock({clockEpoch:"epoch",probeId:"p1",c0:3980,s1:100,s2:110});
    assert.equal(context.lastAuthorityReplyMs,4000,"valid probe did not refresh lease");
}
{
    const {mock,context,player} = scenario();
    context.authenticated=false;
    context.roomState={}; // local playback must not be stopped by the room watchdog
    mock.now=90000;
    context.safeguardAuthority();
    assert.equal(player.playbackState,2);
    assert.equal(mock.pauses,0);
}
console.log("QML authoritative-contact watchdog regressions passed");
