// This file contains the management actions of the shared file list
// (html_file_browser.tmpl, FGS.initFileBrowser in media.js)
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...

// The component owns the views, select mode, the viewer and the visitors'
// actions (open, download, zip, Save to Photos). This registers everything
// that manages a file, reusing the upload page's helpers: edit, replace,
// copy / share / QR, delete, add to collection, change expiry & limits,
// create one link, and (Task C3) remove from collection. The dialogs live in
// "file_browser_dialogs" and "file_browser_support", once per page.
(function () {
    if (!window.FGS || !FGS.registerFileActions) return;

    function plural(n, word) { return n + " " + word + (n === 1 ? "" : "s"); }
    function errorText(error) { return error && error.message ? error.message : String(error); }

    // Per-file data for the actions, from the file's "⋯" menu in the grid,
    // which is always in the DOM
    function fileData(tile) {
        var menu = tile.querySelector("[data-fgs-file-menu]");
        var d = function (k) { return menu.getAttribute("data-" + k); };
        return { id: d("id"), name: tile.getAttribute("data-name"), url: d("url"), e2e: d("e2e") === "true",
            canEdit: d("can-edit") === "true", canDelete: d("can-delete") === "true" };
    }

    // ClipboardJS blurs whatever has focus once it has copied, so give it
    // back to the "⋯" afterwards
    function refocusLater(el) { if (el) window.setTimeout(function () { el.focus(); }, 0); }

    function copyText(text) {
        if (window.ClipboardJS && ClipboardJS.copy) {
            ClipboardJS.copy(text);
            showToast(1000);
        } else if (navigator.clipboard) {
            navigator.clipboard.writeText(text).then(function () { showToast(1000); });
        }
    }

    function editFromMenu(menu, tile, replace) {
        var flag = function (k) { return menu.getAttribute("data-" + k) === "true"; };
        showEditModal(tile.getAttribute("data-name"), menu.getAttribute("data-id"),
            parseInt(menu.getAttribute("data-downloads"), 10), parseInt(menu.getAttribute("data-expire"), 10),
            flag("password"), flag("unlimited-downloads"), flag("unlimited-time"), flag("e2e"), flag("can-replace"));
        if (!replace) return;
        // "Replace content" is the edit dialog's replace field, switched on
        var box = document.getElementById("mc_replace");
        if (box && !box.disabled) {
            box.checked = true;
            handleEditCheckboxChange(box);
        }
    }

    // One file from its menu: deleted after 10 s unless restored from the
    // toast. Not through deleteFile(): its row bookkeeping belongs to the
    // upload page's old table.
    function deleteOne(a) {
        var id = a.ids[0];
        var item = a.button;
        if (item) item.disabled = true;
        apiFilesDelete(id, 10)
            .then(function () {
                // The toast reads the name from cell-name-<id>: before the row goes
                try { showToastFileDeletion(id); } catch (ignored) { /* page without the toast */ }
                a.browser.removeItem(id);
                if (typeof notifyWorker === "function") notifyWorker({ type: "fileDeleted", id: id });
            })
            .catch(function (error) {
                if (item) item.disabled = false;
                alert("Unable to delete file: " + error);
                console.error("Error:", error);
            });
    }

    // Bulk delete: asks first, then deletes each file in turn, at once (no
    // undo, unlike the single delete) and reloads
    async function bulkDelete(a) {
        var tiles = a.tiles;
        if (!confirm("Delete " + plural(tiles.length, "file") + "? This cannot be undone.")) return;
        a.button.disabled = true;
        var failures = [];
        for (var i = 0; i < tiles.length; i++) {
            var f = fileData(tiles[i]);
            a.status.textContent = "Deleting " + (i + 1) + " of " + tiles.length + "…";
            if (!f.canDelete) {
                failures.push(f.name + ": you may not delete this file");
                continue;
            }
            try {
                await apiFilesDelete(f.id, 0);
            } catch (error) {
                failures.push(f.name + ": " + errorText(error));
            }
        }
        if (failures.length) {
            alert("Could not delete " + plural(failures.length, "file") + ":\n" + failures.join("\n"));
        }
        location.reload();
    }

    // One collection link for the selection, through the same endpoint the
    // upload page's automatic link uses. The server again keeps only files
    // this user may list, and refuses end-to-end encrypted ones.
    function createOneLink(a) {
        var tiles = a.tiles, button = a.button;
        if (button.disabled) return;
        button.disabled = true; // each request makes a new collection
        var opts = { holderId: a.linkResult.id, label: "One link for " + plural(tiles.length, "file") + ":" };
        fetch("./collections/create", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify({
                // The name heads the public board: a lone file is named after itself
                name: tiles.length === 1 ? tiles[0].getAttribute("data-name") : tiles.length + " files",
                fileIds: a.ids
            })
        })
            .then(function (response) { return response.json(); })
            .then(function (data) {
                if (data.Result !== "OK") {
                    showAutoLinkResult(null, data.ErrorMessage || "could not create link", opts);
                    return;
                }
                showAutoLinkResult(data.Url, null, opts);
            })
            .catch(function (error) { showAutoLinkResult(null, String(error), opts); })
            .then(function () { button.disabled = false; });
    }

    // "Add to collection…": pick one of your own collections (?mine=1, even
    // with PERM_LIST) or start a new one. The server keeps only files this
    // user may list; an end-to-end encrypted file would make it refuse the
    // whole request, so those are left out here and the dialog says so.
    var collect = null;
    function collectDialog() {
        if (collect) return collect;
        var modal = document.getElementById("fgs-collect-modal");
        if (!modal) return null;
        collect = {
            modal: modal,
            list: document.getElementById("fgs-collect-list"),
            name: document.getElementById("fgs-collect-name"),
            create: document.getElementById("fgs-collect-create"),
            result: document.getElementById("fgs-collect-result"),
            ids: []
        };
        collect.create.addEventListener("click", function () {
            var name = collect.name.value.trim();
            if (!collect.ids.length || collect.create.disabled) return;
            collect.create.disabled = true; // each request makes a new collection
            collectRequest("./collections/create", { name: name, fileIds: collect.ids },
                "New collection" + (name ? " “" + name + "”" : "") + ":",
                function (data) { return data.Url; })
                // Once made, it stays made: another click would create a second
                // collection. Reopening the dialog starts over.
                .then(function (ok) { collect.create.disabled = ok; });
        });
        return collect;
    }

    function collectError(message) {
        collect.result.className = "alert alert-danger mt-3";
        collect.result.textContent = message;
    }

    function collectRequest(endpoint, body, label, urlFromData) {
        collect.result.textContent = "";
        collect.result.className = "";
        return fetch(endpoint, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(body)
        })
            .then(function (response) { return response.json(); })
            .then(function (data) {
                if (data.Result !== "OK") {
                    collectError("Could not add: " + (data.ErrorMessage || "unknown error"));
                    return false;
                }
                showAutoLinkResult(urlFromData(data), null, { holderId: "fgs-collect-result", label: label });
                return true;
            })
            .catch(function (error) { collectError("Could not add: " + errorText(error)); return false; });
    }

    function renderCollections(collections) {
        collect.list.textContent = "";
        if (!collections.length) {
            var none = document.createElement("div");
            none.className = "list-group-item text-muted";
            none.textContent = "No collections yet.";
            collect.list.appendChild(none);
            return;
        }
        collections.forEach(function (c) {
            var b = document.createElement("button");
            b.type = "button";
            b.className = "list-group-item list-group-item-action d-flex justify-content-between align-items-center gap-2";
            var name = document.createElement("span");
            name.textContent = c.name || "Untitled collection";
            var count = document.createElement("span");
            count.className = "text-muted small text-nowrap";
            count.textContent = plural(c.files, "file");
            b.appendChild(name);
            b.appendChild(count);
            b.addEventListener("click", function () {
                if (!collect.ids.length || b.disabled) return;
                b.disabled = true;
                collectRequest("./collections/modify", { id: c.id, addFileIds: collect.ids },
                    "Added to “" + (c.name || "Untitled collection") + "”:",
                    function (data) { return collect.modal.getAttribute("data-server-url") + "c?id=" + data.Id; })
                    .then(function () { b.disabled = false; });
            });
            collect.list.appendChild(b);
        });
    }

    function openCollect(tiles) {
        if (!collectDialog()) return;
        var usable = tiles.filter(function (t) { return !fileData(t).e2e; });
        collect.ids = usable.map(function (t) { return t.getAttribute("data-id"); });
        var what = plural(usable.length, "file");
        if (usable.length < tiles.length) {
            what += " (" + plural(tiles.length - usable.length, "end-to-end encrypted file") + " left out: they cannot be in a collection)";
        }
        document.getElementById("fgs-collect-what").textContent = what;
        collect.result.textContent = "";
        collect.result.className = "";
        collect.name.value = usable.length === 1 ? usable[0].getAttribute("data-name") : usable.length + " files";
        collect.create.disabled = !usable.length;
        collect.list.textContent = "";
        var loading = document.createElement("div");
        loading.className = "list-group-item text-muted";
        loading.textContent = "Loading your collections…";
        collect.list.appendChild(loading);
        fetch("./collections/list?mine=1", { credentials: "same-origin" })
            .then(function (response) { return response.json(); })
            .then(function (data) { renderCollections(Array.isArray(data) ? data : []); })
            .catch(function (error) {
                collect.list.textContent = "";
                collectError("Could not load your collections: " + errorText(error));
            });
        bootstrap.Modal.getOrCreateInstance(collect.modal).show();
    }

    // "Change expiry & limits": one /api/files/modify per file, in turn.
    // "Keep" leaves that header out, so the server keeps whatever is stored
    // now (not what the page loaded with); the password is always kept.
    var expiry = null;
    function localInputValue(date) {
        var pad = function (n) { return (n < 10 ? "0" : "") + n; };
        return date.getFullYear() + "-" + pad(date.getMonth() + 1) + "-" + pad(date.getDate()) +
            "T" + pad(date.getHours()) + ":" + pad(date.getMinutes());
    }
    function failure(list, name, reason) {
        var li = document.createElement("li");
        li.textContent = name + ": " + reason;
        list.appendChild(li);
    }

    function expiryDialog() {
        if (expiry) return expiry;
        var modal = document.getElementById("fgs-expiry-modal");
        if (!modal) return null;
        expiry = {
            modal: modal,
            apply: document.getElementById("fgs-expiry-apply"),
            status: document.getElementById("fgs-expiry-status"),
            failures: document.getElementById("fgs-expiry-failures"),
            date: document.getElementById("fgs-expiry-date"),
            limit: document.getElementById("fgs-limit-number"),
            closeButtons: modal.querySelectorAll("[data-bs-dismiss=modal]"),
            tiles: [], changed: false, running: false
        };
        function setRunning(running) {
            expiry.running = running;
            expiry.closeButtons.forEach(function (b) { b.disabled = running; });
        }
        expiry.setRunning = setRunning;
        // Typing a date or a number picks its option
        expiry.date.addEventListener("input", function () { document.getElementById("fgs-expiry-on").checked = true; });
        expiry.limit.addEventListener("input", function () { document.getElementById("fgs-limit-on").checked = true; });

        expiry.apply.addEventListener("click", async function () {
            var expiryChoice = modal.querySelector("input[name=fgs-expiry]:checked").value;
            var limitChoice = modal.querySelector("input[name=fgs-limit]:checked").value;
            if (expiryChoice === "keep" && limitChoice === "keep") {
                expiry.status.textContent = "Nothing to change: pick a new expiry or download limit.";
                return;
            }
            var expiryAt = 0, limit = 0;
            if (expiryChoice === "date") {
                var ms = new Date(expiry.date.value).getTime();
                if (!expiry.date.value || isNaN(ms) || ms <= Date.now()) {
                    expiry.status.textContent = "Pick an expiry date in the future.";
                    return;
                }
                expiryAt = Math.floor(ms / 1000);
            }
            if (limitChoice === "number") {
                limit = parseInt(expiry.limit.value, 10);
                if (!(limit >= 1)) {
                    expiry.status.textContent = "The download limit must be 1 or more.";
                    return;
                }
            }
            expiry.apply.disabled = true;
            setRunning(true);
            expiry.failures.textContent = "";
            var failed = 0, total = expiry.tiles.length;
            // finally: whatever happens (an exception, a reload cancelled by
            // beforeunload) the dialog must become closable again
            try {
                for (var i = 0; i < total; i++) {
                    expiry.status.textContent = "Updating " + (i + 1) + " of " + total + "…";
                    var f;
                    try {
                        f = fileData(expiry.tiles[i]);
                    } catch (error) {
                        failed++;
                        failure(expiry.failures, expiry.tiles[i].getAttribute("data-name") || "a file", errorText(error));
                        continue;
                    }
                    if (!f.canEdit) {
                        failed++;
                        failure(expiry.failures, f.name, "you may not edit this file");
                        continue;
                    }
                    // null leaves it unchanged; 0 means unlimited / never to the API
                    var downloads = limitChoice === "keep" ? null : limit;
                    var expiresAt = expiryChoice === "keep" ? null : expiryAt;
                    try {
                        await apiFilesModify(f.id, downloads, expiresAt, "", true);
                        expiry.changed = true;
                    } catch (error) {
                        failed++;
                        failure(expiry.failures, f.name, errorText(error));
                    }
                }
                if (!failed) {
                    expiry.status.textContent = "Updated " + plural(total, "file") + ".";
                    location.reload();
                    return;
                }
                expiry.status.textContent = (total - failed) + " of " + plural(total, "file") + " updated; " + failed + " failed:";
            } finally {
                setRunning(false);
            }
        });
        // While files are being changed the dialog cannot be dismissed (backdrop,
        // Escape, Close): closing would reload the page mid-run and lose the failures
        modal.addEventListener("hide.bs.modal", function (e) {
            if (expiry.running) e.preventDefault();
        });
        // After a partial run the failures stay readable until the dialog closes
        modal.addEventListener("hidden.bs.modal", function () {
            if (expiry.changed) location.reload();
        });
        return expiry;
    }

    function openExpiry(tiles) {
        if (!expiryDialog()) return;
        expiry.tiles = tiles;
        expiry.changed = false;
        expiry.setRunning(false);
        document.getElementById("fgs-expiry-keep").checked = true;
        document.getElementById("fgs-limit-keep").checked = true;
        expiry.date.value = localInputValue(new Date(Date.now() + 14 * 24 * 3600 * 1000));
        expiry.date.min = localInputValue(new Date());
        document.getElementById("fgs-expiry-what").textContent = plural(tiles.length, "file");
        expiry.status.textContent = "";
        expiry.failures.textContent = "";
        expiry.apply.disabled = false;
        bootstrap.Modal.getOrCreateInstance(expiry.modal).show();
    }

    FGS.registerFileActions({
        // ClipboardJS copies every .copyurl; this only confirms it
        "copy": function (a) {
            showToast(1000);
            refocusLater(a.trigger);
        },
        "share": function (a) {
            var url = fileData(a.tiles[0]).url;
            if (navigator.share) {
                navigator.share({ title: a.tiles[0].getAttribute("data-name"), url: url })
                    .catch(function () { /* the sheet was dismissed */ });
            } else {
                copyText(url);
                refocusLater(a.trigger);
            }
        },
        "qr": function (a) { showQrCode(fileData(a.tiles[0]).url); },
        "edit": function (a) { editFromMenu(a.menu, a.tiles[0], false); },
        "replace": function (a) { editFromMenu(a.menu, a.tiles[0], true); },
        "add-to-collection": function (a) { openCollect(a.tiles); },
        "change-expiry": function (a) { openExpiry(a.tiles); },
        "link": createOneLink,
        "delete": function (a) {
            if (a.bulk) bulkDelete(a);
            else deleteOne(a);
        }
    });

    // --- Remove from collection (collection-admin) — Task C3 ---------------
    // Single and bulk; a.root.getAttribute("data-collection-id") names the
    // collection. A stub until C3 implements it with ./collections/modify.
    FGS.registerFileActions({
        "remove-from-collection": function (a) {
            a.status.textContent = "Remove from collection is not available yet.";
        }
    }, "collection-admin");
})();
