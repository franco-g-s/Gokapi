// The owner bar of a collection's public page (html_collection.tmpl): Rename,
// Set / Change / Remove password and Turn into a group. The page loads this
// only for the collection's owner or an admin; the public page does not load
// the admin bundle, so this stands alone. Every request is a POST with a JSON
// body (I1), and names are put into the page with textContent only.
(function () {
    "use strict";

    var bar = document.querySelector("[data-fgs-owner-bar]");
    if (!bar) return;
    var id = bar.getAttribute("data-id");
    var status = bar.querySelector("[data-owner-status]");

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

    // Resolves to the parsed answer, which is OK or carries an ErrorMessage
    function postJson(url, payload) {
        return fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(payload)
        }).then(readJson).then(function (data) {
            if (!data || data.Result !== "OK") throw new Error(data && data.ErrorMessage || "unknown error");
            return data;
        });
    }

    function say(kind, text) {
        status.className = "text-center small mt-2" + (kind === "error" ? " text-danger" : " text-muted");
        status.textContent = text;
    }

    // --- Rename and password: inline forms, one open at a time ----------
    var forms = {};
    bar.querySelectorAll("[data-owner-form]").forEach(function (form) {
        forms[form.getAttribute("data-owner-form")] = form;
    });

    function closeForms() {
        Object.keys(forms).forEach(function (key) { forms[key].classList.add("d-none"); });
    }

    function openForm(key, value) {
        closeForms();
        say("", "");
        var form = forms[key];
        var input = form.querySelector("input");
        input.value = value;
        form.classList.remove("d-none");
        input.focus();
        input.select();
    }

    bar.querySelectorAll("[data-owner-cancel]").forEach(function (button) {
        button.addEventListener("click", function () { closeForms(); say("", ""); });
    });

    function busy(form, on) {
        form.querySelectorAll("input, button").forEach(function (el) { el.disabled = on; });
    }

    forms.rename.addEventListener("submit", function (event) {
        event.preventDefault();
        var form = forms.rename;
        var name = form.querySelector("input").value.trim();
        if (name === "") {
            say("error", "A collection needs a name");
            return;
        }
        busy(form, true);
        postJson("./collections/modify", { id: id, name: name })
            .then(function () {
                bar.setAttribute("data-name", name);
                // The page's one card title (dialogs use other headings)
                var title = document.querySelector("h3.card-title");
                if (title) title.textContent = name;
                closeForms();
                say("", "Renamed.");
            })
            .catch(function (error) { say("error", "Could not rename: " + errorText(error)); })
            .then(function () { busy(form, false); });
    });

    // A changed or removed password revokes every unlock others hold; the
    // owner and an admin pass the gate without one, so reloading shows the
    // page again with the bar's new choices
    function setPassword(value, form) {
        if (form) busy(form, true);
        postJson("./collections/modify", { id: id, password: value })
            .then(function () { window.location.reload(); })
            .catch(function (error) {
                say("error", "Could not update the password: " + errorText(error));
                if (form) busy(form, false);
            });
    }

    forms.password.addEventListener("submit", function (event) {
        event.preventDefault();
        var value = forms.password.querySelector("input").value.trim();
        if (value === "") {
            say("error", "Type a password, or use Remove password");
            return;
        }
        setPassword(value, forms.password);
    });

    // --- Turn into a group: the Collections tab's dialog ----------------
    // The collection and its link stay as they are; its password does not
    // carry over, because group members sign in instead (D6).
    var turnModal = document.getElementById("togroup-modal");
    var turnWhat = document.getElementById("togroup-what");
    var turnConfirm = document.getElementById("togroup-confirm");
    var turnStatus = document.getElementById("togroup-status");

    function openTurnIntoGroup() {
        if (!turnModal) return;
        turnWhat.textContent = "“" + bar.getAttribute("data-name") + "” becomes a group with the same files. " +
            "The collection and its link stay as they are.";
        document.getElementById("togroup-days-60").checked = true;
        turnStatus.textContent = "";
        turnStatus.className = "";
        turnConfirm.disabled = false;
        bootstrap.Modal.getOrCreateInstance(turnModal).show();
    }

    if (turnConfirm) {
        turnConfirm.addEventListener("click", function () {
            if (turnConfirm.disabled) return;
            var chosen = turnModal.querySelector("input[name=togroup-days]:checked");
            if (!chosen) return;
            turnConfirm.disabled = true; // each request makes a new group
            turnStatus.className = "text-muted small mt-3";
            turnStatus.textContent = "Turning into a group…";
            postJson("./groups/fromcollection", { collectionId: id, days: parseInt(chosen.value, 10) })
                .then(function (data) { window.location.href = data.Url; })
                .catch(function (error) {
                    turnStatus.className = "alert alert-danger mt-3";
                    turnStatus.textContent = "Could not turn into a group: " + errorText(error);
                    turnConfirm.disabled = false;
                });
        });
    }

    // --- The bar's buttons ----------------------------------------------
    bar.querySelectorAll("[data-owner-act]").forEach(function (button) {
        button.addEventListener("click", function () {
            switch (button.getAttribute("data-owner-act")) {
                case "rename":
                    openForm("rename", bar.getAttribute("data-name"));
                    break;
                case "password":
                    openForm("password", "");
                    break;
                case "nopassword":
                    closeForms();
                    if (!window.confirm("Remove the password? Anyone with the link can then open this collection.")) return;
                    say("", "");
                    setPassword("", null);
                    break;
                case "togroup":
                    closeForms();
                    openTurnIntoGroup();
                    break;
            }
        });
    });
})();
