"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname,"../qml/Main.qml"),"utf8");
function getFunc(name) {
 const m = new RegExp("\\bfunction\\s+"+name+"\\s*\\(").exec(source);
 assert.ok(m,"missing QML function "+name);
 const start=m.index,open=source.indexOf("{",start);let depth=0;
 for(let i=open;i<source.length;i++){if(source[i]==="{")depth++;else if(source[i]==="}"&&--depth===0)return source.slice(start,i+1)}
 throw Error("unclosed "+name);
}
const sent=[];
const ctx={
 roomState:{stateRevision:7,playback:{trackId:"",durationMs:0}},
 selectedHash:"a".repeat(64),memberRole:"host",authenticated:true,clockReady:true,roomId:"room",
 player:{duration:30000},pendingControl:null,logText:"",
 syncTools:{newOperationId:()=> "client-op-001",clockContinuityOk:()=>true,logEvent:()=>{}},
 socket:{status:1,sendTextMessage:data=>sent.push(JSON.parse(data))},
 WebSocket:{Open:1}
};
vm.createContext(ctx);
vm.runInContext(["guardClockContinuity","send","submitControl","shareTrack","reloadTrack","roomCommand"].map(getFunc).join("\n"),ctx);
ctx.shareTrack();
assert.equal(sent.length,1);
assert.equal(sent[0].operationId,"client-op-001");
assert.equal(sent[0].expectedRevision,7);
ctx.roomCommand("seek",0);
assert.equal(sent.length,1,"pending operation should block duplicate control");
assert.equal(ctx.pendingControl.operationId,"client-op-001");
ctx.pendingControl=null;
ctx.roomState.playback={trackId:ctx.selectedHash,durationMs:30000};
ctx.shareTrack();
assert.equal(sent.length,1,"same recording cannot silently reload Ready");
ctx.reloadTrack();
assert.equal(sent.length,2);
assert.equal(sent[1].reload,true);
assert.equal(sent[1].expectedRevision,7);
console.log("QML controlled track idempotency regression passed");
