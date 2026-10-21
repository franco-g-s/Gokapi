// Tests the incremental SHA-1 used for the group duplicate pre-check (G7).
// Run: node build/dev/sha1_test.mjs
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const Sha1 = require(path.join(here, "../../internal/webserver/web/static/js/sha1.js"));

let failures = 0;
function check(name, got, want) {
    if (got === want) {
        console.log("PASS " + name);
    } else {
        console.log("FAIL " + name + ": got " + got + ", want " + want);
        failures++;
    }
}

function hashOf(parts) {
    const h = new Sha1();
    for (const p of parts) h.update(p);
    return h.digest();
}

const ascii = s => new TextEncoder().encode(s);

check("empty", hashOf([]), "da39a3ee5e6b4b0d3255bfef95601890afd80709");
check("empty update", hashOf([new Uint8Array(0)]), "da39a3ee5e6b4b0d3255bfef95601890afd80709");
check("abc", hashOf([ascii("abc")]), "a9993e364706816aba3e25717850c26c9cd0d89d");
check("abc split", hashOf([ascii("a"), ascii("bc")]), "a9993e364706816aba3e25717850c26c9cd0d89d");
check("448-bit message", hashOf([ascii("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")]),
    "84983e441c3bd26ebaae4aa1f95129e5e54670f1");

// 1,000,000 x "a" fed in uneven chunks: 1, 63, 64, 65, 4 MiB slices of the rest
const million = new Uint8Array(1000000).fill(0x61);
const sizes = [1, 63, 64, 65];
const parts = [];
let offset = 0;
for (const size of sizes) {
    parts.push(million.subarray(offset, offset + size));
    offset += size;
}
while (offset < million.length) {
    const end = Math.min(offset + 4 * 1024 * 1024, million.length);
    parts.push(million.subarray(offset, end));
    offset = end;
}
check("million a, uneven chunks", hashOf(parts), "34aa973cd4c4daa4f61eeb2bdbad27316534016f");
check("million a, one update", hashOf([million]), "34aa973cd4c4daa4f61eeb2bdbad27316534016f");

// Byte at a time across several block boundaries
const bytes = [];
for (let i = 0; i < 200; i++) bytes.push(new Uint8Array([0x61]));
const whole = new Uint8Array(200).fill(0x61);
check("byte at a time", hashOf(bytes), hashOf([whole]));

// Input buffers are not modified
const input = ascii("abc");
hashOf([input]);
check("input untouched", Array.from(input).join(","), "97,98,99");

if (failures > 0) {
    console.log(failures + " failed");
    process.exit(1);
}
console.log("all passed");
