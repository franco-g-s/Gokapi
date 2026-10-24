// Tests the badge and circle of a person (Part 4, S2): FGS.fillAvatar puts in
// a photo when there is one and the initials otherwise, with setters only, so
// a display name such as "<b>x</b>" stays text; a photo that fails to load
// falls back to the initials kept in data-initials.
// Run: node build/dev/avatar_test.mjs
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

// Just enough of an element: no innerHTML at all, so any use of it is
// caught as a missing setter rather than parsed
function element(tag) {
    const attrs = new Map();
    const classes = new Set();
    const el = {
        tagName: tag.toUpperCase(), children: [], parentNode: null, _text: "",
        classList: {
            add: c => classes.add(c), remove: c => classes.delete(c), contains: c => classes.has(c)
        },
        get className() { return [...classes].join(" "); },
        set className(v) { classes.clear(); String(v).split(/\s+/).filter(Boolean).forEach(c => classes.add(c)); },
        setAttribute: (n, v) => attrs.set(n, String(v)),
        getAttribute: n => (attrs.has(n) ? attrs.get(n) : null),
        hasAttribute: n => attrs.has(n),
        appendChild(child) { child.parentNode = el; el.children.push(child); return child; },
        get textContent() { return el._text + el.children.map(c => c.textContent).join(""); },
        set textContent(v) { el._text = String(v); el.children.forEach(c => { c.parentNode = null; }); el.children = []; }
    };
    Object.defineProperty(el, "innerHTML", { set() { throw new Error("innerHTML used"); } });
    return el;
}

const listeners = [];
const window = {};
const document = {
    readyState: "loading",
    addEventListener(type, fn, capture) { listeners.push({ type, fn, capture }); },
    createElement: element
};
vm.runInNewContext(source, {
    window, document, localStorage: null,
    navigator: { userAgent: "node", platform: "node", maxTouchPoints: 0 }
});
const FGS = window.FGS;

// A photo: one <img>, set with setters, the initials kept for later
const badge = element("span");
FGS.fillAvatar(badge, "AM", "./avatar?id=7&v=12");
check("photo: one child", badge.children.length, 1);
const img = badge.children[0];
check("photo: an img", img.tagName, "IMG");
check("photo: class", img.className, "fgs-avatar-img");
check("photo: src", img.getAttribute("src"), "./avatar?id=7&v=12");
check("photo: alt", img.getAttribute("alt"), "");
check("photo: lazy", img.getAttribute("loading"), "lazy");
check("photo: referrerpolicy", img.getAttribute("referrerpolicy"), "no-referrer");
check("photo: holder marked", badge.classList.contains("fgs-has-photo"), true);
check("photo: initials kept", badge.getAttribute("data-initials"), "AM");

// No photo: the initials as text
const plain = element("span");
FGS.fillAvatar(plain, "AM", "");
check("initials: no child", plain.children.length, 0);
check("initials: text", plain.textContent, "AM");
check("initials: not marked", plain.classList.contains("fgs-has-photo"), false);

// A display name with markup in it is only ever text
const name = "<b>x</b>";
const marked = element("span");
FGS.fillAvatar(marked, FGS.initialsOf(name), "");
// The same rule as people.InitialsOfName: parts "b", "x", "b"
check("markup name: initials", marked.textContent, "BB");
const raw = element("span");
FGS.fillAvatar(raw, name, "");
check("markup name: text, not elements", raw.children.length === 0 && raw.textContent === name, true);

// A photo that fails to load becomes the initials, through the capture-phase
// error listener registered when media.js loads
const onError = listeners.find(l => l.type === "error" && l.capture === true);
check("error listener in the capture phase", !!onError, true);
onError.fn({ target: img });
check("fallback: img gone", badge.children.length, 0);
check("fallback: initials", badge.textContent, "AM");
check("fallback: unmarked", badge.classList.contains("fgs-has-photo"), false);
// Other images are left alone
const thumb = element("img");
thumb.className = "fgs-thumb";
const tile = element("figure");
tile.appendChild(thumb);
onError.fn({ target: thumb });
check("other images untouched", tile.children.length, 1);

if (failures) {
    console.log(failures + " failed");
    process.exit(1);
}
console.log("all passed");
