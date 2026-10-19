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
