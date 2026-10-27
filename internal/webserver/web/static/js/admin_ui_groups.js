// Groups tab (Part 2, G6): the "Create group" and "Join with a code" dialogs.
// The admin bundle runs on every admin page, so this does nothing unless the
// Groups tab is open. Error text is set with textContent only.
(function () {
    "use strict";

    // Storage errors are lowercase sentences; a dialog shows them capitalised
    function sentence(message) {
        if (!message) return "Something went wrong";
        return message.charAt(0).toUpperCase() + message.slice(1);
    }

    function postJson(url, body) {
        return fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(body)
        }).then(function (response) {
            return response.json().catch(function () {
                return { Result: "error", ErrorMessage: "the server answered " + response.status };
            });
        });
    }

    // wireDialog submits a dialog's form, shows errors inline and opens the
    // group's page on success
    function wireDialog(formId, errorId, submitId, buildRequest) {
        var form = document.getElementById(formId);
        var error = document.getElementById(errorId);
        var submit = document.getElementById(submitId);
        if (!form || !error || !submit) return;
        var modal = form.closest(".modal");
        if (modal) {
            modal.addEventListener("shown.bs.modal", function () {
                error.textContent = "";
                var first = form.querySelector("input[type=text]");
                if (first) first.focus();
            });
        }
        form.addEventListener("submit", function (event) {
            event.preventDefault();
            var request = buildRequest();
            if (request.error) {
                error.textContent = request.error;
                return;
            }
            error.textContent = "";
            submit.disabled = true;
            postJson(request.url, request.body)
                .then(function (data) {
                    if (data.Result !== "OK") {
                        error.textContent = sentence(data.ErrorMessage);
                        submit.disabled = false;
                        return;
                    }
                    window.location = data.Url || ("./g?id=" + encodeURIComponent(data.Id));
                })
                .catch(function () {
                    error.textContent = "Could not reach the server";
                    submit.disabled = false;
                });
        });
    }

    function init() {
        if (!document.getElementById("groupsPage")) return;

        wireDialog("groupCreateForm", "groupCreateError", "groupCreateSubmit", function () {
            var name = document.getElementById("groupCreateName").value.trim();
            if (name === "") return { error: "A group needs a name" };
            var choice = document.querySelector("input[name=groupCreateDays]:checked");
            if (!choice) return { error: "Pick when the group closes" };
            return { url: "./groups/create", body: { name: name, days: parseInt(choice.value, 10) } };
        });

        wireDialog("groupJoinForm", "groupJoinError", "groupJoinSubmit", function () {
            var code = document.getElementById("groupJoinCode").value.trim();
            if (code === "") return { error: "Type the code you were given" };
            return { url: "./groups/join", body: { code: code } };
        });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
