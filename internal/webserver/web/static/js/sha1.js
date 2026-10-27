// Incremental SHA-1 for the group duplicate pre-check (Part 2, G7).
// Fed in slices, so a file of any size is hashed in constant memory. Loaded
// by sha1-worker.js through importScripts, and by node for its test
// (build/dev/sha1_test.mjs). SHA-1 is used here only because it is the
// fingerprint Gokapi already stores; it is not a security check.
(function (root) {
    "use strict";

    function Sha1() {
        this.h = new Int32Array([0x67452301, 0xEFCDAB89, 0x98BADCFE, 0x10325476, 0xC3D2E1F0]);
        this.block = new Uint8Array(64);
        this.blockLength = 0;
        this.totalBytes = 0;
        this.w = new Int32Array(80);
    }

    // Compresses the 64 bytes of data starting at offset into the state
    Sha1.prototype.compress = function (data, offset) {
        var w = this.w;
        var i;
        for (i = 0; i < 16; i++) {
            var j = offset + i * 4;
            w[i] = (data[j] << 24) | (data[j + 1] << 16) | (data[j + 2] << 8) | data[j + 3];
        }
        for (i = 16; i < 80; i++) {
            var x = w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16];
            w[i] = (x << 1) | (x >>> 31);
        }
        var h = this.h;
        var a = h[0], b = h[1], c = h[2], d = h[3], e = h[4];
        for (i = 0; i < 80; i++) {
            var f, k;
            if (i < 20) {
                f = (b & c) | (~b & d);
                k = 0x5A827999;
            } else if (i < 40) {
                f = b ^ c ^ d;
                k = 0x6ED9EBA1;
            } else if (i < 60) {
                f = (b & c) | (b & d) | (c & d);
                k = 0x8F1BBCDC;
            } else {
                f = b ^ c ^ d;
                k = 0xCA62C1D6;
            }
            var t = (((a << 5) | (a >>> 27)) + f + e + k + w[i]) | 0;
            e = d;
            d = c;
            c = (b << 30) | (b >>> 2);
            b = a;
            a = t;
        }
        h[0] = (h[0] + a) | 0;
        h[1] = (h[1] + b) | 0;
        h[2] = (h[2] + c) | 0;
        h[3] = (h[3] + d) | 0;
        h[4] = (h[4] + e) | 0;
    };

    // Adds bytes (a Uint8Array) to the hash. The input is not modified.
    Sha1.prototype.update = function (bytes) {
        var length = bytes.length;
        var offset = 0;
        this.totalBytes += length;
        if (this.blockLength > 0) {
            var fill = Math.min(64 - this.blockLength, length);
            this.block.set(bytes.subarray(0, fill), this.blockLength);
            this.blockLength += fill;
            offset = fill;
            if (this.blockLength < 64) {
                return this;
            }
            this.compress(this.block, 0);
            this.blockLength = 0;
        }
        for (; offset + 64 <= length; offset += 64) {
            this.compress(bytes, offset);
        }
        if (offset < length) {
            this.block.set(bytes.subarray(offset), 0);
            this.blockLength = length - offset;
        }
        return this;
    };

    // Returns the hash as 40 lower-case hex characters. The object is spent.
    Sha1.prototype.digest = function () {
        var bits = this.totalBytes * 8;
        var padding = new Uint8Array(((this.blockLength < 56) ? 56 : 120) - this.blockLength + 8);
        padding[0] = 0x80;
        var end = padding.length;
        // The length in bits, big-endian, as two 32-bit halves
        var high = Math.floor(bits / 0x100000000);
        var low = bits >>> 0;
        padding[end - 8] = high >>> 24;
        padding[end - 7] = (high >>> 16) & 0xff;
        padding[end - 6] = (high >>> 8) & 0xff;
        padding[end - 5] = high & 0xff;
        padding[end - 4] = low >>> 24;
        padding[end - 3] = (low >>> 16) & 0xff;
        padding[end - 2] = (low >>> 8) & 0xff;
        padding[end - 1] = low & 0xff;
        var total = this.totalBytes;
        this.update(padding);
        this.totalBytes = total;
        var hex = "";
        for (var i = 0; i < 5; i++) {
            hex += ("00000000" + (this.h[i] >>> 0).toString(16)).slice(-8);
        }
        return hex;
    };

    if (typeof module !== "undefined" && module.exports) {
        module.exports = Sha1;
    } else {
        root.FGSSha1 = Sha1;
    }
})(typeof self !== "undefined" ? self : this);
