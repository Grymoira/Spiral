"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const crypto = require("node:crypto");
const path = require("node:path");
const root = path.join(__dirname, "..");
const project = path.join(root, "..");
const qml = fs.readFileSync(path.join(root, "qml/Main.qml"), "utf8");
const main = fs.readFileSync(path.join(root, "src/main.cpp"), "utf8");
const cmake = fs.readFileSync(path.join(root, "CMakeLists.txt"), "utf8");
const preview = fs.readFileSync(path.join(project, ".github/workflows/windows-preview.yml"), "utf8");
const licenses = fs.readFileSync(path.join(project, "docs/LICENSING.md"), "utf8");
const original = fs.readFileSync(path.join(root, "assets/fonts/LICENSE.md"), "utf8");
const fonts = {
    Regular: "10c73d7d901cf0dfa8ba03e45e3271d466291520",
    Medium: "492841d1b0227ecbc727f77bf48d1b69a729b5ac",
    Semibold: "6109ddd4af9f738e50939b2a502d76a967615b2c",
    Bold: "a032f5928a96b420267c95916da0a5958960915a"
};
for (const [face, expected] of Object.entries(fonts)) {
    const filename = "SourceCodePro-" + face + ".ttf";
    const bytes = fs.readFileSync(path.join(root, "assets/fonts", filename));
    assert.ok(bytes.length > 100000, "missing / truncated bundled font " + filename);
    assert.ok(bytes.subarray(0,4).equals(Buffer.from([0,1,0,0])), "not sfnt TTF " + filename);
    const gitBlobHash = crypto.createHash("sha1")
        .update(Buffer.from("blob " + bytes.length + "\0"))
        .update(bytes).digest("hex");
    assert.equal(gitBlobHash, expected, "upstream font differs: " + filename);
    assert.ok(cmake.includes(filename), "not in Qt resource target: " + filename);
    assert.ok(main.includes('":/fonts/' + filename + '"'), "font not loaded: " + filename);
}
assert.match(cmake, /qt_add_resources\(spiral-client "spiral-font-resources"/);
assert.match(qml, /^\s*font:\s*spiralUiFont\s*$/m);
assert.match(main, /setContextProperty\("spiralUiFont", uiFont\)/);
assert.match(main, /uiFont\.setFamilies\(/);
for (const family of ["Source Code Pro", "Microsoft YaHei UI", "Microsoft JhengHei UI",
                      "Yu Gothic UI", "Noto Sans CJK SC", "Noto Sans CJK TC", "Noto Sans CJK JP"]) {
    assert.ok(main.includes(family), "missing fallback font: " + family);
}
assert.match(licenses, /SIL Open Font License 1\.1/);
assert.match(licenses, /https:\/\/github\.com\/adobe-fonts\/source-code-pro/);
assert.match(original, /SIL OPEN FONT LICENSE Version 1\.1/);
assert.match(original, /Reserved Font Name 'Source'/);
assert.match(preview, /LICENSES\/Source-Code-Pro-OFL-1\.1\.txt/);
console.log("Source Code Pro bundled font, SHA-1 provenance, CJK fallback and license regressions passed (not visual verification)");
