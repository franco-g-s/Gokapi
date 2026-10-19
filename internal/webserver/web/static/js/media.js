// Shared media front end: device-specific download buttons, help sheet,
// and (later tasks) viewer, board and Save to Photos. No dependencies.
(function () {
  "use strict";
  var FGS = window.FGS = window.FGS || {};

  FGS.isIOS = function () {
    return /iPad|iPhone|iPod/.test(navigator.userAgent) ||
      (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  };

  // Desktop: "Download all" fires each file in turn (unchanged behaviour).
  // iOS: Safari allows one download per tap, so the zip is the primary path
  // until Save to Photos exists (Task 10).
  FGS.initDownloadButtons = function (root) {
    if (!root) return;
    var all = root.querySelector("[data-fgs-download-all]");
    var zip = root.querySelector("[data-fgs-download-zip]");
    var help = root.querySelector("[data-fgs-help]");
    if (FGS.isIOS()) {
      if (all) all.classList.add("d-none");
      if (zip) zip.classList.replace("btn-outline-light", "btn-light");
      if (help) help.classList.remove("d-none");
    }
    if (help) help.addEventListener("click", FGS.openHelpSheet);
  };

  // Fetches each file in turn rather than zipping: no archive to unpack, no
  // in-browser memory ceiling, and the recipient gets the original files.
  // Browsers ask once to allow multiple downloads, then queue them.
  // items: [{url, name}]; items without a url are skipped. opts.button is
  // disabled while it runs; opts.status receives the progress text.
  FGS.downloadSequentially = function (items, opts) {
    opts = opts || {};
    var button = opts.button, status = opts.status;
    items = (items || []).filter(function (item) { return item && item.url; });
    if (!items.length) return;
    if (button) button.disabled = true;
    var index = 0;
    (function next() {
      if (index >= items.length) {
        if (status) status.textContent = "Started " + items.length + " downloads.";
        if (button) button.disabled = false;
        return;
      }
      var a = document.createElement("a");
      a.href = items[index].url;
      a.download = items[index].name || "";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      index++;
      if (status) status.textContent = "Starting download " + index + " of " + items.length + "...";
      // Spacing the clicks keeps the browser from dropping queued downloads.
      window.setTimeout(next, 600);
    })();
  };

  FGS.openHelpSheet = function () {
    var el = document.getElementById("fgs-help-sheet");
    if (el) el.classList.add("open");
  };
  document.addEventListener("click", function (e) {
    if (e.target.closest("[data-fgs-help-close]")) {
      var el = document.getElementById("fgs-help-sheet");
      if (el) el.classList.remove("open");
    }
  });
})();
