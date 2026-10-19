// This file contains JS code for the Files tab
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...

// Files tab: grid/list, My files / Everything, icon size, multi-select.
// Remembers choices in localStorage, like the upload page's defaults.
function initFilesPage() {
    var root = document.getElementById("fgs-files");
    if (!root) return;
    var grid = document.getElementById("fgs-grid");
    var list = document.getElementById("fgs-list");
    var bar = document.getElementById("fgs-selectbar");

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

    function selected() {
        return Array.prototype.slice.call(grid.querySelectorAll(".fgs-tile-check:checked"))
            .map(function (c) { return c.closest(".fgs-tile"); })
            .filter(function (t) { return !t.classList.contains("d-none"); });
    }

    function updateSelection() {
        var n = selected().length;
        bar.classList.toggle("d-none", n === 0);
        document.getElementById("fgs-selcount").textContent = n + " selected";
    }

    function applyLayout(layout) {
        if (layout !== "list") layout = "grid";
        root.setAttribute("data-view", layout);
        grid.classList.toggle("d-none", layout !== "grid");
        list.classList.toggle("d-none", layout !== "list");
        setActive("data-layout", layout);
        localStorage.setItem("filesLayout", layout);
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
            return { url: t.getAttribute("data-download"), name: t.getAttribute("data-name") };
        });
    }

    grid.addEventListener("change", updateSelection);
    bar.addEventListener("click", function (e) {
        var act = e.target.closest("[data-act]");
        if (!act) return;
        var tiles = selected();
        if (!tiles.length) return;
        var what = act.getAttribute("data-act");
        if (what === "zip") {
            window.location = "./files/zip?ids=" + tiles.map(function (t) {
                return encodeURIComponent(t.getAttribute("data-id"));
            }).join(",");
        } else if (what === "download") {
            FGS.downloadSequentially(asItems(tiles));
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

    grid.addEventListener("click", function (e) {
        var open = e.target.closest(".fgs-tile-open");
        if (!open || !window.FGS || !FGS.openViewer || !FGS.itemFromEl) return;
        var tiles = Array.prototype.slice.call(grid.querySelectorAll(".fgs-tile:not(.d-none)"));
        FGS.openViewer(tiles.map(FGS.itemFromEl), tiles.indexOf(open.closest(".fgs-tile")));
    });

    if (window.jQuery && jQuery.fn.DataTable) {
        // No paging: the scope switch hides rows in the DOM, and the edit
        // dialog's "Replace content" list reads every row's name from it
        jQuery("#fgs-filetable").DataTable({
            "order": [[2, "desc"]],
            "columnDefs": [{ "targets": 7, "orderable": false }],
            "paging": false,
            "language": {
                "info": "Files: _TOTAL_",
                "emptyTable": "No files stored yet",
                "infoEmpty": "",
            }
        });
    }
    applyLayout(localStorage.getItem("filesLayout") || "grid");
    applyScope(localStorage.getItem("filesScope") || "mine");
}
