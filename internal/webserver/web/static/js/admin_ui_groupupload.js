// Uploading into a group (Part 2, G7): the SHA-1 worker, the duplicate
// pre-check, the "Upload to" selector of the upload page and the set-up of
// a group page's upload area (html_group_upload.tmpl). Merged into the admin
// bundle with every other admin_*.js file, so everything here is either on
// FGS or carries an upload-target prefix.

;(function () {
    var FGS = window.FGS = window.FGS || {};

    var WORKER_URL = "./js/sha1-worker.js";
    // A hash that makes no progress for this long is given up, and the file
    // is uploaded unchecked: the pre-check must never block an upload (D3)
    var STALL_MS = 20000;
    var PRECHECK_TIMEOUT_MS = 10000;

    // One worker hashes one file at a time; the rest wait in order, so
    // adding hundreds of photos does not start hundreds of workers
    var queue = [];
    var active = null;
    var worker = null;
    var nextJobId = 1;

    function dropWorker() {
        if (worker !== null) {
            worker.terminate();
            worker = null;
        }
    }

    function finishActive(error, hex) {
        if (active === null) {
            return;
        }
        var job = active;
        active = null;
        clearTimeout(job.timer);
        if (error) {
            // A failed or stalled worker is not reused
            dropWorker();
            job.reject(error);
        } else {
            job.resolve(hex);
        }
        runNext();
    }

    function arm(job) {
        clearTimeout(job.timer);
        job.timer = setTimeout(function () {
            if (active === job) {
                finishActive(new Error("SHA-1 made no progress for " + STALL_MS / 1000 + " s"));
            }
        }, STALL_MS);
    }

    function onWorkerMessage(event) {
        var msg = event.data || {};
        if (active === null || msg.id !== active.id) {
            return;
        }
        if (msg.type === "progress") {
            arm(active);
            if (active.onProgress) {
                try {
                    active.onProgress(active.file.size > 0 ? msg.bytes / active.file.size : 1);
                } catch (ignored) {
                }
            }
            return;
        }
        if (msg.type === "done") {
            finishActive(null, msg.hex);
            return;
        }
        finishActive(new Error(msg.message || "SHA-1 failed"));
    }

    function startWorker() {
        if (worker === null) {
            worker = new Worker(WORKER_URL);
            worker.onmessage = onWorkerMessage;
            worker.onerror = function (event) {
                if (event && event.preventDefault) {
                    event.preventDefault();
                }
                finishActive(new Error("SHA-1 worker failed" + (event && event.message ? ": " + event.message : "")));
            };
        }
        return worker;
    }

    function runNext() {
        if (active !== null || queue.length === 0) {
            return;
        }
        var job = queue.shift();
        active = job;
        try {
            startWorker().postMessage({ id: job.id, file: job.file });
            arm(job);
        } catch (error) {
            finishActive(error);
        }
    }

    // FGS.sha1File hashes a File (or Blob) in the worker, in 4 MiB slices,
    // and resolves its SHA-1 as 40 lower-case hex characters. onProgress, if
    // given, gets the fraction done (0..1).
    FGS.sha1File = function (file, onProgress) {
        return new Promise(function (resolve, reject) {
            if (typeof Worker === "undefined") {
                reject(new Error("Web Workers are not available"));
                return;
            }
            queue.push({ id: nextJobId++, file: file, onProgress: onProgress, resolve: resolve, reject: reject, timer: null });
            runNext();
        });
    };

    function fetchJsonWithTimeout(url, timeoutMs) {
        var controller = typeof AbortController !== "undefined" ? new AbortController() : null;
        var timer = null;
        var timeout = new Promise(function (resolve, reject) {
            timer = setTimeout(function () {
                if (controller !== null) {
                    controller.abort();
                }
                reject(new Error("timed out"));
            }, timeoutMs);
        });
        var request = fetch(url, {
            credentials: "same-origin",
            cache: "no-store",
            signal: controller !== null ? controller.signal : undefined
        }).then(function (response) {
            if (!response.ok) {
                throw new Error("status " + response.status);
            }
            return response.json();
        });
        return Promise.race([request, timeout]).finally(function () {
            clearTimeout(timer);
        });
    }

    // Groups whose server said its stored hashes cannot be compared (D3):
    // later files there are not hashed at all
    var uncheckable = {};

    // FGS.groupPrecheck asks whether a file with the same bytes is already in
    // the group, and who put it there. It resolves {exists:false} whenever it
    // cannot tell: the server cannot check, the file is empty, the hash fails
    // or stalls, or the request fails. It never rejects, so it never blocks
    // an upload; the server's own check after the upload is authoritative.
    FGS.groupPrecheck = function (groupId, file, onProgress) {
        var unknown = { exists: false };
        if (!groupId || !file || !file.size || uncheckable[groupId]) {
            return Promise.resolve(unknown);
        }
        return FGS.sha1File(file, onProgress)
            .then(function (hex) {
                return fetchJsonWithTimeout("./groups/has?id=" + encodeURIComponent(groupId) +
                    "&sha1=" + encodeURIComponent(hex), PRECHECK_TIMEOUT_MS);
            })
            .then(function (data) {
                if (!data || data.checkable !== true) {
                    if (data && data.checkable === false) {
                        uncheckable[groupId] = true;
                    }
                    return unknown;
                }
                if (data.exists !== true) {
                    return unknown;
                }
                return { exists: true, uploader: data.uploader || "", fileId: data.fileId || "" };
            })
            .catch(function (error) {
                console.warn("Duplicate pre-check skipped:", error);
                return unknown;
            });
    };

    // FGS.initGroupUploadArea starts the upload area of a group page
    // ({{ template "group_upload_area" .Group.Id }}). Call it from an inline
    // script at the end of the body, after the admin bundle and uuid.min.js:
    //   FGS.initGroupUploadArea({maxFileSize: {{.MaxFileSize}},
    //     chunkSize: {{.ChunkSize}}, maxParallelUploads: {{.MaxParallelUploads}},
    //     e2e: {{.EndToEndEncryption}}});
    // It also opens the upload status stream (registerChangeHandler).
    FGS.initGroupUploadArea = function (opts) {
        opts = opts || {};
        if (opts.e2e) {
            // End-to-end encryption and groups exclude each other (D10)
            Dropzone.options.uploaddropzone = false;
            var zone = document.getElementById("uploaddropzone");
            if (zone !== null) {
                zone.classList.add("fgs-upload-unavailable");
                zone.title = "Not available with end-to-end encryption";
                var title = document.getElementById("uploadBoxTitle");
                var subtitle = document.getElementById("uploadBoxSubtitle");
                if (title !== null) title.textContent = "Not available with end-to-end encryption";
                if (subtitle !== null) subtitle.textContent = "";
            }
            return;
        }
        initDropzone();
        setUploadDropzoneOptions(opts.maxFileSize, opts.chunkSize, opts.maxParallelUploads);
        registerChangeHandler();
    };
})();


// The group the next files go into: the group page's fixed target, else the
// upload page's "Upload to" choice. "" is My files.
function uploadTarget() {
    let zone = document.getElementById("uploaddropzone");
    if (zone !== null && zone.hasAttribute("data-fgs-upload-target")) {
        return zone.getAttribute("data-fgs-upload-target");
    }
    let select = document.getElementById("fgs-upload-target");
    return select === null ? "" : select.value;
}

// uploadTarget() with what else a file needs to remember of it: the group's
// name (the upload page's live add sets GroupLock to it) and whether this is
// a group page (whose list is filled from /groups/item instead)
function uploadTargetDetails() {
    let id = uploadTarget();
    let zone = document.getElementById("uploaddropzone");
    let fixed = zone !== null && zone.hasAttribute("data-fgs-upload-target");
    let name = "";
    let select = document.getElementById("fgs-upload-target");
    if (id !== "" && !fixed && select !== null && select.selectedIndex >= 0) {
        name = select.options[select.selectedIndex].textContent;
    }
    return { id: id, name: name, fixed: fixed };
}

// The upload page's "Upload to" selector: My files and the caller's groups,
// filled with textContent. It is never remembered (D10): it starts on My
// files on every visit, so private files never go into a group by accident.
function initUploadTarget() {
    let select = document.getElementById("fgs-upload-target");
    if (select === null) {
        return;
    }
    select.value = "";
    select.addEventListener("change", applyUploadTarget);
    applyUploadTarget();
    let e2e = select.getAttribute("data-e2e") === "true";
    fetch("./groups/list", { credentials: "same-origin", cache: "no-store" })
        .then(response => response.ok ? response.json() : [])
        .then(groups => {
            if (!Array.isArray(groups)) {
                return;
            }
            groups.forEach(g => {
                if (!g || !g.id || !g.role) {
                    return; // member groups only
                }
                let option = document.createElement("option");
                option.value = g.id;
                option.textContent = g.name;
                if (e2e) {
                    option.disabled = true;
                    option.title = "Not available with end-to-end encryption";
                }
                select.appendChild(option);
            });
            let wrap = document.getElementById("fgs-upload-target-wrap");
            if (wrap !== null && select.options.length > 1) {
                wrap.hidden = false;
            }
        })
        .catch(error => console.warn("Unable to list groups for Upload to:", error));
}

// Choosing a group greys out and disables the expiry, download limit,
// password and Combine settings, because the group sets them (D10)
function applyUploadTarget() {
    let groupChosen = uploadTarget() !== "";
    let area = document.getElementById("fgs-upload-options-area");
    if (area !== null) {
        area.classList.toggle("fgs-target-group", groupChosen);
    }
    let note = document.getElementById("fgs-upload-target-note");
    if (note !== null) {
        note.hidden = !groupChosen;
    }
    document.querySelectorAll(".upload-options-grid input, #enableAutoCollection").forEach(input => {
        if (groupChosen) {
            if (!input.hasAttribute("data-fgs-was-disabled")) {
                input.setAttribute("data-fgs-was-disabled", input.disabled ? "true" : "false");
                input.disabled = true;
            }
        } else if (input.hasAttribute("data-fgs-was-disabled")) {
            input.disabled = input.getAttribute("data-fgs-was-disabled") === "true";
            input.removeAttribute("data-fgs-was-disabled");
        }
    });
}

// Dropzone's accept hook: a file going into a group is first checked against
// the group's files. A copy already there is not uploaded and its row says
// who put it there; anything else, including a failed check, is uploaded.
function uploadTargetAccept(file, done) {
    let target = file.fgsTarget;
    if (!target || target.id === "" || !window.FGS || !FGS.groupPrecheck) {
        done();
        return;
    }
    let settled = false;
    let finish = function (message) {
        if (settled) {
            return;
        }
        settled = true;
        if (message === undefined) {
            // Upload speed is measured from now, not from when checking began
            let container = document.getElementById(`us-container-${file.upload.uuid}`);
            if (container !== null) {
                container.setAttribute("data-starttime", Date.now());
            }
            done();
            return;
        }
        file.fgsDuplicate = true;
        done(message);
    };
    let info = document.getElementById(`us-progress-info-${file.upload.uuid}`);
    if (info !== null) {
        info.textContent = "Checking…";
    }
    FGS.groupPrecheck(target.id, file)
        .then(result => {
            if (result && result.exists) {
                finish(result.uploader ? "Already here, uploaded by " + result.uploader : "Already here");
            } else {
                finish();
            }
        })
        .catch(() => finish());
}

// A row that ended without an upload but without an error either: a copy
// already in the group
function uploadTargetNotice(chunkId, message) {
    let container = document.getElementById(`us-container-${chunkId}`);
    if (container !== null) {
        container.setAttribute("data-complete", "true");
    }
    let bar = document.getElementById(`us-progressbar-${chunkId}`);
    if (bar !== null) {
        bar.style.width = "100%";
        bar.style.backgroundColor = "#6c757d";
    }
    let info = document.getElementById(`us-progress-info-${chunkId}`);
    if (info !== null) {
        info.textContent = message;
        info.classList.remove("uploaderror");
    }
}

// On a group page, a finished upload is shown from /groups/item, which also
// carries the group's terms (GroupLock) and the uploader. The file is put
// into the group a moment after the server reports it finished, so a 404 is
// retried; three 404s mean the server found a copy already in the group and
// deleted this one.
function uploadTargetGroupItem(target, fileId, uid, attempt) {
    fetch("./groups/item?id=" + encodeURIComponent(target.id) + "&file=" + encodeURIComponent(fileId),
        { credentials: "same-origin", cache: "no-store" })
        .then(response => {
            if (response.status === 404) {
                if (attempt < 2) {
                    setTimeout(() => uploadTargetGroupItem(target, fileId, uid, attempt + 1), 1000);
                    return;
                }
                uploadTargetNotice(uid, "Already in the group");
                markBatchFileFailed(dropzoneGetFile(uid));
                return;
            }
            if (!response.ok) {
                throw new Error("Request failed with status: " + response.status);
            }
            return response.json().then(item => {
                try {
                    addUploadedFile(item, item.Preview);
                    notifyWorker({ type: "fileAdded", item: item });
                } catch (error) {
                    console.error("Error showing the uploaded file:", error);
                }
                let file = dropzoneGetFile(uid);
                if (file !== null) {
                    recordBatchFileId(file, item.Id);
                }
                removeFileStatus(uid);
            });
        })
        .catch(error => {
            let file = dropzoneGetFile(uid);
            if (file !== null) {
                dropzoneUploadError(file, error);
            }
            console.error("Error:", error);
        });
}
