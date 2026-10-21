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
    const grid = { classList: classList(), clientWidth: 0, children: [], style: {} };
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
check("group: other choices per context", FGS.browserPrefKey(makeRoot(group), "hideDownloaded"), "fgsBrowser.group.hideDownloaded");
check("files: view key", FGS.browserPrefKey(makeRoot(files), "view"), "fgsBrowser.files.view");

if (failures) {
    console.log(failures + " failed");
    process.exit(1);
}
console.log("all passed");
