// The group page (Part 2, G10): "Remove from group" on the board, the
// header controls (Rename, Extend, Reset code, Delete group, Leave group),
// "Also shared as a link" (R4),
// the members panel (and Remove member in it), the near-copy entries
// (Compare, Keep both, Remove) and the copy buttons. Merged into the
// admin bundle with every other admin_*.js file, so everything is inside
// this function and does nothing unless #groupPage is on the page. Names
// only ever reach the DOM through textContent and attribute setters.
;(function () {
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

    // Set by init: the duplicate check recounts itself when entries go
    var nearChanged = function () {};

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
        nearChanged();
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

        // Reset code: the old code and link stop working. The header shows
        // the new ones in place.
        function resetCode() {
            return postJson("./groups/resetcode", { id: groupId }).then(function (data) {
                document.getElementById("groupInviteUrl").value = data.InviteUrl;
                document.getElementById("groupInviteCopy").setAttribute("data-clipboard-text", data.InviteUrl);
                document.getElementById("groupCode").textContent = data.Code;
                document.getElementById("groupCodeCopy").setAttribute("data-clipboard-text", data.Code);
                return data;
            });
        }
        var reset = document.getElementById("groupResetCode");
        if (reset !== null) {
            reset.addEventListener("click", function () {
                if (!confirm("Reset the code? The current code and invite link stop working.")) return;
                reset.disabled = true;
                resetCode()
                    .then(function () { say("New code and link made."); })
                    .catch(failed("Could not reset the code"))
                    .then(function () { reset.disabled = false; });
            });
        }

        // "Also shared as a link" (R4): the old collection link's mode, and
        // Stop the link, which deletes the collection behind it
        var linkPanel = document.getElementById("groupLink");
        if (linkPanel !== null) {
            var modeRadios = linkPanel.querySelectorAll("input[name=groupLinkMode]");
            var currentMode = (linkPanel.querySelector("input[name=groupLinkMode]:checked") || {}).value;
            Array.prototype.forEach.call(modeRadios, function (radio) {
                radio.addEventListener("change", function () {
                    if (!radio.checked || radio.value === currentMode) return;
                    Array.prototype.forEach.call(modeRadios, function (r) { r.disabled = true; });
                    postJson("./groups/linkmode", { id: groupId, mode: radio.value })
                        .then(function () {
                            currentMode = radio.value;
                            say(radio.value === "require" ? "The old link now requires joining." :
                                "The old link now shows the photos and invites to join.");
                        })
                        .catch(function (error) {
                            Array.prototype.forEach.call(modeRadios, function (r) { r.checked = r.value === currentMode; });
                            failed("Could not change the link")(error);
                        })
                        .then(function () {
                            Array.prototype.forEach.call(modeRadios, function (r) { r.disabled = false; });
                        });
                });
            });
            var stopLink = document.getElementById("groupStopLink");
            stopLink.addEventListener("click", function () {
                if (!confirm("Stop the link? It stops working and the collection behind it is deleted. The group and its files stay.")) return;
                stopLink.disabled = true;
                postJson("./groups/stoplink", { id: groupId })
                    .then(function () {
                        linkPanel.parentNode.removeChild(linkPanel);
                        say("The link is stopped.");
                    })
                    .catch(function (error) {
                        stopLink.disabled = false;
                        failed("Could not stop the link")(error);
                    });
            });
        }

        // The members button shows the first five initials, "+N" for the rest
        // and the count; after a removal it is rebuilt from the panel's list
        var STACK_SIZE = 5;
        var membersButton = document.getElementById("groupMembersButton");
        function refreshMembers() {
            var stack = document.getElementById("groupMembersStack");
            var count = document.getElementById("groupMembersCount");
            var rows = page.querySelectorAll("#groupPeople [data-group-person]");
            var n = rows.length;
            if (stack !== null) {
                stack.textContent = "";
                Array.prototype.slice.call(rows, 0, STACK_SIZE).forEach(function (row) {
                    var circle = document.createElement("span");
                    circle.className = "fgs-avatar";
                    circle.style.backgroundColor = "hsl(" + (parseInt(row.getAttribute("data-hue"), 10) || 0) + ", 45%, 45%)";
                    circle.textContent = row.getAttribute("data-initials") || "";
                    stack.appendChild(circle);
                });
                if (n > STACK_SIZE) {
                    var more = document.createElement("span");
                    more.className = "fgs-avatar fgs-avatar-more";
                    more.textContent = "+" + (n - STACK_SIZE);
                    stack.appendChild(more);
                }
            }
            if (count !== null) count.textContent = n + (n === 1 ? " member" : " members");
            if (membersButton !== null) membersButton.setAttribute("aria-label", "Members: " + n);
        }

        // Remove member: their files stay in the group. They have seen the
        // invite code and link, so the dialog offers to reset both (I4),
        // ticked by default; otherwise they could join again at once.
        var removeModal = document.getElementById("groupRemoveMemberModal");
        var removeForm = document.getElementById("groupRemoveMemberForm");
        var removing = null; // the "x" button of the person being removed
        Array.prototype.forEach.call(page.querySelectorAll("[data-group-remove-member]"), function (button) {
            button.addEventListener("click", function () {
                if (removeModal === null || removeForm === null || !window.bootstrap) return;
                removing = button;
                // The dialog opens over the page: close the panel behind it
                if (membersButton !== null) {
                    var panel = bootstrap.Dropdown.getInstance(membersButton);
                    if (panel !== null) panel.hide();
                }
                var name = button.getAttribute("data-name") || "this person";
                document.getElementById("groupRemoveMemberText").textContent =
                    "Remove " + name + " from the group? Their files stay in it.";
                document.getElementById("groupRemoveMemberReset").checked = true;
                document.getElementById("groupRemoveMemberError").textContent = "";
                bootstrap.Modal.getOrCreateInstance(removeModal).show();
            });
        });
        if (removeForm !== null) {
            removeForm.addEventListener("submit", function (event) {
                event.preventDefault();
                var button = removing;
                if (button === null) return;
                var submit = document.getElementById("groupRemoveMemberSubmit");
                var error = document.getElementById("groupRemoveMemberError");
                var alsoReset = document.getElementById("groupRemoveMemberReset").checked;
                var userId = parseInt(button.getAttribute("data-group-remove-member"), 10);
                var name = button.getAttribute("data-name") || "this person";
                submit.disabled = true;
                postJson("./groups/removemember", { id: groupId, userId: userId })
                    .then(function () {
                        var person = button.closest("[data-group-person]");
                        if (person !== null) person.parentNode.removeChild(person);
                        refreshMembers();
                        removing = null;
                        bootstrap.Modal.getOrCreateInstance(removeModal).hide();
                        if (!alsoReset) {
                            say("Removed " + name + ". The code and link still work.");
                            return;
                        }
                        // The removal stands even if the reset fails: say so
                        return resetCode()
                            .then(function () { say("Removed " + name + ". New code and link made."); })
                            .catch(function (err) {
                                failed("Removed " + name + ", but the code was not reset")(err);
                            });
                    })
                    .catch(function (err) { error.textContent = "Could not remove " + name + ": " + errorText(err); })
                    .then(function () { submit.disabled = false; });
            });
        }

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

        // Near-copies. Compare opens the viewer on the pair, older first, so a
        // swipe goes between the two at full preview size. The board's own
        // tile is used when it is on the page; a tile that is filtered out
        // is rebuilt from the entry's data-*.
        function itemFromBanner(button) {
            var d = function (k) { return button.getAttribute("data-" + k) || ""; };
            return { id: d("id"), name: d("name"), view: d("view"), video: "", kind: "", download: d("download"),
                uploader: d("uploader"), taken: 0, uploaded: 0, size: "", bytes: 0,
                w: parseInt(d("w"), 10) || 0, h: parseInt(d("h"), 10) || 0 };
        }
        function pairItem(button) {
            var tile = null;
            Array.prototype.forEach.call(byAttr("data-id", button.getAttribute("data-id")), function (el) {
                if (tile === null && el.classList.contains("fgs-tile")) tile = el;
            });
            return tile !== null ? FGS.itemFromEl(tile) : itemFromBanner(button);
        }
        function compare(entry, startAtNewer) {
            if (!window.FGS || !FGS.openViewer || !FGS.itemFromEl) return;
            var older = entry.querySelector('[data-near-open="0"]');
            var newer = entry.querySelector('[data-near-open="1"]');
            if (older === null || newer === null) return;
            FGS.openViewer([pairItem(older), pairItem(newer)], startAtNewer ? 1 : 0);
        }

        // Keep both settles the pair for good, for everyone: its banner entry
        // goes, and in the duplicate check the pair stays, marked Kept
        function markKept(entry) {
            entry.setAttribute("data-near-kept", "true");
            var keep = entry.querySelector("[data-near-keep]");
            if (keep !== null) keep.parentNode.removeChild(keep);
            if (entry.querySelector(".fgs-near-kept") === null) {
                var badge = document.createElement("span");
                badge.className = "badge text-bg-secondary fgs-near-kept";
                badge.textContent = "Kept";
                entry.insertBefore(badge, entry.querySelector("[data-near-compare]"));
            }
        }
        function settleKept(fileId) {
            Array.prototype.forEach.call(byAttr("data-near-file", fileId), function (entry) {
                if (entry.parentNode.id === "groupDupesList") {
                    markKept(entry);
                } else {
                    entry.parentNode.removeChild(entry);
                }
            });
            dropNearCopies([]);
        }

        // Remove takes the newer item out of the group (it stays in its
        // uploader's files). The server is told which copy stays, so the
        // downloaded state follows it.
        function bindNearEntry(entry) {
            var newerId = entry.getAttribute("data-near-file");
            var keptId = entry.getAttribute("data-near-match");
            Array.prototype.forEach.call(entry.querySelectorAll("[data-near-open]"), function (button) {
                button.addEventListener("click", function () {
                    compare(entry, button.getAttribute("data-near-open") === "1");
                });
            });
            var compareButton = entry.querySelector("[data-near-compare]");
            if (compareButton !== null) {
                compareButton.addEventListener("click", function () { compare(entry, false); });
            }
            var keep = entry.querySelector("[data-near-keep]");
            if (keep !== null) {
                keep.addEventListener("click", function () {
                    keep.disabled = true;
                    postJson("./groups/nearkeep", { id: groupId, fileId: newerId })
                        .then(function () { settleKept(newerId); })
                        .catch(function (error) {
                            keep.disabled = false;
                            failed("Could not keep both")(error);
                        });
                });
            }
            var remove = entry.querySelector("[data-near-remove]");
            if (remove !== null) {
                remove.addEventListener("click", function () {
                    remove.disabled = true;
                    postJson("./groups/removefiles", { id: groupId, fileIds: [newerId], keptId: keptId })
                        .then(function () {
                            var browser = groupBrowser();
                            if (browser !== null) browser.removeItem(newerId);
                            dropNearCopies([newerId]);
                        })
                        .catch(function (error) {
                            remove.disabled = false;
                            failed("Could not remove it")(error);
                        });
                });
            }
        }
        Array.prototype.forEach.call(page.querySelectorAll("[data-near-file]"), bindNearEntry);

        // --- Check for duplicates (creator and admin) ----------------------
        // Every pair in the group, as the server lists it: bursts by one
        // uploader too, and pairs already kept.
        var dupes = document.getElementById("groupDupes");
        var dupesList = document.getElementById("groupDupesList");
        var dupesTitle = document.getElementById("groupDupesTitle");

        function dupesCount() {
            if (dupesList === null) return;
            var n = dupesList.children.length;
            dupesTitle.textContent = n === 0 ? "No duplicates found" :
                n + (n === 1 ? " possible duplicate" : " possible duplicates");
        }
        nearChanged = dupesCount;

        function clock(ts) {
            var d = new Date(ts * 1000);
            return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
        }
        function thumbButton(side, which) {
            var button = document.createElement("button");
            button.type = "button";
            button.className = "fgs-near-open";
            button.setAttribute("data-near-open", which);
            button.setAttribute("data-id", side.Id);
            button.setAttribute("data-name", side.Name);
            button.setAttribute("data-view", side.View);
            button.setAttribute("data-download", side.Download);
            button.setAttribute("data-uploader", side.Uploader);
            button.setAttribute("data-w", String(side.Width));
            button.setAttribute("data-h", String(side.Height));
            button.setAttribute("aria-label", "Compare " + side.Name + " with the other photo");
            button.title = side.Name;
            var img = document.createElement("img");
            img.className = "fgs-near-thumb";
            img.src = side.Thumb;
            img.alt = "";
            img.loading = "lazy";
            img.decoding = "async";
            button.appendChild(img);
            return button;
        }
        function actionButton(attr, value, style, text) {
            var button = document.createElement("button");
            button.type = "button";
            button.className = "btn btn-sm " + style;
            button.setAttribute(attr, value);
            button.textContent = text;
            return button;
        }
        function dupeEntry(pair) {
            var entry = document.createElement("div");
            entry.className = "d-flex flex-wrap align-items-center gap-2 border border-secondary rounded p-2 mb-2";
            entry.setAttribute("data-near-file", pair.Newer.Id);
            entry.setAttribute("data-near-match", pair.Older.Id);
            entry.appendChild(thumbButton(pair.Newer, "1"));
            entry.appendChild(thumbButton(pair.Older, "0"));
            var text = document.createElement("span");
            text.className = "me-auto";
            if (pair.NewerUser === pair.MatchUser) {
                text.textContent = pair.NewerUser + "'s two photos look alike";
            } else {
                text.textContent = "Looks like " + pair.MatchUser + "'s photo from " + clock(pair.MatchAt) +
                    (pair.Mine ? "" : " (uploaded by " + pair.NewerUser + ")");
            }
            entry.appendChild(text);
            entry.appendChild(actionButton("data-near-compare", pair.Newer.Id, "btn-outline-light", "Compare"));
            if (!pair.Kept) entry.appendChild(actionButton("data-near-keep", pair.Newer.Id, "btn-outline-light", "Keep both"));
            entry.appendChild(actionButton("data-near-remove", pair.Newer.Id, "btn-outline-danger",
                pair.Mine ? "Remove mine" : "Remove newer"));
            if (pair.Kept) markKept(entry);
            bindNearEntry(entry);
            return entry;
        }

        var checkButton = document.getElementById("groupCheckDupes");
        if (checkButton !== null && dupes !== null) {
            checkButton.addEventListener("click", function () {
                checkButton.disabled = true;
                say("Checking…");
                fetch("./groups/neardupes?id=" + encodeURIComponent(groupId), { credentials: "same-origin" })
                    .then(function (response) {
                        return response.text().then(function (body) {
                            var data;
                            try {
                                data = JSON.parse(body);
                            } catch (ignored) {
                                throw new Error(response.redirected || body.trim().charAt(0) === "<" ?
                                    "Your session expired — reload the page" : "The server answered " + response.status);
                            }
                            if (!data || data.Result !== "OK") throw new Error(sentence(data && data.ErrorMessage));
                            return data;
                        });
                    })
                    .then(function (data) {
                        dupesList.textContent = "";
                        data.Pairs.forEach(function (pair) { dupesList.appendChild(dupeEntry(pair)); });
                        dupesCount();
                        dupes.hidden = false;
                        say("");
                    })
                    .catch(failed("Could not check for duplicates"))
                    .then(function () { checkButton.disabled = false; });
            });
            document.getElementById("groupDupesClose").addEventListener("click", function () {
                dupes.hidden = true;
            });
        }
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
