// "View as…" (Part 3, V1): the banner, the Exit button and hiding what the
// server would refuse. Loaded synchronously in <head> on every page.
//
// The server decides everything; the fgs_viewas cookie only carries the
// target's display name for the banner, and hiding controls here is a
// courtesy. Names are set with textContent only.
(function () {
  "use strict";

  function displayName() {
    var parts = document.cookie ? document.cookie.split("; ") : [];
    for (var i = 0; i < parts.length; i++) {
      var eq = parts[i].indexOf("=");
      if (eq > 0 && parts[i].substring(0, eq) === "fgs_viewas") {
        var raw = parts[i].substring(eq + 1);
        if (!raw) return "";
        // The server writes url.QueryEscape, which encodes a space as "+"
        try { return decodeURIComponent(raw.replace(/\+/g, " ")); } catch (e) { return raw; }
      }
    }
    return "";
  }

  function exitViewAs(button) {
    if (button) button.disabled = true;
    fetch("./viewas/end", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: "{}"
    }).then(function () {
      location.href = "./admin";
    }).catch(function () {
      if (button) button.disabled = false;
      alert("Could not exit View as…. Please try again.");
    });
  }

  // Every Exit button, the banner's and the refusal page's
  document.addEventListener("click", function (e) {
    var button = e.target.closest && e.target.closest("[data-fgs-viewas-exit]");
    if (!button) return;
    e.preventDefault();
    exitViewAs(button);
  });

  var name = displayName();
  if (!name) return;
  document.documentElement.classList.add("fgs-viewas");

  var style = document.createElement("style");
  style.textContent = [
    ".fgs-viewas-banner{position:fixed;top:0;left:0;right:0;z-index:10000;background:#ffc107;color:#212529;",
    "padding:.35rem .75rem;font-size:.9rem;text-align:center}",
    ".fgs-viewas-banner button{margin-left:.25rem;padding:0 .6rem;border:1px solid #212529;border-radius:.25rem;",
    "background:transparent;color:#212529;font-size:.85rem}",
    "html.fgs-viewas body{padding-top:2.2rem}",
    // The selection bar's actions
    ".fgs-viewas [data-act]",
    // The \"⋯\" entries other than Open, and the list's quick download and copy buttons
    ",.fgs-viewas [data-menu]:not([data-menu=open])",
    ",.fgs-viewas li:has(> [data-menu]:not([data-menu=open]))",
    ",.fgs-viewas .fgs-list-actions .copyurl",
    // Download all, .zip and Save to Photos on shared pages
    ",.fgs-viewas [data-fgs-download-all],.fgs-viewas [data-fgs-download-zip],.fgs-viewas [data-fgs-photos]",
    // The viewer's download and Save to Photos
    ",.fgs-viewas .fgs-v-download,.fgs-viewas .fgs-v-photos",
    // Upload drop zones and upload buttons
    ",.fgs-viewas #uploaddropzone,.fgs-viewas .fgs-group-upload,.fgs-viewas .upload-box",
    // The groups tab's create and join, and the group page's management
    ",.fgs-viewas [data-bs-target='#groupCreateModal'],.fgs-viewas [data-bs-target='#groupJoinModal']",
    ",.fgs-viewas #groupControls,.fgs-viewas [data-group-remove-member],.fgs-viewas #groupStopLink",
    ",.fgs-viewas #groupInviteCopy,.fgs-viewas #groupCodeCopy,.fgs-viewas #groupLinkCopy",
    ",.fgs-viewas [data-near-keep],.fgs-viewas [data-near-remove]",
    "{display:none!important}"
  ].join("");
  document.head.appendChild(style);

  function insertBanner() {
    if (document.querySelector(".fgs-viewas-banner")) return;
    var banner = document.createElement("div");
    banner.className = "fgs-viewas-banner";
    banner.setAttribute("role", "status");
    banner.appendChild(document.createTextNode("Viewing as " + name + " · read-only · "));
    var exit = document.createElement("button");
    exit.type = "button";
    exit.setAttribute("data-fgs-viewas-exit", "");
    exit.textContent = "Exit";
    banner.appendChild(exit);
    document.body.insertBefore(banner, document.body.firstChild);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", insertBanner);
  else insertBanner();
})();
