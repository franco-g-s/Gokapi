// Web Worker that hashes a File with SHA-1 in 4 MiB slices (Part 2, G7), so
// the page stays responsive and memory stays flat whatever the file's size.
// Started by FGS.sha1File (admin_ui_groupupload.js), one file at a time.
//
// In:  {id, file}
// Out: {id, type: "progress", bytes}, then {id, type: "done", hex}
//      or {id, type: "error", message}
importScripts("sha1.js");

var SLICE_BYTES = 4 * 1024 * 1024;

function readSlice(blob) {
    if (typeof FileReaderSync !== "undefined") {
        return Promise.resolve(new FileReaderSync().readAsArrayBuffer(blob));
    }
    return blob.arrayBuffer();
}

self.onmessage = function (event) {
    var id = event.data.id;
    var file = event.data.file;
    var hash = new FGSSha1();
    var offset = 0;

    function next() {
        if (offset >= file.size) {
            self.postMessage({ id: id, type: "done", hex: hash.digest() });
            return;
        }
        var end = Math.min(offset + SLICE_BYTES, file.size);
        readSlice(file.slice(offset, end))
            .then(function (buffer) {
                hash.update(new Uint8Array(buffer));
                offset = end;
                self.postMessage({ id: id, type: "progress", bytes: offset });
                next();
            })
            .catch(function (error) {
                self.postMessage({ id: id, type: "error", message: String(error) });
            });
    }

    try {
        next();
    } catch (error) {
        self.postMessage({ id: id, type: "error", message: String(error) });
    }
};
