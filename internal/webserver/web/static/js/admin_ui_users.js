// This file contains JS code for the User view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...


function changeUserPermission(userId, permission, buttonId) {

    let indicator = document.getElementById(buttonId);
    if (indicator.classList.contains("perm-processing") || indicator.classList.contains("perm-nochange")) {
        return;
    }
    let wasGranted = indicator.classList.contains("perm-granted");
    indicator.classList.add("perm-processing");
    indicator.classList.remove("perm-granted");
    indicator.classList.remove("perm-notgranted");

    let modifier = "GRANT";
    if (wasGranted) {
        modifier = "REVOKE";
    }

    if (permission == "PERM_REPLACE_OTHER" && !wasGranted) {
        hasNotPermissionReplace = document.getElementById("perm_replace_" + userId).classList.contains("perm-notgranted");
        if (hasNotPermissionReplace) {
            showToast(2000, "Also granting permission to replace own files");
            changeUserPermission(userId, "PERM_REPLACE", "perm_replace_" + userId);
        }
    }
    if (permission == "PERM_REPLACE" && wasGranted) {
        hasPermissionReplaceOthers = document.getElementById("perm_replace_other_" + userId).classList.contains("perm-granted");
        if (hasPermissionReplaceOthers) {
            showToast(2000, "Also revoking permission to replace files of other users");
            changeUserPermission(userId, "PERM_REPLACE_OTHER", "perm_replace_other_" + userId);
        }
    }


    apiUserModify(userId, permission, modifier)
        .then(data => {
            if (wasGranted) {
                indicator.classList.add("perm-notgranted");
            } else {
                indicator.classList.add("perm-granted");
            }
            indicator.classList.remove("perm-processing");
        })
        .catch(error => {
            if (wasGranted) {
                indicator.classList.add("perm-granted");
            } else {
                indicator.classList.add("perm-notgranted");
            }
            indicator.classList.remove("perm-processing");
            alert("Unable to set permission: " + error);
            console.error('Error:', error);
        });
}



function changeRank(userId, newRank, buttonId) {

    let indicator = document.getElementById(buttonId);
    if (indicator.disabled) {
        return;
    }
    indicator.disabled = true;

    apiUserChangeRank(userId, newRank)
        .then(data => {
            location.reload();
        })
        .catch(error => {
            indicator.disabled = false;
            alert("Unable to change rank: " + error);
            console.error('Error:', error);
        });
}



function showDeleteUserModal(userId, userEmail) {
    let checkboxDelete = document.getElementById("checkboxDelete");
    checkboxDelete.checked = false;
    document.getElementById("deleteModalBody").innerText = userEmail;
    $('#deleteModal').modal('show');

    document.getElementById("buttonDelete").onclick = function() {
        apiUserDelete(userId, checkboxDelete.checked)
            .then(data => {
                $('#deleteModal').modal('hide');
                document.getElementById("row-" + userId).classList.add("rowDeleting");
                setTimeout(() => {
                    document.getElementById("row-" + userId).remove();
                }, 290);
            })
            .catch(error => {
                alert("Unable to delete user: " + error);
                console.error('Error:', error);
            });
    };
}


function showAddUserModal() {
    // Cloning removes any previous values or form validation
    let originalModal = $('#newUserModal').clone();
    $("#newUserModal").on('hide.bs.modal', function() {
        $('#newUserModal').remove();
        let myClone = originalModal.clone();
        $('body').append(myClone);
    });
    $('#newUserModal').modal('show');
}


function showResetPwModal(userid, name) {
    // Cloning removes any previous values or form validation
    let originalModal = $('#resetPasswordModal').clone();
    $("#resetPasswordModal").on('hide.bs.modal', function() {
        $('#resetPasswordModal').remove();
        let myClone = originalModal.clone();
        $('body').append(myClone);
    });

    document.getElementById("l_userpwreset").innerText = name;
    let button = document.getElementById("resetPasswordButton");
    button.onclick = function() {
        resetPw(userid, document.getElementById("generateRandomPassword").checked);
    };
    $('#resetPasswordModal').modal('show');
}

function resetPw(userid, newPw) {
    let button = document.getElementById("resetPasswordButton");
    document.getElementById("resetPasswordButton").disabled = true;
    apiUserResetPassword(userid, newPw)
        .then(data => {
            if (!newPw) {
                $('#resetPasswordModal').modal('hide');
                showToast(1000, 'Password change requirement set successfully')
                return;
            }
            button.style.display = 'none';
            document.getElementById("cancelPasswordButton").style.display = 'none';
            document.getElementById("formentryReset").style.display = 'none';
            document.getElementById("randomPasswordContainer").style.display = 'block';
            document.getElementById("closeModalResetPw").style.display = 'block';
            document.getElementById("l_returnedPw").innerText = data.password;
            document.getElementById("copypwclip").onclick = function() {
                // For some reason ClipboardJs is not working on the user PW reset modal, even when initilising again. Manually writing to clipboard
                navigator.clipboard.writeText(data.password);
                showToast(1000, "Password copied to clipboard");
            }
        })
        .catch(error => {
            alert("Unable to reset user password: " + error);
            console.error('Error:', error);
            button.disabled = false;
        });
}


function addNewUser() {
    let button = document.getElementById("mb_addUser");
    button.disabled = true;
    let form = document.getElementById('newUserForm');
    if (!form.checkValidity()) {
        form.classList.add('was-validated');
        button.disabled = false;
    } else {
        let editName = document.getElementById("e_userName");
        apiUserCreate(editName.value.trim())
            .then(data => {
                $('#newUserModal').modal('hide');
                addRowUser(data.id, data.name, data.permissions);
                console.log(data);
            })
            .catch(error => {
                if (error.message == "duplicate") {
                    alert("A user already exists with that name");
                    button.disabled = false;
                } else {
                    alert("Unable to create user: " + error);
                    console.error('Error:', error);
                    button.disabled = false;
                }
            });
    }
}



const PermissionDefinitions = [
    {
        key: "UserPermGuestUploads",
        bit: 1 << 8,
        icon: "bi bi-box-arrow-in-down",
        title: "Create file requests",
        htmlId: userid => `perm_guest_upload_${userid}`,
        apiName: "PERM_GUEST_UPLOAD"
    },
    {
        key: "UserPermReplaceUploads",
        bit: 1 << 0,
        icon: "bi bi-recycle",
        title: "Replace own uploads",
        htmlId: userid => `perm_replace_${userid}`,
        apiName: "PERM_REPLACE"
    },
    {
        key: "UserPermListOtherUploads",
        bit: 1 << 1,
        icon: "bi bi-eye",
        title: "List other uploads",
        htmlId: userid => `perm_list_${userid}`,
        apiName: "PERM_LIST"
    },
    {
        key: "UserPermEditOtherUploads",
        bit: 1 << 2,
        icon: "bi bi-pencil",
        title: "Edit other uploads",
        htmlId: userid => `perm_edit_${userid}`,
        apiName: "PERM_EDIT"
    },
    {
        key: "UserPermDeleteOtherUploads",
        bit: 1 << 4,
        icon: "bi bi-trash3",
        title: "Delete other uploads",
        htmlId: userid => `perm_delete_${userid}`,
        apiName: "PERM_DELETE"
    },
    {
        key: "UserPermReplaceOtherUploads",
        bit: 1 << 3,
        icon: "bi bi-arrow-left-right",
        title: "Replace other uploads",
        htmlId: userid => `perm_replace_other_${userid}`,
        apiName: "PERM_REPLACE_OTHER"
    },
    {
        key: "UserPermManageLogs",
        bit: 1 << 5,
        icon: "bi bi-card-list",
        title: "Manage system logs",
        htmlId: userid => `perm_logs_${userid}`,
        apiName: "PERM_LOGS"
    },
    {
        key: "UserPermManageUsers",
        bit: 1 << 7,
        icon: "bi bi-people",
        title: "Manage users",
        htmlId: userid => `perm_users_${userid}`,
        apiName: "PERM_USERS"
    },
    {
        key: "UserPermManageApiKeys",
        bit: 1 << 6,
        icon: "bi bi-sliders2",
        title: "Manage all API keys",
        htmlId: userid => `perm_api_${userid}`,
        apiName: "PERM_API"
    }
];

function hasPermission(userPerm, permissionBit) {
    return (userPerm & permissionBit) !== 0;
}


// A dropdown cell control, as html_users.tmpl renders it (R2, V5)
function userRowDropdown(label, ariaLabel, autoCloseOutside) {
    const wrapper = document.createElement("div");
    wrapper.className = "dropdown d-inline-block";
    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = "btn btn-outline-light btn-sm";
    toggle.setAttribute("data-bs-toggle", "dropdown");
    toggle.setAttribute("data-bs-popper-config", '{"strategy":"fixed"}');
    toggle.setAttribute("aria-expanded", "false");
    if (autoCloseOutside) toggle.setAttribute("data-bs-auto-close", "outside");
    if (ariaLabel) {
        toggle.setAttribute("aria-label", ariaLabel);
        toggle.title = ariaLabel;
    } else {
        toggle.classList.add("dropdown-toggle");
    }
    toggle.textContent = label;
    const menu = document.createElement("ul");
    menu.className = "dropdown-menu dropdown-menu-dark dropdown-menu-end";
    wrapper.appendChild(toggle);
    wrapper.appendChild(menu);
    return { wrapper: wrapper, menu: menu };
}

// One menu entry: an icon and a label, both set without HTML
function userRowMenuItem(menu, id, iconClass, label, onclick) {
    const li = document.createElement("li");
    const item = document.createElement("button");
    item.type = "button";
    item.className = "dropdown-item";
    if (id) item.id = id;
    const icon = document.createElement("i");
    icon.className = iconClass;
    item.appendChild(icon);
    item.appendChild(document.createTextNode(" " + label));
    if (onclick) item.onclick = onclick;
    li.appendChild(item);
    menu.appendChild(li);
    return { item: item, icon: icon };
}

function addRowUser(userid, name, permissions) {

    userid = sanitizeUserId(userid);

    let table = document.getElementById("usertable");
    let row = table.insertRow(1);
    row.id = "row-" + userid;
    // Same cells as html_users.tmpl
    let cellName = row.insertCell(0);
    let cellType = row.insertCell(1);
    let cellLastOnline = row.insertCell(2);
    let cellUploads = row.insertCell(3);
    let cellSize = row.insertCell(4);
    let cellGroups = row.insertCell(5);
    let cellActions = row.insertCell(6);
    const cells = [cellName, cellType, cellLastOnline, cellUploads, cellSize, cellGroups, cellActions];
    cells.forEach(cell => cell.classList.add("newUser"));

    cellName.className += " fgs-user-name";
    cellName.textContent = name;
    // A new user is never an admin, nor yourself
    if (typeof canViewAs !== "undefined" && canViewAs) {
        const viewAs = document.createElement("button");
        viewAs.type = "button";
        viewAs.className = "btn btn-outline-light btn-sm fgs-viewas-btn";
        viewAs.setAttribute("data-fgs-viewas-user", userid);
        viewAs.textContent = "View as…";
        cellName.appendChild(document.createTextNode(" "));
        cellName.appendChild(viewAs);
    }
    cellType.id = "userlevel_" + userid;
    cellType.textContent = "User";
    cellLastOnline.textContent = "Never";
    const fileCount = document.createElement("span");
    fileCount.className = "user-uploadcount";
    fileCount.textContent = "0";
    cellUploads.appendChild(fileCount);
    cellSize.textContent = "0 B";
    cellGroups.textContent = "—";
    cellActions.classList.add("text-nowrap");

    // Permissions: the same toggles and calls as the server-rendered rows
    const perms = userRowDropdown("Permissions", null, true);
    perms.wrapper.classList.add("prevent-select");
    perms.menu.classList.add("fgs-perm-menu");
    PermissionDefinitions.forEach(perm => {
        const id = perm.htmlId(userid);
        let classes = perm.icon + " " + (hasPermission(permissions, perm.bit) ? "perm-granted" : "perm-notgranted");
        if (!hasPermission(userPermissions, perm.bit)) {
            classes += " perm-nochange";
        }
        const entry = userRowMenuItem(perms.menu, null, classes, perm.title,
            () => changeUserPermission(userid, perm.apiName, id));
        entry.item.classList.add("fgs-perm-item");
        entry.icon.id = id;
    });
    cellActions.appendChild(perms.wrapper);
    cellActions.appendChild(document.createTextNode(" "));

    // The "⋯" actions
    const actions = userRowDropdown("⋯", "Actions", false);
    actions.menu.classList.add("fgs-user-actions");
    if (isInternalAuth) {
        userRowMenuItem(actions.menu, `pwchange-${userid}`, "bi bi-key-fill", "Reset password",
            () => showResetPwModal(userid, name));
    }
    const promote = userRowMenuItem(actions.menu, `changeRank_${userid}`, "bi bi-chevron-double-up",
        "Change user type to Admin", () => changeRank(userid, 'ADMIN', `changeRank_${userid}`));
    if (!isAdmin) {
        promote.item.disabled = true;
    }
    const divider = document.createElement("li");
    divider.innerHTML = '<hr class="dropdown-divider">';
    actions.menu.appendChild(divider);
    const del = userRowMenuItem(actions.menu, `delete-${userid}`, "bi bi-trash3", "Delete",
        () => showDeleteUserModal(userid, name));
    del.item.classList.add("text-danger");
    cellActions.appendChild(actions.wrapper);

    setTimeout(() => {
        cells.forEach(cell => cell.classList.remove("newUser"));
    }, 700);
}

function sanitizeUserId(id) {
    const numericId = id.toString().trim();
    if (!/^\d+$/.test(numericId)) {
        throw new Error("Invalid ID: must contain only digits.");
    }
    return numericId;
}


// Part 3, V1: "View as…", started from the Users tab (R2, V4)
function viewAsStart(userId, button) {
    if (button) button.disabled = true;
    fetch("./viewas/start", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ userId: userId })
    }).then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (data) {
            if (!response.ok || data.Result !== "OK") {
                throw new Error(data.ErrorMessage || ("HTTP " + response.status));
            }
            location.href = "./files";
        });
    }).catch(function (error) {
        if (button) button.disabled = false;
        alert("Unable to view as this person: " + error.message);
    });
}

document.addEventListener("click", function (e) {
    var button = e.target.closest && e.target.closest("[data-fgs-viewas-user]");
    if (!button) return;
    e.preventDefault();
    viewAsStart(parseInt(button.getAttribute("data-fgs-viewas-user"), 10), button);
});


// Part 4, K: the blocklist

// postBlock sends one blocklist change as JSON and rejects with the server's message
async function postBlock(path, entry) {
    const response = await fetch(path, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({entry: entry})
    });
    let data = {};
    try {
        data = await response.json();
    } catch (e) {
        // not JSON: fall through to the status check
    }
    if (!response.ok) {
        throw new Error(data.ErrorMessage || ('Error ' + response.status));
    }
}

// reloadBlockedList replaces the Blocked list with the one the server renders
// into /users now
async function reloadBlockedList() {
    const response = await fetch('users', {credentials: 'same-origin'});
    if (!response.ok) {
        location.reload();
        return;
    }
    const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
    const fresh = doc.getElementById('blocked-list');
    const current = document.getElementById('blocked-list');
    if (!fresh || !current) {
        location.reload();
        return;
    }
    current.replaceChildren(...Array.from(fresh.children).map(child => document.importNode(child, true)));
}

function addBlockedEntry() {
    const input = document.getElementById('blocked-input');
    const entry = input.value.trim();
    if (entry === '') {
        return;
    }
    postBlock('users/block', entry)
        .then(() => {
            input.value = '';
            showToast(1000, 'Blocked');
            return reloadBlockedList();
        })
        .catch(error => {
            alert(error.message);
        });
}

function removeBlockedEntry(entry) {
    postBlock('users/unblock', entry)
        .then(() => reloadBlockedList())
        .catch(error => {
            alert(error.message);
        });
}

function blockPerson(email) {
    if (!confirm('Block ' + email + '? They are signed out now and cannot sign in again. Their files stay.')) {
        return;
    }
    postBlock('users/block', email)
        .then(() => {
            showToast(1000, 'Blocked');
            return reloadBlockedList();
        })
        .catch(error => {
            alert(error.message);
        });
}

document.addEventListener('click', function(event) {
    const unblock = event.target.closest('[data-fgs-unblock]');
    if (unblock) {
        removeBlockedEntry(unblock.getAttribute('data-fgs-unblock'));
        return;
    }
    const block = event.target.closest('[data-fgs-block-user]');
    if (block) {
        blockPerson(block.getAttribute('data-fgs-block-user'));
    }
});
