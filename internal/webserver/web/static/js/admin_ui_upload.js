// This file contains JS code for the API view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...


var dropzoneObject;
var isE2EEnabled = false;

var isUploading = false;

// The upload page's list of files uploaded now: the shared file list
// (html_file_browser.tmpl) in its "upload" context, or null on every other
// page that loads the admin bundle
function uploadBrowser() {
    let root = document.querySelector('[data-fgs-browser][data-context="upload"]');
    if (root === null || !window.FGS || !FGS.initFileBrowser) {
        return null;
    }
    return FGS.initFileBrowser(root);
}

// The list this page adds its finished uploads to: the upload page's list,
// or a group page's board (data-upload-sink="true"). Only uploads made on
// this page go there; files another tab added still use uploadBrowser().
function uploadSinkBrowser() {
    let root = document.querySelector('[data-fgs-browser][data-upload-sink="true"]');
    if (root === null || !window.FGS || !FGS.initFileBrowser) {
        return null;
    }
    return FGS.initFileBrowser(root);
}


function initDropzone() {

    Dropzone.options.uploaddropzone = {
        paramName: "file",
        dictDefaultMessage: "",
        createImageThumbnails: false,
        chunksUploaded: function(file, done) {
            sendChunkComplete(file, done);
        },
        // Runs after "addedfile": a file going into a group is checked for
        // a copy already there first (admin_ui_groupupload.js)
        accept: function(file, done) {
            uploadTargetAccept(file, done);
        },
        init: function() {
            dropzoneObject = this;
            this.on("addedfile", file => {
                file.upload.uuid = getUuid();
                // Where it goes is fixed when it is added, not when it finishes
                file.fgsTarget = uploadTargetDetails();
                saveUploadDefaults();
                addFileProgress(file);
                addFileToUploadBatch(file);
            });
            this.on("queuecomplete", function() {
                isUploading = false;
                closeUploadBatch();
            });
            // Every failure path (chunk upload, chunk-complete, server-side
            // processing, file info lookup) ends in Dropzone's "error" event.
            this.on("error", file => markBatchFileFailed(file));
            this.on("canceled", file => markBatchFileFailed(file));
            this.on("removedfile", file => markBatchFileFailed(file));
            this.on("sending", function(file, xhr, formData) {
                isUploading = true;
            });

            // Error handling for chunk upload, especially returning 413 error code (invalid nginx configuration)
            this.on("error", function(file, errorMessage, xhr) {
                console.log(errorMessage);
                if (file.fgsDuplicate === true) {
                    uploadTargetNotice(file.upload.uuid, errorMessage);
                    return;
                }
                if (xhr) {
                    if (xhr.status === 413) {
                        showError(file, "File too large to upload. If you are using a reverse proxy, make sure that the allowed body size is at least 70MB.");
                        return;
                    }
                    try {
                        console.log(xhr);
                        errInfo = JSON.parse(xhr.responseText);
                        showError(file, "Error: " + errInfo.ErrorMessage);
                    } catch (ignored) {
                        showError(file, "Error: " + xhr.responseText);
                    }
                } else {
                    showError(file, "Error: " + errorMessage);
                }
            });
            this.on("uploadprogress", function(file, progress, bytesSent) {
                updateProgressbar(file, progress, bytesSent);
            });

            // This will be executed after the page has loaded. If e2e ist enabled, the end2end_admin.js has set isE2EEnabled to true
            if (isE2EEnabled) {
                dropzoneObject.disable();
                setE2eUpload();
            }
        },
    };


    document.onpaste = function(event) {
        if (dropzoneObject.disabled) {
            return;
        }
        const activeElement = document.activeElement;
        // disable upload on paste if edit field with data-allow-regular-paste is selected or the calendar view (with placeholder attribute)
        if (activeElement && (activeElement.hasAttribute('data-allow-regular-paste') || activeElement.hasAttribute('placeholder'))) {
            return;
        }

        var items = (event.clipboardData || event.originalEvent.clipboardData).items;
        for (let index in items) {
            var item = items[index];
            if (item.kind === 'file') {
                dropzoneObject.addFile(item.getAsFile());
            }
            if (item.kind === 'string') {
                item.getAsString(function(s) {
                    // If a picture was copied from a website, the origin information might be submitted, which is filtered with this regex out
                    const pattern = /<img *.+>/gi;
                    if (pattern.test(s) === false) {
                        let blob = new Blob([s], {
                            type: 'text/plain'
                        });
                        let file = new File([blob], "Pasted Text.txt", {
                            type: "text/plain",
                            lastModified: new Date(0)
                        });
                        dropzoneObject.addFile(file);
                    }
                });
            }
        }
    };

    window.addEventListener('beforeunload', (event) => {
        if (isUploading) {
            event.returnValue = 'Upload is still in progress. Do you want to close this page?';
        }
    });
}


// The upload page's and a group page's Dropzone settings. Call after
// initDropzone(), from an inline script, before Dropzone discovers the form.
function setUploadDropzoneOptions(maxFileSize, chunkSizeMb, maxParallelUploads) {
    let options = Dropzone.options.uploaddropzone;
    options["maxFilesize"] = maxFileSize;
    options["timeout"] = 1200000;
    options["chunking"] = true;
    options["chunkSize"] = chunkSizeMb * 1024 * 1024;
    options["parallelUploads"] = 1;
    options["parallelChunkUploads"] = maxParallelUploads;
    options["forceChunking"] = true;
    options["retryChunksLimit"] = 3;
    options["retryChunks"] = true;
    options["previewsContainer"] = false;
}


function updateProgressbar(file, progress, bytesSent) {
    let chunkId = file.upload.uuid;
    let container = document.getElementById(`us-container-${chunkId}`);
    if (container == null || container.getAttribute('data-complete') === "true") {
        return;
    }
    let rounded = Math.round(progress);
    if (rounded < 0) {
        rounded = 0;
    }
    if (rounded > 100) {
        rounded = 100;
    }
    let millisSinceUpload = Date.now() - container.getAttribute('data-starttime');
    let megabytePerSecond = bytesSent / (millisSinceUpload / 1000) / 1024 / 1024;
    document.getElementById(`us-progressbar-${chunkId}`).style.width = rounded + "%";

    let uploadSpeed = Math.round(megabytePerSecond * 10) / 10;
    if (!Number.isNaN(uploadSpeed))
        document.getElementById(`us-progress-info-${chunkId}`).innerText = rounded + "% - " + uploadSpeed + "MB/s";
}

function addFileProgress(file) {
    addFileStatus(file.upload.uuid, file.upload.filename);
}


function setUploadDefaults() {
    let defaultDownloads = getLocalStorageWithDefault("defaultDownloads", 1);
    let defaultExpiry = getLocalStorageWithDefault("defaultExpiry", 14);
    let defaultPassword = getLocalStorageWithDefault("defaultPassword", "");
    // Default is now UNLIMITED downloads. The storage key is versioned so an
    // older remembered "limit on" preference does not override the new default.
    // Note getLocalStorageWithDefault returns the fallback as-is, so it must be
    // the string "true" to survive the === "true" comparison below.
    let defaultUnlimitedDownloads = getLocalStorageWithDefault("defaultUnlimitedDownloads2", "true") === "true";
    let defaultUnlimitedTime = getLocalStorageWithDefault("defaultUnlimitedTime", false) === "true";

    document.getElementById("allowedDownloads").value = defaultDownloads;
    document.getElementById("expiryDays").value = defaultExpiry;
    document.getElementById("password").value = defaultPassword;
    document.getElementById("enableDownloadLimit").checked = !defaultUnlimitedDownloads;
    document.getElementById("enableTimeLimit").checked = !defaultUnlimitedTime;

    if (defaultPassword === "") {
        document.getElementById("enablePassword").checked = false;
        document.getElementById("password").disabled = true;
    } else {
        document.getElementById("enablePassword").checked = true;
        document.getElementById("password").disabled = false;
    }

    if (defaultUnlimitedDownloads) {
        document.getElementById("allowedDownloads").disabled = true;
    }
    if (defaultUnlimitedTime) {
        document.getElementById("expiryDays").disabled = true;
    }

}

function saveUploadDefaults() {
    // A group page has none of these settings
    if (["allowedDownloads", "expiryDays", "password", "enableDownloadLimit", "enableTimeLimit"]
        .some(id => document.getElementById(id) === null)) {
        return;
    }
    localStorage.setItem("defaultDownloads", document.getElementById("allowedDownloads").value);
    localStorage.setItem("defaultExpiry", document.getElementById("expiryDays").value);
    localStorage.setItem("defaultPassword", document.getElementById("password").value);
    localStorage.setItem("defaultUnlimitedDownloads2", !document.getElementById("enableDownloadLimit").checked);
    localStorage.setItem("defaultUnlimitedTime", !document.getElementById("enableTimeLimit").checked);
}

function getLocalStorageWithDefault(key, default_value) {
    var value = localStorage.getItem(key);
    if (value === null) {
        return default_value;
    }
    return value;
}

function urlencodeFormData(fd) {
    let s = '';

    function encode(s) {
        return encodeURIComponent(s).replace(/%20/g, '+');
    }
    for (var pair of fd.entries()) {
        if (typeof pair[1] == 'string') {
            s += (s ? '&' : '') + encode(pair[0]) + '=' + encode(pair[1]);
        }
    }
    return s;
}


function sendChunkComplete(file, done) {
    let uuid = file.upload.uuid;
    let filename = file.name;
    let filesize = file.size;
    let realsize = file.size;
    let contenttype = file.type;
    let isE2E = file.isEndToEndEncrypted === true;
    let nonblocking = true;
    let groupId = file.fgsTarget ? file.fgsTarget.id : "";

    // null leaves a setting out. Into a group, none is sent: the group sets
    // them. A page without the settings sends the server's defaults.
    let allowedDownloads = null;
    let expiryDays = null;
    let password = null;
    if (groupId === "") {
        let setting = id => document.getElementById(id);
        if (setting("allowedDownloads") !== null) {
            allowedDownloads = setting("allowedDownloads").value;
        }
        if (setting("expiryDays") !== null) {
            expiryDays = setting("expiryDays").value;
        }
        if (setting("password") !== null) {
            password = setting("password").value;
        }
        if (setting("enableDownloadLimit") !== null && !setting("enableDownloadLimit").checked) {
            allowedDownloads = 0;
        }
        if (setting("enableTimeLimit") !== null && !setting("enableTimeLimit").checked) {
            expiryDays = 0;
        }
    }

    if (isE2E) {
        filesize = file.sizeEncrypted;
        filename = "Encrypted File";
        contenttype = "";
    }

    apiChunkComplete(uuid, filename, filesize, realsize, contenttype, allowedDownloads, expiryDays, password, isE2E, nonblocking, groupId)
        .then(data => {
            done();
            let progressText = document.getElementById(`us-progress-info-${file.upload.uuid}`);
            if (progressText != null)
                progressText.innerText = "In Queue...";
        })
        .catch(error => {
            console.error('Error:', error);
            dropzoneUploadError(file, error);
        });
}

function dropzoneUploadError(file, errormessage) {
    file.accepted = false;
    dropzoneObject._errorProcessing([file], errormessage);
    showError(file, errormessage);
}

function dropzoneGetFile(uid) {
    for (let i = 0; i < dropzoneObject.files.length; i++) {
        const currentFile = dropzoneObject.files[i];
        if (currentFile.upload.uuid === uid) {
            return currentFile;
        }
    }
    return null;
}

function requestFileInfo(fileId, uid) {
    let uploaded = dropzoneGetFile(uid);
    let target = uploaded !== null && uploaded.fgsTarget ? uploaded.fgsTarget : null;
    if (target !== null && target.id !== "") {
        // Into a group, from a group page or the Upload page's "Upload to":
        // shown as the group has it (its terms, GroupLock). The server may
        // still find it a duplicate and delete it; /groups/item then never
        // finds it, and the row says "Already in the group" (M1)
        uploadTargetGroupItem(target, fileId, uid, 0);
        return;
    }
    apiFilesListById(fileId)
        .then(data => {
            // The list is only a view: an error drawing it must not mark an
            // upload that is stored as failed (and with it the auto-collection batch)
            try {
                addUploadedFile(data);
                notifyWorker({ type: "fileAdded", item: data });
            } catch (error) {
                console.error('Error showing the uploaded file:', error);
            }
            let file = dropzoneGetFile(uid);
            if (file == null) {
                return;
            }
            recordBatchFileId(file, data.Id);
            if (file.isEndToEndEncrypted === true) {
                apiE2eMutexLockUnlock(false)
                    .then(() => apiE2eGet())
                    .then(freshData => {
                        let parseResult = GokapiE2EInfoParse(freshData);
                        if (parseResult instanceof Error) throw parseResult;

                        let result = GokapiE2EAddFile(uid, fileId, file.name);
                        if (result instanceof Error) throw result;

                        let info = GokapiE2EInfoEncrypt();
                        if (info instanceof Error) throw info;

                        return apiE2eStore(info);
                    })
                    .then(() => {
                        GokapiE2EDecryptMenu();
                        removeFileStatus(uid);
                    })
                    .catch(err => {
                        file.accepted = false;
                        dropzoneObject._errorProcessing([file], err);
                        console.error('Error:', err);
                    })
                    .finally(() => {
                        apiE2eMutexLockUnlock(true).catch(error => {
                            console.error("Failed to release E2E mutex after write: " + error);
                        });
                    });
            } else {
                removeFileStatus(uid);
            }
        })
        .catch(error => {
            let file = dropzoneGetFile(uid);
            if (file != null) {
                dropzoneUploadError(file, error);
            }
            console.error('Error:', error);
        });
}


function parseProgressStatus(eventData) {
    let container = document.getElementById(`us-container-${eventData.chunk_id}`);
    if (container == null) {
        return;
    }
    container.setAttribute('data-complete', 'true');
    let text;
    switch (eventData.upload_status) {
        case 0:
            text = "Processing file...";
            break;
        case 1:
            text = "Saving file...";
            break;
        case 2:
            text = "Finalising...";
            requestFileInfo(eventData.file_id, eventData.chunk_id);
            break;
        case 3:
            text = "Error";
            let file = dropzoneGetFile(eventData.chunk_id);
            if (eventData.error_message == "")
                eventData.error_message = "Server Error";
            if (file != null) {
                dropzoneUploadError(file, eventData.error_message);
            }
            return;
        default:
            text = "Unknown status";
            break;
    }
    document.getElementById(`us-progress-info-${eventData.chunk_id}`).innerText = text;
}

function showError(file, message) {
    let chunkId = file.upload.uuid;
    document.getElementById(`us-progressbar-${chunkId}`).style.width = "100%";
    document.getElementById(`us-progressbar-${chunkId}`).style.backgroundColor = "red";
    document.getElementById(`us-progress-info-${chunkId}`).innerText = message;
    document.getElementById(`us-progress-info-${chunkId}`).classList.add('uploaderror');
}


function editFile() {
    const button = document.getElementById('mb_save');
    button.disabled = true;
    let id = button.getAttribute('data-fileid');

    // D4: in a live group the expiry, download limit and password follow the
    // group. None of them is sent (null leaves the header out); the server
    // reads a missing originalPassword as "remove the password", so it says keep.
    let allowedDownloads = null;
    let expiryTimestamp = null;
    let password = null;
    let originalPassword = true;
    if (!button.hasAttribute('data-group-lock')) {
        allowedDownloads = document.getElementById('mi_edit_down').value;
        expiryTimestamp = document.getElementById('mi_edit_expiry').value;
        password = document.getElementById('mi_edit_pw').value;
        originalPassword = (password === '(unchanged)');

        if (!document.getElementById('mc_download').checked) {
            allowedDownloads = 0;
        }
        if (!document.getElementById('mc_expiry').checked) {
            expiryTimestamp = 0;
        }
        if (!document.getElementById('mc_password').checked) {
            originalPassword = false;
            password = "";
        }
    }

    let replaceFile = false;
    let replaceId = "";
    if (document.getElementById('mc_replace').checked) {
        replaceId = document.getElementById('mi_edit_replace').value;
        replaceFile = (replaceId != "");
    }

    // The name is only sent when it was changed
    let nameInput = document.getElementById('mi_edit_name');
    let newName = null;
    if (!nameInput.disabled && nameInput.value !== nameInput.getAttribute('data-original')) {
        newName = nameInput.value.trim();
        if (newName === '') {
            alert("The name cannot be empty");
            button.disabled = false;
            return;
        }
    }

    apiFilesModify(id, allowedDownloads, expiryTimestamp, password, originalPassword, newName)
        .then(data => {
            if (!replaceFile) {
                showEditedFile(data, false);
                return;
            }
            apiFilesReplace(id, replaceId)
                .then(data => {
                    showEditedFile(data, true);
                })
                .catch(error => {
                    alert("Unable to edit file: " + error);
                    console.error('Error:', error);
                    button.disabled = false;
                });
        })
        .catch(error => {
            alert("Unable to edit file: " + error);
            console.error('Error:', error);
            button.disabled = false;
        });
}

// After a successful edit: the file as the API now holds it, shown in place
// in every list on the page that has it (the Files tab, the upload page's
// uploads, a collection's files), without a reload. Replaced content has a
// new preview, which is asked for first and polled if it is still being made.
function showEditedFile(fileInfo, replaced) {
    let modal = bootstrap.Modal.getInstance(document.getElementById("modaledit"));
    if (modal != null) {
        modal.hide();
    }
    let showPreview = Promise.resolve(null);
    if (replaced) {
        showPreview = fetch("./files/previewinfo?id=" + encodeURIComponent(fileInfo.Id), { credentials: "same-origin", cache: "no-store" })
            .then(response => response.ok ? response.json() : null)
            .catch(() => null);
    }
    showPreview.then(preview => {
        FGS.eachFileBrowser(function(browser) {
            if (browser.tile(fileInfo.Id) === null) {
                return;
            }
            browser.updateItem(fileInfo);
            if (preview !== null) {
                browser.applyPreview(fileInfo.Id, preview);
            } else if (replaced) {
                // The new preview could not be fetched: keep what is shown
                // and ask again until it arrives (or the poll gives up)
                browser.pollPreview(fileInfo.Id);
            }
        });
        if (isE2EEnabled) {
            GokapiE2EDecryptMenu();
        }
    });
}

function showEditModal(filename, id, downloads, expiry, password, unlimitedown, unlimitedtime, isE2e, canReplace, groupLock) {
    // Cloning removes any previous values or form validation
    let originalModal = $('#modaledit').clone();
    $("#modaledit").on('hide.bs.modal', function() {
        $('#modaledit').remove();
        let myClone = originalModal.clone();
        $('body').append(myClone);
    });

    document.getElementById("m_filenamelabel").innerText = filename;
    let nameInput = document.getElementById("mi_edit_name");
    nameInput.value = filename;
    nameInput.setAttribute("data-original", filename);
    if (isE2e) {
        // The real name of an encrypted file lives inside the encrypted data
        nameInput.disabled = true;
        nameInput.title = "Renaming is not available for end-to-end encrypted files";
    }
    document.getElementById("mc_expiry").setAttribute("data-timestamp", expiry);
    document.getElementById("mb_save").setAttribute('data-fileid', id);
    createCalendar("mi_edit_expiry", expiry);

    if (unlimitedown) {
        document.getElementById("mi_edit_down").value = "1";
        document.getElementById("mi_edit_down").disabled = true;
        document.getElementById("mc_download").checked = false;
    } else {
        document.getElementById("mi_edit_down").value = downloads;
        document.getElementById("mi_edit_down").disabled = false;
        document.getElementById("mc_download").checked = true;
    }

    if (unlimitedtime) {
        document.getElementById("mi_edit_expiry").value = add14DaysIfBeforeCurrentTime(expiry);
        document.getElementById("mi_edit_expiry").disabled = true;
        document.getElementById("mc_expiry").checked = false;
        calendarInstance._input.disabled = true;
    } else {
        document.getElementById("mi_edit_expiry").value = expiry;
        document.getElementById("mi_edit_expiry").disabled = false;
        document.getElementById("mc_expiry").checked = true;
        calendarInstance._input.disabled = false;
    }

    if (password) {
        document.getElementById("mi_edit_pw").value = "(unchanged)";
        document.getElementById("mi_edit_pw").disabled = false;
        document.getElementById("mc_password").checked = true;
    } else {
        document.getElementById("mi_edit_pw").value = "";
        document.getElementById("mi_edit_pw").disabled = true;
        document.getElementById("mc_password").checked = false;
    }

    let selectReplace = document.getElementById("mi_edit_replace");
    if (canReplace) {
        document.getElementById("replaceGroup").style.display = 'flex';
        if (!isE2e) {
            // Filled once the server has answered; a dialog closed or opened
            // for another file meanwhile is left alone
            let fill = ++replaceFillToken;
            getAllAvailableFiles().then(files => {
                if (fill !== replaceFillToken || !selectReplace.isConnected) return;
                selectReplace.textContent = "";
                for (let i = 0; i < files[0].length; i++) {
                    if (files[0][i] == id)
                        continue;
                    // Name as text, never as markup; the id tells equal names apart
                    let option = document.createElement("option");
                    option.value = files[0][i];
                    option.text = files[1][i] + " (" + files[0][i] + ")";
                    selectReplace.add(option);
                }
            }).catch(error => console.error("Could not load the files to replace with:", error));
        } else {
            document.getElementById("mc_replace").disabled = true;
            document.getElementById("mc_replace").title = "Replacing content is not available for end-to-end encrypted files";
            selectReplace.add(new Option("Unavailable", 0));
            selectReplace.title = "Replacing content is not available for end-to-end encrypted files";
            selectReplace.value = "0";
        }
    } else {
        document.getElementById("replaceGroup").style.display = 'none';
    }

    // D4: a file in a live group keeps the group's expiry, download limit
    // and password; only its name and content can be changed here
    if (groupLock) {
        ["mc_download", "mc_expiry", "mc_password"].forEach(function(field) {
            document.getElementById(field).closest(".input-group").classList.add("d-none");
        });
        let note = document.createElement("p");
        note.className = "text-muted small";
        note.textContent = "Expiry and download limit follow the group \u201c" + groupLock + "\u201d.";
        nameInput.closest(".input-group").after(note);
        document.getElementById("mb_save").setAttribute("data-group-lock", groupLock);
    }


    new bootstrap.Modal('#modaledit', {}).show();
}

function selectTextForPw(input) {
    if (input.value === "(unchanged)") {
        input.setSelectionRange(0, input.value.length);
    }
}

function add14DaysIfBeforeCurrentTime(unixTimestamp) {
    let currentTime = Date.now();
    let timestampInMilliseconds = unixTimestamp * 1000;
    if (timestampInMilliseconds < currentTime) {
        let newTimestamp = currentTime + (14 * 24 * 60 * 60 * 1000);
        return Math.floor(newTimestamp / 1000);
    } else {
        return unixTimestamp;
    }
}

// The files "Replace content" can take its content from, from the server
// (Part 3: a list shows only its first batch): the user's own, or anyone's
// they may list, never end-to-end encrypted ones. Resolves to [ids, names].
let replaceFillToken = 0;
function getAllAvailableFiles() {
    return fetch("./files/replacecandidates", { credentials: "same-origin", cache: "no-store" })
        .then(response => {
            if (!response.ok) throw new Error("The server answered " + response.status);
            return response.json();
        })
        .then(list => {
            let ids = [], filenames = [];
            (Array.isArray(list) ? list : []).forEach(c => {
                ids.push(String(c.Id));
                filenames.push(c.Name || "");
            });
            return [ids, filenames];
        });
}


function checkBoxChanged(checkBox, correspondingInput) {
    let disable = !checkBox.checked;

    if (disable) {
        document.getElementById(correspondingInput).setAttribute("disabled", "");
    } else {
        document.getElementById(correspondingInput).removeAttribute("disabled");
    }
    if (correspondingInput === "password" && disable) {
        document.getElementById("password").value = "";
    }
}

function parseSseData(data) {
    let eventData;
    try {
        eventData = JSON.parse(data);
    } catch (e) {
        console.error("Failed to parse event data:", e);
        return;
    }
    switch (eventData.event) {
        case "download":
            setNewDownloadCount(eventData.file_id, eventData.download_count, eventData.downloads_remaining);
            return;
        case "uploadStatus":
            parseProgressStatus(eventData);
            return;
        default:
            console.error("Unknown event", eventData);
    }
}

function setNewDownloadCount(id, downloadCount, downloadsRemaining) {
    let downloadCell = document.getElementById("cell-downloads-" + id);
    if (downloadCell != null) {
        downloadCell.innerText = downloadCount;
        downloadCell.classList.add("updatedDownloadCount");
        setTimeout(() => downloadCell.classList.remove("updatedDownloadCount"), 500);
    }
    if (downloadsRemaining != -1) {
        // The edit dialog opens with the remaining downloads its "⋯" menu holds
        document.querySelectorAll('[data-fgs-file-menu][data-id="' + sanitizeId(String(id)) + '"]').forEach(menu => {
            if (menu.hasAttribute("data-downloads")) {
                menu.setAttribute("data-downloads", downloadsRemaining);
            }
        });
    }
}


/**
 * sseWorkerPort holds the active SharedWorker MessagePort so that sseLogout()
 * in admin_ui_allPages.js can send a "close" message before navigating away.
 * Null when running in direct-EventSource fallback mode.
 */
var sseWorkerPort = null;

/**
 * notifyWorker posts a message to the SharedWorker if one is active.
 * The worker will relay it to all other connected tabs.
 * Safe to call in fallback (direct SSE) mode — it's a no-op when the port is null.
 * @param {Object} msg
 */
function notifyWorker(msg) {
    if (sseWorkerPort !== null) {
        sseWorkerPort.postMessage(msg);
    }
}

/**
 * registerChangeHandler
 *
 * Uses a SharedWorker to hold a single SSE connection that is shared across
 * all open tabs. This prevents hitting the browser's per-origin HTTP/1.1
 * connection limit (typically 6), which would otherwise block new tabs from
 * receiving updates when many admin pages are open simultaneously.
 *
 * Falls back to a direct EventSource when SharedWorker is unavailable
 * (e.g. Firefox private-browsing mode, or very old browsers).
 */
function registerChangeHandler() {
    if (typeof SharedWorker !== "undefined") {
        try {
            const worker = new SharedWorker("./js/sse-worker.js");

            worker.port.onmessage = (event) => {
                if (event.data.type === "message") {
                    parseSseData(event.data.data);
                } else if (event.data.type === "error") {
                    console.error("SSE worker connection error:", event.data.detail);
                } else if (event.data.type === "shutdown") {
                    // Another tab logged out — the session is gone for everyone.
                    setTimeout(function() {  
                   	 window.location.href = "./login";
		     }, 1000);
                } else if (event.data.type === "fileAdded") {
                    // Another tab finished an upload or restored a file — add it
                    // if it isn't already listed (guard against duplicate IDs).
                    let browser = uploadBrowser();
                    if (browser !== null && !isRequestFile(event.data.item) && browser.tile(sanitizeId(event.data.item.Id)) === null) {
                        addUploadedFile(event.data.item);
                    } else if (browser === null) {
                        addToFilesTab(sanitizeId(event.data.item.Id));
                    }
                } else if (event.data.type === "fileDeleted") {
                    // Another tab deleted a file — remove it silently (no toast,
                    // since this tab didn't initiate the deletion).
                    // ...from every list on this page that shows it
                    if (window.FGS && FGS.eachFileBrowser) {
                        let id = sanitizeId(event.data.id);
                        FGS.eachFileBrowser(function(browser) {
                            if (browser.tile(id) !== null) {
                                browser.removeItem(id);
                            }
                        });
                    }
                } else if (event.data.type === "log") {
                    const { level, message, detail } = event.data;
                    if (detail) {
                        console[level](message, detail);
                    } else {
                        console[level](message);
                    }
                }
            };

            worker.onerror = (err) => {
                // Worker itself failed to load – fall back to a direct connection.
                console.warn("SharedWorker failed, falling back to direct SSE:", err);
                sseWorkerPort = null;
                _registerDirectSSE();
            };

            worker.port.start();
            sseWorkerPort = worker.port;
            return;
        } catch (e) {
            console.warn("SharedWorker unavailable, falling back to direct SSE:", e);
        }
    }
    _registerDirectSSE();
}

/**
 * _registerDirectSSE
 *
 * Fallback: open an EventSource directly in this tab. Used when SharedWorker
 * is not supported or fails to load. Each tab will hold its own connection,
 * which may hit browser connection limits if many tabs are open.
 */
function _registerDirectSSE() {
    const source = new EventSource("./uploadStatus");
    source.onmessage = (event) => {
        parseSseData(event.data);
    };
    source.onerror = (error) => {
        // Check for net::ERR_HTTP2_PROTOCOL_ERROR 200 (OK) and ignore it
        if (error.target.readyState !== EventSource.CLOSED) {
            source.close();
        }
        console.log("Reconnecting to SSE (direct)...");
        setTimeout(_registerDirectSSE, 5000);
    };
}

var statusItemCount = 0;


function addFileStatus(chunkId, filename) {
    const container = document.createElement('div');
    container.setAttribute('id', `us-container-${chunkId}`);
    container.classList.add('us-container');

    // create filename div
    const filenameDiv = document.createElement('div');
    filenameDiv.classList.add('filename');
    filenameDiv.textContent = filename;
    container.appendChild(filenameDiv);

    // create progress bar container div
    const progressContainerDiv = document.createElement('div');
    progressContainerDiv.classList.add('upload-progress-container');
    progressContainerDiv.setAttribute('id', `us-progress-container-${chunkId}`);

    // create progress bar div
    const progressBarDiv = document.createElement('div');
    progressBarDiv.classList.add('upload-progress-bar');

    // create progress bar progress div
    const progressBarProgressDiv = document.createElement('div');
    progressBarProgressDiv.setAttribute('id', `us-progressbar-${chunkId}`);
    progressBarProgressDiv.classList.add('upload-progress-bar-progress');
    progressBarProgressDiv.style.width = '0%';
    progressBarDiv.appendChild(progressBarProgressDiv);

    // create progress info div
    const progressInfoDiv = document.createElement('div');
    progressInfoDiv.setAttribute('id', `us-progress-info-${chunkId}`);
    progressInfoDiv.classList.add('upload-progress-info');
    progressInfoDiv.textContent = '0%';

    // append progress bar and progress info to progress bar container
    progressContainerDiv.appendChild(progressBarDiv);
    progressContainerDiv.appendChild(progressInfoDiv);

    // append progress bar container to container
    container.appendChild(progressContainerDiv);

    container.setAttribute('data-starttime', Date.now());
    container.setAttribute('data-complete', "false");

    const uploadstatusContainer = document.getElementById("uploadstatus");
    uploadstatusContainer.appendChild(container);
    uploadstatusContainer.style.visibility = "visible";
    statusItemCount++;
}

function removeFileStatus(chunkId) {
    const container = document.getElementById(`us-container-${chunkId}`);
    if (container == null) {
        return;
    }
    container.remove();
    statusItemCount--;
    if (statusItemCount < 1) {
        document.getElementById("uploadstatus").style.visibility = "hidden";
    }
}


// One file uploaded now (or restored, or edited) onto the upload page's list.
// Without a preview the tile starts pending and the list polls for it.
function addUploadedFile(item, preview) {
    let browser = uploadSinkBrowser();
    if (browser === null) {
        return null;
    }
    item.Id = sanitizeId(item.Id);
    browser.addItem(item, preview || undefined);
    return item.Id;
}

// A file another tab added back (an undone delete) onto the Files tab's list:
// its current info is fetched first, so an expired or changed file shows as
// it is. Other pages have no list for it and ignore the message.
// A file request's upload belongs to that request's page, on no other list
function isRequestFile(item) {
    return !!item && (item.IsFileRequest === true || !!item.FileRequestId);
}

function addToFilesTab(id) {
    let root = document.querySelector('[data-fgs-browser][data-context="files"]');
    if (root === null || !window.FGS || !FGS.initFileBrowser) {
        return;
    }
    let browser = FGS.initFileBrowser(root);
    if (browser.tile(id) !== null) {
        return;
    }
    apiFilesListById(id)
        .then(item => {
            if (browser.tile(id) !== null || isRequestFile(item)) {
                return;
            }
            return fetch("./files/previewinfo?id=" + encodeURIComponent(id), { credentials: "same-origin", cache: "no-store" })
                .then(response => response.ok ? response.json() : undefined)
                .catch(() => undefined)
                .then(preview => {
                    if (browser.tile(id) === null) {
                        browser.addItem(item, preview);
                    }
                });
        })
        .catch(error => console.error("Unable to show the added file:", error));
}

function sanitizeId(str) {
    return str.replace(/[^a-zA-Z0-9]/g, '');
}

function hideQrCode() {
    document.getElementById("qroverlay").style.display = "none";
    document.getElementById("qrcode").innerHTML = "";
}

function showQrCode(url) {
    const overlay = document.getElementById("qroverlay");
    overlay.style.display = "block";
    new QRCode(document.getElementById("qrcode"), {
        text: url,
        width: 200,
        height: 200,
        colorDark: "#000000",
        colorLight: "#ffffff",
        correctLevel: QRCode.CorrectLevel.H
    });
    overlay.addEventListener("click", hideQrCode);
}


function showToastFileDeletion(id) {
    let notification = document.getElementById("toastnotificationUndo");
    let filename = document.getElementById("cell-name-" + id).innerText;
    let filenameToast = document.getElementById("toastFilename");
    let button = document.getElementById("toastUndoButton");

    filenameToast.innerText = filename;

    button.dataset.fileid = id;
    hideToast();
    notification.classList.add("show");

    clearTimeout(undoToastId);
    undoToastId = setTimeout(() => {
        hideFileToast();
    }, UNDO_TOAST_MS);
}

function hideFileToast() {
    document.getElementById("toastnotificationUndo").classList.remove("show");
}

// Restores the file and puts it back into the lists it was deleted from
// (FGS.restoreFile). Never reloads the page: that would collapse open rows.
// Whether it worked or not, the toast says so.
function handleUndo(button) {
    hideFileToast();
    let label = document.getElementById("toastFilename");
    let name = label ? label.textContent : "the file";
    FGS.restoreFile(button.dataset.fileid)
        .then(result => FGS.undoToast(name, result))
        .catch(error => {
            FGS.undoToast(name, null, error);
            console.error('Error:', error);
        });
}


function showDeprecationNotice() {
    let notification = document.getElementById("toastDeprecation");
    notification.classList.add("show");
    setTimeout(() => {
        notification.classList.remove("show");
    }, 5000);
}


// --- Auto-link: one shared link per upload batch -----------------------------
// Dropzone already knows which files went up together, so a batch of several
// files can become a single collection automatically. On by default; a saved
// "false" keeps it off.
//
// A batch is every file Dropzone queued up to its "queuecomplete". That event
// is not the moment the files have ids: chunk-complete is non-blocking, so the
// id only arrives later through the SSE status and requestFileInfo. And it can
// fire more than once (every late error re-emits it; in E2E mode it fires
// before chunk-complete has even run). So the collection is created only when
// the batch is closed AND every one of its files has either an id or failed.

let currentUploadBatch = null;

function addFileToUploadBatch(file) {
    if (currentUploadBatch === null) {
        currentUploadBatch = { files: [], closed: false, finished: false };
        // A link still on screen belongs to the previous upload.
        clearAutoLinkResult();
    }
    currentUploadBatch.files.push(file);
    file.fgsBatch = currentUploadBatch;
}

function closeUploadBatch() {
    let batch = currentUploadBatch;
    if (batch === null) {
        return;
    }
    currentUploadBatch = null;
    batch.closed = true;
    maybeCreateBatchCollection(batch);
}

function recordBatchFileId(file, id) {
    if (!file.fgsBatch || file.fgsBatchFailed || file.fgsBatchId !== undefined) {
        return;
    }
    file.fgsBatchId = id;
    maybeCreateBatchCollection(file.fgsBatch);
}

function markBatchFileFailed(file) {
    if (!file || !file.fgsBatch || file.fgsBatchFailed) {
        return;
    }
    // A failure after the id arrived (e.g. the E2E info store) still wins.
    file.fgsBatchFailed = true;
    maybeCreateBatchCollection(file.fgsBatch);
}

function isAutoLinkEnabled() {
    let toggle = document.getElementById("enableAutoCollection");
    return toggle !== null && toggle.checked;
}

function saveAutoLinkDefault() {
    localStorage.setItem("defaultAutoCollection", isAutoLinkEnabled());
}

function setAutoLinkDefault() {
    let toggle = document.getElementById("enableAutoCollection");
    if (toggle === null) {
        return;
    }
    toggle.checked = getLocalStorageWithDefault("defaultAutoCollection", "true") === "true";
}

function maybeCreateBatchCollection(batch) {
    if (batch.finished || !batch.closed) {
        return;
    }
    let settled = batch.files.every(f => f.fgsBatchFailed || f.fgsBatchId !== undefined);
    if (!settled) {
        return;
    }
    batch.finished = true;
    // Files that went into a group are shared there: no link is made for them
    let ids = batch.files.filter(f => !f.fgsBatchFailed && !(f.fgsTarget && f.fgsTarget.id !== ""))
        .map(f => f.fgsBatchId);
    // A single file already has its own link, so there is nothing to combine.
    if (!isAutoLinkEnabled() || ids.length < 2) {
        return;
    }
    fetch("./collections/create", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ name: ids.length + " files", fileIds: ids })
    })
        .then(response => response.json())
        .then(data => {
            if (data.Result !== "OK") {
                showAutoLinkResult(null, data.ErrorMessage || "could not create link");
                return;
            }
            showAutoLinkResult(data.Url, null);
        })
        .catch(error => showAutoLinkResult(null, String(error)));
}

function clearAutoLinkResult() {
    let holder = document.getElementById("autoCollectionResult");
    if (holder === null) {
        return;
    }
    holder.textContent = "";
    holder.className = "";
}

// opts (optional): holderId and label, so the Files tab's select bar can
// reuse this for its "Create one link" result. Defaults are the upload page's.
function showAutoLinkResult(url, errorMessage, opts) {
    opts = opts || {};
    let holder = document.getElementById(opts.holderId || "autoCollectionResult");
    if (holder === null) {
        return;
    }
    holder.textContent = "";
    if (errorMessage !== null) {
        holder.className = "alert alert-danger mt-3";
        holder.textContent = "Could not create a combined link: " + errorMessage;
        return;
    }
    holder.className = "alert alert-success mt-3 d-flex align-items-center gap-2";

    let label = document.createElement("span");
    label.className = "text-nowrap";
    label.textContent = opts.label || "One link for this upload:";
    holder.appendChild(label);

    let field = document.createElement("input");
    field.type = "text";
    field.readOnly = true;
    field.value = url;
    field.className = "form-control";
    holder.appendChild(field);

    let copy = document.createElement("button");
    copy.type = "button";
    copy.className = "btn btn-sm btn-light text-nowrap";
    copy.textContent = "Copy";
    copy.addEventListener("click", function () {
        navigator.clipboard.writeText(url).then(function () {
            copy.textContent = "Copied";
            window.setTimeout(function () { copy.textContent = "Copy"; }, 1500);
        });
    });
    holder.appendChild(copy);
}
