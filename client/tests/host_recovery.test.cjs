"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
const startMatch = /\bfunction\s+removeGuest\s*\(/.exec(qml);
assert.ok(startMatch, "missing shipping QML host-only member recovery action");
const begin = qml.indexOf("{", startMatch.index);
let depth = 0, source = "";
for (let i = begin; i < qml.length; ++i) {
    if (qml[i] === "{") ++depth;
    else if (qml[i] === "}" && --depth === 0) {
        source = qml.slice(startMatch.index, i + 1);
        break;
    }
}
assert.ok(source, "unterminated member removal action");

function setup() {
    const calls = [], events = [];
    function XHR() {
        const request = {method:"", url:"", headers:{}, readyState:0, status:204};
        request.open = function(method, url) { this.method = method; this.url = url; };
        request.setRequestHeader = function(key, value) {this.headers[key] = value;};
        request.send = function() {
            calls.push(this);
            this.readyState = XHR.DONE;
            this.onreadystatechange();
        };
        return request;
    }
    XHR.DONE = 4;
    const ctx = {
        authenticated:true, memberRole:"host", memberId:"host-id", memberToken:"secret-bearer",
        roomId:"room-id", serverUrl:"http://127.0.0.1:8787", logText:"",
        removingGuestId:"", removalFeedback:"", roomSessionGeneration:1,
        syncTools:{
            validatedServerBase: url => url,
            logEvent: e => events.push(e),
            logEventLevel: e => events.push(e)
        },
        XMLHttpRequest:XHR, console
    };
    vm.createContext(ctx);
    vm.runInContext(source,ctx,{filename:"client/qml/Main.qml"});
    return {ctx,calls,events};
}
{
    const {ctx,calls,events}=setup();
    ctx.removeGuest("guest-id");
    assert.equal(calls.length,1);
    assert.equal(calls[0].method,"DELETE");
    assert.equal(calls[0].url,"http://127.0.0.1:8787/v1/rooms/room-id/members/guest-id");
    assert.equal(calls[0].headers.Authorization,"Bearer secret-bearer");
    assert.ok(!calls[0].url.includes("secret-bearer"),"token leaked into the request URL");
    assert.ok(events.includes("client.member_removed"));
    assert.match(ctx.logText,/Member removed/);
}
{
    const {ctx,calls}=setup();
    ctx.memberRole="member";
    ctx.removeGuest("other");
    ctx.memberRole="host";
    ctx.authenticated=false;
    ctx.removeGuest("other");
    ctx.authenticated=true;
    ctx.removeGuest("host-id");
    ctx.removeGuest("");
    assert.equal(calls.length,0,"QML sent a removal request without host eligibility");
}
{
    const {ctx,calls,events}=setup();
    ctx.removeGuest("guest-id");
    const request=calls[0];
    request.status=403;
    request.onreadystatechange();
    assert.match(ctx.logText,/HTTP 403/);
    assert.ok(events.includes("client.room_error"));
    assert.ok(!ctx.logText.includes("secret-bearer"),"authorization leaked into error");
}
assert.match(qml,/onClicked:window\.confirmGuestRemoval\(modelData\.id\)/,
    "host recovery action is not wired into shipping member UI");
console.log("QML host-only stuck-member recovery regressions passed");
