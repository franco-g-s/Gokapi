// Part 3, P: temporary photos probe. Measures what iOS Safari's share sheet
// accepts. Names go into the page with textContent only.
(function () {
  "use strict";
  var MIME = { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", gif: "image/gif",
    webp: "image/webp", heic: "image/heic", heif: "image/heif", mov: "video/quicktime",
    mp4: "video/mp4", m4v: "video/x-m4v" };
  var LOG_KEY = "photosProbeLog", PENDING_KEY = "photosProbePending";

  var items = [];
  try { items = JSON.parse(document.getElementById("probe-data").textContent) || []; } catch (e) { items = []; }
  var photos = items.filter(function (i) { return i.kind === "image"; });
  var videos = items.filter(function (i) { return i.kind === "video"; });

  function $(id) { return document.getElementById(id); }
  function mb(bytes) { return (bytes / 1e6).toFixed(1); }

  function mimeFor(name, blobType) {
    if (blobType && blobType !== "application/octet-stream") return blobType;
    var m = /\.([a-z0-9]+)$/i.exec(name || "");
    return (m && MIME[m[1].toLowerCase()]) || blobType || "application/octet-stream";
  }

  // The log lives in sessionStorage, so a Safari tab reload keeps it
  function readLog() {
    try { return JSON.parse(sessionStorage.getItem(LOG_KEY)) || []; } catch (e) { return []; }
  }
  function writeLog(lines) {
    try { sessionStorage.setItem(LOG_KEY, JSON.stringify(lines)); } catch (e) { /* full or blocked */ }
  }
  function renderLog() { $("probe-log").textContent = readLog().join("\n"); }
  function addLine(line) {
    var lines = readLog();
    lines.push(line);
    writeLog(lines);
    renderLog();
  }
  function setStatus(text) { $("probe-status").textContent = text; }

  // An attempt that was started but never finished: the tab was reloaded
  var pending = null;
  try { pending = sessionStorage.getItem(PENDING_KEY); } catch (e) { /* ignore */ }
  if (pending) {
    addLine(pending + " | NO RESULT: the tab reloaded or crashed");
    try { sessionStorage.removeItem(PENDING_KEY); } catch (e) { /* ignore */ }
  }
  renderLog();

  function activationAlive() {
    var ua = navigator.userActivation;
    return !!(ua && ua.isActive);
  }

  var busy = false;
  function setBusy(b) {
    busy = b;
    $("probe-photos-try").disabled = b;
    $("probe-video-try").disabled = b;
  }

  // list: [{name, bytes, url}]. kind: "photos" or "video".
  function attempt(kind, list) {
    if (busy) return;
    if (!list.length) { setStatus("Nothing to try."); return; }
    if (!navigator.canShare || !navigator.share) {
      addLine(kind + " | navigator.share is not available in this browser");
      setStatus("navigator.share is not available.");
      return;
    }
    setBusy(true);
    $("probe-share").hidden = true;
    var started = Date.now();
    var total = list.reduce(function (s, i) { return s + i.bytes; }, 0);
    var head = kind + " | N=" + list.length + " | " + mb(total) + " MB";
    try { sessionStorage.setItem(PENDING_KEY, head); } catch (e) { /* ignore */ }

    var files = [], fetched = 0, failure = null;
    var chain = Promise.resolve();
    list.forEach(function (it) {
      chain = chain.then(function () {
        if (failure) return null;
        setStatus("Fetching " + (files.length + 1) + "/" + list.length + "…");
        return fetch(it.url, { credentials: "same-origin" }).then(function (r) {
          if (!r.ok) throw new Error("fetch " + it.name + ": HTTP " + r.status);
          return r.blob();
        }).then(function (b) {
          fetched += b.size;
          files.push(new File([b], it.name, { type: mimeFor(it.name, b.type) }));
        }).catch(function (e) { failure = (e && e.name ? e.name + ": " : "") + (e && e.message ? e.message : String(e)); });
      });
    });
    chain.then(function () {
      var secs = ((Date.now() - started) / 1000).toFixed(1);
      var summary = "fetched " + files.length + " files, " + mb(fetched) + " MB in " + secs + " s";
      setStatus(summary);
      var fetchPart = head + " | fetch " + secs + " s";
      if (failure) {
        finish(fetchPart + " | fetch failed: " + failure);
        return;
      }
      var run = function () {
        $("probe-share").hidden = true;
        var data = { files: files };
        try {
          if (!navigator.canShare(data)) { finish(fetchPart + " | share: canShare returned false"); return; }
        } catch (e) { finish(fetchPart + " | share: canShare threw " + e.name + ": " + e.message); return; }
        navigator.share(data).then(function () {
          finish(fetchPart + " | share: resolved");
        }, function (err) {
          finish(fetchPart + " | share: " + (err && err.name) + ": " + (err && err.message));
        });
      };
      if (activationAlive()) {
        run();
      } else {
        // The fetch outlasted the tap: one more tap opens the sheet
        $("probe-share").hidden = false;
        $("probe-share").onclick = run;
        setStatus(summary + ". Tap Share.");
      }
    });
  }

  function finish(line) {
    try { sessionStorage.removeItem(PENDING_KEY); } catch (e) { /* ignore */ }
    addLine(line);
    setBusy(false);
  }

  $("probe-photos-try").onclick = function () {
    var n = parseInt($("probe-n").value, 10) || 0;
    var cap = (parseFloat($("probe-cap").value) || 0) * 1e6;
    var list = [], total = 0;
    for (var i = 0; i < photos.length && list.length < n; i++) {
      if (total + photos[i].bytes > cap) continue;
      total += photos[i].bytes;
      list.push(photos[i]);
    }
    attempt("photos", list);
  };

  $("probe-video-try").onclick = function () {
    var idx = parseInt($("probe-video").value, 10);
    if (isNaN(idx) || !videos[idx]) { setStatus("No video selected."); return; }
    attempt("video", [videos[idx]]);
  };

  $("probe-copy").onclick = function () {
    var text = readLog().join("\n");
    var done = function () { setStatus("Log copied."); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { setStatus("Copy failed: select the log text by hand."); });
    } else {
      setStatus("Copy is not available: select the log text by hand.");
    }
  };
  $("probe-clear").onclick = function () { writeLog([]); renderLog(); };

  var totalPhotoBytes = photos.reduce(function (s, i) { return s + i.bytes; }, 0);
  $("probe-photo-summary").textContent = photos.length + " photos, " + mb(totalPhotoBytes) + " MB in all.";
  var select = $("probe-video");
  if (!videos.length) {
    var none = document.createElement("option");
    none.textContent = "No videos";
    select.appendChild(none);
  }
  videos.forEach(function (v, i) {
    var o = document.createElement("option");
    o.value = String(i);
    o.textContent = v.name + " (" + mb(v.bytes) + " MB)";
    select.appendChild(o);
  });
})();
