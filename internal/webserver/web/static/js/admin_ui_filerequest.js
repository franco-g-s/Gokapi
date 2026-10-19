// This file contains JS code for the API view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...

function deleteFileRequest(requestId) {
    document.getElementById("delete-" + requestId).disabled = true;

    apiURequestDelete(requestId)
        .then(data => {
            const mainRow = document.getElementById("row-" + requestId);
            const fileListRow = document.getElementById("filelist-" + requestId);
            mainRow.classList.add("rowDeleting");
            if (fileListRow !== null) {
                fileListRow.classList.add("rowDeleting");
            }

            setTimeout(() => {
                mainRow.remove();
                if (fileListRow !== null) {
                    fileListRow.remove();
                }
            }, 290);
        })
        .catch(error => {
            alert("Unable to delete file request: " + error);
            console.error('Error:', error);
        });
}

function deleteOrShowModal(requestId, requestName, count) {
    if (count === 0) {
        deleteFileRequest(requestId);
    } else {
        showDeleteFRequestModal(requestId, requestName, count);
    }
}


function changeFileCountFr(frId, change) {
	let countCell = document.getElementById("totalFiles-fr-" + frId);
	if (countCell === null) {
		return;
	}
	let currentCount = Number(countCell.innerText) || 0;
	let newCount = currentCount + change;
	countCell.innerText = newCount;	
}

function removeDownloadFileReference(fileId, frId) {
    const btn = document.getElementById(`download-${frId}`);
    if (!btn) return;

    const currentOnclick = btn.getAttribute('onclick') || '';

    const zipMatch = currentOnclick.match(/downloadFilesZipWithPresign\('([^']*)',\s*'([^']*)'\)/);
    const singleMatch = currentOnclick.match(/downloadFileWithPresign\('([^']*)'\)/);

    let fileIds = [];
    let recordName = '';

    if (zipMatch) {
        fileIds = zipMatch[1].split(',').filter(id => id !== '');
        recordName = zipMatch[2];
    } else if (singleMatch) {
        fileIds = [singleMatch[1]];
        recordName = btn.dataset.recordName || '';
    }

    // Remove the deleted file
    fileIds = fileIds.filter(id => id !== fileId);

    if (fileIds.length === 0) {
        // No files left — disable button
        btn.classList.add('disabled');
        btn.removeAttribute('onclick');
    } else if (fileIds.length === 1) {
        // Single file — switch to single download
        btn.classList.remove('disabled');
        btn.setAttribute('onclick', `downloadFileWithPresign('${fileIds[0]}');`);
    } else {
        // Multiple files — keep zip download
        btn.classList.remove('disabled');
        btn.setAttribute('onclick', `downloadFilesZipWithPresign('${fileIds.join(',')}', '${recordName}');`);
    }
}


function showToastFileDeletionFr(id, filename) {
    let notification = document.getElementById("toastnotificationUndo");
    let filenameToast = document.getElementById("toastFilename");
    let button = document.getElementById("toastUndoButton");

    filenameToast.textContent = filename;

    button.dataset.fileid = id;
    hideToast();
    notification.classList.add("show");

    clearTimeout(toastId);
    toastId = setTimeout(() => {
        hideFileToast();
    }, 5000);
}


function handleUndoFr(button) {
    hideFileToast();
    apiFilesRestore(button.dataset.fileid)
        .then(data => {
            window.location.reload();
        })
        .catch(error => {
            alert("Unable to restore file: " + error);
            console.error('Error:', error);
        });
}


function showDeleteFRequestModal(requestId, requestName, count) {
    document.getElementById("deleteModalBodyName").innerText = requestName;
    document.getElementById("deleteModalBodyCount").innerText = count;
    $('#deleteModal').modal('show');

    document.getElementById("buttonDelete").onclick = function() {
        $('#deleteModal').modal('hide');
        deleteFileRequest(requestId);
    };
}


function newFileRequest() {
    loadFileRequestDefaults();
    document.getElementById("m_urequestlabel").innerText = "New File Request";
    $('#addEditModal').modal('show');

    document.getElementById("b_fr_save").onclick = function() {
        saveFileRequestDefaults();
        saveFileRequest();
        $('#addEditModal').modal('hide');
    };
}

function saveFileRequestDefaults() {
    if (document.getElementById("mc_maxfiles").checked) {
        localStorage.setItem("fr_maxfiles", document.getElementById("mi_maxfiles").value);
    } else {
        localStorage.setItem("fr_maxfiles", 0);
    }
    if (document.getElementById("mc_maxsize").checked) {
        localStorage.setItem("fr_maxsize", document.getElementById("mi_maxsize").value);
    } else {
        localStorage.setItem("fr_maxsize", 0);
    }
    if (document.getElementById("mc_expiry").checked) {
        let diff = document.getElementById("mi_expiry").value - Math.round(Date.now() / 1000);
        localStorage.setItem("fr_expiry", diff);
    } else {
        localStorage.setItem("fr_expiry", 0);
    }
}

function loadFileRequestDefaults() {
    const defaultMaxFiles = localStorage.getItem("fr_maxfiles");
    const defaultMaxSize = localStorage.getItem("fr_maxsize");
    let defaultExpiry = localStorage.getItem("fr_expiry");

    if (defaultExpiry !== "0" && defaultExpiry !== null) {
        let defaultDate = new Date(Date.now() + Number((defaultExpiry) * 1000));
        defaultDate.setHours(12, 0, 0, 0);
        defaultExpiry = Math.floor(defaultDate.getTime() / 1000);
    }

    setModalValues("", "", defaultMaxFiles, defaultMaxSize, defaultExpiry, "");
}

function setModalValues(id, name, maxFiles, maxSize, expiry, notes) {
    document.getElementById("freqId").value = id;

    if (name === null) {
        document.getElementById("mFriendlyName").value = "";
    } else {
        document.getElementById("mFriendlyName").value = name;
    }


    if (limitMaxFiles != 0) {
        let checkbox = document.getElementById("mc_maxfiles");
        if (maxFiles === null || maxFiles == 0) {
            maxFiles = limitMaxFiles;
        }
        checkbox.checked = true;
        checkbox.disabled = true;
        checkbox.title = "Only admins can set this to unlimited";
        checkbox.value = "1";
        document.getElementById("mi_maxfiles").setAttribute("max", limitMaxFiles);
    } else {
        let checkbox = document.getElementById("mc_maxfiles");
        checkbox.disabled = false;
        checkbox.title = "";
        document.getElementById("mi_maxfiles").setAttribute("max", "");
    }

    if (limitMaxSize != 0) {
        let checkbox = document.getElementById("mc_maxsize");
        if (maxSize === null || maxSize == 0) {
            maxSize = limitMaxSize;
        }
        checkbox.checked = true;
        checkbox.disabled = true;
        checkbox.title = "Only admins can set this to unlimited";
        checkbox.value = "1";
        document.getElementById("mi_maxsize").setAttribute("max", limitMaxSize);
    } else {
        let checkbox = document.getElementById("mc_maxsize");
        checkbox.disabled = false;
        checkbox.title = "";
        document.getElementById("mi_maxsize").setAttribute("max", "");
    }

    if (maxFiles === null || maxFiles == 0) {
        document.getElementById("mi_maxfiles").value = "1";
        document.getElementById("mi_maxfiles").disabled = true;
        document.getElementById("mc_maxfiles").checked = false;
    } else {
        document.getElementById("mi_maxfiles").value = maxFiles;
        document.getElementById("mi_maxfiles").disabled = false;
        document.getElementById("mc_maxfiles").checked = true;
    }


    if (maxSize === null || maxSize == 0) {
        document.getElementById("mi_maxsize").value = "10";
        document.getElementById("mi_maxsize").disabled = true;
        document.getElementById("mc_maxsize").checked = false;
    } else {
        document.getElementById("mi_maxsize").value = maxSize;
        document.getElementById("mi_maxsize").disabled = false;
        document.getElementById("mc_maxsize").checked = true;
    }

    if (expiry === null || expiry == 0) {
        const defaultDate = Math.floor(new Date(Date.now() + (14 * 24 * 60 * 60 * 1000)).getTime() / 1000);
        document.getElementById("mi_expiry").disabled = true;
        document.getElementById("mc_expiry").checked = false;
        document.getElementById("mi_expiry").value = defaultDate;
        createCalendar("mi_expiry", defaultDate);
    } else {
        document.getElementById("mi_expiry").value = expiry;
        document.getElementById("mi_expiry").disabled = false;
        document.getElementById("mc_expiry").checked = true;
        createCalendar("mi_expiry", expiry);
    }
    document.getElementById("mNotes").value = notes;
}

function editFileRequest(id, name, maxFiles, maxSize, expiry, notes) {
    setModalValues(id, name, maxFiles, maxSize, expiry, notes);
    document.getElementById("m_urequestlabel").innerText = "Edit File Request";
    $('#addEditModal').modal('show');

    document.getElementById("b_fr_save").onclick = function() {
        saveFileRequest();
        $('#addEditModal').modal('hide');
    };
}


function saveFileRequest() {
    const buttonSave = document.getElementById("b_fr_save");
    const id = document.getElementById("freqId").value;
    const name = document.getElementById("mFriendlyName").value;
    const notes = document.getElementById("mNotes").value;
    let maxFiles = 0;
    let maxSize = 0;
    let expiry = 0;

    if (document.getElementById("mc_maxfiles").checked) {
        maxFiles = document.getElementById("mi_maxfiles").value;
    }
    if (document.getElementById("mc_maxsize").checked) {
        maxSize = document.getElementById("mi_maxsize").value;
    }
    if (document.getElementById("mc_expiry").checked) {
        expiry = document.getElementById("mi_expiry").value;
    }

    buttonSave.disabled = true;
    apiURequestSave(id, name, maxFiles, maxSize, expiry, notes)
        .then(data => {
            document.getElementById("b_fr_save").disabled = false;
            insertOrReplaceFileRequest(data);
        })
        .catch(error => {
            alert("Unable to save file request: " + error);
            console.error('Error:', error);
            document.getElementById("b_fr_save").disabled = false;
        });
}

function checkMaxNumber(element) {
    if (element.value == "") {
        element.value = "1";
        return;
    }
    let maxVal = element.getAttribute("max");
    if (maxVal == "") {
        return;
    }
    if (element.value > maxVal) {
        element.value = maxVal;
    }
}

function insertOrReplaceFileRequest(jsonResult) {
    const tbody = document.getElementById("filerequesttable");
    let row = document.getElementById(`row-${jsonResult.id}`);

    if (row) {
        const user = document.getElementById(`cell-username-${jsonResult.id}`).innerText;
        row.replaceWith(createFileRequestRow(jsonResult, user));
    } else {
        let tr = createFileRequestRow(jsonResult, userName);
        tr.querySelectorAll('td').forEach((td) => {
            td.classList.add("newFileRequest");
            setTimeout(() => {
                td.classList.remove("newFileRequest");
            }, 700);
        });
        tbody.prepend(tr);
    }
}


function createFileRequestRow(jsonResult, user) {

    function tdText(text) {
        const td = document.createElement("td");
        td.textContent = text;
        return td;
    }

    function tdLink(text, href) {
        const td = document.createElement("td");
        const a = document.createElement("a");
        a.textContent = text;
        a.href = href;
        a.target = "_blank";
        td.appendChild(a);
        return td;
    }

    function icon(classes) {
        const i = document.createElement("i");
        i.className = `bi ${classes}`;
        return i;
    }

    const publicUrl = `${baseUrl}publicUpload?id=${jsonResult.id}&key=${jsonResult.apikey}`;

    const tr = document.createElement("tr");
    tr.id = `row-${jsonResult.id}`;
    tr.className = "filerequest-item";

    // Name
    tr.appendChild(tdLink(jsonResult.name, publicUrl));
    // Uploaded files / Max files
    if (jsonResult.maxfiles == 0) {
        tr.appendChild(tdText(jsonResult.uploadedfiles));
    } else {
        tr.appendChild(tdText(`${jsonResult.uploadedfiles} / ${jsonResult.maxfiles}`));
    }
    // Total size
    tr.appendChild(tdText(getReadableSize(jsonResult.totalfilesize)));
    // Last upload
    tr.appendChild(tdText(formatTimestampWithNegative(jsonResult.lastupload, "None")));
    // Expiry
    tr.appendChild(tdText(formatFileRequestExpiry(jsonResult.expiry)));
    // Optional user column
    if (canViewOtherRequests) {
        let userTd = tdText(user);
        userTd.id = `cell-username-${jsonResult.id}`;
        tr.appendChild(userTd);
    }
    // Buttons
    const td = document.createElement("td");

    const group = document.createElement("div");
    group.className = "btn-group";
    group.role = "group";

    // Download
    const downloadBtn = document.createElement("button");
    downloadBtn.id = `download-${jsonResult.id}`;
    downloadBtn.type = "button";
    downloadBtn.className = "btn btn-outline-light btn-sm";
    downloadBtn.title = "Download all";

    if (jsonResult.uploadedfiles == 0) {
        downloadBtn.classList.add("disabled");
    }

    downloadBtn.appendChild(icon("bi-download"));


    // Copy
    const copyBtn = document.createElement("button");
    copyBtn.id = `copy-${jsonResult.id}`;
    copyBtn.type = "button";
    copyBtn.className = "copyurl btn btn-outline-light btn-sm";
    copyBtn.title = "Copy URL";
    copyBtn.setAttribute("data-clipboard-text", publicUrl);
    copyBtn.onclick = () =>
        showToast(1000);

    copyBtn.appendChild(icon("bi-copy"));


    // Edit
    const editBtn = document.createElement("button");
    editBtn.id = `edit-${jsonResult.id}`;
    editBtn.type = "button";
    editBtn.className = "btn btn-outline-light btn-sm";
    editBtn.title = "Edit request";
    editBtn.onclick = () =>
        editFileRequest(jsonResult.id, jsonResult.name, jsonResult.maxfiles, jsonResult.maxsize, jsonResult.expiry, jsonResult.notes);

    editBtn.appendChild(icon("bi-pencil"));

    // Delete
    const deleteBtn = document.createElement("button");
    deleteBtn.id = `delete-${jsonResult.id}`;
    deleteBtn.type = "button";
    deleteBtn.className = "btn btn-outline-danger btn-sm";
    deleteBtn.title = "Delete";
    deleteBtn.onclick = () =>
        deleteOrShowModal(jsonResult.id, jsonResult.name, jsonResult.uploadedfiles);

    deleteBtn.appendChild(icon("bi-trash3"));

    group.append(downloadBtn, copyBtn, editBtn, deleteBtn);
    td.appendChild(group);
    tr.appendChild(td);
    return tr;
}


// --- The per-request file lists (shared file browser, context "filerequest") ---
// Request files are downloaded through the presigned flow (a short-lived,
// single-use link from the API), so the list's built-in download, zip and Save
// to Photos are replaced here. Open (the viewer) is the built-in one.
(function () {
    if (!window.FGS || !FGS.registerFileActions) return;

    function requestIdOf(root) {
        return root.id.indexOf("fr-") === 0 ? root.id.slice(3) : "";
    }

    function requestNameOf(root) {
        const row = document.getElementById("row-" + requestIdOf(root));
        const text = row && row.cells[0] ? row.cells[0].textContent.trim() : "";
        return text || "Uploaded files";
    }

    function errorText(error) {
        return error && error.message ? error.message : String(error);
    }

    function presignUrl(id) {
        return apiFilesListDownloadSingle(id).then(data => {
            if (!data || !data.downloadUrl) {
                throw new Error("Unable to get presigned key");
            }
            return data.downloadUrl;
        });
    }

    function pause(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }

    // A presigned link lives for 30 seconds and works once, so each is
    // requested right before it is used
    async function downloadEach(a) {
        a.button.disabled = true;
        const failures = [];
        for (let i = 0; i < a.tiles.length; i++) {
            const name = a.tiles[i].getAttribute("data-name");
            a.status.textContent = "Starting download " + (i + 1) + " of " + a.tiles.length + "...";
            try {
                const link = document.createElement("a");
                link.href = await presignUrl(a.ids[i]);
                link.style.display = "none";
                document.body.appendChild(link);
                link.click();
                link.remove();
            } catch (error) {
                failures.push(name + ": " + errorText(error));
            }
            // Spacing the clicks keeps the browser from dropping queued downloads
            await pause(600);
        }
        a.button.disabled = false;
        a.status.textContent = failures.length
            ? "Could not download " + failures.length + " file(s): " + failures.join("; ")
            : "Started " + a.tiles.length + " download" + (a.tiles.length === 1 ? "" : "s") + ".";
    }

    function download(a) {
        if (!a.bulk) {
            downloadFileWithPresign(a.ids[0]);
            return;
        }
        downloadEach(a);
    }

    function zip(a) {
        downloadFilesZipWithPresign(a.ids.join(","), requestNameOf(a.root));
    }

    // Save to Photos: the list's own two-tap flow, fed with links that are
    // only requested when each file is about to be fetched
    function photos(a) {
        const button = a.button;
        const prepared = button._fgs && button._fgs.files;
        const items = prepared ? [] : a.tiles.map(tile => ({
            url: () => presignUrl(tile.getAttribute("data-id")),
            name: tile.getAttribute("data-name"),
            bytes: parseInt(tile.getAttribute("data-bytes"), 10)
        }));
        FGS.saveToPhotos(items, button);
    }

    function removeFile(a, id, name) {
        const requestId = requestIdOf(a.root);
        a.browser.removeItem(id);
        changeFileCountFr(requestId, -1);
        removeDownloadFileReference(id, requestId);
        showToastFileDeletionFr(id, name);
    }

    function mayDelete(a, tile) {
        return tile.getAttribute("data-mine") === "true" ||
            a.root.getAttribute("data-perm-delete-others") === "true";
    }

    function deleteOne(a) {
        const id = a.ids[0];
        const name = a.tiles[0].getAttribute("data-name");
        a.button.disabled = true;
        apiFilesDelete(id, 10)
            .then(() => removeFile(a, id, name))
            .catch(error => {
                a.button.disabled = false;
                alert("Unable to delete file: " + error);
                console.error("Error:", error);
            });
    }

    async function deleteMany(a) {
        if (!confirm("Delete " + a.tiles.length + " file(s)? This cannot be undone.")) return;
        a.button.disabled = true;
        const failures = [];
        for (let i = 0; i < a.tiles.length; i++) {
            const name = a.tiles[i].getAttribute("data-name");
            a.status.textContent = "Deleting " + (i + 1) + " of " + a.tiles.length + "...";
            if (!mayDelete(a, a.tiles[i])) {
                failures.push(name + ": you may not delete this file");
                continue;
            }
            try {
                await apiFilesDelete(a.ids[i], 0);
            } catch (error) {
                failures.push(name + ": " + errorText(error));
            }
        }
        if (failures.length) {
            alert("Could not delete " + failures.length + " file(s):\n" + failures.join("\n"));
        }
        location.reload();
    }

    FGS.registerFileActions({
        "download": download,
        "zip": zip,
        "photos": photos,
        "delete": a => (a.bulk ? deleteMany(a) : deleteOne(a))
    }, "filerequest");

    // The lists sit in collapsed rows: lay out and size them once shown
    document.addEventListener("shown.bs.collapse", event => {
        const root = event.target.querySelector("[data-fgs-browser]");
        if (!root || !root.fgsBrowser) return;
        root.fgsBrowser.relayout();
        if (window.jQuery && jQuery.fn.dataTable) {
            jQuery(root).find("table.fgs-table").each(function () {
                if (jQuery.fn.dataTable.isDataTable(this)) jQuery(this).DataTable().columns.adjust();
            });
        }
    });
})();
