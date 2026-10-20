// "Add to group…" on the shared file list (html_file_browser.tmpl,
// #fgs-group-modal) and "Turn into a group" on the Collections tab.
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...
// Every name, group and person is put into the page with textContent, never
// innerHTML.
(function () {
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

    // POST a JSON body. Resolves to the parsed answer whatever the status
    // (the server's errors carry an ErrorMessage); rejects only when there
    // was no usable answer at all.
    function postJson(url, payload) {
        return fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(payload)
        }).then(readJson);
    }

    function unique(list) {
        return list.filter(function (item, i) { return item && list.indexOf(item) === i; });
    }

    // --- Add to group… ------------------------------------------------------
    // Pick one of your groups (/groups/list: the caller's own, even for the
    // admin). The server keeps only files this user may add; an end-to-end
    // encrypted file cannot be in a group, so those are left out here and the
    // dialog says so.
    var dialog = null;
    function groupDialog() {
        if (dialog) return dialog;
        var modal = document.getElementById("fgs-group-modal");
        if (!modal) return null;
        dialog = {
            modal: modal,
            what: document.getElementById("fgs-group-what"),
            list: document.getElementById("fgs-group-list"),
            result: document.getElementById("fgs-group-result"),
            ids: [],
            loads: 0
        };
        return dialog;
    }

    function isEncrypted(tile) {
        var menu = tile.querySelector("[data-fgs-file-menu]");
        return !!menu && menu.getAttribute("data-e2e") === "true";
    }

    function showResult(kind, lines) {
        dialog.result.textContent = "";
        dialog.result.className = "alert alert-" + kind + " mt-3";
        lines.forEach(function (line) {
            var p = document.createElement("div");
            p.textContent = line;
            dialog.result.appendChild(p);
        });
    }

    function addedText(group, data) {
        var lines = ["Added " + data.Added + " to “" + group.name + "”."];
        var duplicates = data.Duplicates || [];
        if (duplicates.length) {
            var by = unique(duplicates.map(function (d) { return d.Uploader; }));
            lines.push(duplicates.length + " already there" + (by.length ? " (uploaded by " + by.join(", ") + ")" : "") + ".");
        }
        var refused = data.Refused || [];
        if (refused.length) {
            lines.push(refused.length + " left out: " +
                unique(refused.map(function (r) { return r.Reason; })).join(", ") + ".");
        }
        return lines;
    }

    function addTo(group, buttons) {
        buttons.forEach(function (b) { b.disabled = true; });
        dialog.result.textContent = "";
        dialog.result.className = "";
        postJson("./groups/add", { id: group.id, fileIds: dialog.ids })
            .then(function (data) {
                if (data.Result !== "OK") throw new Error(data.ErrorMessage || "unknown error");
                showResult(data.Added > 0 ? "success" : "warning", addedText(group, data));
            })
            .catch(function (error) { showResult("danger", ["Could not add: " + errorText(error)]); })
            .then(function () { buttons.forEach(function (b) { b.disabled = false; }); });
    }

    function renderGroups(groups) {
        dialog.list.textContent = "";
        if (!groups.length) {
            var none = document.createElement("div");
            none.className = "list-group-item text-muted";
            none.textContent = "You are not in any group yet.";
            dialog.list.appendChild(none);
            return;
        }
        var buttons = [];
        groups.forEach(function (g) {
            var b = document.createElement("button");
            b.type = "button";
            b.className = "list-group-item list-group-item-action d-flex justify-content-between align-items-center gap-2";
            b.disabled = !dialog.ids.length;
            var name = document.createElement("span");
            name.textContent = g.name || "Untitled group";
            var count = document.createElement("span");
            count.className = "text-muted small text-nowrap";
            count.textContent = plural(g.files, "file");
            b.appendChild(name);
            b.appendChild(count);
            b.addEventListener("click", function () {
                if (!dialog.ids.length || b.disabled) return;
                addTo(g, buttons);
            });
            buttons.push(b);
            dialog.list.appendChild(b);
        });
    }

    function openAddToGroup(a) {
        if (!groupDialog()) return;
        var tiles = a.tiles || [];
        var usable = tiles.filter(function (t) { return !isEncrypted(t); });
        dialog.ids = usable.map(function (t) { return t.getAttribute("data-id"); });
        var what = plural(usable.length, "file");
        if (usable.length < tiles.length) {
            what += " (" + plural(tiles.length - usable.length, "end-to-end encrypted file") +
                " left out: they cannot be in a group)";
        }
        dialog.what.textContent = what;
        dialog.result.textContent = "";
        dialog.result.className = "";
        dialog.list.textContent = "";
        var loading = document.createElement("div");
        loading.className = "list-group-item text-muted";
        loading.textContent = "Loading your groups…";
        dialog.list.appendChild(loading);
        // Only the latest open may fill the list: a slow answer from an
        // earlier open would otherwise replace it with stale buttons
        var load = ++dialog.loads;
        fetch("./groups/list", { credentials: "same-origin" })
            .then(readJson)
            .then(function (data) {
                if (load !== dialog.loads) return;
                if (!Array.isArray(data)) throw new Error(data && data.ErrorMessage || "unknown error");
                renderGroups(data);
            })
            .catch(function (error) {
                if (load !== dialog.loads) return;
                dialog.list.textContent = "";
                showResult("danger", ["Could not load your groups: " + errorText(error)]);
            });
        bootstrap.Modal.getOrCreateInstance(dialog.modal).show();
    }

    if (window.FGS && FGS.registerFileActions) {
        FGS.registerFileActions({ "add-to-group": openAddToGroup });
    }

    // --- Turn into a group (Collections tab) --------------------------------
    // The collection and its link stay as they are; its password does not
    // carry over, because group members sign in instead (D6).
    var turnModal = document.getElementById("togroup-modal");
    if (!turnModal) return;
    var turnWhat = document.getElementById("togroup-what");
    var turnConfirm = document.getElementById("togroup-confirm");
    var turnStatus = document.getElementById("togroup-status");
    var turnId = null;

    document.querySelectorAll(".collection-togroup").forEach(function (button) {
        button.addEventListener("click", function () {
            turnId = button.getAttribute("data-id");
            turnWhat.textContent = "“" + button.getAttribute("data-name") + "” becomes a group with the same files. " +
                "The collection and its link stay as they are.";
            document.getElementById("togroup-days-60").checked = true;
            turnStatus.textContent = "";
            turnStatus.className = "";
            turnConfirm.disabled = false;
            bootstrap.Modal.getOrCreateInstance(turnModal).show();
        });
    });

    turnConfirm.addEventListener("click", function () {
        if (!turnId || turnConfirm.disabled) return;
        var chosen = turnModal.querySelector("input[name=togroup-days]:checked");
        if (!chosen) return;
        turnConfirm.disabled = true; // each request makes a new group
        turnStatus.className = "text-muted small mt-3";
        turnStatus.textContent = "Turning into a group…";
        postJson("./groups/fromcollection", { collectionId: turnId, days: parseInt(chosen.value, 10) })
            .then(function (data) {
                if (data.Result !== "OK") throw new Error(data.ErrorMessage || "unknown error");
                window.location.href = data.Url;
            })
            .catch(function (error) {
                turnStatus.className = "alert alert-danger mt-3";
                turnStatus.textContent = "Could not turn into a group: " + errorText(error);
                turnConfirm.disabled = false;
            });
    });
})();
