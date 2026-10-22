// Tests the shared file list's remembered view (F1): which localStorage key
// each context reads, the Files tab's legacy key, private mode, and
// FGS.presetView, which the inline script after each list root calls so the
// remembered view is in place before the first paint.
// Run: node build/dev/browser_view_test.mjs
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import vm from "node:vm";

const here = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(here, "../../internal/webserver/web/static/js/media.js"), "utf8");

let failures = 0;
function check(name, got, want) {
    if (got === want) {
        console.log("PASS " + name);
    } else {
        console.log("FAIL " + name + ": got " + got + ", want " + want);
        failures++;
    }
}

// A storage like localStorage; throwing: as in Safari's private mode
function storage(entries, throwing) {
    const m = new Map(Object.entries(entries || {}));
    const fail = () => { throw new Error("SecurityError"); };
    return {
        getItem: k => throwing ? fail() : (m.has(k) ? m.get(k) : null),
        setItem: (k, v) => throwing ? fail() : m.set(k, String(v)),
        dump: () => Object.fromEntries(m)
    };
}

// media.js with a document that is still loading, so nothing initialises
function loadMedia(store) {
    const window = { localStorage: store };
    const ctx = {
        window, localStorage: store, navigator: { userAgent: "node", platform: "node", maxTouchPoints: 0 },
        document: { readyState: "loading", addEventListener() {} }
    };
    vm.runInNewContext(source, ctx);
    return window.FGS;
}

function classList() {
    const s = new Set();
    return {
        add: c => s.add(c), remove: c => s.delete(c), contains: c => s.has(c),
        toggle: (c, on) => { if (on === undefined) on = !s.has(c); if (on) s.add(c); else s.delete(c); return on; }
    };
}
function button(layout) {
    const b = { classList: classList(), getAttribute: n => n === "data-layout" ? layout : null };
    b.classList.add("btn-outline-light");
    b.closest = () => root; // set per root below
    return b;
}
let root;
// The list root as html_file_browser.tmpl renders it, before media.js runs
function makeRoot(attrs) {
    const a = Object.assign({ "data-current-view": attrs["data-default-view"] }, attrs);
    const buttons = ["grid", "board", "list"].map(button);
    const grid = { classList: classList(), clientWidth: 0, children: [], style: { setProperty(k, v) { this[k] = v; } } };
    root = {
        getAttribute: n => (n in a ? a[n] : null),
        setAttribute: (n, v) => { a[n] = String(v); },
        querySelector: s => (s === "[data-fgs-grid]" ? grid : null),
        querySelectorAll: s => (s === "button[data-layout]" ? buttons : []),
        contains: () => true,
        buttons, grid
    };
    buttons.forEach(b => { b.closest = () => root; });
    return root;
}
function active(r) {
    return r.buttons.filter(b => b.classList.contains("btn-light")).map(b => b.getAttribute("data-layout")).join(",");
}
function preset(entries, attrs, throwing) {
    const r = makeRoot(attrs);
    loadMedia(storage(entries, throwing)).presetView(r);
    return r;
}

const group = { "data-context": "group", "data-group-id": "g123", "data-default-view": "board" };
const files = { "data-context": "files", "data-default-view": "grid" };

// A fresh browser: each context's own default, with its button active
let r = preset({}, group);
check("group, fresh: Board", r.getAttribute("data-current-view"), "board");
check("group, fresh: Board button active", active(r), "board");
check("group, fresh: the grid is in Board mode before the first paint", r.grid.classList.contains("fgs-board-mode"), true);
r = preset({}, files);
check("files, fresh: Grid", r.getAttribute("data-current-view"), "grid");
check("files, fresh: Grid button active", active(r), "grid");
check("files, fresh: no Board mode", r.grid.classList.contains("fgs-board-mode"), false);

// Each group remembers its own view
r = preset({ "fgsBrowser.group.g123.view": "list" }, group);
check("group: its own remembered List", r.getAttribute("data-current-view"), "list");
check("group: List button active", active(r), "list");
check("group: no Board mode in List", r.grid.classList.contains("fgs-board-mode"), false);
r = preset({ "fgsBrowser.group.other.view": "grid" }, group);
check("group: another group's Grid does not apply", r.getAttribute("data-current-view"), "board");
// The old per-context key is not migrated: every group starts on Board
r = preset({ "fgsBrowser.group.view": "grid" }, group);
check("group: the old shared key is ignored", r.getAttribute("data-current-view"), "board");

// The Files tab: its own key, then the legacy one
r = preset({ "fgsBrowser.files.view": "list", filesLayout: "grid" }, files);
check("files: own key first", r.getAttribute("data-current-view"), "list");
r = preset({ filesLayout: "list" }, files);
check("files: legacy filesLayout", r.getAttribute("data-current-view"), "list");
r = preset({ filesLayout: "list" }, { "data-context": "collection-public", "data-default-view": "list" });
check("collection: own default", r.getAttribute("data-current-view"), "list");
r = preset({ filesLayout: "board" }, { "data-context": "upload", "data-default-view": "grid" });
check("upload: legacy key is only the Files tab's", r.getAttribute("data-current-view"), "grid");
r = preset({ "fgsBrowser.collection-public.view": "grid" }, { "data-context": "collection-public", "data-default-view": "list" });
check("collection: remembered Grid", r.getAttribute("data-current-view"), "grid");

// Junk and private mode fall back to the default, and never throw
r = preset({ "fgsBrowser.files.view": "mosaic" }, files);
check("files: an unknown view is ignored", r.getAttribute("data-current-view"), "grid");
let threw = false;
try { r = preset({}, group, true); } catch (e) { threw = true; }
check("private mode: no exception", threw, false);
check("private mode: the default", r.getAttribute("data-current-view"), "board");
check("private mode: Board button active", active(r), "board");
const FGS = loadMedia(storage({}));
threw = false;
try { FGS.presetView(null); } catch (e) { threw = true; }
check("no root: no exception", threw, false);

// Choosing a view stores it under the same per-group key presetView reads;
// every other choice stays per context
check("group: view key per group", FGS.browserPrefKey(makeRoot(group), "view"), "fgsBrowser.group.g123.view");
// R1 (V1): the choices are remembered per list, as the view is
check("group: hide per group", FGS.browserPrefKey(makeRoot(group), "hideDownloaded"), "fgsBrowser.group.g123.hideDownloaded");
check("group: type per group", FGS.browserPrefKey(makeRoot(group), "kind"), "fgsBrowser.group.g123.kind");
check("group: sort per group", FGS.browserPrefKey(makeRoot(group), "sort"), "fgsBrowser.group.g123.sort");
check("group: shape stays per context", FGS.browserPrefKey(makeRoot(group), "shape"), "fgsBrowser.group.shape");
check("files: type key", FGS.browserPrefKey(makeRoot(files), "kind"), "fgsBrowser.files.kind");

// R1: Type, as the server's kindOf: the preview's kind, else the extension
check("kind: preview video", FGS.fileKind("video", "clip.bin"), "video");
check("kind: preview image", FGS.fileKind("image", "scan.bin"), "image");
check("kind: HEIC by name", FGS.fileKind("", "IMG_1.HEIC"), "image");
check("kind: m4v by name", FGS.fileKind("", "a.M4V"), "video");
check("kind: text", FGS.fileKind("", "notes.txt"), "other");
check("kind: svg", FGS.fileKind("", "logo.svg"), "other");
check("kind: no extension", FGS.fileKind("", "README"), "other");
check("isPhotoOrVideo follows it", FGS.isPhotoOrVideo("", "notes.txt") || !FGS.isPhotoOrVideo("", "a.mov"), false);
check("files: view key", FGS.browserPrefKey(makeRoot(files), "view"), "fgsBrowser.files.view");

// R3 (V9): the slider counts columns. The left end is the most that fit at
// >= 96 px (gap 12), the right end 1; the default is the old 160 px look
// (never under 2 columns); a remembered count is clamped to what fits.
let c = FGS.tileColumns(324, null);
check("390 phone: left end 3 columns", c.max, 3);
check("390 phone: default 2 columns", c.def, 2);
check("390 phone: fresh shows the default", c.cols, 2);
check("390 phone: remembered 8 clamps to 3", FGS.tileColumns(324, "8").cols, 3);
check("390 phone: remembered 1 stays 1", FGS.tileColumns(324, "1").cols, 1);
check("junk remembered: the default", FGS.tileColumns(324, "big").cols, 2);
check("pixels from the old size key are not columns", FGS.tileColumns(324, "0").cols, 2);
c = FGS.tileColumns(1000, null);
check("desktop: left end 9", c.max, 9);
check("desktop: default 5", c.def, 5);
check("tiny: 1 column at most", FGS.tileColumns(80, null).max, 1);
check("tiny: default clamped to 1", FGS.tileColumns(80, null).def, 1);
// Released within one step of the default: the default; else the nearest stop
check("snap: 4.4 next to 5", FGS.snapColumns(4.4, 5), 5);
check("snap: 5.9 next to 5", FGS.snapColumns(5.9, 5), 5);
check("snap: exactly one away stays", FGS.snapColumns(4, 5), 4);
check("snap: 3.6 rounds to 4", FGS.snapColumns(3.6, 5), 4);
check("snap: 1 (right end) on a phone stays 1", FGS.snapColumns(1, 2), 1);
check("snap: 3 (left end) on a phone stays 3", FGS.snapColumns(3, 2), 3);
// Before the first paint Board places its tiles in the remembered count
r = makeRoot(group);
r.grid.clientWidth = 324;
loadMedia(storage({ "fgsBrowser.group.cols-board": "1" })).presetView(r);
check("preset: Board gets the remembered count", r.grid.style["--fgs-cols"], "1");
check("preset: and the class that uses it", r.grid.classList.contains("fgs-has-cols"), true);

// Fix round 1: Grid and Board each have their own count and default.
// Board keeps its old look: max(2, round(W / 260)), within what fits.
check("board 1214: default 5", FGS.tileColumns(1214, null, "board").def, 5);
check("grid 1214: default 7", FGS.tileColumns(1214, null, "grid").def, 7);
check("board 324: default 2", FGS.tileColumns(324, null, "board").def, 2);
check("board 324: left end still 3", FGS.tileColumns(324, null, "board").max, 3);
check("board 80: default within what fits", FGS.tileColumns(80, null, "board").def, 1);
check("no view: Grid's default", FGS.tileColumns(1214, null).def, 7);
// Remembered per view; the shared "cols" of the first R3 build is Grid's
r = makeRoot(group); r.grid.clientWidth = 1214;
loadMedia(storage({ "fgsBrowser.group.cols": "3" })).presetView(r);
check("preset Board: the old shared count is not Board's", r.grid.style["--fgs-cols"], "5");
r = makeRoot(group); r.grid.clientWidth = 1214;
loadMedia(storage({ "fgsBrowser.group.cols-board": "4" })).presetView(r);
check("preset Board: its own count", r.grid.style["--fgs-cols"], "4");
r = makeRoot(files); r.grid.clientWidth = 1214;
loadMedia(storage({ "fgsBrowser.files.cols": "3" })).presetView(r);
check("preset Grid: the old shared count migrates to Grid", r.grid.style["--fgs-cols"], "3");
r = makeRoot(files); r.grid.clientWidth = 1214;
loadMedia(storage({ "fgsBrowser.files.cols": "3", "fgsBrowser.files.cols-grid": "9" })).presetView(r);
check("preset Grid: its own key wins", r.grid.style["--fgs-cols"], "9");

if (failures) {
    console.log(failures + " failed");
    process.exit(1);
}
console.log("all passed");
