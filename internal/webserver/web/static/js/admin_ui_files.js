// This file contains JS code for the Files tab
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...

// Files tab: grid/list, My files / Everything, icon size, select mode.
// Remembers choices in localStorage, like the upload page's defaults.
//
// Select mode is off by default: no checkboxes, and a tile or a name in the
// list opens the viewer. With it on, the same clicks toggle selection. The
// grid's checkboxes are the selection; the list's mirror them, because
// DataTables takes filtered-out rows out of the DOM.
function initFilesPage() {
    var root = document.getElementById("fgs-files");
    if (!root) return;
    var grid = document.getElementById("fgs-grid");
    var list = document.getElementById("fgs-list");
    var bar = document.getElementById("fgs-selectbar");
    var status = document.getElementById("fgs-selstatus");
    var selectToggle = root.querySelector("[data-fgs-select-toggle]");
    var selecting = false;
    var dataTable = null;

    root.querySelectorAll("[data-ts]").forEach(function (el) {
        var ts = parseInt(el.getAttribute("data-ts"), 10);
        if (ts) el.textContent = new Date(ts * 1000).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
    });

    function setActive(attr, value) {
        root.querySelectorAll("button[" + attr + "]").forEach(function (b) {
            var on = b.getAttribute(attr) === value;
            b.classList.toggle("btn-light", on);
            b.classList.toggle("btn-outline-light", !on);
        });
    }

    // id -> tile and list-row checkbox, collected before DataTables can
    // detach any row
    var tilesById = {}, rowChecksById = {};
    grid.querySelectorAll(".fgs-tile").forEach(function (t) { tilesById[t.getAttribute("data-id")] = t; });
    list.querySelectorAll("tr[data-row-id]").forEach(function (tr) {
        rowChecksById[tr.getAttribute("data-row-id")] = tr.querySelector(".fgs-row-check");
    });
    function tileCheck(tile) { return tile.querySelector(".fgs-tile-check"); }

    function currentLayout() { return root.getAttribute("data-view") === "list" ? "list" : "grid"; }

    // The files the user can see, in the order they see them: the grid in
    // page order, the list in the table's current sort and filter. Both skip
    // what the My files / Everything switch hides, and files deleted since
    // the page loaded.
    function visibleTiles() {
        if (currentLayout() === "list" && dataTable) {
            return dataTable.rows({ order: "applied", search: "applied" }).nodes().toArray()
                .filter(function (tr) { return !tr.classList.contains("d-none"); })
                .map(function (tr) { return tilesById[tr.getAttribute("data-row-id")]; })
                .filter(function (t) { return t && t.isConnected; });
        }
        return Array.prototype.slice.call(grid.querySelectorAll(".fgs-tile:not(.d-none)"));
    }

    function selected() {
        return visibleTiles().filter(function (t) { return tileCheck(t).checked; });
    }

    function updateSelection() {
        var n = selected().length;
        bar.classList.toggle("d-none", !selecting);
        document.getElementById("fgs-selcount").textContent = n + " selected";
        // Greyed out rather than disabled: some actions disable their own
        // button while they run, and a selection change must not undo that
        bar.classList.toggle("fgs-none-selected", n === 0);
        // A prepared set belongs to the selection it was made for
        var photos = bar.querySelector("[data-act=photos]");
        if (photos && window.FGS && FGS.resetSaveToPhotos) FGS.resetSaveToPhotos(photos);
        // ...and so does a link made from it
        var linkResult = document.getElementById("fgs-linkresult");
        if (linkResult) { linkResult.textContent = ""; linkResult.className = ""; }
    }

    function setChecked(id, on) {
        var tile = tilesById[id];
        if (tile) tileCheck(tile).checked = on;
        if (rowChecksById[id]) rowChecksById[id].checked = on;
    }

    function toggleSelected(tile) {
        setChecked(tile.getAttribute("data-id"), !tileCheck(tile).checked);
        status.textContent = "";
        updateSelection();
    }

    function setSelecting(on) {
        selecting = on;
        root.classList.toggle("fgs-selecting", on);
        selectToggle.textContent = on ? "Done" : "Select";
        selectToggle.setAttribute("aria-pressed", on ? "true" : "false");
        selectToggle.classList.toggle("btn-light", on);
        selectToggle.classList.toggle("btn-outline-light", !on);
        // Leaving select mode drops the selection
        if (!on) Object.keys(tilesById).forEach(function (id) { setChecked(id, false); });
        status.textContent = "";
        updateSelection();
        // The checkbox column appeared or went away
        if (dataTable && currentLayout() === "list") dataTable.columns.adjust();
    }

    // The viewer, over everything visible, starting at this file
    function openAt(tile) {
        if (!tile || !window.FGS || !FGS.openViewer || !FGS.itemFromEl) return;
        var tiles = visibleTiles();
        FGS.openViewer(tiles.map(FGS.itemFromEl), Math.max(0, tiles.indexOf(tile)));
    }

    // One collection link for the selection, through the same endpoint the
    // upload page's automatic link uses. The server again keeps only files
    // this user may list, and refuses end-to-end encrypted ones.
    function createOneLink(tiles, button) {
        if (button.disabled) return;
        button.disabled = true; // each request makes a new collection
        var opts = { holderId: "fgs-linkresult", label: "One link for " + tiles.length + " file" + (tiles.length === 1 ? "" : "s") + ":" };
        fetch("./collections/create", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify({
                // The name heads the public board: a lone file is named after itself
                name: tiles.length === 1 ? tiles[0].getAttribute("data-name") : tiles.length + " files",
                fileIds: tiles.map(function (t) { return t.getAttribute("data-id"); })
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

    function applyLayout(layout) {
        if (layout !== "list") layout = "grid";
        root.setAttribute("data-view", layout);
        grid.classList.toggle("d-none", layout !== "grid");
        list.classList.toggle("d-none", layout !== "list");
        setActive("data-layout", layout);
        localStorage.setItem("filesLayout", layout);
        if (dataTable) {
            // The grid ignores the table's search box, so a search must not
            // keep rows (and the elements the upload page's helpers look up
            // by id) out of the DOM while the grid is showing
            if (layout === "grid" && dataTable.search() !== "") dataTable.search("").draw();
            // The table was laid out while hidden, so its column widths are wrong
            if (layout === "list") dataTable.columns.adjust();
        }
        updateSelection();
    }

    // Only users with PERM_LIST get the switch, and only they are ever sent
    // other users' files. Without the switch everything shown is their own.
    function applyScope(scope) {
        if (scope !== "all") scope = "mine";
        root.querySelectorAll("[data-mine]").forEach(function (el) {
            el.classList.toggle("d-none", scope === "mine" && el.getAttribute("data-mine") !== "true");
        });
        setActive("data-scope", scope);
        localStorage.setItem("filesScope", scope);
        updateSelection();
    }

    root.querySelectorAll("button[data-layout]").forEach(function (b) {
        b.addEventListener("click", function () { applyLayout(b.getAttribute("data-layout")); });
    });
    root.querySelectorAll("button[data-scope]").forEach(function (b) {
        b.addEventListener("click", function () { applyScope(b.getAttribute("data-scope")); });
    });
    selectToggle.addEventListener("click", function () { setSelecting(!selecting); });

    // Square crops every thumbnail to fill its tile; True shape shows the
    // whole picture, letterboxed, so orientation and aspect ratio show
    function applyShape(shape) {
        if (shape !== "true") shape = "square";
        grid.classList.toggle("fgs-shape-true", shape === "true");
        setActive("data-shape", shape);
        localStorage.setItem("filesTileShape", shape);
    }
    root.querySelectorAll("button[data-shape]").forEach(function (b) {
        b.addEventListener("click", function () { applyShape(b.getAttribute("data-shape")); });
    });
    applyShape(localStorage.getItem("filesTileShape") || "square");

    var size = document.getElementById("fgs-icon-size");
    size.value = localStorage.getItem("filesIconSize") || "160";
    function applySize() {
        grid.style.setProperty("--fgs-tile", size.value + "px");
        localStorage.setItem("filesIconSize", size.value);
    }
    size.addEventListener("input", applySize);
    applySize();

    function asItems(tiles) {
        return tiles.map(function (t) {
            return { url: t.getAttribute("data-download"), name: t.getAttribute("data-name"),
                bytes: parseInt(t.getAttribute("data-bytes"), 10) };
        });
    }

    function downloadZip(tiles) {
        window.location = "./files/zip?ids=" + tiles.map(function (t) {
            return encodeURIComponent(t.getAttribute("data-id"));
        }).join(",");
    }

    // A checkbox ticked directly, in either view
    root.addEventListener("change", function (e) {
        var check = e.target.closest(".fgs-tile-check, .fgs-row-check");
        if (!check) return;
        var tile = check.classList.contains("fgs-tile-check") ? check.closest(".fgs-tile")
            : tilesById[check.closest("tr").getAttribute("data-row-id")];
        if (tile) setChecked(tile.getAttribute("data-id"), check.checked);
        status.textContent = "";
        updateSelection();
    });
    bar.addEventListener("click", function (e) {
        var act = e.target.closest("[data-act]");
        if (!act) return;
        var tiles = selected();
        if (!tiles.length) return;
        var what = act.getAttribute("data-act");
        if (what === "zip") {
            downloadZip(tiles);
        } else if (what === "download") {
            // Only end-to-end encrypted files lack a link: they can only be
            // decrypted on their own download page, and the zip skips them
            // too, so fetch the rest and say what was left out
            var usable = tiles.filter(function (t) { return t.getAttribute("data-download"); });
            var skipped = tiles.length - usable.length;
            if (usable.length) FGS.downloadSequentially(asItems(usable), { button: act, status: status });
            if (skipped) {
                window.setTimeout(function () {
                    status.textContent = skipped + " end-to-end encrypted file(s) left out: open each from its own download link.";
                }, usable.length * 600 + 100); // after downloadSequentially's own last message
            }
        } else if (what === "link") {
            createOneLink(tiles, act);
        } else if (what === "photos" && FGS.saveToPhotos) {
            FGS.saveToPhotos(asItems(tiles), act);
        } else if (what === "add-to-collection") {
            openCollect(tiles);
        } else if (what === "change-expiry") {
            openExpiry(tiles);
        } else if (what === "delete") {
            bulkDelete(tiles, act);
        }
    });

    var help = bar.querySelector("[data-fgs-help]");
    if (window.FGS && FGS.isIOS()) {
        // Safari allows one download per tap, so the zip (and, once it exists,
        // Save to Photos) is the way to take several at once
        bar.querySelector("[data-act=download]").classList.add("d-none");
        if (FGS.saveToPhotos) bar.querySelector("[data-act=photos]").classList.remove("d-none");
        help.classList.remove("d-none");
    }
    help.addEventListener("click", function () { FGS.openHelpSheet(); });

    // A tile, or a name in the list: open it, or in select mode (de)select it
    grid.addEventListener("click", function (e) {
        var open = e.target.closest(".fgs-tile-open");
        if (!open) return;
        var tile = open.closest(".fgs-tile");
        if (selecting) toggleSelected(tile);
        else openAt(tile);
    });
    list.addEventListener("click", function (e) {
        var name = e.target.closest(".fgs-name-open");
        if (!name) return;
        var tile = tilesById[name.closest("tr").getAttribute("data-row-id")];
        if (!tile) return;
        if (selecting) toggleSelected(tile);
        else openAt(tile);
    });

    // The "⋯" menu, the same in both views. Each entry reuses the upload
    // page's helper behind the button it replaces.
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

    root.addEventListener("click", function (e) {
        var item = e.target.closest("[data-menu]");
        if (!item || item.disabled) return;
        var menu = item.closest("[data-fgs-file-menu]");
        var id = menu.getAttribute("data-id");
        var tile = tilesById[id];
        var url = menu.getAttribute("data-url");
        // Dialogs and the viewer hand focus back to the "⋯", not to an
        // entry of the menu that just closed
        menu.querySelector(".fgs-menu-btn").focus();
        switch (item.getAttribute("data-menu")) {
            case "open": openAt(tile); break;
            case "download": FGS.downloadSequentially(asItems([tile])); break;
            case "copy": showToast(1000); break; // ClipboardJS copies every .copyurl
            case "share":
                if (navigator.share) shareNativeUrl(id);
                else copyText(url);
                break;
            case "qr": showQrCode(url); break;
            case "edit": editFromMenu(menu, tile, false); break;
            case "replace": editFromMenu(menu, tile, true); break;
            case "add-to-collection": openCollect([tile]); break;
            case "delete": deleteFile(id); break;
        }
    });
    // Per-file data for the bulk actions, from the file's "⋯" menu in the
    // grid, which is always in the DOM
    function fileData(tile) {
        var menu = tile.querySelector("[data-fgs-file-menu]");
        var d = function (k) { return menu.getAttribute("data-" + k); };
        return { id: d("id"), name: tile.getAttribute("data-name"), e2e: d("e2e") === "true",
            canEdit: d("can-edit") === "true", canDelete: d("can-delete") === "true",
            downloads: parseInt(d("downloads"), 10) || 0, unlimitedDownloads: d("unlimited-downloads") === "true",
            expire: parseInt(d("expire"), 10) || 0, unlimitedTime: d("unlimited-time") === "true" };
    }
    function plural(n, word) { return n + " " + word + (n === 1 ? "" : "s"); }
    function errorText(error) { return error && error.message ? error.message : String(error); }

    // "Add to collection…": pick one of your collections or start a new one.
    // The server keeps only files this user may list; an end-to-end
    // encrypted file would make it refuse the whole request, so those are
    // left out here and the dialog says so.
    var collectModal = document.getElementById("fgs-collect-modal");
    var collectList = document.getElementById("fgs-collect-list");
    var collectName = document.getElementById("fgs-collect-name");
    var collectCreate = document.getElementById("fgs-collect-create");
    var collectResult = document.getElementById("fgs-collect-result");
    var collectIds = [];

    function collectError(message) {
        collectResult.className = "alert alert-danger mt-3";
        collectResult.textContent = message;
    }

    function collectRequest(endpoint, body, label, urlFromData) {
        collectResult.textContent = "";
        collectResult.className = "";
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
                    return;
                }
                showAutoLinkResult(urlFromData(data), null, { holderId: "fgs-collect-result", label: label });
            })
            .catch(function (error) { collectError("Could not add: " + errorText(error)); });
    }

    function renderCollections(collections) {
        collectList.textContent = "";
        if (!collections.length) {
            var none = document.createElement("div");
            none.className = "list-group-item text-muted";
            none.textContent = "No collections yet.";
            collectList.appendChild(none);
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
                if (!collectIds.length || b.disabled) return;
                b.disabled = true;
                collectRequest("./collections/modify", { id: c.id, addFileIds: collectIds },
                    "Added to \u201c" + (c.name || "Untitled collection") + "\u201d:",
                    function (data) { return collectModal.getAttribute("data-server-url") + "c?id=" + data.Id; })
                    .then(function () { b.disabled = false; });
            });
            collectList.appendChild(b);
        });
    }

    function openCollect(tiles) {
        var usable = tiles.filter(function (t) { return !fileData(t).e2e; });
        collectIds = usable.map(function (t) { return t.getAttribute("data-id"); });
        var what = plural(usable.length, "file");
        if (usable.length < tiles.length) {
            what += " (" + plural(tiles.length - usable.length, "end-to-end encrypted file") + " left out: they cannot be in a collection)";
        }
        document.getElementById("fgs-collect-what").textContent = what;
        collectResult.textContent = "";
        collectResult.className = "";
        collectName.value = usable.length === 1 ? usable[0].getAttribute("data-name") : usable.length + " files";
        collectCreate.disabled = !usable.length;
        collectList.textContent = "";
        var loading = document.createElement("div");
        loading.className = "list-group-item text-muted";
        loading.textContent = "Loading your collections…";
        collectList.appendChild(loading);
        fetch("./collections/list", { credentials: "same-origin" })
            .then(function (response) { return response.json(); })
            .then(function (data) { renderCollections(Array.isArray(data) ? data : []); })
            .catch(function (error) {
                collectList.textContent = "";
                collectError("Could not load your collections: " + errorText(error));
            });
        bootstrap.Modal.getOrCreateInstance(collectModal).show();
    }

    collectCreate.addEventListener("click", function () {
        var name = collectName.value.trim();
        if (!collectIds.length || collectCreate.disabled) return;
        collectCreate.disabled = true; // each request makes a new collection
        collectRequest("./collections/create", { name: name, fileIds: collectIds },
            "New collection" + (name ? " \u201c" + name + "\u201d" : "") + ":",
            function (data) { return data.Url; })
            .then(function () { collectCreate.disabled = false; });
    });

    // "Change expiry & limits": one /api/files/modify per file, in turn.
    // "Keep" sends the file's own current value, since the API always sets
    // both; the password is always kept.
    var expiryModal = document.getElementById("fgs-expiry-modal");
    var expiryApply = document.getElementById("fgs-expiry-apply");
    var expiryStatus = document.getElementById("fgs-expiry-status");
    var expiryFailures = document.getElementById("fgs-expiry-failures");
    var expiryDate = document.getElementById("fgs-expiry-date");
    var limitNumber = document.getElementById("fgs-limit-number");
    var expiryTiles = [], expiryChanged = false;

    function localInputValue(date) {
        var pad = function (n) { return (n < 10 ? "0" : "") + n; };
        return date.getFullYear() + "-" + pad(date.getMonth() + 1) + "-" + pad(date.getDate()) +
            "T" + pad(date.getHours()) + ":" + pad(date.getMinutes());
    }
    // Typing a date or a number picks its option
    expiryDate.addEventListener("input", function () { document.getElementById("fgs-expiry-on").checked = true; });
    limitNumber.addEventListener("input", function () { document.getElementById("fgs-limit-on").checked = true; });

    function openExpiry(tiles) {
        expiryTiles = tiles;
        expiryChanged = false;
        document.getElementById("fgs-expiry-keep").checked = true;
        document.getElementById("fgs-limit-keep").checked = true;
        expiryDate.value = localInputValue(new Date(Date.now() + 14 * 24 * 3600 * 1000));
        expiryDate.min = localInputValue(new Date());
        document.getElementById("fgs-expiry-what").textContent = plural(tiles.length, "file");
        expiryStatus.textContent = "";
        expiryFailures.textContent = "";
        expiryApply.disabled = false;
        bootstrap.Modal.getOrCreateInstance(expiryModal).show();
    }

    function failure(list, name, reason) {
        var li = document.createElement("li");
        li.textContent = name + ": " + reason;
        list.appendChild(li);
    }

    expiryApply.addEventListener("click", async function () {
        var expiryChoice = expiryModal.querySelector("input[name=fgs-expiry]:checked").value;
        var limitChoice = expiryModal.querySelector("input[name=fgs-limit]:checked").value;
        if (expiryChoice === "keep" && limitChoice === "keep") {
            expiryStatus.textContent = "Nothing to change: pick a new expiry or download limit.";
            return;
        }
        var expiryAt = 0, limit = 0;
        if (expiryChoice === "date") {
            var ms = new Date(expiryDate.value).getTime();
            if (!expiryDate.value || isNaN(ms) || ms <= Date.now()) {
                expiryStatus.textContent = "Pick an expiry date in the future.";
                return;
            }
            expiryAt = Math.floor(ms / 1000);
        }
        if (limitChoice === "number") {
            limit = parseInt(limitNumber.value, 10);
            if (!(limit >= 1)) {
                expiryStatus.textContent = "The download limit must be 1 or more.";
                return;
            }
        }
        expiryApply.disabled = true;
        expiryFailures.textContent = "";
        var failed = 0, total = expiryTiles.length;
        for (var i = 0; i < total; i++) {
            var f = fileData(expiryTiles[i]);
            expiryStatus.textContent = "Updating " + (i + 1) + " of " + total + "…";
            if (!f.canEdit) {
                failed++;
                failure(expiryFailures, f.name, "you may not edit this file");
                continue;
            }
            // 0 means unlimited / never to the API
            var downloads = limitChoice === "keep" ? (f.unlimitedDownloads ? 0 : f.downloads) : limit;
            var expiry = expiryChoice === "keep" ? (f.unlimitedTime ? 0 : f.expire) : expiryAt;
            try {
                await apiFilesModify(f.id, downloads, expiry, "", true);
                expiryChanged = true;
            } catch (error) {
                failed++;
                failure(expiryFailures, f.name, errorText(error));
            }
        }
        if (!failed) {
            expiryStatus.textContent = "Updated " + plural(total, "file") + ".";
            location.reload();
            return;
        }
        expiryStatus.textContent = (total - failed) + " of " + plural(total, "file") + " updated; " + failed + " failed:";
    });
    // After a partial run the failures stay readable until the dialog closes
    expiryModal.addEventListener("hidden.bs.modal", function () {
        if (expiryChanged) location.reload();
    });

    // Bulk delete: asks first, then deletes each file in turn, at once (no
    // undo, unlike the single delete) and reloads
    async function bulkDelete(tiles, button) {
        if (!confirm("Delete " + plural(tiles.length, "file") + "? This cannot be undone.")) return;
        button.disabled = true;
        var failures = [];
        for (var i = 0; i < tiles.length; i++) {
            var f = fileData(tiles[i]);
            status.textContent = "Deleting " + (i + 1) + " of " + tiles.length + "…";
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

    // A file deleted from its menu leaves the selection
    if ("MutationObserver" in window) new MutationObserver(updateSelection).observe(grid, { childList: true });

    if (window.jQuery && jQuery.fn.DataTable) {
        // No paging: the scope switch hides rows in the DOM, and the edit
        // dialog's "Replace content" list reads every row's name from it
        dataTable = jQuery("#fgs-filetable").DataTable({
            "order": [[3, "desc"]], // Uploaded; column 0 is the checkbox
            "columnDefs": [{ "targets": [0, 8], "orderable": false, "searchable": false }],
            "paging": false,
            // Laid out while the grid may be showing: measured widths would be wrong
            "autoWidth": false,
            "language": {
                "info": "Files: _TOTAL_",
                "emptyTable": "No files stored yet",
                "infoEmpty": "",
            }
        });
        // A search changes which selected files are visible
        dataTable.on("draw", updateSelection);
    }
    applyLayout(localStorage.getItem("filesLayout") || "grid");
    applyScope(localStorage.getItem("filesScope") || "mine");
}
