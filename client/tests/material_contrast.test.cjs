"use strict";
// This protects the source palette and theme inheritance. It is not a
// substitute for a real Windows/Qt screenshot or high-contrast UI audit.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const qml = fs.readFileSync(path.join(__dirname, "../qml/Main.qml"), "utf8");
assert.match(qml, /^import QtQuick\.Controls\.Material$/m);
assert.match(qml, /^\s*Material\.theme:\s*Material\.Dark\s*$/m);
assert.match(qml, /^\s*Material\.foreground:\s*window\.textColor\s*$/m);

function color(pattern, description) {
    const match = qml.match(pattern);
    assert.ok(match, `Missing ${description} palette value`);
    return match[1];
}
function linearChannel(value) {
    const channel = parseInt(value, 16) / 255;
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
}
function luminance(hex) {
    return 0.2126 * linearChannel(hex.slice(1, 3)) +
           0.7152 * linearChannel(hex.slice(3, 5)) +
           0.0722 * linearChannel(hex.slice(5, 7));
}
function contrast(a, b) {
    const x = luminance(a), y = luminance(b);
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}
const palette = {
    window: color(/^\s*color:\s*"(#[\da-f]{6})"\s*$/mi, "window"),
    panel: color(/background:\s*Rectangle\s*\{\s*color:\s*"(#[\da-f]{6})"/i, "panel"),
    control: color(/^\s*Material\.background:\s*"(#[\da-f]{6})"\s*$/mi, "controls"),
    text: color(/readonly property color textColor:\s*"(#[\da-f]{6})"/i, "body text"),
    secondary: color(/readonly property color secondary:\s*"(#[\da-f]{6})"/i, "secondary text"),
    accent: color(/^\s*Material\.accent:\s*"(#[\da-f]{6})"\s*$/mi, "accent")
};
for (const surface of ["window", "panel", "control"]) {
    for (const ink of ["text", "secondary"]) {
        assert.ok(contrast(palette[surface], palette[ink]) >= 4.5,
            `${ink} on ${surface} must have WCAG normal-text contrast >= 4.5:1`);
    }
    assert.ok(contrast(palette[surface], palette.accent) >= 3,
        `accent on ${surface} must have non-text contrast >= 3:1`);
}
const fields = qml.match(/TextField\s*\{[^}]*\}/g) || [];
assert.equal(fields.length, 3, "Review the palette contract when adding a text field");
for (const field of fields) {
    assert.match(field, /placeholderTextColor:\s*window\.secondary/,
        "Every input must keep placeholder text readable on dark controls");
}
console.log("QML dark Material palette source/contrast regressions passed (not visual GUI verification)");
