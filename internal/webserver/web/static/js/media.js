// Shared media front end: device-specific download buttons, help sheet,
// and (later tasks) viewer, board and Save to Photos. No dependencies.
(function () {
  "use strict";
  var FGS = window.FGS = window.FGS || {};

  FGS.isIOS = function () {
    return /iPad|iPhone|iPod/.test(navigator.userAgent) ||
      (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  };

  // Every file on a collection page, in list order, as the counted /cd
  // download URLs: saving to Photos is a download.
  function collectionDownloads() {
    return Array.prototype.map.call(document.querySelectorAll("[data-fgs-browser] .fgs-tile"), function (t) {
      var url = t.getAttribute("data-download");
      return { url: url ? new URL(url, window.location.href).href : "", name: t.getAttribute("data-name") || "",
        bytes: parseInt(t.getAttribute("data-bytes"), 10) };
    }).filter(function (it) { return it.url; });
  }

  // Desktop: "Download all" fires each file in turn (unchanged behaviour).
  // iOS: Safari allows one download per tap, so Save to Photos is primary and
  // the zip is the fallback (the zip alone where navigator.canShare is missing).
  FGS.initDownloadButtons = function (root) {
    if (!root) return;
    var all = root.querySelector("[data-fgs-download-all]");
    var zip = root.querySelector("[data-fgs-download-zip]");
    var help = root.querySelector("[data-fgs-help]");
    var status = root.querySelector("#downloadAllStatus");
    if (all) {
      if (!collectionDownloads().length) all.style.display = "none";
      all.addEventListener("click", function () {
        FGS.downloadSequentially(collectionDownloads(), { button: all, status: status });
      });
    }
    if (FGS.isIOS()) {
      if (all) all.classList.add("d-none");
      if (zip) zip.classList.replace("btn-outline-light", "btn-light");
      if (help) help.classList.remove("d-none");
      var photos = root.querySelector("[data-fgs-photos]");
      if (photos && navigator.canShare) {
        photos.classList.remove("d-none");
        if (zip) zip.classList.replace("btn-light", "btn-outline-light");
        photos.addEventListener("click", function () {
          FGS.saveToPhotos(collectionDownloads(), photos);
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
  // where "Save N Images" puts them straight into Photos. Always pass the
  // original-file download URLs (/cd on a board, counted; /files/download on
  // the Files tab, uncounted), never view or video URLs. Batching and the video limit
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

  var PREPARE_MAX_BYTES = 1.5e9;  // per prepare tap; the rest goes in another batch or the zip
  var VIDEO_RE = /\.(mov|mp4|m4v)$/i;

  // Forget any prepared set and put the button back to its first-tap label.
  // Called when the selection changes, and on unexpected failure.
  FGS.resetSaveToPhotos = function (button) {
    if (!button) return;
    if (button._fgs) button._fgs.cancelled = true;
    button._fgs = null;
    if (button._fgsTimer) { window.clearTimeout(button._fgsTimer); button._fgsTimer = null; }
    button.disabled = false;
    if (button._fgsLabel !== undefined) button.innerHTML = button._fgsLabel;
  };

  // items: [{url, name, bytes}]. bytes is the stored size and is what decides
  // what is fetched: /cd counts a download before it sends a body, so a file
  // that can't be shared must be ruled out BEFORE the request, never after.
  FGS.saveToPhotos = function (list, button) {
    if (!navigator.canShare) { FGS.openHelpSheet(); return; }
    if (button._fgsLabel === undefined) button._fgsLabel = button.innerHTML;
    var state = button._fgs || (button._fgs = { files: null, next: 0 });
    if (state.busy) return;
    if (button._fgsTimer) { window.clearTimeout(button._fgsTimer); button._fgsTimer = null; }

    if (!state.files) {
      // Spend no download on files this browser can't share anyway
      var probeOk = false;
      try { probeOk = navigator.canShare({ files: [new File([""], "probe.jpg", { type: "image/jpeg" })] }); } catch (e) { /* unsupported */ }
      if (!probeOk) { button._fgs = null; FGS.openHelpSheet(); return; }

      // Items without a url (password or client-side encrypted files) cannot be fetched
      var all = list || [];
      var noUrl = 0, bigVideos = 0, overCap = 0, failed = 0, total = 0;
      var items = [];
      all.forEach(function (it) {
        if (!it || !it.url) { noUrl++; return; }
        var known = typeof it.bytes === "number" && it.bytes >= 0;
        if (known && VIDEO_RE.test(it.name) && it.bytes > VIDEO_MAX_BYTES) { bigVideos++; return; }
        if (known) {
          if (total + it.bytes > PREPARE_MAX_BYTES && items.length) { overCap++; return; }
          total += it.bytes;
        }
        items.push(it);
      });
      function report() {
        var notes = [];
        if (bigVideos) notes.push(bigVideos + " large video(s) were left out");
        if (overCap) notes.push(overCap + " file(s) didn't fit in one go");
        if (failed) notes.push(failed + " file(s) could not be fetched");
        if (noUrl) notes.push(noUrl + " file(s) can't be fetched here");
        if (notes.length) alert(notes.join("; ") + ". Use Download as .zip for those" + (failed ? ", or try again." : "."));
      }
      if (!items.length) { button._fgs = null; report(); return; }

      state.busy = true;
      button.disabled = true;
      var done = 0, files = [], fetched = 0;
      button.textContent = "Preparing 0/" + items.length + "…";
      var tick = function () { if (state.cancelled) return; done++; button.textContent = "Preparing " + done + "/" + items.length + "…"; };
      items.reduce(function (p, it) {
        return p.then(function () {
          if (state.cancelled) return null;
          var known = typeof it.bytes === "number" && it.bytes >= 0;
          // Size unknown: stop before a fetch that would pass the cap, so the
          // rest is left for another batch
          if (!known && fetched >= PREPARE_MAX_BYTES) { overCap++; return null; }
          return fetch(it.url, { credentials: "same-origin" }).then(function (r) {
            // An error page must never be wrapped up as a photo
            if (!r.ok) { failed++; return null; }
            // Fallback only (size unknown): by now the request has been counted
            var len = parseInt(r.headers.get("Content-Length") || "0", 10);
            if (!known && VIDEO_RE.test(it.name) && len > VIDEO_MAX_BYTES) {
              bigVideos++;
              if (r.body && r.body.cancel) r.body.cancel();
              return null;
            }
            return r.blob();
          }).then(function (b) {
            if (b) { fetched += b.size; files.push(new File([b], it.name, { type: photoMime(it.name, b.type) })); }
          }).catch(function () { failed++; }).then(tick);
        });
      }, Promise.resolve()).catch(function () {
        failed++;
      }).then(function () {
        if (state.cancelled) return;  // selection changed meanwhile: drop it all
        state.busy = false;
        button.disabled = false;
        if (!files.length) {
          // Nothing usable: back to the first tap so they can retry
          button._fgs = null;
          button.innerHTML = button._fgsLabel;
        } else {
          state.files = files; state.next = 0;
          button.textContent = "Save to Photos" + (files.length > PHOTO_BATCH ? " (1/" + Math.ceil(files.length / PHOTO_BATCH) + ")" : "");
        }
        report();
      }).catch(function () {
        // Anything unexpected: leave the button usable rather than stuck
        if (!state.cancelled) FGS.resetSaveToPhotos(button);
      });
      return;
    }

    var batch = state.files.slice(state.next, state.next + PHOTO_BATCH);
    if (!navigator.canShare({ files: batch })) { FGS.openHelpSheet(); return; }
    state.busy = true;
    navigator.share({ files: batch }).then(function () {
      state.busy = false;
      if (state.cancelled) return;  // selection changed while the sheet was open
      state.next += PHOTO_BATCH;
      if (state.next >= state.files.length) {
        button.textContent = "Saved ✓"; button._fgs = null;
        // Back to the first-tap label, so a later tap reads as a new action
        button._fgsTimer = window.setTimeout(function () {
          button._fgsTimer = null;
          if (!button._fgs) button.innerHTML = button._fgsLabel;
        }, 2000);
        return;
      }
      button.textContent = "Save to Photos (" + (state.next / PHOTO_BATCH + 1) + "/" + Math.ceil(state.files.length / PHOTO_BATCH) + ")";
    }, function (err) {
      // Cancelling the sheet is normal: keep the prepared files, they can tap again
      state.busy = false;
      if (state.cancelled) return;
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
    return { id: d("id"), name: d("name"), view: d("view"), video: d("video"), kind: d("kind"), download: d("download"),
      uploader: d("uploader"), taken: parseInt(d("taken"), 10) || 0, uploaded: parseInt(d("uploaded"), 10) || 0,
      size: d("size"), bytes: parseInt(d("bytes"), 10), w: parseInt(d("w"), 10) || 0, h: parseInt(d("h"), 10) || 0 };
  };

  var v = null, items = [], at = 0, touchX = null, returnFocus = null;
  // Backdrop-click bookkeeping: where the press began, and when a swipe last ended
  var downOnBackdrop = true, touchStart = null, swipeUntil = 0;
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
    // Files without a direct download URL (end-to-end encrypted on the Files tab): hide the button rather than render a dead link.
    var dl = v.querySelector(".fgs-v-download");
    if (it.download) {
      dl.href = it.download; dl.setAttribute("download", it.name); dl.hidden = false;
    } else {
      dl.removeAttribute("href"); dl.hidden = true;
    }
    // Photos are shown as a smaller preview; say so wherever the original can be fetched.
    // Kind, not the stream URL: a capped video withholds the stream but still shows only a poster.
    v.querySelector(".fgs-v-lowres").hidden = !(it.view && it.kind === "image" && it.download);
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
      // Remember where every touch began, so a swipe is never mistaken for a click
      touchStart = e.touches.length === 1 ? { x: e.touches[0].clientX, y: e.touches[0].clientY, moved: false } : null;
      // Leave video gestures (scrubbing, pinch) alone
      touchX = (e.touches.length > 1 || (e.target.closest && e.target.closest("video"))) ? null : e.touches[0].clientX;
    }, { passive: true });
    stage.addEventListener("touchmove", function (e) {
      if (touchStart && !touchStart.moved && e.touches.length === 1 &&
          Math.hypot(e.touches[0].clientX - touchStart.x, e.touches[0].clientY - touchStart.y) > 10) touchStart.moved = true;
    }, { passive: true });
    stage.addEventListener("touchend", function (e) {
      // A touch that moved more than ~10 px was a swipe: drop the click that follows it
      if (touchStart && touchStart.moved) swipeUntil = Date.now() + 500;
      touchStart = null;
      if (touchX === null) return;
      var dx = e.changedTouches[0].clientX - touchX; touchX = null;
      if (Math.abs(dx) > 50) go(dx < 0 ? 1 : -1);
    }, { passive: true });
    stage.addEventListener("touchcancel", function () { touchX = null; touchStart = null; }, { passive: true });
  }

  // Desktop trackpad: a horizontal two-finger swipe moves one item. Inertial
  // scrolling keeps emitting events for a second or so, so accumulate to a
  // threshold, then ignore input for a short lockout: one gesture, one item.
  var wheelSum = 0, wheelLast = 0, wheelLockUntil = 0;
  function bindWheel() {
    v.addEventListener("wheel", function (e) {
      if (v.hidden) return;
      // Pinch-zoom arrives as ctrl+wheel: not a swipe.
      if (e.ctrlKey) return;
      // Vertical-dominant: leave to the browser (nothing scrolls under a fixed viewer anyway).
      if (Math.abs(e.deltaX) <= Math.abs(e.deltaY)) return;
      // Horizontal: never let the browser turn it into back/forward navigation.
      e.preventDefault();
      // Over a video the swipe may be a scrub; swallow it without navigating.
      if (e.target.closest && e.target.closest("video")) return;
      var now = Date.now();
      // While locked, every event extends the lock: the gesture (and its inertial
      // tail) ends only after ~150 ms of silence, so one swipe is one item.
      if (now < wheelLockUntil) { wheelLockUntil = Math.max(wheelLockUntil, now + 150); return; }
      // Normalise to pixels (line and page modes come from mice and some browsers).
      var dx = e.deltaX * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? window.innerWidth : 1);
      // A pause, or a change of direction, starts a new gesture.
      if (now - wheelLast > 200 || (wheelSum && (wheelSum > 0) !== (dx > 0))) wheelSum = 0;
      wheelLast = now;
      wheelSum += dx;
      if (Math.abs(wheelSum) >= 80) {
        var dir = wheelSum > 0 ? 1 : -1;
        wheelSum = 0; wheelLockUntil = now + 350;
        go(dir);
      }
    }, { passive: false });
  }

  FGS.openViewer = function (list, index) {
    if (!v) {
      v = document.getElementById("fgs-viewer");
      if (v) {
        bindTouch(); bindWheel();
        // iOS Safari dispatches click on a non-interactive element (the backdrop,
        // the bare stage) only if it or an ancestor below the document has a
        // click listener. The close itself is decided in the document handler.
        v.addEventListener("click", function () {});
      }
    }
    if (!v || !list || !list.length) return;
    if (v.hidden) returnFocus = document.activeElement;
    items = list; at = Math.min(Math.max(0, index | 0), list.length - 1);
    wheelSum = 0; wheelLockUntil = 0;
    v.hidden = false; document.body.style.overflow = "hidden";
    render();
    v.querySelector(".fgs-v-close").focus();
  };
  // Empty space: the backdrop, the bare stage, or any other wrapper that is not
  // the picture, a control, a link, the caption bar or a note.
  function isEmptySpace(el) {
    if (!v || !el || !v.contains(el)) return false;
    if (el === v) return true;
    return !(el.closest && el.closest("img, video, button, a, .fgs-v-info, .fgs-v-note"));
  }
  // A press that starts on the picture and is released on the backdrop (a drag)
  // makes the browser click their common ancestor: only close if the press began in empty space too.
  document.addEventListener("pointerdown", function (e) {
    downOnBackdrop = isEmptySpace(e.target);
  }, true);
  document.addEventListener("click", function (e) {
    if (!v || v.hidden) return;
    var pressedOnBackdrop = downOnBackdrop; downOnBackdrop = true;
    if (e.target.closest(".fgs-v-close")) close();
    else if (e.target.closest(".fgs-v-prev")) go(-1);
    else if (e.target.closest(".fgs-v-next")) go(1);
    else if (pressedOnBackdrop && Date.now() >= swipeUntil && isEmptySpace(e.target)) close();
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
  // Places tiles in the shortest column, sized from their data-w / data-h
  // (square without them). Returns false while the container is hidden:
  // nothing sensible to compute, and a later show lays out again.
  function masonry(container, tiles) {
    var width = container.clientWidth, gap = 8;
    if (!width) return false;
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
    container.style.height = Math.max.apply(null, heights) + "px";
    return true;
  }

  // ---------------------------------------------------------------------
  // The shared file list (html_file_browser.tmpl): Grid, Board and List over
  // one set of tiles, Select mode, the selection bar, the viewer in visible
  // order, live thumbnails and live adds. Pages differ only in their context
  // (data-context) and the actions it allows. Management actions are not
  // here: the admin bundle supplies them through FGS.registerFileActions.
  // ---------------------------------------------------------------------

  // action key -> handler, for every context or for one context. A handler
  // receives one argument:
  //   { browser, root, context, tiles, ids, items, bulk, button, trigger,
  //     menu, status, linkResult }
  // tiles are the files' grid tiles (they carry every data-* attribute),
  // items the FGS.itemFromEl of each, bulk is true for the selection bar,
  // button the element clicked, trigger the "⋯" button that opened the menu
  // (focus goes back to it), menu the file's [data-fgs-file-menu] element.
  // A registered handler wins over the built-in open / download / zip /
  // photos, and a context's own handler wins over one for every context.
  var fileActions = { all: {}, byContext: {} };
  FGS.registerFileActions = function (map, context) {
    var target = fileActions.all;
    if (context) target = fileActions.byContext[context] = fileActions.byContext[context] || {};
    Object.keys(map || {}).forEach(function (k) { target[k] = map[k]; });
  };
  function fileAction(context, key) {
    var own = fileActions.byContext[context];
    return (own && own[key]) || fileActions.all[key] || null;
  }

  var POLL_EVERY_MS = 3000, POLL_FOR_MS = 120000, POLL_BATCH = 8;

  // The icon a file without a thumbnail gets, by extension. Must equal
  // fileTypeIconGroups in Files.go (TestFileTypeIconsMatchJs checks).
  // FILE_TYPE_ICONS_BEGIN
  var FILE_TYPE_ICONS = {
    "bi-file-earmark-pdf": "pdf",
    "bi-file-earmark-image": "jpg jpeg png gif webp heic heif avif bmp tif tiff svg dng raw cr2 nef arw",
    "bi-file-earmark-play": "mp4 mov m4v mkv webm avi wmv mpg mpeg 3gp",
    "bi-file-earmark-music": "mp3 m4a wav flac aac ogg opus aiff",
    "bi-file-earmark-zip": "zip rar 7z tar gz tgz bz2 xz zst dmg iso",
    "bi-file-earmark-text": "txt md rtf log",
    "bi-file-earmark-word": "doc docx odt pages",
    "bi-file-earmark-spreadsheet": "xls xlsx ods csv numbers",
    "bi-file-earmark-slides": "ppt pptx odp key",
    "bi-file-earmark-code": "go js ts py c h cpp java rs sh html css json xml yaml yml sql"
  };
  // FILE_TYPE_ICONS_END
  var iconByExtension = {};
  Object.keys(FILE_TYPE_ICONS).forEach(function (icon) {
    FILE_TYPE_ICONS[icon].split(" ").forEach(function (ext) { iconByExtension[ext] = icon; });
  });
  function fileTypeIcon(name) {
    var m = /\.([^.]+)$/.exec(name || "");
    return (m && iconByExtension[m[1].toLowerCase()]) || "bi-file-earmark";
  }
  var VIEWS = ["grid", "board", "list"];

  function setData(el, name, value) {
    if (el) el.setAttribute("data-" + name, value === undefined || value === null ? "" : String(value));
  }
  function formatTimes(scope) {
    Array.prototype.forEach.call(scope.querySelectorAll("[data-ts]"), function (el) {
      var ts = parseInt(el.getAttribute("data-ts"), 10);
      el.textContent = ts ? fmt(ts) : "";
    });
  }

  FGS.initFileBrowser = function (root) {
    if (!root) return null;
    if (root.fgsBrowser) return root.fgsBrowser;
    var context = root.getAttribute("data-context") || "";
    var grid = root.querySelector("[data-fgs-grid]");
    var listWrap = root.querySelector("[data-fgs-list]");
    var table = listWrap.querySelector("table");
    var tbody = table.tBodies[0];
    var bar = root.querySelector("[data-fgs-selectbar]");
    var selCount = bar.querySelector(".fgs-selcount");
    var status = bar.querySelector(".fgs-selstatus");
    var linkResult = bar.querySelector(".fgs-linkresult");
    var selectToggle = root.querySelector("[data-fgs-select-toggle]");
    var countEl = root.querySelector(".fgs-count");
    var size = root.querySelector(".fgs-size");
    var tplTile = root.querySelector("template[data-fgs-tpl=tile]");
    var tplRow = root.querySelector("template[data-fgs-tpl=row]");
    var infoUrl = root.getAttribute("data-info-url") || "";
    var zipUrl = root.getAttribute("data-zip-url") || "";
    var manages = !!(tplTile && tplTile.content.querySelector("[data-menu=edit]"));
    // The upload page's element ids, only where the page holds one browser
    var legacyIds = root.getAttribute("data-legacy-ids") === "true";
    var selecting = false, view = null, scope = "all", dataTable = null, api = null;
    var tilesById = {}, rowsById = {};

    // Choices are remembered per context. The Files tab keeps the choices
    // made before it moved onto this component.
    var legacyKeys = context === "files" ? { view: "filesLayout", shape: "filesTileShape", size: "filesIconSize", scope: "filesScope" } : {};
    function load(k) {
      try {
        var v = localStorage.getItem("fgsBrowser." + context + "." + k);
        if (v === null && legacyKeys[k]) v = localStorage.getItem(legacyKeys[k]);
        return v;
      } catch (e) { return null; }
    }
    function store(k, v) { try { localStorage.setItem("fgsBrowser." + context + "." + k, v); } catch (e) { /* private mode */ } }

    function setActive(attr, value) {
      root.querySelectorAll("button[" + attr + "]").forEach(function (b) {
        if (!root.contains(b) || b.closest("[data-fgs-browser]") !== root) return;
        var on = b.getAttribute(attr) === value;
        b.classList.toggle("btn-light", on);
        b.classList.toggle("btn-outline-light", !on);
      });
    }

    function tileCheck(tile) { return tile.querySelector(".fgs-tile-check"); }
    function rowCheck(row) { return row && row.querySelector(".fgs-row-check"); }
    function idOf(el) { return el.getAttribute("data-id"); }
    function menuOf(tile) { return tile.querySelector("[data-fgs-file-menu]"); }

    // id -> tile and list row, collected before DataTables can detach a row
    function index(tile, row) {
      tilesById[idOf(tile)] = tile;
      if (row) rowsById[row.getAttribute("data-row-id")] = row;
    }
    Array.prototype.forEach.call(grid.querySelectorAll(".fgs-tile"), function (t) { index(t, null); });
    Array.prototype.forEach.call(tbody.querySelectorAll("tr[data-row-id]"), function (tr) {
      rowsById[tr.getAttribute("data-row-id")] = tr;
    });

    // The files the user sees, in the order they see them: Grid and Board in
    // page order, List in the table's current sort and filter. Hidden by the
    // scope switch or deleted since: left out.
    function visibleTiles() {
      if (view === "list") {
        var rows = dataTable ? dataTable.rows({ order: "applied", search: "applied" }).nodes().toArray()
          : Array.prototype.slice.call(tbody.rows);
        return rows.filter(function (tr) { return !tr.classList.contains("d-none"); })
          .map(function (tr) { return tilesById[tr.getAttribute("data-row-id")]; })
          .filter(function (t) { return t && t.isConnected; });
      }
      return Array.prototype.filter.call(grid.children, function (t) {
        return t.classList.contains("fgs-tile") && !t.classList.contains("d-none");
      });
    }
    function selected() { return visibleTiles().filter(function (t) { return tileCheck(t).checked; }); }

    function refreshCount() {
      var all = Object.keys(tilesById).length;
      var shown = Array.prototype.filter.call(grid.children, function (t) {
        return t.classList.contains("fgs-tile") && !t.classList.contains("d-none");
      }).length;
      if (countEl) countEl.textContent = (root.getAttribute("data-count-label") || "Files: ") + shown;
      root.classList.toggle("fgs-is-empty", all === 0);
    }

    function updateSelection() {
      var n = selected().length;
      bar.classList.toggle("d-none", !selecting);
      selCount.textContent = n + " selected";
      // Greyed out rather than disabled: some actions disable their own
      // button while they run, and a selection change must not undo that
      bar.classList.toggle("fgs-none-selected", n === 0);
      // A prepared set, or a link made from it, belongs to its selection
      var photos = bar.querySelector("[data-act=photos]");
      if (photos) FGS.resetSaveToPhotos(photos);
      if (linkResult) { linkResult.textContent = ""; linkResult.className = "fgs-linkresult"; }
    }

    function setChecked(id, on) {
      var tile = tilesById[id];
      if (tile) tileCheck(tile).checked = on;
      var check = rowCheck(rowsById[id]);
      if (check) check.checked = on;
    }
    function toggleSelected(tile) {
      setChecked(idOf(tile), !tileCheck(tile).checked);
      status.textContent = "";
      updateSelection();
    }
    function setSelecting(on) {
      selecting = !!on;
      root.classList.toggle("fgs-selecting", selecting);
      selectToggle.textContent = selecting ? "Done" : "Select";
      selectToggle.setAttribute("aria-pressed", selecting ? "true" : "false");
      selectToggle.classList.toggle("btn-light", selecting);
      selectToggle.classList.toggle("btn-outline-light", !selecting);
      // Leaving select mode drops the selection
      if (!selecting) Object.keys(tilesById).forEach(function (id) { setChecked(id, false); });
      status.textContent = "";
      updateSelection();
      // The checkbox column appeared or went away
      if (dataTable && view === "list") dataTable.columns.adjust();
    }

    // The viewer, over everything visible, starting at this file
    function openAt(tile) {
      if (!tile) return;
      var tiles = visibleTiles();
      FGS.openViewer(tiles.map(FGS.itemFromEl), Math.max(0, tiles.indexOf(tile)));
    }

    // Board: the grid's own tiles in masonry, true proportions, no captions
    function layoutBoard() {
      if (view !== "board") return;
      masonry(grid, visibleTiles());
    }
    function clearBoard() {
      Array.prototype.forEach.call(grid.querySelectorAll(".fgs-tile"), function (t) {
        ["position", "width", "height", "left", "top"].forEach(function (p) { t.style[p] = ""; });
      });
      grid.style.height = "";
    }
    var boardTimer = null;
    function layoutLater() { window.clearTimeout(boardTimer); boardTimer = window.setTimeout(layoutBoard, 120); }
    if ("ResizeObserver" in window) new ResizeObserver(layoutLater).observe(grid);
    else window.addEventListener("resize", layoutLater);

    function applyView(v) {
      if (VIEWS.indexOf(v) < 0) v = root.getAttribute("data-default-view") || "grid";
      var was = view;
      view = v;
      root.setAttribute("data-current-view", v);
      grid.classList.toggle("d-none", v === "list");
      listWrap.classList.toggle("d-none", v !== "list");
      grid.classList.toggle("fgs-board-mode", v === "board");
      if (was === "board" && v !== "board") clearBoard();
      setActive("data-layout", v);
      store("view", v);
      if (dataTable) {
        // The tiles ignore the table's search box, so a search must not keep
        // rows (and the ids the upload page's helpers look up) out of the DOM
        // while the tiles are showing
        if (v !== "list" && dataTable.search() !== "") dataTable.search("").draw();
        // The table was laid out while hidden, so its column widths are wrong
        if (v === "list") dataTable.columns.adjust();
      }
      layoutBoard();
      updateSelection();
    }

    // Only users with PERM_LIST get the switch, and only they are ever sent
    // other users' files. Without the switch everything shown is their own.
    function hideForScope(el) {
      el.classList.toggle("d-none", scope === "mine" && el.getAttribute("data-mine") !== "true");
    }
    function applyScope(s) {
      if (!root.querySelector("button[data-scope]")) { scope = "all"; return; }
      scope = s === "all" ? "all" : "mine";
      Object.keys(tilesById).forEach(function (id) { hideForScope(tilesById[id]); });
      Object.keys(rowsById).forEach(function (id) { hideForScope(rowsById[id]); });
      setActive("data-scope", scope);
      store("scope", scope);
      refreshCount();
      layoutBoard();
      updateSelection();
    }

    // Square crops every thumbnail to fill its tile; True shape shows the
    // whole picture, letterboxed. Grid only: Board always keeps proportions.
    function applyShape(shape) {
      if (shape !== "true") shape = "square";
      grid.classList.toggle("fgs-shape-true", shape === "true");
      setActive("data-shape", shape);
      store("shape", shape);
    }
    function applySize() {
      grid.style.setProperty("--fgs-tile", size.value + "px");
      store("size", size.value);
    }

    function asItems(tiles) {
      return tiles.map(function (t) {
        return { url: t.getAttribute("data-download"), name: t.getAttribute("data-name"),
          bytes: parseInt(t.getAttribute("data-bytes"), 10) };
      });
    }

    function actionArg(tiles, button, bulk, menu, trigger) {
      return { browser: api, root: root, context: context, tiles: tiles, ids: tiles.map(idOf),
        items: tiles.map(FGS.itemFromEl), bulk: bulk, button: button, trigger: trigger || button,
        menu: menu || (tiles.length === 1 ? menuOf(tiles[0]) : null), status: status, linkResult: linkResult };
    }

    // Built-in selection-bar actions: the ones visitors have too
    function bulkDownload(tiles, button) {
      // Files without a link (end-to-end encrypted on the Files tab) can only
      // be decrypted on their own download page; the zip skips them too, so
      // fetch the rest and say what was left out
      var usable = tiles.filter(function (t) { return t.getAttribute("data-download"); });
      var skipped = tiles.length - usable.length;
      if (usable.length) FGS.downloadSequentially(asItems(usable), { button: button, status: status });
      if (skipped) {
        window.setTimeout(function () {
          status.textContent = manages
            ? skipped + " end-to-end encrypted file(s) left out: open each from its own download link."
            : skipped + " file(s) left out: they cannot be downloaded here.";
        }, usable.length * 600 + 100); // after downloadSequentially's own last message
      }
    }
    function bulkZip(tiles) {
      if (!zipUrl) return;
      window.location = zipUrl + tiles.map(function (t) { return encodeURIComponent(idOf(t)); }).join(",");
    }

    bar.addEventListener("click", function (e) {
      var act = e.target.closest("[data-act]");
      if (!act || !bar.contains(act)) return;
      var tiles = selected();
      if (!tiles.length) return;
      var what = act.getAttribute("data-act");
      var handler = fileAction(context, what);
      if (handler) { handler(actionArg(tiles, act, true, null)); return; }
      if (what === "download") bulkDownload(tiles, act);
      else if (what === "zip") bulkZip(tiles);
      else if (what === "photos") FGS.saveToPhotos(asItems(tiles), act);
    });

    var help = bar.querySelector("[data-fgs-help]");
    if (FGS.isIOS()) {
      // Safari allows one download per tap, so the zip and Save to Photos
      // are the way to take several at once
      var dl = bar.querySelector("[data-act=download]");
      var photos = bar.querySelector("[data-act=photos]");
      if (dl && (photos || bar.querySelector("[data-act=zip]"))) dl.classList.add("d-none");
      if (photos) photos.classList.remove("d-none");
      if (help) help.classList.remove("d-none");
    }
    if (help) help.addEventListener("click", function () { FGS.openHelpSheet(); });

    // A checkbox ticked directly, in either view
    root.addEventListener("change", function (e) {
      var check = e.target.closest(".fgs-tile-check, .fgs-row-check");
      if (!check || check.closest("[data-fgs-browser]") !== root) return;
      var id = check.classList.contains("fgs-tile-check") ? idOf(check.closest(".fgs-tile"))
        : check.closest("tr").getAttribute("data-row-id");
      setChecked(id, check.checked);
      status.textContent = "";
      updateSelection();
    });

    // A tile, or a name or thumbnail in the list: open it, or in select mode
    // (de)select it
    grid.addEventListener("click", function (e) {
      var open = e.target.closest(".fgs-tile-open");
      if (!open) return;
      var tile = open.closest(".fgs-tile");
      if (selecting) toggleSelected(tile);
      else openAt(tile);
    });
    listWrap.addEventListener("click", function (e) {
      var name = e.target.closest(".fgs-name-open, .fgs-list-thumb");
      if (!name) return;
      var tr = name.closest("tr[data-row-id]");
      var tile = tr && tilesById[tr.getAttribute("data-row-id")];
      if (!tile) return;
      if (selecting) toggleSelected(tile);
      else openAt(tile);
    });

    // The "⋯" menu, and the list's quick buttons, in every view
    root.addEventListener("click", function (e) {
      var item = e.target.closest("[data-menu]");
      if (!item || item.disabled || item.closest("[data-fgs-browser]") !== root) return;
      var menu = item.closest("[data-fgs-file-menu]");
      var holder = menu || item.closest("tr[data-row-id]");
      if (!holder) return;
      var tile = tilesById[holder.getAttribute(menu ? "data-id" : "data-row-id")];
      if (!tile) return;
      // Dialogs and the viewer hand focus back to the "⋯", not to an entry
      // of the menu that just closed
      var trigger = menu ? menu.querySelector(".fgs-menu-btn") : item;
      trigger.focus();
      var what = item.getAttribute("data-menu");
      var handler = fileAction(context, what);
      if (handler) handler(actionArg([tile], item, false, menu || menuOf(tile), trigger));
      else if (what === "open") openAt(tile);
      else if (what === "download") FGS.downloadSequentially(asItems([tile]));
    });

    // Live thumbnails: a tile whose preview is still being made asks for it
    // every few seconds, and shows it once it is ready. The worker makes
    // previews one at a time, so a big batch can stay pending for minutes:
    // a tile gives up only after POLL_FOR_MS without progress, measured from
    // when it was first seen pending or, if later, from when any tile of
    // this list last left pending (the queue is moving).
    var polls = {}, inFlight = {}, pollTimer = null, pollCursor = 0, lastProgress = Date.now();
    function startPolling(id) {
      if (!infoUrl || polls[id]) return;
      polls[id] = Date.now(); // first seen pending
      if (!pollTimer) pollTimer = window.setInterval(pollTick, POLL_EVERY_MS);
    }
    function pollTick() {
      var now = Date.now();
      Object.keys(polls).forEach(function (id) {
        if (!tilesById[id] || now - Math.max(polls[id], lastProgress) > POLL_FOR_MS) delete polls[id];
      });
      var ids = Object.keys(polls);
      if (!ids.length) { window.clearInterval(pollTimer); pollTimer = null; return; }
      if (document.hidden) return;
      var batch = [];
      for (var i = 0; i < Math.min(POLL_BATCH, ids.length); i++) batch.push(ids[(pollCursor + i) % ids.length]);
      pollCursor = (pollCursor + batch.length) % ids.length;
      batch.forEach(function (id) {
        if (inFlight[id]) return;
        inFlight[id] = true;
        fetch(infoUrl + encodeURIComponent(id), { credentials: "same-origin", cache: "no-store" })
          .then(function (r) {
            if (!r.ok) { if (r.status === 404 || r.status === 401) delete polls[id]; return null; }
            return r.json();
          })
          .then(function (info) {
            if (!info || !info.Status || info.Status === "pending") return;
            delete polls[id];
            lastProgress = Date.now();
            applyPreview(id, info);
          })
          .catch(function () { /* try again next tick */ })
          .then(function () { delete inFlight[id]; });
      });
    }

    function thumbImg(className, info) {
      var img = document.createElement("img");
      if (className) img.className = className;
      img.decoding = "async";
      img.alt = "";
      img.src = info.Thumb;
      if (info.Color) img.style.background = info.Color;
      return img;
    }

    // A preview (the /files/previewinfo or /p/info answer) onto the tile and
    // its list row. The download URL is the page's own and is left alone.
    function applyPreview(id, info) {
      var tile = tilesById[id];
      if (!tile || !info) return;
      var row = rowsById[id];
      setData(tile, "preview-status", info.Status);
      setData(tile, "view", info.View);
      setData(tile, "video", info.Video);
      setData(tile, "kind", info.Kind);
      setData(tile, "w", info.Width || 0);
      setData(tile, "h", info.Height || 0);
      if (info.TakenAt) setData(tile, "taken", info.TakenAt);
      if (info.Thumb) {
        var open = tile.querySelector(".fgs-tile-open");
        open.textContent = "";
        open.appendChild(thumbImg("", info));
        if (info.Video) {
          var play = document.createElement("span");
          play.className = "fgs-play";
          var icon = document.createElement("i");
          icon.className = "bi bi-play-fill";
          play.appendChild(icon);
          open.appendChild(play);
        }
        var old = row && row.querySelector(".fgs-list-thumb");
        if (old) old.parentNode.replaceChild(thumbImg("fgs-list-thumb", info), old);
      }
      var taken = row && row.querySelector("td[data-col=taken]");
      if (taken && info.TakenAt) {
        taken.setAttribute("data-order", String(info.TakenAt));
        taken.textContent = fmt(info.TakenAt);
        if (dataTable) dataTable.row(row).invalidate("dom");
      }
      // A replaced file's new preview is still being made: ask for it
      if (info.Status === "pending") startPolling(id);
      layoutBoard();
    }

    Object.keys(tilesById).forEach(function (id) {
      if (tilesById[id].getAttribute("data-preview-status") === "pending") startPolling(id);
    });

    // Live adds (the upload page): a tile and a row from the <template>s,
    // filled in with textContent and setters only. file is a FileApiOutput
    // (or anything with its field names), preview a previewInfo or nothing.
    function fillMenu(menu, file, perms) {
      setData(menu, "id", file.Id);
      var trigger = menu.querySelector(".fgs-menu-btn");
      if (trigger) trigger.setAttribute("aria-label", "Actions for " + (file.Name || ""));
      if (manages) {
        setData(menu, "url", file.UrlDownload);
        setData(menu, "downloads", file.DownloadsRemaining);
        setData(menu, "unlimited-downloads", !!file.UnlimitedDownloads);
        setData(menu, "expire", file.ExpireAt);
        setData(menu, "unlimited-time", !!file.UnlimitedTime);
        setData(menu, "password", !!file.IsPasswordProtected);
        setData(menu, "e2e", !!file.IsEndToEndEncrypted);
        setData(menu, "can-edit", perms.edit);
        setData(menu, "can-delete", perms.del);
        setData(menu, "can-replace", perms.replace);
      }
      var e2e = !!file.IsEndToEndEncrypted;
      menu.querySelectorAll("[data-menu]").forEach(function (b) {
        var k = b.getAttribute("data-menu");
        if (k === "copy") b.setAttribute("data-clipboard-text", file.UrlDownload || "");
        if (k === "hotlink") {
          // Only files that have a hotlink offer it
          b.setAttribute("data-clipboard-text", file.UrlHotlink || "");
          if (b.parentNode) b.parentNode.classList.toggle("d-none", !file.UrlHotlink);
        }
        if (k === "download" || k === "add-to-collection") b.disabled = e2e;
        if (k === "edit") b.disabled = !perms.edit;
        if (k === "replace") b.disabled = !perms.replace;
        if (k === "delete") b.disabled = !perms.del;
      });
    }

    function addItem(file, preview) {
      if (!file || !file.Id || !tplTile || !tplRow) return null;
      preview = preview || {};
      if (tilesById[file.Id]) removeItem(file.Id);
      var id = String(file.Id);
      var userId = parseInt(root.getAttribute("data-user-id"), 10) || 0;
      var mine = file.UploaderId === undefined || file.UploaderId === null || file.UploaderId === userId;
      var flag = function (k) { return root.getAttribute("data-perm-" + k) === "true"; };
      var perms = { edit: mine || flag("edit-others"), del: mine || flag("delete-others") };
      perms.replace = perms.edit && !file.IsEndToEndEncrypted && ((mine && flag("replace")) || flag("replace-others"));
      var download = preview.Download || (manages && !file.IsEndToEndEncrypted ? "./files/download?id=" + encodeURIComponent(id) : "");
      var uploader = mine ? (root.getAttribute("data-user-name") || "") : "";
      var name = file.Name || "";

      var tile = tplTile.content.firstElementChild.cloneNode(true);
      setData(tile, "mine", mine);
      setData(tile, "id", id);
      setData(tile, "name", name);
      setData(tile, "download", download);
      setData(tile, "uploader", uploader);
      setData(tile, "uploaded", file.UploadDate || 0);
      setData(tile, "size", file.Size);
      setData(tile, "bytes", file.SizeBytes);
      setData(tile, "taken", preview.TakenAt || 0);
      tile.querySelector(".fgs-tile-open").setAttribute("aria-label", "Open " + name);
      tileCheck(tile).setAttribute("aria-label", "Select " + name);
      var small = tile.querySelector(".fgs-typeicon small");
      if (small) small.textContent = name.slice(0, 40);
      var icon = fileTypeIcon(name);
      Array.prototype.forEach.call(tile.querySelectorAll(".fgs-typeicon i"), function (i) { i.className = "bi " + icon; });
      tile.querySelector(".fgs-name").textContent = name;
      tile.querySelector(".fgs-meta-uploader").textContent = uploader ? uploader + " · " : "";
      setData(tile.querySelector("figcaption [data-ts]"), "ts", file.UploadDate || 0);
      fillMenu(menuOf(tile), file, perms);

      var row = tplRow.content.firstElementChild.cloneNode(true);
      setData(row, "mine", mine);
      setData(row, "row-id", id);
      if (legacyIds) row.id = "row-" + id;
      rowCheck(row).setAttribute("aria-label", "Select " + name);
      var nameCell = row.querySelector("td[data-col=name]");
      nameCell.setAttribute("data-order", name);
      var nameSpan = row.querySelector(".fgs-row-name");
      nameSpan.textContent = name;
      if (legacyIds) nameSpan.id = "cell-name-" + id;
      var cell = function (k) { return row.querySelector("td[data-col=" + k + "]"); };
      if (cell("uploader")) cell("uploader").textContent = uploader;
      cell("uploaded").setAttribute("data-order", String(file.UploadDate || 0));
      setData(cell("uploaded").querySelector("[data-ts]"), "ts", file.UploadDate || 0);
      cell("taken").setAttribute("data-order", String(preview.TakenAt || 0));
      cell("taken").textContent = preview.TakenAt ? fmt(preview.TakenAt) : "—";
      cell("size").setAttribute("data-order", String(file.SizeBytes || 0));
      cell("size").textContent = file.Size || "";
      if (cell("expires")) {
        if (file.UnlimitedTime) {
          cell("expires").setAttribute("data-order", "9223372036854775807");
          cell("expires").textContent = "Never";
        } else {
          cell("expires").setAttribute("data-order", String(file.ExpireAt || 0));
          cell("expires").textContent = fmt(file.ExpireAt);
        }
      }
      if (cell("downloads")) {
        if (legacyIds) cell("downloads").id = "cell-downloads-" + id;
        cell("downloads").textContent = String(file.DownloadCount || 0);
      }
      var copy = row.querySelector(".copyurl");
      if (copy) {
        if (legacyIds) copy.id = "url-button-" + id;
        copy.setAttribute("data-clipboard-text", file.UrlDownload || "");
      }
      Array.prototype.forEach.call(row.querySelectorAll(".fgs-list-icon i"), function (i) { i.className = "bi " + icon; });
      fillMenu(row.querySelector("[data-fgs-file-menu]"), file, perms);
      formatTimes(tile);
      formatTimes(row);

      grid.insertBefore(tile, grid.firstChild);
      index(tile, row);
      if (dataTable) dataTable.row.add(row).draw(false);
      else tbody.insertBefore(row, tbody.firstChild);
      hideForScope(tile);
      hideForScope(row);
      if (preview.Status === "ready") applyPreview(id, preview);
      else if (!file.IsEndToEndEncrypted && (!preview.Status || preview.Status === "pending")) startPolling(id);
      refreshCount();
      layoutBoard();
      updateSelection();
      return tile;
    }

    // A file changed on the server (edit, expiry & limits): its new
    // FileApiOutput onto the tile, the row and both menus, in place. The
    // preview, position, selection and permissions stay as they are.
    function updateItem(file) {
      var id = file && file.Id;
      var tile = tilesById[id], row = rowsById[id];
      if (!tile) return null;
      var menu = menuOf(tile);
      var was = function (k) {
        if (manages) return menu.getAttribute("data-can-" + k) === "true";
        var b = menu.querySelector("[data-menu=" + k + "]");
        return !!b && !b.disabled;
      };
      var perms = { edit: was("edit"), del: was("delete"), replace: was("replace") };
      var name = file.Name || tile.getAttribute("data-name") || "";
      setData(tile, "name", name);
      if (file.Size !== undefined) setData(tile, "size", file.Size);
      if (file.SizeBytes !== undefined) setData(tile, "bytes", file.SizeBytes);
      tile.querySelector(".fgs-tile-open").setAttribute("aria-label", "Open " + name);
      tileCheck(tile).setAttribute("aria-label", "Select " + name);
      tile.querySelector(".fgs-name").textContent = name;
      var small = tile.querySelector(".fgs-typeicon small");
      if (small) small.textContent = name.slice(0, 40);
      // A new extension is a new type icon (on tiles and rows without a thumbnail)
      var icon = fileTypeIcon(name);
      Array.prototype.forEach.call(tile.querySelectorAll(".fgs-typeicon i"), function (i) { i.className = "bi " + icon; });
      fillMenu(menu, file, perms);
      if (row) {
        Array.prototype.forEach.call(row.querySelectorAll(".fgs-list-icon i"), function (i) { i.className = "bi " + icon; });
        rowCheck(row).setAttribute("aria-label", "Select " + name);
        row.querySelector("td[data-col=name]").setAttribute("data-order", name);
        row.querySelector(".fgs-row-name").textContent = name;
        var cell = function (k) { return row.querySelector("td[data-col=" + k + "]"); };
        if (cell("size") && file.Size !== undefined) {
          cell("size").setAttribute("data-order", String(file.SizeBytes || 0));
          cell("size").textContent = file.Size;
        }
        if (cell("expires")) {
          cell("expires").setAttribute("data-order", file.UnlimitedTime ? "9223372036854775807" : String(file.ExpireAt || 0));
          cell("expires").textContent = file.UnlimitedTime ? "Never" : fmt(file.ExpireAt);
        }
        if (cell("downloads") && file.DownloadCount !== undefined) cell("downloads").textContent = String(file.DownloadCount);
        var copy = row.querySelector(".copyurl");
        if (copy) copy.setAttribute("data-clipboard-text", file.UrlDownload || "");
        fillMenu(row.querySelector("[data-fgs-file-menu]"), file, perms);
        if (dataTable) dataTable.row(row).invalidate("dom");
      }
      return tile;
    }

    function removeItem(id) {
      var tile = tilesById[id], row = rowsById[id];
      delete tilesById[id];
      delete rowsById[id];
      delete polls[id];
      if (tile && tile.parentNode) tile.parentNode.removeChild(tile);
      if (row) {
        if (dataTable) dataTable.row(row).remove().draw(false);
        if (row.parentNode) row.parentNode.removeChild(row);
      }
      refreshCount();
      layoutBoard();
      updateSelection();
    }

    api = {
      root: root,
      context: context,
      addItem: addItem,
      removeItem: removeItem,
      updateItem: updateItem,
      applyPreview: applyPreview,
      tile: function (id) { return tilesById[id] || null; },
      row: function (id) { return rowsById[id] || null; },
      selected: selected,
      visible: visibleTiles,
      setSelecting: setSelecting,
      view: function () { return view; },
      relayout: layoutBoard
    };
    root.fgsBrowser = api;

    // Wiring, then the remembered (or default) state
    root.querySelectorAll("button[data-layout]").forEach(function (b) {
      b.addEventListener("click", function () { applyView(b.getAttribute("data-layout")); });
    });
    root.querySelectorAll("button[data-scope]").forEach(function (b) {
      b.addEventListener("click", function () { applyScope(b.getAttribute("data-scope")); });
    });
    root.querySelectorAll("button[data-shape]").forEach(function (b) {
      b.addEventListener("click", function () { applyShape(b.getAttribute("data-shape")); });
    });
    selectToggle.addEventListener("click", function () { setSelecting(!selecting); });
    if (size) {
      size.value = load("size") || "160";
      size.addEventListener("input", applySize);
      applySize();
    }
    applyShape(load("shape") || "square");
    formatTimes(root.querySelector(".fgs-views"));

    if (window.jQuery && jQuery.fn && jQuery.fn.DataTable) {
      var heads = Array.prototype.slice.call(table.tHead.rows[0].cells);
      var sortAt = heads.findIndex(function (th) { return th.hasAttribute("data-fgs-sort"); });
      var noSort = [];
      heads.forEach(function (th, i) { if (th.hasAttribute("data-fgs-nosort")) noSort.push(i); });
      // No paging: the scope switch hides rows in the DOM, and the edit
      // dialog's "Replace content" list reads every row's name from it
      dataTable = jQuery(table).DataTable({
        "order": sortAt >= 0 ? [[sortAt, "desc"]] : [],
        "columnDefs": [{ "targets": noSort, "orderable": false, "searchable": false }],
        "paging": false,
        "info": false,
        // Laid out while the tiles may be showing: measured widths would be wrong
        "autoWidth": false,
        "language": { "emptyTable": "No files yet", "zeroRecords": "No matching files" }
      });
      // A search changes which selected files are visible
      dataTable.on("draw", updateSelection);
    }
    var savedView = load("view");
    applyView(VIEWS.indexOf(savedView) >= 0 ? savedView : root.getAttribute("data-default-view"));
    applyScope(load("scope") || "mine");
    refreshCount();
    return api;
  };

  function initAllFileBrowsers() {
    Array.prototype.forEach.call(document.querySelectorAll("[data-fgs-browser]"), FGS.initFileBrowser);
  }
  // fn(browser) for every file list on the page, e.g. to drop a deleted file
  // from each list that shows it
  FGS.eachFileBrowser = function (fn) {
    Array.prototype.forEach.call(document.querySelectorAll("[data-fgs-browser]"), function (root) {
      var b = FGS.initFileBrowser(root);
      if (b) fn(b);
    });
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initAllFileBrowsers);
  else initAllFileBrowsers();
})();
