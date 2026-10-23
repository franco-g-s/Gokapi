// Settings page (Part 4, S1): saves the display name and the two switches as
// JSON. The admin bundle runs on every admin page, so this does nothing
// unless the Settings page is open. Text is set with textContent only: a
// display name is user input.
(function () {
    "use strict";

    function init() {
        var page = document.getElementById("settingsPage");
        var form = document.getElementById("settingsForm");
        var save = document.getElementById("settingsSave");
        if (!page || !form || !save || page.getAttribute("data-readonly") === "true") return;
        var nameInput = document.getElementById("settingsDisplayName");
        var useInitials = document.getElementById("settingsUseInitials");
        var emailDownloads = document.getElementById("settingsEmailDownloads");
        var shownName = document.getElementById("settingsShownName");
        var nameError = document.getElementById("settingsNameError");
        var error = document.getElementById("settingsError");

        form.addEventListener("submit", function (event) {
            event.preventDefault();
            var body = {
                displayName: nameInput.value,
                useInitials: useInitials.checked
            };
            if (!emailDownloads.disabled) body.emailDownloads = emailDownloads.checked;
            nameError.textContent = "";
            error.textContent = "";
            save.disabled = true;
            fetch("./settings/save", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                credentials: "same-origin",
                body: JSON.stringify(body)
            }).then(function (response) {
                return response.json().catch(function () {
                    return { Result: "error", ErrorMessage: "The server answered " + response.status };
                });
            }).then(function (data) {
                save.disabled = false;
                if (data.Result !== "OK") {
                    var target = /display name/i.test(data.ErrorMessage || "") ? nameError : error;
                    target.textContent = data.ErrorMessage || "Something went wrong";
                    return;
                }
                nameInput.value = nameInput.value.trim();
                if (shownName && typeof data.Name === "string") shownName.textContent = data.Name;
                showToast(1500, "Saved");
            }).catch(function () {
                save.disabled = false;
                error.textContent = "Could not reach the server";
            });
        });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
