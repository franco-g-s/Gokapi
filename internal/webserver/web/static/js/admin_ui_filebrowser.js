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

    // A JSON answer, or a clear error: an expired session answers with the
    // login page (a redirect), which would otherwise surface as a parse error
    function readJson(response) {
        return response.text().then(function (text) {
            try {
                return JSON.parse(text);
            } catch (ignored) {
                throw new Error(response.redirected || text.trim().charAt(0) === "<" ?
                    "Your session expired — reload the page" : "Unexpected answer from the server");
            }
        });
    }

    // The figures of a collection changed by something other than its own
    // list (a file deleted, files added from another list): asked of the
    // server once the burst of changes is over, and handed to the page's
    // summary row through "fgs:collection-changed"
    var summaryWanted = {}, summaryTimer = null;
    function refreshCollectionSummary(collectionId) {
        if (!collectionId) return;
        summaryWanted[collectionId] = true;
        window.clearTimeout(summaryTimer);
        summaryTimer = window.setTimeout(function () {
            var wanted = summaryWanted;
            summaryWanted = {};
            fetch("./collections/list", { credentials: "same-origin" })
                .then(readJson)
                .then(function (list) {
                    (Array.isArray(list) ? list : []).forEach(function (c) {
                        if (!wanted[c.id]) return;
                        document.dispatchEvent(new CustomEvent("fgs:collection-changed", { detail: {
                            collectionId: c.id, fileCount: c.files, totalSize: c.totalSize,
                            expiry: c.expiry, neverExpires: c.neverExpires } }));
                    });
                })
                .catch(function (error) { console.warn("Could not refresh collection figures:", errorText(error)); });
        }, 400);
    }

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

    // The undo toast, named from the tile rather than from the cell-name-<id>
    // element showToastFileDeletion reads, which only single-list pages have
    function showDeletedToast(id, name) {
        var toast = document.getElementById("toastnotificationUndo");
        var label = document.getElementById("toastFilename");
        var button = document.getElementById("toastUndoButton");
        if (!toast || !label || !button) return;
        if (document.getElementById("cell-name-" + id)) {
            showToastFileDeletion(id);
        } else {
            button.dataset.fileid = id;
            hideToast();
            toast.classList.add("show");
            clearTimeout(toastId);
            toastId = setTimeout(hideFileToast, UNDO_TOAST_MS);
        }
        label.textContent = name;
    }

    // One file from its menu or from the selection bar with one file
    // selected: deleted after 10 s unless restored from the toast, and gone
    // from every list on the page that shows it. The undo toast is named from
    // the tile, not from a table row.
    function deleteOne(a) {
        var id = a.ids[0];
        var name = a.tiles[0].getAttribute("data-name") || "";
        var item = a.button;
        if (item) item.disabled = true;
        apiFilesDelete(id, 10)
            .then(function () {
                try { showDeletedToast(id, name); } catch (ignored) { /* page without the toast */ }
                rememberDeleted(id, removeEverywhere(id));
                if (typeof notifyWorker === "function") notifyWorker({ type: "fileDeleted", id: id });
            })
            .catch(function (error) {
                alert("Unable to delete file: " + error);
                console.error("Error:", error);
            })
            // The selection bar's button is reused for the next selection
            .then(function () { if (item) item.disabled = false; });
    }

    // Every list on the page that shows the file (a file can be in several
    // collections), so no list keeps a file that is gone or out of date.
    // Returns the lists' roots: where an undo puts the file back.
    function removeEverywhere(id) {
        var from = [];
        FGS.eachFileBrowser(function (b) {
            if (!b.tile(id)) return;
            from.push(b.root);
            b.removeItem(id);
            // A collection's row on the Collections page shows its figures
            if (b.context === "collection-admin") refreshCollectionSummary(b.root.getAttribute("data-collection-id"));
        });
        return from;
    }

    // --- Undo ---------------------------------------------------------------
    // A single delete is scheduled 10 s ahead on the server. The lists it was
    // taken out of are remembered here, so that restoring puts it back into
    // exactly those, with no reload (a reload would collapse open rows).
    var deletedFrom = {};
    function rememberDeleted(id, roots) { deletedFrom[id] = roots || []; }
    FGS.rememberDeleted = rememberDeleted;

    // The file as the lists show it now: its current info plus its preview.
    // The restore's own answer stands in when the info call fails.
    function currentListItem(id, restored) {
        var info = apiFilesListById(id).catch(function () { return restored; });
        var preview = fetch("./files/previewinfo?id=" + encodeURIComponent(id), { credentials: "same-origin", cache: "no-store" })
            .then(function (response) { return response.ok ? response.json() : undefined; })
            .catch(function () { return undefined; });
        return Promise.all([info, preview]);
    }

    // Restores a file whose delete is pending and puts it back into the lists
    // it was removed from. Resolves to { name, roots, shown }: roots are the
    // lists that took it (one already holding it is left alone), shown is false
    // when a list could not be given it. Rejects when the restore itself failed.
    FGS.restoreFile = function (id) {
        var wanted = deletedFrom[id] || [];
        return apiFilesRestore(id).then(function (data) {
            delete deletedFrom[id];
            var restored = data && data.FileInfo;
            if (!restored || !restored.Id) throw new Error("Unexpected answer from the server");
            return currentListItem(id, restored).then(function (pair) {
                var item = pair[0] || restored, preview = pair[1];
                var added = [], shown = true;
                wanted.forEach(function (root) {
                    try {
                        var b = root.isConnected ? FGS.initFileBrowser(root) : null;
                        if (!b) { shown = false; return; }
                        if (b.tile(item.Id)) return;
                        if (!b.addItem(item, preview)) { shown = false; return; }
                        added.push(root);
                        if (b.context === "collection-admin") refreshCollectionSummary(root.getAttribute("data-collection-id"));
                    } catch (error) {
                        shown = false;
                        console.error("Could not show the restored file:", error);
                    }
                });
                // Nothing recorded: the page cannot say where the file belongs
                if (!wanted.length) shown = false;
                if (typeof notifyWorker === "function") notifyWorker({ type: "fileAdded", item: item });
                if (typeof isE2EEnabled !== "undefined" && isE2EEnabled && typeof GokapiE2EDecryptMenu === "function") {
                    GokapiE2EDecryptMenu();
                }
                return { name: item.Name || restored.Name || "", roots: added, shown: shown };
            });
        });
    };

    // The toast after an undo: what happened, whichever way it went
    FGS.undoToast = function (name, result, error) {
        var text;
        if (error) text = "Could not restore " + name + ": " + errorText(error);
        else if (!result.shown) text = "Restored " + name + ", but it could not be shown here. Reload the page.";
        else text = "Restored: " + name;
        showToast(error || !result.shown ? 7000 : 2500, text);
    };

    function updateEverywhere(file) {
        FGS.eachFileBrowser(function (b) { if (b.tile(file.Id)) b.updateItem(file); });
    }

    // Bulk delete: asks first, then deletes each file in turn, at once (no
    // undo, unlike the single delete), taking each out of every list as it
    // goes. No reload: on the upload page the list exists only in this tab.
    async function bulkDelete(a) {
        var tiles = a.tiles;
        if (!confirm("Delete " + plural(tiles.length, "file") + "? This cannot be undone.")) return;
        a.button.disabled = true;
        var failures = [], deleted = 0;
        try {
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
                    continue;
                }
                // Deleted on the server. Showing it must not turn it into a failure.
                deleted++;
                try {
                    removeEverywhere(f.id);
                    if (typeof notifyWorker === "function") notifyWorker({ type: "fileDeleted", id: f.id });
                } catch (error) {
                    console.error("Error:", error);
                }
            }
        } finally {
            a.button.disabled = false;
        }
        a.status.textContent = "Deleted " + plural(deleted, "file") + ".";
        if (failures.length) {
            alert("Could not delete " + plural(failures.length, "file") + ":\n" + failures.join("\n"));
        }
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
                // Files added to a collection the page lists: its list and row follow
                if (endpoint === "./collections/modify" && body.addFileIds) {
                    document.dispatchEvent(new CustomEvent("fgs:collection-members-added", {
                        detail: { collectionId: body.id, ids: body.addFileIds, data: data } }));
                }
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
            tiles: [], running: false
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
            var failed = 0, total = expiry.tiles.length, done = false;
            // finally: whatever happens (an exception included) the dialog
            // must become closable again
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
                        // The answer is the file as stored now: shown in place,
                        // in every list, rather than by reloading the page
                        var updated = await apiFilesModify(f.id, downloads, expiresAt, "", true);
                        if (updated && updated.Id) updateEverywhere(updated);
                    } catch (error) {
                        failed++;
                        failure(expiry.failures, f.name, errorText(error));
                    }
                }
                if (!failed) {
                    expiry.status.textContent = "Updated " + plural(total, "file") + ".";
                    done = true;
                    return;
                }
                expiry.status.textContent = (total - failed) + " of " + plural(total, "file") + " updated; " + failed + " failed:";
            } finally {
                setRunning(false);
                // All done: close. With failures the dialog stays open, so
                // they can be read.
                if (done) bootstrap.Modal.getOrCreateInstance(modal).hide();
            }
        });
        // While files are being changed the dialog cannot be dismissed (backdrop,
        // Escape, Close): the run would carry on unseen and its failures be lost
        modal.addEventListener("hide.bs.modal", function (e) {
            if (expiry.running) e.preventDefault();
        });
        return expiry;
    }

    function openExpiry(tiles) {
        if (!expiryDialog()) return;
        expiry.tiles = tiles;
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

    // ClipboardJS copies every .copyurl; this only confirms it
    function copied(a) {
        showToast(1000);
        refocusLater(a.trigger);
    }

    FGS.registerFileActions({
        "copy": copied,
        "hotlink": copied,
        // A plain mailto: link; the body is made from the current URL, which
        // for an end-to-end encrypted file carries its #key (data-url)
        "email": function (a) {
            a.button.setAttribute("href", "mailto:?body=" + encodeURIComponent(fileData(a.tiles[0]).url));
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
        // One selected file is the menu's Delete (10 s undo); several are
        // confirmed and permanent. a.bulk is no test: the bar always sets it.
        "delete": function (a) {
            if (a.ids.length > 1) bulkDelete(a);
            else deleteOne(a);
        }
    });

    // --- Remove from collection (collection-admin) -------------------------
    // Single and bulk, through ./collections/modify's removeFileIds: the server
    // drops those members and keeps the rest, so a list that is out of date
    // (a file added from another list on this page) cannot lose files. Acts on
    // this list only: the same file may be in other collections, and deleting
    // is what removes a file from all of them. Reports the collection's new
    // figures in "fgs:collection-changed" for the page's summary row.
    function removeFromCollection(a) {
        var root = a.root, collectionId = root.getAttribute("data-collection-id");
        if (!collectionId || root.getAttribute("data-fgs-removing") === "true") return;
        var ids = a.ids;
        var all = root.querySelectorAll("[data-fgs-grid] .fgs-tile").length;
        if (ids.length >= all) {
            alert("A collection needs at least one file.\n\nDelete the collection instead.");
            return;
        }
        root.setAttribute("data-fgs-removing", "true");
        if (a.button) a.button.disabled = true;
        a.status.textContent = "Removing " + plural(ids.length, "file") + "…";
        fetch("./collections/modify", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify({ id: collectionId, removeFileIds: ids })
        })
            .then(readJson)
            .then(function (data) {
                if (data.Result !== "OK") throw new Error(data.ErrorMessage || "unknown error");
                ids.forEach(function (id) { a.browser.removeItem(id); });
                a.status.textContent = "Removed " + plural(ids.length, "file") + " from the collection.";
                document.dispatchEvent(new CustomEvent("fgs:collection-changed", { detail: {
                    collectionId: collectionId, fileCount: data.FileCount, totalSize: data.TotalSize,
                    expiry: data.Expiry, neverExpires: data.NeverExpires } }));
            })
            .catch(function (error) {
                a.status.textContent = "";
                alert("Could not remove: " + errorText(error));
            })
            .then(function () {
                root.removeAttribute("data-fgs-removing");
                if (a.button) a.button.disabled = false;
            });
    }
    FGS.registerFileActions({ "remove-from-collection": removeFromCollection }, "collection-admin");
})();
