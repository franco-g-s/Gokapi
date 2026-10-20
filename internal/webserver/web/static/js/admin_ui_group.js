// The group page (Part 2, G10): "Remove from group" on the board, the
// header controls (Rename, Extend, Reset code, Remove member, Delete group,
// Leave group), the near-copy entries and the copy buttons. Merged into the
// admin bundle with every other admin_*.js file, so everything is inside
// this function and does nothing unless #groupPage is on the page. Names
// only ever reach the DOM through textContent and attribute setters.
(function () {
    "use strict";

    // Storage errors are lowercase sentences; the page shows them capitalised
    function sentence(message) {
        if (!message) return "Something went wrong";
        return message.charAt(0).toUpperCase() + message.slice(1);
    }

    // POSTs JSON and resolves the answer, or rejects with a readable error.
    // An expired session answers with the login page, not JSON.
    function postJson(url, body) {
        return fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(body)
        }).then(function (response) {
            return response.text().then(function (text) {
                var data;
                try {
                    data = JSON.parse(text);
                } catch (ignored) {
                    throw new Error(response.redirected || text.trim().charAt(0) === "<" ?
                        "Your session expired — reload the page" : "The server answered " + response.status);
                }
                if (!data || data.Result !== "OK") {
                    throw new Error(sentence(data && data.ErrorMessage));
                }
                return data;
            });
        });
    }

    function errorText(error) {
        return error && error.message ? error.message : String(error);
    }

    function groupBrowser() {
        var root = document.querySelector('[data-fgs-browser][data-context="group"]');
        if (root === null || !window.FGS || !FGS.initFileBrowser) return null;
        return FGS.initFileBrowser(root);
    }

    function byAttr(name, value) {
        var escaped = window.CSS && CSS.escape ? CSS.escape(value) : String(value).replace(/[^a-zA-Z0-9_-]/g, "");
        return document.querySelectorAll("[" + name + '="' + escaped + '"]');
    }

    // A file that left the group takes its near-copy entries with it, on
    // either side of the pair
    function dropNearCopies(ids) {
        ids.forEach(function (id) {
            ["data-near-file", "data-near-match"].forEach(function (attr) {
                Array.prototype.forEach.call(byAttr(attr, id), function (entry) {
                    entry.parentNode.removeChild(entry);
                });
            });
        });
        var list = document.getElementById("groupNearCopies");
        if (list !== null && list.children.length === 0) list.parentNode.removeChild(list);
    }

    // --- Remove from group (board menu and selection bar) -----------------
    // Only tiles whose menu says data-can-remove="true" are sent (D4); the
    // server skips anything else as well.
    function canRemove(tile) {
        var menu = tile.querySelector("[data-fgs-file-menu]");
        return menu !== null && menu.getAttribute("data-can-remove") === "true";
    }

    function removeFromGroup(a) {
        var root = a.root, groupId = root.getAttribute("data-group-id");
        if (!groupId || root.getAttribute("data-fgs-removing") === "true") return;
        var ids = [], skipped = 0;
        a.tiles.forEach(function (tile) {
            if (canRemove(tile)) ids.push(tile.getAttribute("data-id"));
            else skipped++;
        });
        var skippedText = skipped ? skipped + " skipped: not yours." : "";
        if (ids.length === 0) {
            a.status.textContent = skippedText;
            return;
        }
        root.setAttribute("data-fgs-removing", "true");
        if (a.button) a.button.disabled = true;
        a.status.textContent = "Removing…";
        postJson("./groups/removefiles", { id: groupId, fileIds: ids })
            .then(function (data) {
                ids.forEach(function (id) { a.browser.removeItem(id); });
                dropNearCopies(ids);
                var removed = typeof data.Removed === "number" ? data.Removed : ids.length;
                a.status.textContent = "Removed " + removed + " from the group." + (skippedText ? " " + skippedText : "");
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

    if (window.FGS && FGS.registerFileActions) {
        FGS.registerFileActions({ "remove-from-group": removeFromGroup }, "group");
    }

    // --- The page ----------------------------------------------------------

    function init() {
        var page = document.getElementById("groupPage");
        if (page === null) return;
        var groupId = page.getAttribute("data-group-id");
        var status = document.getElementById("groupStatus");

        function say(text) {
            if (status !== null) status.textContent = text;
        }

        function failed(what) {
            return function (error) {
                say("");
                alert(what + ": " + errorText(error));
            };
        }

        function groupName() {
            return page.getAttribute("data-group-name") || "";
        }

        // ClipboardJS copies every .copyurl; this only confirms it
        ["groupInviteCopy", "groupCodeCopy"].forEach(function (id) {
            var button = document.getElementById(id);
            if (button === null) return;
            button.addEventListener("click", function () {
                if (typeof showToast === "function") showToast(1000);
            });
        });

        // "from 14:32" in the viewer's own time zone
        Array.prototype.forEach.call(page.querySelectorAll("[data-fgs-hm]"), function (el) {
            var ts = parseInt(el.getAttribute("data-fgs-hm"), 10);
            if (!ts) return;
            var d = new Date(ts * 1000);
            el.textContent = String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
        });

        // Wires a dialog's form: errors inline, the modal closed on success
        function wireDialog(formId, errorId, submitId, send) {
            var form = document.getElementById(formId);
            var error = document.getElementById(errorId);
            var submit = document.getElementById(submitId);
            if (form === null || error === null || submit === null) return;
            var modalEl = form.closest(".modal");
            if (modalEl !== null) {
                modalEl.addEventListener("shown.bs.modal", function () {
                    error.textContent = "";
                    var first = form.querySelector("input[type=text]");
                    if (first !== null) first.focus();
                });
            }
            form.addEventListener("submit", function (event) {
                event.preventDefault();
                var request = send();
                if (request.error) {
                    error.textContent = request.error;
                    return;
                }
                error.textContent = "";
                submit.disabled = true;
                request.promise
                    .then(function () {
                        if (modalEl !== null && window.bootstrap) bootstrap.Modal.getOrCreateInstance(modalEl).hide();
                    })
                    .catch(function (err) { error.textContent = errorText(err); })
                    .then(function () { submit.disabled = false; });
            });
        }

        // Rename: the title, and the edit dialog's lock text on the files
        // whose terms follow this group (D4)
        wireDialog("groupRenameForm", "groupRenameError", "groupRenameSubmit", function () {
            var input = document.getElementById("groupRenameName");
            var name = input.value.trim();
            if (name === "") return { error: "A group needs a name" };
            var old = groupName();
            return {
                promise: postJson("./groups/rename", { id: groupId, name: name }).then(function (data) {
                    var renamed = data.Name || name;
                    page.setAttribute("data-group-name", renamed);
                    document.getElementById("groupName").textContent = renamed;
                    input.value = renamed;
                    var root = document.querySelector('[data-fgs-browser][data-context="group"]');
                    if (root !== null) {
                        Array.prototype.forEach.call(root.querySelectorAll("[data-group-lock]"), function (menu) {
                            if (menu.getAttribute("data-group-lock") === old) menu.setAttribute("data-group-lock", renamed);
                        });
                    }
                })
            };
        });

        // Extend: a choice is offered only if it closes later than now (D18)
        function refreshExtendChoices() {
            var expires = parseInt(page.getAttribute("data-expires"), 10) || 0;
            var now = Math.floor(Date.now() / 1000);
            Array.prototype.forEach.call(document.querySelectorAll("input[name=groupExtendDays]"), function (radio) {
                var days = parseInt(radio.getAttribute("data-days"), 10) || 0;
                radio.disabled = expires === 0 || (days !== 0 && now + days * 24 * 3600 <= expires);
                if (radio.disabled) radio.checked = false;
            });
        }
        var extendModal = document.getElementById("groupExtendModal");
        if (extendModal !== null) extendModal.addEventListener("show.bs.modal", refreshExtendChoices);
        wireDialog("groupExtendForm", "groupExtendError", "groupExtendSubmit", function () {
            var choice = document.querySelector("input[name=groupExtendDays]:checked");
            if (choice === null) return { error: "Pick when the group closes" };
            return {
                promise: postJson("./groups/extend", { id: groupId, days: parseInt(choice.value, 10) }).then(function (data) {
                    page.setAttribute("data-expires", data.NeverExpires ? "0" : String(data.Expires || 0));
                    if (data.Closes) document.getElementById("groupCloses").textContent = data.Closes;
                    choice.checked = false;
                    refreshExtendChoices();
                })
            };
        });

        // Reset code: the old code and link stop working
        var reset = document.getElementById("groupResetCode");
        if (reset !== null) {
            reset.addEventListener("click", function () {
                if (!confirm("Reset the code? The current code and invite link stop working.")) return;
                reset.disabled = true;
                postJson("./groups/resetcode", { id: groupId })
                    .then(function (data) {
                        document.getElementById("groupInviteUrl").value = data.InviteUrl;
                        document.getElementById("groupInviteCopy").setAttribute("data-clipboard-text", data.InviteUrl);
                        document.getElementById("groupCode").textContent = data.Code;
                        document.getElementById("groupCodeCopy").setAttribute("data-clipboard-text", data.Code);
                        say("New code and link made.");
                    })
                    .catch(failed("Could not reset the code"))
                    .then(function () { reset.disabled = false; });
            });
        }

        // Remove member: their files stay in the group
        Array.prototype.forEach.call(page.querySelectorAll("[data-group-remove-member]"), function (button) {
            button.addEventListener("click", function () {
                var userId = parseInt(button.getAttribute("data-group-remove-member"), 10);
                var name = button.getAttribute("data-name") || "this person";
                if (!confirm("Remove " + name + " from the group? Their files stay in it.")) return;
                button.disabled = true;
                postJson("./groups/removemember", { id: groupId, userId: userId })
                    .then(function () {
                        var person = button.closest("[data-group-person]");
                        if (person !== null) person.parentNode.removeChild(person);
                        say("Removed " + name + ".");
                    })
                    .catch(function (error) {
                        button.disabled = false;
                        failed("Could not remove " + name)(error);
                    });
            });
        });

        // Delete group and Leave group go back to the Groups tab
        function leaveWith(buttonId, url, question, what) {
            var button = document.getElementById(buttonId);
            if (button === null) return;
            button.addEventListener("click", function () {
                if (!confirm(question())) return;
                button.disabled = true;
                postJson(url, { id: groupId })
                    .then(function () { window.location = "./groups"; })
                    .catch(function (error) {
                        button.disabled = false;
                        failed(what)(error);
                    });
            });
        }
        leaveWith("groupDelete", "./groups/delete", function () {
            return "Delete the group “" + groupName() + "”? Its files stay with the people who uploaded them.";
        }, "Could not delete the group");
        leaveWith("groupLeave", "./groups/leave", function () {
            return "Leave “" + groupName() + "”? The files you put in stay in the group.";
        }, "Could not leave the group");

        // Near-copies: Keep both hides the entry for good; Remove mine takes
        // the file out of the group (it stays in your files)
        Array.prototype.forEach.call(page.querySelectorAll("[data-near-keep]"), function (button) {
            button.addEventListener("click", function () {
                var fileId = button.getAttribute("data-near-keep");
                button.disabled = true;
                postJson("./groups/nearkeep", { id: groupId, fileId: fileId })
                    .then(function () {
                        var entry = button.closest("[data-near-file]");
                        if (entry !== null) entry.parentNode.removeChild(entry);
                        dropNearCopies([]);
                    })
                    .catch(function (error) {
                        button.disabled = false;
                        failed("Could not keep both")(error);
                    });
            });
        });
        Array.prototype.forEach.call(page.querySelectorAll("[data-near-remove]"), function (button) {
            button.addEventListener("click", function () {
                var fileId = button.getAttribute("data-near-remove");
                button.disabled = true;
                postJson("./groups/removefiles", { id: groupId, fileIds: [fileId] })
                    .then(function () {
                        var browser = groupBrowser();
                        if (browser !== null) browser.removeItem(fileId);
                        dropNearCopies([fileId]);
                    })
                    .catch(function (error) {
                        button.disabled = false;
                        failed("Could not remove it")(error);
                    });
            });
        });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
