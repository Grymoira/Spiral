"use strict";
const assert=require("node:assert/strict"), fs=require("node:fs"), path=require("node:path"), vm=require("node:vm");
const qml=fs.readFileSync(path.join(__dirname,"../qml/Main.qml"),"utf8");
function fn(name){
 const match=new RegExp("\\bfunction\\s+"+name+"\\s*\\(").exec(qml);
 assert.ok(match,"missing QML method "+name);
 let depth=0, pos=qml.indexOf("{",match.index);
 for(let i=pos;i<qml.length;i++){
  if(qml[i]==="{")depth++;
  else if(qml[i]==="}"&&--depth===0)return qml.slice(match.index,i+1);
 }
 throw Error("unclosed method "+name);
}
function setup(){
 const sent=[],events=[],dialog={targetMemberId:"",originRoomId:"",originSession:-1,opens:0,open(){this.opens++}};
 function XMLHttpRequest(){
  const r={method:"",url:"",headers:{},readyState:0,status:0};
  r.open=function(m,u){this.method=m;this.url=u;};
  r.setRequestHeader=function(k,v){this.headers[k]=v;};
  r.send=function(){sent.push(this);};
  return r;
 }
 XMLHttpRequest.DONE=4;
 const ctx={
  authenticated:true,memberRole:"host",roomId:"room-a",memberId:"host-id",
  memberToken:"bearer-secret",roomSessionGeneration:2,
  roomState:{members:[{id:"host-id",role:"host"},{id:"guest-a",role:"member"}]},
  removingGuestId:"",removalFeedback:"",logText:"",serverUrl:"http://127.0.0.1:8787",
  guestRemovalDialog:dialog,XMLHttpRequest,
  syncTools:{validatedServerBase:v=>v,logEvent:e=>events.push(e),logEventLevel:e=>events.push(e)}
 };
 vm.createContext(ctx);
 vm.runInContext(["canRemoveGuest","confirmGuestRemoval","approveGuestRemoval","removeGuest"].map(fn).join("\n"),ctx);
 return {ctx,sent,events,dialog};
}
{
 const {ctx,sent,dialog}=setup();
 assert.equal(ctx.confirmGuestRemoval("guest-a"),true);
 assert.equal(dialog.opens,1);
 assert.equal(sent.length,0,"opening confirmation must not send DELETE");
 assert.equal(ctx.approveGuestRemoval("guest-a","room-a",2),true);
 assert.equal(sent.length,1);
 assert.equal(ctx.removingGuestId,"guest-a");
 ctx.removeGuest("guest-a");
 assert.equal(sent.length,1,"duplicate in-flight request");
 assert.equal(sent[0].method,"DELETE");
 assert.ok(!sent[0].url.includes("bearer-secret"));
 assert.equal(sent[0].headers.Authorization,"Bearer bearer-secret");
 sent[0].readyState=4;sent[0].status=204;sent[0].onreadystatechange();
 assert.equal(ctx.removingGuestId,"");
 assert.match(ctx.removalFeedback,/accepted/);
}
{
 const {ctx,sent,dialog}=setup();
 ctx.confirmGuestRemoval("guest-a");
 assert.equal(ctx.approveGuestRemoval("guest-a","room-a",1),false);
 assert.equal(sent.length,0,"old session allowed removal");
 ctx.roomState.members=[{id:"host-id",role:"host"}];
 assert.equal(ctx.approveGuestRemoval("guest-a","room-a",2),false);
 assert.equal(sent.length,0,"stale membership allowed removal");
 ctx.memberRole="member";
 assert.equal(ctx.confirmGuestRemoval("guest-a"),false);
 assert.equal(dialog.opens,1);
}
{
 const {ctx,sent}=setup();
 ctx.confirmGuestRemoval("guest-a");ctx.approveGuestRemoval("guest-a","room-a",2);
 ctx.roomSessionGeneration++;
 ctx.removingGuestId="";ctx.removalFeedback="";
 sent[0].readyState=4;sent[0].status=204;sent[0].onreadystatechange();
 assert.equal(ctx.removalFeedback,"","stale response modified new session");
}
{
 const {ctx,sent}=setup();
 ctx.confirmGuestRemoval("guest-a");ctx.approveGuestRemoval("guest-a","room-a",2);
 sent[0].readyState=4;sent[0].status=403;sent[0].onreadystatechange();
 assert.equal(ctx.removingGuestId,"");
 assert.match(ctx.removalFeedback,/HTTP 403/);
 assert.ok(!ctx.removalFeedback.includes("bearer-secret"));
}
assert.match(qml,/standardButtons:\s*Dialog\.Ok\s*\|\s*Dialog\.Cancel/);
assert.match(qml,/onAccepted:\s*window\.approveGuestRemoval/);
assert.match(qml,/onRejected:\s*window\.removalFeedback/);
assert.match(qml,/onClicked:window\.confirmGuestRemoval\(modelData\.id\)/);
assert.match(qml,/No response yet; removal outcome unknown/);
console.log("QML guest removal confirmation and in-flight safety regressions passed");
