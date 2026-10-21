// "Turn into a group": the dialog (template collection_togroup_modal) shared by
// the Collections tab and the owner bar of a collection's own page. Both pages
// load this file; neither bundle has it. Collections-tab rows open it from
// their .collection-togroup button (data-id, data-name), and the owner bar
// calls FGSTurnIntoGroup.open(id, name).
//
// Before the dialog opens it asks who opened the link while signed in, so it
// can offer to add them. The collection then leaves the Collections list and
// its link lives on in the chosen mode (Part 2, R4). Every request is a POST
// with a JSON body, and names reach the page through textContent only.
(function () {
    "use strict";

    var modal = document.getElementById("togroup-modal");
    if (!modal) return;
    var what = document.getElementById("togroup-what");
    var viewersBox = document.getElementById("togroup-viewers");
    var viewersCheck = document.getElementById("togroup-add-viewers");
    var viewerNames = document.getElementById("togroup-viewer-names");
    var confirmButton = document.getElementById("togroup-confirm");
    var status = document.getElementById("togroup-status");
    var collectionId = null;
    var viewerIds = [];
    var opens = 0;

    function errorText(error) { return error && error.message ? error.message : String(error); }

    // A JSON answer that is OK, or a clear error: an expired session answers
    // with the login page (a redirect), which would otherwise surface as a
    // parse error
    function readJson(response) {
        return response.text().then(function (text) {
            var data;
            try {
                data = JSON.parse(text);
            } catch (ignored) {
                throw new Error(response.redirected || text.trim().charAt(0) === "<" ?
                    "Your session expired — reload the page" : "Unexpected answer from the server");
            }
            if (!data || (data.Result === "error")) throw new Error(data && data.ErrorMessage || "unknown error");
            return data;
        });
    }

    function postJson(url, payload) {
        return fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(payload)
        }).then(readJson);
    }

    function setStatus(kind, text) {
        status.className = kind === "error" ? "alert alert-danger mt-3" : (text ? "text-muted small mt-3" : "");
        status.textContent = text;
    }

    function showViewers(viewers) {
        viewerIds = viewers.map(function (v) { return v.id; });
        viewerNames.textContent = viewers.map(function (v) { return v.name; }).join(", ");
        viewersCheck.checked = true;
        viewersBox.classList.toggle("d-none", viewers.length === 0);
    }

    // Opens the dialog for a collection once its viewers are known
    function open(id, name) {
        collectionId = id;
        var load = ++opens;
        what.textContent = "“" + name + "” becomes a group with the same files. " +
            "Its link keeps working, and the collection leaves your Collections list.";
        document.getElementById("togroup-days-60").checked = true;
        document.getElementById("togroup-link-invite").checked = true;
        viewersBox.classList.add("d-none");
        viewerNames.textContent = "";
        viewerIds = [];
        setStatus("", "");
        confirmButton.disabled = true;
        fetch("./groups/fromcollection/preview?collectionId=" + encodeURIComponent(id), { credentials: "same-origin" })
            .then(readJson)
            .then(function (data) {
                if (load !== opens) return;
                showViewers(Array.isArray(data.viewers) ? data.viewers : []);
                confirmButton.disabled = false;
            })
            .catch(function (error) {
                if (load !== opens) return;
                // Without the list the default ("add them") cannot be shown,
                // so nothing is sent until the dialog is opened again
                setStatus("error", "Could not load who opened the link: " + errorText(error));
            })
            .then(function () {
                if (load === opens) bootstrap.Modal.getOrCreateInstance(modal).show();
            });
    }

    confirmButton.addEventListener("click", function () {
        if (!collectionId || confirmButton.disabled) return;
        var days = modal.querySelector("input[name=togroup-days]:checked");
        var mode = modal.querySelector("input[name=togroup-link]:checked");
        if (!days || !mode) return;
        confirmButton.disabled = true; // each request makes a new group
        setStatus("", "Turning into a group…");
        postJson("./groups/fromcollection", {
            collectionId: collectionId,
            days: parseInt(days.value, 10),
            addViewers: !viewersBox.classList.contains("d-none") && viewersCheck.checked,
            // Only the people listed: someone opening the link meanwhile is not added unseen
            viewerIds: viewerIds,
            linkMode: mode.value
        })
            .then(function (data) { window.location.href = data.Url; })
            .catch(function (error) {
                setStatus("error", "Could not turn into a group: " + errorText(error));
                confirmButton.disabled = false;
            });
    });

    document.querySelectorAll(".collection-togroup").forEach(function (button) {
        button.addEventListener("click", function () {
            open(button.getAttribute("data-id"), button.getAttribute("data-name"));
        });
    });

    window.FGSTurnIntoGroup = { open: open };
})();
