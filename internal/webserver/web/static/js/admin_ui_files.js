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
        bar.querySelectorAll("[data-act]").forEach(function (b) { b.disabled = n === 0; });
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
            case "delete": deleteFile(id); break;
        }
    });
    // A file deleted from its menu leaves the selection
    if ("MutationObserver" in window) new MutationObserver(updateSelection).observe(grid, { childList: true });

    if (window.jQuery && jQuery.fn.DataTable) {
        // No paging: the scope switch hides rows in the DOM, and the edit
        // dialog's "Replace content" list reads every row's name from it
        dataTable = jQuery("#fgs-filetable").DataTable({
            "order": [[3, "desc"]], // Uploaded; column 0 is the checkbox
            "columnDefs": [{ "targets": [0, 8], "orderable": false, "searchable": false }],
            "paging": false,
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
