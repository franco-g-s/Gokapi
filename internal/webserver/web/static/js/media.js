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
  // iOS: Safari allows one download per tap, so Save to Photos is primary and
  // the zip is the fallback (the zip alone where navigator.canShare is missing).
  FGS.initDownloadButtons = function (root) {
    if (!root) return;
    var all = root.querySelector("[data-fgs-download-all]");
    var zip = root.querySelector("[data-fgs-download-zip]");
    var help = root.querySelector("[data-fgs-help]");
    if (FGS.isIOS()) {
      if (all) all.classList.add("d-none");
      if (zip) zip.classList.replace("btn-outline-light", "btn-light");
      if (help) help.classList.remove("d-none");
      var photos = root.querySelector("[data-fgs-photos]");
      if (photos && navigator.canShare) {
        photos.classList.remove("d-none");
        if (zip) zip.classList.replace("btn-light", "btn-outline-light");
        photos.addEventListener("click", function () {
          var links = Array.prototype.slice.call(document.querySelectorAll("#collectiontable a[data-fileid]"));
          // These are the counted download URLs: saving to Photos is a download.
          FGS.saveToPhotos(links.map(function (a) { return { url: a.href, name: a.dataset.filename || "" }; }), photos);
        });
      }
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

  // Save to Photos, in two taps: navigator.share needs a fresh tap, and a
  // share() after fetching large originals is likely to be rejected by Safari.
  // Tap 1 fetches the originals ("Preparing n/N"); tap 2 opens the share sheet,
  // where "Save N Images" puts them straight into Photos. Always pass counted
  // download URLs (/cd), never view or video URLs. Batching and the video limit
  // are refined on-device in Part 3.
  var PHOTO_BATCH = 20;
  var VIDEO_MAX_BYTES = 500e6; // larger videos go to the zip instead
  var MIME_BY_EXT = { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", gif: "image/gif",
    webp: "image/webp", heic: "image/heic", heif: "image/heif", mov: "video/quicktime",
    mp4: "video/mp4", m4v: "video/x-m4v" };

  function photoMime(name, blobType) {
    // /cd serves downloads as octet-stream; Photos needs a real media type
    if (blobType && blobType !== "application/octet-stream") return blobType;
    var m = /\.([a-z0-9]+)$/i.exec(name || "");
    return (m && MIME_BY_EXT[m[1].toLowerCase()]) || blobType || "application/octet-stream";
  }

  FGS.saveToPhotos = function (list, button) {
    if (!navigator.canShare) { FGS.openHelpSheet(); return; }
    if (button._fgsLabel === undefined) button._fgsLabel = button.innerHTML;
    var state = button._fgs || (button._fgs = { files: null, next: 0 });
    if (state.busy) return;

    if (!state.files) {
      // Items without a url (password or client-side encrypted files) cannot be fetched
      var items = (list || []).filter(function (it) { return it && it.url; });
      var noUrl = (list || []).length - items.length;
      if (!items.length) {
        if (noUrl) alert(noUrl + " file(s) can't be fetched here. Use Download as .zip for those.");
        return;
      }
      state.busy = true;
      button.disabled = true;
      var done = 0, files = [], bigVideos = 0, failed = 0;
      button.textContent = "Preparing 0/" + items.length + "…";
      var tick = function () { done++; button.textContent = "Preparing " + done + "/" + items.length + "…"; };
      items.reduce(function (p, it) {
        return p.then(function () {
          return fetch(it.url, { credentials: "same-origin" }).then(function (r) {
            // An error page must never be wrapped up as a photo
            if (!r.ok) { failed++; return null; }
            var len = parseInt(r.headers.get("Content-Length") || "0", 10);
            if (/\.(mov|mp4|m4v)$/i.test(it.name) && len > VIDEO_MAX_BYTES) {
              bigVideos++;
              if (r.body && r.body.cancel) r.body.cancel();
              return null;
            }
            return r.blob();
          }).then(function (b) {
            if (b) files.push(new File([b], it.name, { type: photoMime(it.name, b.type) }));
            tick();
          }, function () { failed++; tick(); });
        });
      }, Promise.resolve()).then(function () {
        state.busy = false;
        button.disabled = false;
        var notes = [];
        if (bigVideos) notes.push(bigVideos + " large video(s) were left out");
        if (failed) notes.push(failed + " file(s) could not be fetched");
        if (noUrl) notes.push(noUrl + " file(s) can't be fetched here");
        if (!files.length) {
          // Nothing usable: back to the first tap so they can retry
          button._fgs = null;
          button.innerHTML = button._fgsLabel;
        } else {
          state.files = files; state.next = 0;
          button.textContent = "Save to Photos" + (files.length > PHOTO_BATCH ? " (1/" + Math.ceil(files.length / PHOTO_BATCH) + ")" : "");
        }
        if (notes.length) alert(notes.join("; ") + ". Use Download as .zip for those" + (failed ? ", or try again." : "."));
      });
      return;
    }

    var batch = state.files.slice(state.next, state.next + PHOTO_BATCH);
    if (!navigator.canShare({ files: batch })) { FGS.openHelpSheet(); return; }
    state.busy = true;
    navigator.share({ files: batch }).then(function () {
      state.busy = false;
      state.next += PHOTO_BATCH;
      if (state.next >= state.files.length) { button.textContent = "Saved ✓"; button._fgs = null; return; }
      button.textContent = "Save to Photos (" + (state.next / PHOTO_BATCH + 1) + "/" + Math.ceil(state.files.length / PHOTO_BATCH) + ")";
    }, function (err) {
      // Cancelling the sheet is normal: keep the prepared files, they can tap again
      state.busy = false;
      if (!err || err.name !== "AbortError") FGS.openHelpSheet();
    });
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

  FGS.itemFromEl = function (el) {
    var d = function (k) { return el.getAttribute("data-" + k) || ""; };
    return { id: d("id"), name: d("name"), view: d("view"), video: d("video"), download: d("download"),
      uploader: d("uploader"), taken: parseInt(d("taken"), 10) || 0, uploaded: parseInt(d("uploaded"), 10) || 0,
      size: d("size"), w: parseInt(d("w"), 10) || 0, h: parseInt(d("h"), 10) || 0 };
  };

  var v = null, items = [], at = 0, touchX = null, returnFocus = null;
  function fmt(ts) { return ts ? new Date(ts * 1000).toLocaleString([], { dateStyle: "medium", timeStyle: "short" }) : ""; }

  // Stop a playing video and release its network connection before the element is dropped.
  function clearStage(stage) {
    var old = stage.querySelector("video");
    if (old) { old.pause(); old.removeAttribute("src"); old.load(); }
    stage.innerHTML = "";
  }

  function render() {
    var it = items[at], stage = v.querySelector(".fgs-v-stage");
    clearStage(stage);
    if (it.video) {
      var vid = document.createElement("video");
      vid.controls = true; vid.playsInline = true; vid.preload = "metadata";
      if (it.view) vid.poster = it.view;
      vid.src = it.video;
      vid.addEventListener("error", function () {
        if (items[at] !== it) return; // stale: the viewer has moved on
        // Browser cannot play this codec (e.g. HEVC in Firefox): poster + download.
        clearStage(stage);
        if (it.view) {
          var pimg = document.createElement("img"); pimg.src = it.view; pimg.alt = "";
          stage.appendChild(pimg);
        }
        var note = document.createElement("p"); note.className = "fgs-v-note";
        note.textContent = it.download
          ? "This browser can't play this video. Download the original to watch it."
          : "This browser can't play this video.";
        stage.appendChild(note);
      });
      stage.appendChild(vid);
    } else if (it.view) {
      var img = document.createElement("img"); img.src = it.view; img.alt = it.name;
      stage.appendChild(img);
    } else {
      var p = document.createElement("p"); p.className = "fgs-v-note"; p.textContent = "No preview for this file.";
      stage.appendChild(p);
    }
    v.querySelector(".fgs-v-name").textContent = it.name;
    var meta = [it.uploader, it.taken ? "Taken " + fmt(it.taken) : (it.uploaded ? "Uploaded " + fmt(it.uploaded) : ""), it.size].filter(Boolean);
    v.querySelector(".fgs-v-meta").textContent = meta.join(" \u00b7 ");
    // Password-protected and client-side-encrypted files have no direct download URL: hide the button rather than render a dead link.
    var dl = v.querySelector(".fgs-v-download");
    if (it.download) {
      dl.href = it.download; dl.setAttribute("download", it.name); dl.hidden = false;
    } else {
      dl.removeAttribute("href"); dl.hidden = true;
    }
    v.querySelector(".fgs-v-prev").disabled = at === 0;
    v.querySelector(".fgs-v-next").disabled = at === items.length - 1;
    // Warm the neighbour so arrowing feels instant
    [at - 1, at + 1].forEach(function (i) { if (items[i] && items[i].view) { (new Image()).src = items[i].view; } });
  }
  function go(delta) { var n = at + delta; if (n >= 0 && n < items.length) { at = n; render(); } }
  function close() {
    v.hidden = true; clearStage(v.querySelector(".fgs-v-stage")); document.body.style.overflow = "";
    var back = returnFocus; returnFocus = null;
    if (back && back.focus && document.contains(back)) back.focus();
  }

  function bindTouch() {
    var stage = v.querySelector(".fgs-v-stage");
    stage.addEventListener("touchstart", function (e) {
      // Leave video gestures (scrubbing, pinch) alone
      touchX = (e.touches.length > 1 || (e.target.closest && e.target.closest("video"))) ? null : e.touches[0].clientX;
    }, { passive: true });
    stage.addEventListener("touchend", function (e) {
      if (touchX === null) return;
      var dx = e.changedTouches[0].clientX - touchX; touchX = null;
      if (Math.abs(dx) > 50) go(dx < 0 ? 1 : -1);
    }, { passive: true });
    stage.addEventListener("touchcancel", function () { touchX = null; }, { passive: true });
  }

  FGS.openViewer = function (list, index) {
    if (!v) {
      v = document.getElementById("fgs-viewer");
      if (v) bindTouch();
    }
    if (!v || !list || !list.length) return;
    if (v.hidden) returnFocus = document.activeElement;
    items = list; at = Math.min(Math.max(0, index | 0), list.length - 1);
    v.hidden = false; document.body.style.overflow = "hidden";
    render();
    v.querySelector(".fgs-v-close").focus();
  };
  document.addEventListener("click", function (e) {
    if (!v || v.hidden) return;
    if (e.target.closest(".fgs-v-close")) close();
    else if (e.target.closest(".fgs-v-prev")) go(-1);
    else if (e.target.closest(".fgs-v-next")) go(1);
  });
  document.addEventListener("keydown", function (e) {
    if (!v || v.hidden) return;
    if (e.key === "Escape") { close(); return; }
    if (e.key === "Tab") {
      // Keep focus inside the dialog
      var f = Array.prototype.filter.call(v.querySelectorAll("button, a[href], video[controls]"),
        function (x) { return !x.disabled && !x.hidden; });
      if (!f.length) { e.preventDefault(); return; }
      var i = f.indexOf(document.activeElement);
      if (i === -1) { e.preventDefault(); f[0].focus(); }
      else if (e.shiftKey && i === 0) { e.preventDefault(); f[f.length - 1].focus(); }
      else if (!e.shiftKey && i === f.length - 1) { e.preventDefault(); f[0].focus(); }
      return;
    }
    // Arrow keys on a focused video seek; leave them to the player.
    if (e.target && e.target.tagName === "VIDEO") return;
    if (e.key === "ArrowLeft") go(-1);
    if (e.key === "ArrowRight") go(1);
  });

  // Masonry by shortest column. Heights come from stored dimensions, so the
  // whole board is placed before any image arrives and nothing jumps.
  FGS.initBoard = function (board) {
    if (!board || board.dataset.ready) return;
    board.dataset.ready = "1";
    var tiles = Array.prototype.slice.call(board.querySelectorAll(".fgs-board-item"));
    function layout() {
      var width = board.clientWidth, gap = 8;
      if (!width) return; // hidden: nothing sensible to compute, and a later show relays out
      var cols = Math.max(2, Math.round(width / 260));
      var colW = (width - gap * (cols - 1)) / cols, heights = [];
      for (var i = 0; i < cols; i++) heights.push(0);
      tiles.forEach(function (t) {
        var w = parseInt(t.dataset.w, 10), h = parseInt(t.dataset.h, 10);
        var tileH = (w && h) ? colW * h / w : colW;
        var c = heights.indexOf(Math.min.apply(null, heights));
        var st = t.style;
        st.position = "absolute"; st.width = colW + "px"; st.height = tileH + "px";
        st.left = (c * (colW + gap)) + "px"; st.top = heights[c] + "px";
        heights[c] += tileH + gap;
      });
      board.style.height = Math.max.apply(null, heights) + "px";
    }
    board._fgsLayout = layout;
    function load(img, eager) {
      if (eager) img.setAttribute("fetchpriority", "high");
      img.src = img.dataset.src;
    }
    var imgs = tiles.map(function (t) { return t.querySelector("img[data-src]"); }).filter(Boolean);
    imgs.slice(0, 12).forEach(function (img) { load(img, true); });
    if ("IntersectionObserver" in window) {
      var io = new IntersectionObserver(function (entries) {
        entries.forEach(function (e) { if (e.isIntersecting) { load(e.target, false); io.unobserve(e.target); } });
      }, { rootMargin: "1500px 0px" });
      imgs.slice(12).forEach(function (img) { io.observe(img); });
    } else {
      imgs.slice(12).forEach(function (img) { load(img, false); });
    }
    board.addEventListener("click", function (e) {
      var t = e.target.closest(".fgs-board-item");
      if (t) FGS.openViewer(tiles.map(function (x) { return FGS.itemFromEl(x); }), tiles.indexOf(t));
    });
    layout();
    var timer;
    function later() { clearTimeout(timer); timer = setTimeout(layout, 120); }
    if ("ResizeObserver" in window) new ResizeObserver(later).observe(board);
    else window.addEventListener("resize", later);
  };

  // Board/List toggle, remembered per page type.
  FGS.initLayoutToggle = function (toggle, board, list, key) {
    if (!toggle || !board || !list) return;
    function store(layout) { try { localStorage.setItem(key, layout); } catch (e) { /* private mode */ } }
    function apply(layout) {
      board.classList.toggle("d-none", layout !== "board");
      list.classList.toggle("d-none", layout !== "list");
      toggle.querySelectorAll("[data-layout]").forEach(function (b) {
        var on = b.dataset.layout === layout;
        b.classList.toggle("btn-light", on); b.classList.toggle("btn-outline-light", !on);
      });
      if (layout === "board") { FGS.initBoard(board); if (board._fgsLayout) board._fgsLayout(); }
      store(layout);
    }
    toggle.addEventListener("click", function (e) { var b = e.target.closest("[data-layout]"); if (b) apply(b.dataset.layout); });
    var saved = null; try { saved = localStorage.getItem(key); } catch (e) { /* private mode */ }
    apply(saved === "board" || saved === "list" ? saved : toggle.dataset.layoutDefault);
  };
})();
