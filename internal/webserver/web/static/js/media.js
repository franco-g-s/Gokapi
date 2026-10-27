// Shared media front end: device-specific download buttons, help sheet,
// and (later tasks) viewer, board and Save to Photos. No dependencies.
(function () {
  "use strict";
  var FGS = window.FGS = window.FGS || {};

  FGS.isIOS = function () {
    return /iPad|iPhone|iPod/.test(navigator.userAgent) ||
      (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  };

  // The collection page's "Back" button. Shown only when the visitor came from
  // another page of this site: a link opened from a chat has no same-origin
  // referrer, and the password form posting back to /c has this very page as
  // its referrer, so neither shows it. A history entry cannot be read, so
  // history.back() is used when this tab has a history to go back through,
  // and the referrer URL is navigated to otherwise (a link opened in a new tab).
  FGS.initBackButton = function (button, referrer, here, historyLength) {
    if (!button) return;
    var from = referrer === undefined ? document.referrer : referrer;
    if (!from) return;
    var page = here || window.location;
    var ref;
    try { ref = new URL(from); } catch (e) { return; }
    if (ref.origin !== page.origin || ref.pathname === page.pathname) return;
    var length = historyLength === undefined ? window.history.length : historyLength;
    button.classList.remove("d-none");
    button.addEventListener("click", function () {
      if (length <= 1) {
        window.location.href = ref.href;
        return;
      }
      // history.back() does nothing when there is no usable entry, and goes
      // elsewhere when the entry is not the referrer. If this page is still
      // here shortly after, go to the referrer; leaving the page (pagehide,
      // also a bfcache restore later) cancels the fallback.
      var timer = setTimeout(function () { window.location.href = ref.href; }, 350);
      window.addEventListener("pagehide", function () { clearTimeout(timer); }, { once: true });
      window.history.back();
    });
  };

  // Desktop: "Download all" fires each file in turn (unchanged behaviour).
  // iOS: Safari allows one download per tap, so Save to Photos is primary and
  // the zip is the fallback (the zip alone where navigator.canShare is missing).
  // Every file of the page's list, loaded or not (resolveAll, Part 3).
  FGS.initDownloadButtons = function (root) {
    if (!root) return;
    var all = root.querySelector("[data-fgs-download-all]");
    var zip = root.querySelector("[data-fgs-download-zip]");
    var help = root.querySelector("[data-fgs-help]");
    var status = root.querySelector("#downloadAllStatus");
    var listRoot = document.querySelector("[data-fgs-browser]");
    // Every file on the collection page, in list order, as the counted /cd
    // download URLs: saving to Photos is a download
    function everything() {
      var browser = listRoot && FGS.initFileBrowser(listRoot);
      if (!browser) return Promise.resolve([]);
      return browser.resolveAll().then(function (list) {
        return list.map(function (it) {
          it.url = it.url ? new URL(it.url, window.location.href).href : "";
          return it;
        }).filter(function (it) { return it.url; });
      });
    }
    function failed() { if (status) status.textContent = "Couldn't load the list of files. Try again."; }
    if (all) {
      if (!listRoot || !(parseInt(listRoot.getAttribute("data-all"), 10) > 0)) all.style.display = "none";
      all.addEventListener("click", function () {
        if (all.disabled) return;
        all.disabled = true;
        if (status) status.textContent = "Loading…";
        everything().then(function (items) {
          all.disabled = false;
          if (status) status.textContent = "";
          FGS.downloadSequentially(items, { button: all, status: status });
        }, function () { all.disabled = false; failed(); });
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
          // A prepared set shares on this tap: nothing may come between the
          // tap and the share sheet
          if (FGS.photosPending(photos)) { FGS.saveToPhotos([], photos); return; }
          if (photos._fgsLoading) return;
          photos._fgsLoading = true;
          everything().then(function (items) {
            photos._fgsLoading = false;
            var browser = FGS.initFileBrowser(listRoot);
            FGS.saveToPhotos(items, photos, { status: status, zipIds: browser && browser.zipIds });
          }, function () { photos._fgsLoading = false; failed(); });
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

  // Save to Photos, in measured batches. The first tap plans the whole set
  // from the stored sizes (FGS.planPhotos) and fetches the first batch only;
  // each later batch is fetched by its own tap, once the previous share sheet
  // has closed, so never more than one batch is held in memory. If the tap's
  // user activation is still alive once a batch has arrived
  // (navigator.userActivation.isActive), the share sheet opens at once, where
  // "Save N Images" puts them straight into Photos. Safari only opens the
  // sheet with that activation, and it lapses during a slow fetch (or the
  // feed fetch that resolves a "Select all"): then, and where the browser
  // cannot say, the button turns into a highlighted "Ready — tap to save"
  // and the next tap shares. Each File carries the date taken as its
  // lastModified, the upload date standing in. Always pass the original-file
  // download URLs (/cd or /gd, counted; /files/download on the Files tab,
  // uncounted), never view or video URLs. Videos over VIDEO_MAX_BYTES are
  // left out before any request and handed to the zip (opts.zipIds).
  // Defaults, not yet measured on an iPhone (2026-10-22): change once real limits are measured
  var PHOTO_BATCH_FILES = 20;      // files per share sheet
  var PHOTO_BATCH_BYTES = 1.5e9;   // bytes per share sheet
  var VIDEO_MAX_BYTES  = 500e6;    // larger videos go to the zip
  var MIME_BY_EXT = { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", gif: "image/gif",
    webp: "image/webp", heic: "image/heic", heif: "image/heif", mov: "video/quicktime",
    mp4: "video/mp4", m4v: "video/x-m4v" };

  function photoMime(name, blobType) {
    // /cd serves downloads as octet-stream; Photos needs a real media type
    if (blobType && blobType !== "application/octet-stream") return blobType;
    var m = /\.([a-z0-9]+)$/i.exec(name || "");
    return (m && MIME_BY_EXT[m[1].toLowerCase()]) || blobType || "application/octet-stream";
  }

  // A file's Type: the preview's kind, or by the name where there is no
  // preview (yet); "other" for the rest. The server's kindOf (FileList.go)
  // holds the same rule for the Type filter.
  FGS.fileKind = function (kind, name) {
    if (kind === "image" || kind === "video") return kind;
    var m = /^(image|video)\//.exec(photoMime(name, ""));
    return m ? m[1] : "other";
  };
  // A photo or video Photos can take
  FGS.isPhotoOrVideo = function (kind, name) {
    return FGS.fileKind(kind, name) !== "other";
  };

  // A file list tile (or anything carrying its data-*) as a Save to Photos /
  // download item: the original's URL, name, stored size and dates (seconds)
  FGS.fileItemFromEl = function (el) {
    return { url: el.getAttribute("data-download"), name: el.getAttribute("data-name"),
      bytes: parseInt(el.getAttribute("data-bytes"), 10),
      taken: parseInt(el.getAttribute("data-taken"), 10) || 0,
      uploaded: parseInt(el.getAttribute("data-uploaded"), 10) || 0 };
  };

  // The File's date: when it was taken, else when it was uploaded, in ms.
  // Undefined leaves the browser's default (now).
  function photoDate(it) {
    var s = (it && (it.taken || it.uploaded)) || 0;
    return s > 0 ? s * 1000 : undefined;
  }

  var VIDEO_RE = /\.(mov|mp4|m4v)$/i;
  var READY_LABEL = "Ready — tap to save";

  // The stored size, or null where it is not known (NaN from a missing data-bytes)
  function knownBytes(it) {
    return typeof it.bytes === "number" && it.bytes >= 0 ? it.bytes : null;
  }
  function isVideo(it) { return it.kind === "video" || VIDEO_RE.test(it.name || ""); }

  // Pure: no fetch, no DOM. Returns
  // {batches: [[item]], bigVideos: [item], noUrl: [item], notMedia: [item]}
  // in input order; a batch closes when adding the next item would pass
  // PHOTO_BATCH_FILES or PHOTO_BATCH_BYTES (a single item over the byte limit
  // that is not a video gets a batch of its own). limits: {files, bytes,
  // videoMax}, each defaulting to the constants above. An unknown size counts
  // as 0 here; the fetch checks a video's Content-Length instead.
  FGS.planPhotos = function (items, limits) {
    limits = limits || {};
    var maxFiles = limits.files || PHOTO_BATCH_FILES;
    var maxBytes = limits.bytes || PHOTO_BATCH_BYTES;
    var videoMax = limits.videoMax || VIDEO_MAX_BYTES;
    var plan = { batches: [], bigVideos: [], noUrl: [], notMedia: [] };
    var batch = null, sum = 0;
    (items || []).forEach(function (it) {
      if (!it) return;
      // Password or client-side encrypted files: nothing to fetch
      if (!it.url) { plan.noUrl.push(it); return; }
      if (!FGS.isPhotoOrVideo(it.kind, it.name)) { plan.notMedia.push(it); return; }
      var b = knownBytes(it);
      if (b !== null && isVideo(it) && b > videoMax) { plan.bigVideos.push(it); return; }
      b = b || 0;
      if (!batch || batch.length >= maxFiles || sum + b > maxBytes) {
        batch = []; sum = 0;
        plan.batches.push(batch);
      }
      batch.push(it);
      sum += b;
    });
    return plan;
  };

  // The tap that started the downloads still counts as a user gesture.
  // Without the API there is no telling, so the explicit second tap it is.
  function activationAlive() {
    var ua = navigator.userActivation;
    return !!(ua && ua.isActive);
  }

  // The label, keeping a leading icon (the "⋯" menu entry has one)
  function setPhotosLabel(button, text, ready) {
    if (button._fgsIcon) {
      button.textContent = "";
      button.appendChild(button._fgsIcon.cloneNode(true));
      button.appendChild(document.createTextNode(" " + text));
    } else {
      button.textContent = text;
    }
    button.classList.toggle("fgs-photos-ready", !!ready);
  }
  function batchOf(state) {
    var b = state.plan.batches.length;
    return b > 1 ? " (batch " + (state.k + 1) + " of " + b + ")" : "";
  }
  // Prepared and waiting for its tap
  function showReady(button, state) {
    setPhotosLabel(button, READY_LABEL + batchOf(state), true);
  }
  // A batch is saved and another is to come: its tap fetches it
  function showNext(button, state) {
    setPhotosLabel(button, "Next batch — tap to prepare (" + (state.k + 1) + " of " + state.plan.batches.length + ")", true);
  }
  // Back to the first-tap label
  function restoreLabel(button) {
    button.classList.remove("fgs-photos-ready");
    if (button._fgsLabel !== undefined) button.innerHTML = button._fgsLabel;
  }

  // What was left out: big videos with their zip button, then the rest. It
  // goes in the caller's status (aria-live) where that is on screen; else
  // (the "⋯" menu outside select mode, where the selection bar is hidden, or
  // the viewer, which has no status) in a note fixed at the foot of the page.
  // Neither costs the tap its activation, unlike an alert.
  function onScreen(el) { return !!el && el.isConnected && el.getClientRects().length > 0; }
  function floatingNotes() {
    var holder = document.getElementById("fgs-photos-float");
    if (holder) return holder;
    holder = document.createElement("div");
    holder.id = "fgs-photos-float";
    holder.setAttribute("role", "status");
    holder.setAttribute("aria-live", "polite");
    var st = holder.style;
    st.position = "fixed"; st.left = "0.75rem"; st.right = "0.75rem"; st.bottom = "0.75rem"; st.zIndex = "10055";  // above the viewer (10050), below the help sheet (10060)
    st.padding = "0.6rem 2.2rem 0.6rem 0.8rem"; st.borderRadius = "10px"; st.fontSize = "0.875rem";
    st.background = "#212529"; st.color = "#fff"; st.boxShadow = "0 4px 16px rgba(0,0,0,.35)";
    var close = document.createElement("button");
    close.type = "button";
    close.className = "btn-close btn-close-white";
    close.setAttribute("aria-label", "Close");
    close.style.position = "absolute"; close.style.top = "0.6rem"; close.style.right = "0.6rem";
    close.addEventListener("click", function () { if (holder.parentNode) holder.parentNode.removeChild(holder); });
    holder.appendChild(close);
    document.body.appendChild(holder);
    return holder;
  }
  function removeNote(button) {
    var note = button._fgsNote;
    button._fgsNote = null;
    if (!note || !note.parentNode) return;
    var holder = note.parentNode;
    holder.removeChild(note);
    // The fixed note goes once it holds nothing but its close button
    if (holder.id === "fgs-photos-float" && !holder.querySelector(".fgs-photos-note") && holder.parentNode) {
      holder.parentNode.removeChild(holder);
    }
  }
  // While the viewer is open the note sits above its caption bar, so it
  // never covers "Download original" or "Save to Photos"; else at the foot.
  function placeFloatingNotes(holder) {
    var viewer = document.getElementById("fgs-viewer");
    var info = viewer && !viewer.hidden ? viewer.querySelector(".fgs-v-info") : null;
    var top = info ? info.getBoundingClientRect().top : 0;
    holder.style.bottom = top > 0 ? Math.max(12, window.innerHeight - top + 8) + "px" : "0.75rem";
  }
  function showLeftOut(button, state) {
    var big = state.plan.bigVideos.concat(state.lateVideos);
    // The Files tab's zip only takes the viewer's own files: big videos from
    // their groups are zipped on the group's page instead
    var zippable = state.opts.zipSkipsForeign ? big.filter(function (it) { return !it.foreign; }) : big;
    var notes = [];
    if (big.length > zippable.length) notes.push((big.length - zippable.length) + " video(s) from your groups: zip them on their group's page.");
    if (state.plan.notMedia.length) notes.push(state.plan.notMedia.length + " file(s) aren't photos or videos and were left out.");
    if (state.plan.noUrl.length) notes.push(state.plan.noUrl.length + " file(s) can't be fetched here. Use Download as .zip for those.");
    if (state.failed) notes.push(state.failed + " file(s) could not be fetched. Try again, or use Download as .zip.");
    var bigText = big.length ? big.length + " video(s) are too big to save to Photos from Safari. Download them as a .zip, then save them from the Files app." : "";
    removeNote(button);
    if (!bigText && !notes.length) return;
    var note = document.createElement("span");
    note.className = "fgs-photos-note";
    if (bigText) {
      note.appendChild(document.createTextNode(bigText));
      var ids = zippable.map(function (it) { return it.id; }).filter(Boolean);
      if (state.opts.zipIds && ids.length && ids.length === zippable.length) {
        var zip = document.createElement("button");
        zip.type = "button";
        zip.className = "btn btn-link btn-sm p-0 align-baseline";
        zip.textContent = "Download those as .zip";
        zip.addEventListener("click", function () { state.opts.zipIds(ids); });
        note.appendChild(document.createTextNode(" "));
        note.appendChild(zip);
      }
    }
    if (notes.length) note.appendChild(document.createTextNode((bigText ? " " : "") + notes.join(" ")));
    var status = state.opts.status;
    if (onScreen(status)) {
      status.textContent = "";
      status.appendChild(note);
    } else {
      var holder = floatingNotes();
      // One note at a time: a newer one replaces any earlier one
      Array.prototype.forEach.call(holder.querySelectorAll(".fgs-photos-note"), function (n) { holder.removeChild(n); });
      if (note.querySelector("button")) note.querySelector("button").classList.add("link-light");
      holder.insertBefore(note, holder.firstChild);
      placeFloatingNotes(holder);
    }
    button._fgsNote = note;
  }

  // Forget any prepared set and put the button back to its first-tap label.
  // Called when the selection changes, and on unexpected failure.
  FGS.resetSaveToPhotos = function (button) {
    if (!button) return;
    if (button._fgs) button._fgs.cancelled = true;
    button._fgs = null;
    if (button._fgsTimer) { window.clearTimeout(button._fgsTimer); button._fgsTimer = null; }
    removeNote(button);
    button.disabled = false;
    restoreLabel(button);
  };

  // A set under way (being fetched, prepared, or between batches): the next
  // tap continues it, and the caller must not resolve its list again first,
  // which would cost the tap its activation
  FGS.photosPending = function (button) {
    var s = button && button._fgs;
    return !!(s && (s.busy || s.files || s.plan));
  };

  // Opens the share sheet for the prepared batch. Its Files are dropped once
  // the sheet has closed on a save.
  function sharePrepared(button, state) {
    var files = state.files;
    if (!navigator.canShare({ files: files })) { FGS.openHelpSheet(); return; }
    state.busy = true;
    navigator.share({ files: files }).then(function () {
      state.busy = false;
      if (state.cancelled) return;  // selection changed while the sheet was open
      state.files = null;
      state.k++;
      if (state.k < state.plan.batches.length) { showNext(button, state); return; }
      if (offerRetry(button, state)) return;
      setPhotosLabel(button, "Saved ✓", false); button._fgs = null;
      // Back to the first-tap label, so a later tap reads as a new action
      button._fgsTimer = window.setTimeout(function () {
        button._fgsTimer = null;
        if (!button._fgs) restoreLabel(button);
      }, 2000);
    }, function (err) {
      state.busy = false;
      if (state.cancelled) return;
      // "NotAllowedError": the tap's activation had lapsed after all;
      // "AbortError": the sheet was closed. Either way the batch stays
      // prepared, and the next tap shares it.
      showReady(button, state);
      if (!err || (err.name !== "AbortError" && err.name !== "NotAllowedError")) FGS.openHelpSheet();
    });
  }

  function failedOne(state, it) {
    state.failed++;
    state.failedItems.push(it);
  }
  // The set is done but some files could not be fetched: the button offers
  // those again, and its tap fetches only them, so nothing already saved is
  // fetched (and counted) twice
  function offerRetry(button, state) {
    var again = state.failedItems;
    if (!again.length) return false;
    var plan = FGS.planPhotos(again);
    // What else was left out stays in the note
    plan.bigVideos = state.plan.bigVideos; plan.noUrl = state.plan.noUrl; plan.notMedia = state.plan.notMedia;
    button._fgs = { plan: plan, k: 0, files: null, busy: false, failed: 0, failedItems: [],
      lateVideos: state.lateVideos, opts: state.opts, freshNote: true };
    setPhotosLabel(button, "Try the " + again.length + " again", true);
    return true;
  }

  // Fetches batch state.k, one file after another, then shares it on this
  // tap if its activation is still alive, else waits for the next tap
  function prepareBatch(button, state) {
    var batch = state.plan.batches[state.k];
    var files = [], fetched = [], done = 0, failedBefore = state.failed, lateBefore = state.lateVideos.length;
    state.busy = true;
    button.disabled = true;
    setPhotosLabel(button, "Preparing 0/" + batch.length + "…", false);
    var tick = function () {
      if (state.cancelled) return;
      done++;
      setPhotosLabel(button, "Preparing " + done + "/" + batch.length + "…", false);
    };
    batch.reduce(function (p, it) {
      return p.then(function () {
        if (state.cancelled) return null;
        // it.url may be a function returning a promise of the url, for links
        // that are only valid for a moment (the File requests page)
        return Promise.resolve(typeof it.url === "function" ? it.url() : it.url).then(function (url) {
          return fetch(url, { credentials: "same-origin" });
        }).then(function (r) {
          // An error page must never be wrapped up as a photo
          if (!r.ok) { failedOne(state, it); return null; }
          if (it.id) fetched.push(it.id);
          // Fallback only (size unknown): by now the request has been counted
          var len = parseInt(r.headers.get("Content-Length") || "0", 10);
          if (knownBytes(it) === null && isVideo(it) && len > VIDEO_MAX_BYTES) {
            state.lateVideos.push(it);
            if (r.body && r.body.cancel) r.body.cancel();
            return null;
          }
          return r.blob();
        }).then(function (b) {
          if (!b) return;
          var opts = { type: photoMime(it.name, b.type) };
          var date = photoDate(it);
          if (date) opts.lastModified = date;
          files.push(new File([b], it.name, opts));
        }).catch(function () { failedOne(state, it); }).then(tick);
      });
    }, Promise.resolve()).then(function () {
      // Group pages: these are downloaded now (kept visible), whatever
      // happens to the set: the server has counted them
      if (fetched.length && state.opts.fetched) state.opts.fetched(fetched);
      if (state.cancelled) return;  // selection changed meanwhile: drop it all
      state.busy = false;
      button.disabled = false;
      var changed = state.failed !== failedBefore || state.lateVideos.length !== lateBefore;
      if (!files.length) {
        // Nothing usable in this batch: on to the next, or back to the first tap
        state.k++;
        if (state.k < state.plan.batches.length) showNext(button, state);
        else if (!offerRetry(button, state)) { button._fgs = null; restoreLabel(button); }
        showLeftOut(button, state);
        return;
      }
      state.files = files;
      showReady(button, state);
      // One tap: the sheet now, while the tap still counts. The note costs
      // no activation, so it can follow.
      if (activationAlive()) sharePrepared(button, state);
      if (changed) showLeftOut(button, state);
    }).catch(function () {
      // Anything unexpected: leave the button usable rather than stuck
      if (!state.cancelled) FGS.resetSaveToPhotos(button);
    });
  }

  // items: [{id, url, name, kind, bytes, taken, uploaded}] (FGS.itemData).
  // bytes is the stored size and is what decides what is fetched: /cd and
  // /gd count a download before they send a body, so a file that can't be
  // shared must be ruled out BEFORE the request, never after. taken /
  // uploaded (seconds) become the File's date. opts (first tap; kept for the
  // set's later taps): zipIds(ids) zips the big videos, status is the
  // aria-live element for what was left out, fetched(ids) is told each
  // batch's files once they have been requested.
  FGS.saveToPhotos = function (list, button, opts) {
    if (!navigator.canShare) { FGS.openHelpSheet(); return; }
    if (button._fgsLabel === undefined) {
      button._fgsLabel = button.innerHTML;
      button._fgsIcon = button.querySelector("i");
    }
    var state = button._fgs;
    if (state && state.busy) return;
    if (button._fgsTimer) { window.clearTimeout(button._fgsTimer); button._fgsTimer = null; }

    // A set under way: this tap shares the prepared batch, or fetches the
    // next one. The list is the same selection (a change resets the set).
    if (state && state.files) { sharePrepared(button, state); return; }
    if (state && state.plan) {
      // A retry: the note no longer says these could not be fetched
      if (state.freshNote) { state.freshNote = false; showLeftOut(button, state); }
      prepareBatch(button, state);
      return;
    }

    // Spend no download on files this browser can't share anyway
    var probeOk = false;
    try { probeOk = navigator.canShare({ files: [new File([""], "probe.jpg", { type: "image/jpeg" })] }); } catch (e) { /* unsupported */ }
    if (!probeOk) { button._fgs = null; FGS.openHelpSheet(); return; }

    state = { plan: FGS.planPhotos(list), k: 0, files: null, busy: false, failed: 0, failedItems: [], lateVideos: [], opts: opts || {} };
    showLeftOut(button, state);
    if (!state.plan.batches.length) { button._fgs = null; restoreLabel(button); return; }
    button._fgs = state;
    prepareBatch(button, state);
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
      size: d("size"), bytes: parseInt(d("bytes"), 10), w: parseInt(d("w"), 10) || 0, h: parseInt(d("h"), 10) || 0,
      foreign: d("foreign") === "true" };
  };

  // One file's data for the actions (Part 3), from its tile (whose "⋯" menu
  // carries the management data) or from a feed item (listItemJson), so an
  // action works the same on files that are not rendered. url is the
  // original's download (Download, Save to Photos), fileUrl the share link.
  FGS.itemData = function (x) {
    if (!x) return null;
    if (x.nodeType === 1) {
      var t = function (k) { return x.getAttribute("data-" + k) || ""; };
      var menu = x.querySelector("[data-fgs-file-menu]");
      var m = function (k) { return menu ? menu.getAttribute("data-" + k) : null; };
      var flag = function (k) { return m(k) === "true"; };
      // Rendered without data-can-*: the menu's own entry says
      var can = function (k, entry) {
        var v = m("can-" + k);
        if (v !== null) return v === "true";
        var b = menu && menu.querySelector("[data-menu=" + entry + "]");
        return !!b && !b.disabled;
      };
      return { id: t("id"), name: t("name"), url: t("download"), bytes: parseInt(t("bytes"), 10),
        taken: parseInt(t("taken"), 10) || 0, uploaded: parseInt(t("uploaded"), 10) || 0, kind: t("kind"),
        mine: t("mine") === "true", foreign: t("foreign") === "true", downloaded: t("downloaded") === "true",
        canEdit: can("edit", "edit"), canDelete: can("delete", "delete"), canReplace: can("replace", "replace"),
        canRemove: can("remove", "remove-from-group"), e2e: flag("e2e"), groupLock: m("group-lock") || "",
        downloads: parseInt(m("downloads"), 10) || 0, unlimitedDownloads: flag("unlimited-downloads"),
        expire: parseInt(m("expire"), 10) || 0, unlimitedTime: flag("unlimited-time"), password: flag("password"),
        fileUrl: m("url") || "" };
    }
    return { id: String(x.Id || ""), name: x.Name || "", url: x.Download || "",
      bytes: typeof x.Bytes === "number" ? x.Bytes : parseInt(x.SizeBytes, 10),
      taken: x.TakenAt || 0, uploaded: x.UploadDate || 0, kind: (x.Preview && x.Preview.Kind) || "",
      mine: !!x.Mine, foreign: !!x.Foreign, downloaded: !!x.Downloaded,
      canEdit: !!x.CanEdit, canDelete: !!x.CanDelete, canReplace: !!x.CanReplace, canRemove: !!x.CanRemove,
      e2e: !!x.IsEndToEndEncrypted, groupLock: x.GroupLock || "", downloads: x.DownloadsRemaining || 0,
      unlimitedDownloads: !!x.UnlimitedDownloads, expire: x.ExpireAt || 0, unlimitedTime: !!x.UnlimitedTime,
      password: !!x.IsPasswordProtected, fileUrl: x.UrlDownload || "" };
  };

  var v = null, items = [], at = 0, touchX = null, returnFocus = null;
  // The opening page's Save to Photos options (its zip for big videos)
  var viewerPhotos = null;
  // Opened while selecting (R3, V8): { isSelected(id), setSelected(id, on) }
  // of the opening list, behind the "Selected" checkbox; null otherwise
  var viewerPick = null;
  // Paging (Part 3): more() promises the list's next files, asked for when
  // the reader reaches the last one; moreBusy while it is on its way,
  // moreWanted when they asked to go on meanwhile. session drops an answer
  // that arrives after the viewer closed or opened another list.
  var more = null, moreBusy = false, moreWanted = false, session = 0;
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
    // Save to Photos: the item on screen, on iOS with navigator.canShare, for
    // photos and videos whose original can be fetched. A prepared set belongs
    // to its item, so moving on forgets it.
    var photos = v.querySelector(".fgs-v-photos");
    if (photos) {
      FGS.resetSaveToPhotos(photos);
      photos.hidden = !(FGS.isIOS() && navigator.canShare && it.download && FGS.isPhotoOrVideo(it.kind, it.name));
    }
    // Photos are shown as a smaller preview; say so wherever the original can be fetched.
    // Kind, not the stream URL: a capped video withholds the stream but still shows only a poster.
    v.querySelector(".fgs-v-lowres").hidden = !(it.view && it.kind === "image" && it.download);
    var pick = v.querySelector(".fgs-v-select");
    if (pick) {
      pick.hidden = !viewerPick;
      if (viewerPick) pick.querySelector("[data-fgs-v-select]").checked = viewerPick.isSelected(it.id);
    }
    updateNav();
    // The last one loaded: ask for the next files now
    if (at === items.length - 1) fetchMore();
    // Warm the neighbour so arrowing feels instant
    [at - 1, at + 1].forEach(function (i) { if (items[i] && items[i].view) { (new Image()).src = items[i].view; } });
  }
  function updateNav() {
    v.querySelector(".fgs-v-prev").disabled = at === 0;
    v.querySelector(".fgs-v-next").disabled = at === items.length - 1 && !more;
  }
  // The list's next files onto the end. Nothing more (or a failure) ends
  // the paging for this viewing.
  function fetchMore() {
    if (!more || moreBusy) return;
    moreBusy = true;
    var mine = session, ask = more;
    var done = function (list) {
      if (mine !== session) return;
      moreBusy = false;
      list = (list || []).filter(Boolean);
      if (list.length) items = items.concat(list);
      else more = null;
      var onward = moreWanted && at < items.length - 1;
      moreWanted = false;
      if (onward) { at++; render(); } else updateNav();
    };
    Promise.resolve().then(ask).then(done, function () { done([]); });
  }
  function go(delta) {
    var n = at + delta;
    if (n >= 0 && n < items.length) { at = n; render(); }
    else if (delta > 0 && more) { moreWanted = true; fetchMore(); }
  }
  function close() {
    session++; more = null; moreBusy = false; moreWanted = false; viewerPick = null;
    v.hidden = true; clearStage(v.querySelector(".fgs-v-stage")); document.body.style.overflow = "";
    FGS.resetSaveToPhotos(v.querySelector(".fgs-v-photos"));
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

  // list: FGS.itemFromEl items; more (optional): a function returning a
  // Promise of the items that follow the list's last one; photosOpts
  // (optional): FGS.saveToPhotos options for the viewer's Save to Photos
  // (zipIds, zipSkipsForeign; never a status, which the viewer covers);
  // selection (optional, R3): { isSelected(id), setSelected(id, on) } of a
  // list in select mode, which shows the "Selected" checkbox
  FGS.openViewer = function (list, index, moreItems, photosOpts, selection) {
    if (!v) {
      v = document.getElementById("fgs-viewer");
      if (v) {
        bindTouch(); bindWheel();
        // iOS Safari dispatches click on a non-interactive element (the backdrop,
        // the bare stage) only if it or an ancestor below the document has a
        // click listener. The close itself is decided in the document handler.
        v.addEventListener("click", function () {});
        var pickBox = v.querySelector("[data-fgs-v-select]");
        if (pickBox) pickBox.addEventListener("change", function () {
          if (viewerPick && items[at]) viewerPick.setSelected(items[at].id, pickBox.checked);
        });
      }
    }
    if (!v || !list || !list.length) return;
    if (v.hidden) returnFocus = document.activeElement;
    items = list.slice(); at = Math.min(Math.max(0, index | 0), list.length - 1);
    session++; more = typeof moreItems === "function" ? moreItems : null; moreBusy = false; moreWanted = false;
    viewerPhotos = photosOpts || null;
    viewerPick = selection && typeof selection.isSelected === "function" ? selection : null;
    wheelSum = 0; wheelLockUntil = 0;
    v.hidden = false; document.body.style.overflow = "hidden";
    render();
    v.querySelector(".fgs-v-close").focus();
  };
  // The viewer's Save to Photos: the item on screen
  function savePhotoInViewer(button) {
    var it = items[at];
    if (!it || !it.download) return;
    FGS.saveToPhotos([{ id: it.id, url: it.download, name: it.name, kind: it.kind, bytes: it.bytes, taken: it.taken,
      uploaded: it.uploaded, foreign: it.foreign }], button, viewerPhotos ? { zipIds: viewerPhotos.zipIds, zipSkipsForeign: viewerPhotos.zipSkipsForeign } : null);
  }
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
    else if (e.target.closest(".fgs-v-photos")) savePhotoInViewer(e.target.closest(".fgs-v-photos"));
    else if (pressedOnBackdrop && Date.now() >= swipeUntil && isEmptySpace(e.target)) close();
  });
  document.addEventListener("keydown", function (e) {
    if (!v || v.hidden) return;
    if (e.key === "Escape") { close(); return; }
    if (e.key === "Tab") {
      // Keep focus inside the dialog
      var f = Array.prototype.filter.call(v.querySelectorAll("button, a[href], video[controls], input"),
        function (x) { return !x.disabled && !x.hidden && !x.closest("[hidden]"); });
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
  // (square without them), in cols columns (the slider's count, R3).
  // Returns false while the container is hidden: nothing sensible to
  // compute, and a later show lays out again.
  function masonry(container, tiles, cols) {
    var width = container.clientWidth, gap = 8;
    if (!width) return false;
    cols = Math.max(1, cols | 0) || 1;
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

  // Grid and Board columns (R3, V9). The slider counts columns: its left
  // end is the most that fit at >= 96 px a tile, its right end 1. The
  // default is each view's old look, never under two columns, as before:
  // Grid's 160 px tiles, Board's ~260 px columns. remembered is the
  // view's stored count, clamped to what fits now.
  var TILE_GAP = 12, MIN_TILE = 96, DEFAULT_TILE = 160, BOARD_COLUMN = 260;
  FGS.tileColumns = function (width, remembered, view) {
    var max = Math.max(1, Math.floor((width + TILE_GAP) / (MIN_TILE + TILE_GAP)));
    var old = view === "board" ? Math.round(width / BOARD_COLUMN) : Math.floor((width + TILE_GAP) / (DEFAULT_TILE + TILE_GAP));
    var def = Math.min(max, Math.max(2, old));
    var n = parseInt(remembered, 10);
    var cols = n >= 1 && String(n) === String(remembered) ? Math.min(n, max) : def;
    return { max: max, def: def, cols: cols };
  };
  // Where a drag was released, as a column count between stops: within one
  // step of the default it is the default, else the nearest stop. So the
  // default catches a release from either neighbour's near half, and every
  // other count, the ends included, can still be chosen.
  FGS.snapColumns = function (position, def) {
    return Math.abs(position - def) < 1 ? def : Math.round(position);
  };
  // The view's columns for the grid's width now (null while it is
  // hidden), in place on the grid for Grid's CSS (--fgs-cols) and Board's
  // masonry. Each view remembers its own count ("cols-grid", "cols-board");
  // the one shared "cols" of the first R3 build was Grid's.
  function placeColumns(root, grid, view) {
    var width = grid.clientWidth;
    if (!width) return null;
    var remembered = loadBrowserPref(root, "cols-" + view);
    if (remembered === null && view === "grid") remembered = loadBrowserPref(root, "cols");
    var c = FGS.tileColumns(width, remembered, view);
    grid.style.setProperty("--fgs-cols", String(c.cols));
    grid.classList.add("fgs-has-cols");
    return c;
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
  // The tile badge of a name: the first letters of its first and last parts,
  // upper case ("anna.mueller" -> "AM"). Must equal initialsOf in
  // FileBrowser.go.
  function initialsOf(name) {
    var parts = String(name || "").split(/[^\p{L}\p{Nd}]+/u).filter(Boolean);
    if (!parts.length) return "";
    var first = Array.from(parts[0])[0];
    if (parts.length === 1) return first.toUpperCase();
    return (first + Array.from(parts[parts.length - 1])[0]).toUpperCase();
  }
  // A person's badge or circle (Part 4, S2): their photo when there is one,
  // else their initials. The initials stay in data-initials, for when the
  // photo fails to load. Built with setters only: a name is user input.
  function fillAvatar(holder, initials, photo, size) {
    if (!holder) return;
    initials = initials || "";
    holder.textContent = "";
    holder.setAttribute("data-initials", initials);
    if (!photo) {
      holder.classList.remove("fgs-has-photo");
      holder.textContent = initials;
      return;
    }
    holder.classList.add("fgs-has-photo");
    var img = document.createElement("img");
    img.className = "fgs-avatar-img";
    img.setAttribute("src", photo);
    img.setAttribute("alt", "");
    img.setAttribute("loading", "lazy");
    img.setAttribute("referrerpolicy", "no-referrer");
    img.setAttribute("width", String(size || 26));
    img.setAttribute("height", String(size || 26));
    holder.appendChild(img);
  }
  FGS.fillAvatar = fillAvatar;
  FGS.initialsOf = initialsOf;
  // A photo that does not load (its person left the group, or chose
  // initials since the page was rendered) becomes the initials beside it.
  // error does not bubble, so this listens in the capture phase, from
  // <head>, before any photo can fail.
  function avatarFailed(e) {
    var img = e.target;
    // The user menu's photo (Part 4 polish) becomes the person icon it replaced
    if (img && img.tagName === "IMG" && img.classList && img.classList.contains("fgs-usermenu-photo")) {
      var icon = document.createElement("i");
      icon.className = "bi bi-person-fill";
      if (img.parentNode) {
        img.parentNode.classList.remove("fgs-usermenu-has-photo");
        img.parentNode.replaceChild(icon, img);
      }
      return;
    }
    if (!img || img.tagName !== "IMG" || !img.classList || !img.classList.contains("fgs-avatar-img")) return;
    var holder = img.parentNode;
    if (holder && holder.hasAttribute && holder.hasAttribute("data-initials")) {
      fillAvatar(holder, holder.getAttribute("data-initials"), "");
    }
  }
  document.addEventListener("error", avatarFailed, true);
  // A cell's content as the template renders a time: <span data-ts>
  function setTimeCell(cell, ts) {
    cell.textContent = "";
    var span = document.createElement("span");
    span.setAttribute("data-ts", String(ts || 0));
    span.textContent = ts ? fmt(ts) : "";
    cell.appendChild(span);
  }
  function formatTimes(scope) {
    Array.prototype.forEach.call(scope.querySelectorAll("[data-ts]"), function (el) {
      var ts = parseInt(el.getAttribute("data-ts"), 10);
      el.textContent = ts ? fmt(ts) : "";
    });
  }

  // A list's remembered choices, per context: "fgsBrowser.<context>.<k>".
  // A group page's view, sort and filters are per group (R1: as the view
  // is), so a tap on Grid in one group leaves every other group on Board.
  // The Files tab falls back to the keys it used before it moved onto this
  // component.
  // localStorage throws in private mode: then nothing is remembered.
  var PER_GROUP_PREFS = { view: true, sort: true, hideDownloaded: true, kind: true };
  var LEGACY_FILES_KEYS = { view: "filesLayout", shape: "filesTileShape", scope: "filesScope" };
  function browserPrefKey(root, k) {
    var context = root.getAttribute("data-context") || "";
    var group = root.getAttribute("data-group-id") || "";
    if (PER_GROUP_PREFS[k] && context === "group" && group) context = "group." + group;
    return "fgsBrowser." + context + "." + k;
  }
  FGS.browserPrefKey = browserPrefKey;
  function loadBrowserPref(root, k) {
    try {
      var v = localStorage.getItem(browserPrefKey(root, k));
      if (v === null && root.getAttribute("data-context") === "files" && LEGACY_FILES_KEYS[k]) v = localStorage.getItem(LEGACY_FILES_KEYS[k]);
      return v;
    } catch (e) { return null; }
  }
  function storeBrowserPref(root, k, v) {
    try { localStorage.setItem(browserPrefKey(root, k), v); } catch (e) { /* private mode */ }
  }
  // The remembered view if it is one, else the context's default
  function initialView(root) {
    var v = loadBrowserPref(root, "view");
    return VIEWS.indexOf(v) >= 0 ? v : root.getAttribute("data-default-view");
  }

  // List batching (Part 3). A page renders the first batch of its list in
  // the list's own order (data-sort-key, data-sort-desc) and with no filter
  // but the Files tab's "My files"; its feed (data-list-url) answers the
  // later batches and every other query. A list without a feed (the upload
  // page: only this visit's uploads) sorts and searches itself.
  var SORT_KEYS = ["name", "uploaded", "taken", "size", "expires", "downloads"];
  // A header's first click: newest, biggest and most first; names and
  // shooting order from the start
  var DESC_FIRST = { uploaded: true, size: true, expires: true, downloads: true };
  // The Type filter's values (kind=)
  var KINDS = ["image", "video", "other"];
  function defaultOrder(root) {
    var key = root.getAttribute("data-sort-key");
    if (SORT_KEYS.indexOf(key) >= 0) return { sort: key, desc: root.getAttribute("data-sort-desc") === "true" };
    var taken = root.getAttribute("data-sort") === "taken-asc";
    return { sort: taken ? "taken" : "uploaded", desc: !taken };
  }
  // The remembered List sort ("name:asc"), where this list has that column
  function rememberedOrder(root) {
    var m = /^([a-z]+):(asc|desc)$/.exec(loadBrowserPref(root, "sort") || "");
    if (!m || SORT_KEYS.indexOf(m[1]) < 0 || !root.querySelector('th[data-fgs-sortkey="' + m[1] + '"]')) return null;
    return { sort: m[1], desc: m[2] === "desc" };
  }
  // The query a list starts with: its defaults, then the remembered scope,
  // "Hide what I've downloaded", Type and sort. The person and the search
  // are never remembered.
  function initialQuery(root) {
    var order = rememberedOrder(root) || defaultOrder(root);
    // A list without a feed (the upload page) starts unfiltered every visit:
    // a remembered filter would hide an upload of another type as it lands.
    var feed = !!root.getAttribute("data-list-url");
    var kind = feed ? loadBrowserPref(root, "kind") : null;
    var q = { scope: "", person: "", hide: false, kind: KINDS.indexOf(kind) >= 0 ? kind : "", q: "", sort: order.sort, desc: order.desc };
    if (feed && root.querySelector("button[data-scope]")) q.scope = loadBrowserPref(root, "scope") === "all" ? "all" : "mine";
    if (feed && root.querySelector("[data-fgs-hide-downloaded]")) q.hide = loadBrowserPref(root, "hideDownloaded") === "true";
    return q;
  }
  FGS.initialQuery = initialQuery;
  // The server rendered another first batch than the remembered query asks
  // for: the list reloads it before showing anything
  function needsReload(root) {
    if (!root.getAttribute("data-list-url")) return false;
    var q = initialQuery(root), d = defaultOrder(root);
    return q.scope === "all" || q.hide || !!q.kind || q.sort !== d.sort || q.desc !== d.desc;
  }

  // The button of this value is the active one in its group of buttons
  function markActive(root, attr, value) {
    root.querySelectorAll("button[" + attr + "]").forEach(function (b) {
      if (b.closest("[data-fgs-browser]") !== root) return;
      var on = b.getAttribute(attr) === value;
      b.classList.toggle("btn-light", on);
      b.classList.toggle("btn-outline-light", !on);
    });
  }
  // Called by the inline script directly after each list root, before the
  // first paint: the remembered view, its button, and Board's columns, so
  // a List or Board page never flashes Grid; and where a remembered filter
  // or sort needs another first batch, the views stay invisible until it is
  // in (fgs-reloading), so the wrong batch never flashes either.
  // initFileBrowser confirms it.
  FGS.presetView = function (root) {
    if (!root) return;
    if (needsReload(root)) {
      var views = root.querySelector(".fgs-views");
      if (views) views.classList.add("fgs-reloading");
    }
    var v = initialView(root);
    if (VIEWS.indexOf(v) < 0) return;
    root.setAttribute("data-current-view", v);
    markActive(root, "data-layout", v);
    var grid = root.querySelector("[data-fgs-grid]");
    if (!grid || v === "list") return;
    var c = placeColumns(root, grid, v);
    if (v !== "board") return;
    grid.classList.add("fgs-board-mode");
    if (c) masonry(grid, Array.prototype.filter.call(grid.children, function (t) {
      return t.classList.contains("fgs-tile") && !t.classList.contains("d-none");
    }), c.cols);
  };

  FGS.initFileBrowser = function (root) {
    if (!root) return null;
    if (root.fgsBrowser) return root.fgsBrowser;
    var context = root.getAttribute("data-context") || "";
    var grid = root.querySelector("[data-fgs-grid]");
    var listWrap = root.querySelector("[data-fgs-list]");
    var table = listWrap.querySelector("table");
    var tbody = table.tBodies[0];
    var views = root.querySelector(".fgs-views");
    var emptyNote = root.querySelector(".fgs-empty");
    var bar = root.querySelector("[data-fgs-selectbar]");
    var selCount = bar.querySelector(".fgs-selcount");
    var status = bar.querySelector(".fgs-selstatus");
    var linkResult = bar.querySelector(".fgs-linkresult");
    var selectToggle = root.querySelector("[data-fgs-select-toggle]");
    var countEl = root.querySelector(".fgs-count");
    var size = root.querySelector(".fgs-size");
    var sizeStop = size && size.list ? size.list.querySelector("option") : null;
    var toolbar = root.querySelector("[data-fgs-toolbar]");
    var searchBox = root.querySelector("[data-fgs-search]");
    var sentinels = Array.prototype.slice.call(root.querySelectorAll("[data-fgs-more]"));
    var tplTile = root.querySelector("template[data-fgs-tpl=tile]");
    var tplRow = root.querySelector("template[data-fgs-tpl=row]");
    var infoUrl = root.getAttribute("data-info-url") || "";
    var zipUrl = root.getAttribute("data-zip-url") || "";
    // The zip of every live member (no files=), where the page has one
    var zipAllUrl = root.getAttribute("data-zip-all-url") || "";
    var manages = !!(tplTile && tplTile.content.querySelector("[data-menu=edit]"));
    // data-can-delete / data-can-remove are rendered without the rest of the
    // management data where the menu offers Delete or Remove from group
    var itemPerms = !!(tplTile && tplTile.content.querySelector("[data-menu=delete], [data-menu=remove-from-group]"));
    // Group pages (Part 2): the group's own download for live adds, shooting
    // order, and the person filter, "Hide what I've downloaded" and ticks
    var downloadBase = root.getAttribute("data-download-base") || "";
    var sortTakenAsc = root.getAttribute("data-sort") === "taken-asc";
    var groupTools = root.getAttribute("data-group-tools") === "true";
    var selectAllButton = bar.querySelector("[data-fgs-select-all]");
    var selectNewButton = bar.querySelector("[data-fgs-select-new]");
    var personSelect = root.querySelector("[data-fgs-person]");
    var hideDownloaded = root.querySelector("[data-fgs-hide-downloaded]");
    var scopeSwitch = !!root.querySelector("button[data-scope]");
    // The Sort & filter dropdown (R1)
    var sortRadios = Array.prototype.slice.call(root.querySelectorAll("[data-fgs-sort-option]"));
    var dirRadios = Array.prototype.slice.call(root.querySelectorAll("[data-fgs-sort-dir]"));
    var kindRadios = Array.prototype.slice.call(root.querySelectorAll("[data-fgs-kind]"));
    var filterBadge = root.querySelector("[data-fgs-filter-badge]");
    var filterReset = root.querySelector("[data-fgs-filter-reset]");
    // The upload page's element ids, only where the page holds one browser
    var legacyIds = root.getAttribute("data-legacy-ids") === "true";
    var selecting = false, view = null, api = null;
    var tilesById = {}, rowsById = {};

    // The feed and its state: the query, the cursor after the last rendered
    // item ("" once everything is in), and the counts of the last answer
    var listUrl = root.getAttribute("data-list-url") || "";
    var reloadFirst = needsReload(root);
    var query = initialQuery(root);
    var next = root.getAttribute("data-next") || "";
    function attrNumber(name) { return parseInt(root.getAttribute(name), 10) || 0; }
    var counts = { total: attrNumber("data-total"), all: attrNumber("data-all"), fresh: attrNumber("data-new") };
    // The one request in flight ({promise, abort}); a reload makes every
    // older answer stale (generation)
    var pending = null, generation = 0;
    // The sentinels' state: "", "loading" or "error"
    var moreState = "";

    // Remembered choices (browserPrefKey)
    function load(k) { return loadBrowserPref(root, k); }
    function store(k, v) { storeBrowserPref(root, k, v); }
    function setActive(attr, value) { markActive(root, attr, value); }

    function tileCheck(tile) { return tile.querySelector(".fgs-tile-check"); }
    function rowCheck(row) { return row && row.querySelector(".fgs-row-check"); }
    function idOf(el) { return el.getAttribute("data-id"); }
    function menuOf(tile) { return tile.querySelector("[data-fgs-file-menu]"); }

    // id -> tile and list row
    function index(tile, row) {
      tilesById[idOf(tile)] = tile;
      if (row) rowsById[row.getAttribute("data-row-id")] = row;
    }
    Array.prototype.forEach.call(grid.querySelectorAll(".fgs-tile"), function (t) { index(t, null); });
    Array.prototype.forEach.call(tbody.querySelectorAll("tr[data-row-id]"), function (tr) {
      rowsById[tr.getAttribute("data-row-id")] = tr;
    });

    // The files the user sees, in the order they see them: Grid and Board in
    // page order, List in the table's (the same, the server's order). Hidden
    // by a search of a list without a feed, or deleted since: left out.
    function visibleTiles() {
      var tiles = view === "list"
        ? Array.prototype.map.call(tbody.rows, function (tr) {
          return tr.classList.contains("d-none") ? null : tilesById[tr.getAttribute("data-row-id")];
        })
        : Array.prototype.filter.call(grid.children, function (t) {
          return t.classList.contains("fgs-tile") && !t.classList.contains("d-none");
        });
      return tiles.filter(function (t) { return t && t.isConnected; });
    }
    // The selection (Part 3). "ids": files ticked one by one, all of them
    // rendered. "query": "Select all" or "Select everything new to me" over
    // the whole list, loaded or not: the query as it was then (fresh: only
    // what is new to me), less the files unticked since (except), plus files
    // outside it ticked since (plus: the new-to-me capture's had files).
    // Any reload, and leaving select mode, clears it.
    var sel = null;
    function clearSelection() { sel = { mode: "ids", ids: new Set() }; }
    clearSelection();
    // A rendered file is in the captured query: every rendered file matches
    // the query, which is the captured one until a reload clears it
    function inCapture(tile) { return sel.mode === "query" && (!sel.fresh || isNewToMe(tile)); }
    function isSelected(tile) {
      var id = idOf(tile);
      if (sel.mode === "ids") return sel.ids.has(id);
      return (inCapture(tile) && !sel.except.has(id)) || sel.plus.has(id);
    }
    // A file no longer listed leaves the selection
    function forget(id) {
      if (sel.mode === "ids") sel.ids.delete(id);
      else { sel.except.delete(id); sel.plus.delete(id); }
    }
    // The ticks of these rendered files (all of them by default) as the
    // selection says
    function syncTicks(tiles) {
      (tiles || Object.keys(tilesById).map(function (id) { return tilesById[id]; })).forEach(function (t) {
        var on = isSelected(t);
        tileCheck(t).checked = on;
        var check = rowCheck(rowsById[idOf(t)]);
        if (check) check.checked = on;
      });
    }
    // "N selected": a query selection counts the files matching it, loaded or not
    function selectedCount() {
      if (sel.mode === "ids") return sel.ids.size;
      return Math.max(0, (sel.fresh ? counts.fresh : counts.total) - sel.except.size) + sel.plus.size;
    }
    function selected() { return visibleTiles().filter(isSelected); }
    function selectionInfo() {
      if (sel.mode === "ids") return { mode: "ids", ids: selected().map(idOf) };
      var q = Object.assign({}, sel.query);
      if (sel.fresh) q["new"] = true;
      return { mode: "query", query: q, except: Array.from(sel.except), plus: Array.from(sel.plus) };
    }

    // "Files: N" and "Select all (N)": N is every file matching the query,
    // loaded or not. A list without a feed counts what it shows.
    function refreshCount() {
      var shown = visibleTiles().length;
      if (!listUrl) {
        counts.total = shown;
        counts.all = Object.keys(tilesById).length;
      }
      if (countEl) {
        // The label is a word phones drop (media.css), the number stays
        var label = document.createElement("span");
        label.className = "fgs-label";
        label.textContent = root.getAttribute("data-count-label") || "Files: ";
        countEl.textContent = "";
        countEl.appendChild(label);
        countEl.appendChild(document.createTextNode(String(counts.total)));
      }
      // Empty, unless a batch is on its way or failed (the sentinel says so)
      root.classList.toggle("fgs-is-empty", shown === 0 && moreState === "");
      if (emptyNote) emptyNote.textContent = counts.all > 0 ? "No matching files" : "No files yet";
      refreshSelectionCount();
    }
    function refreshSelectAll() {
      if (selectAllButton) selectAllButton.textContent = "Select all (" + counts.total + ")";
    }
    // "N selected" and "Select all (N)" after the counts changed, the
    // selection itself unchanged (a batch arrived, a file left)
    function refreshSelectionCount() {
      var n = selectedCount();
      selCount.textContent = n + " selected";
      // Greyed out rather than disabled: some actions disable their own
      // button while they run, and a selection change must not undo that
      bar.classList.toggle("fgs-none-selected", n === 0);
      refreshSelectAll();
    }

    function updateSelection() {
      bar.classList.toggle("d-none", !selecting);
      placeBarsSoon();
      // A prepared set, or a link made from it, belongs to its selection
      var photos = bar.querySelector("[data-act=photos]");
      if (photos) FGS.resetSaveToPhotos(photos);
      if (linkResult) { linkResult.textContent = ""; linkResult.className = "fgs-linkresult"; }
      refreshSelectionCount();
    }

    function setChecked(id, on) {
      var tile = tilesById[id];
      if (tile) tileCheck(tile).checked = on;
      var check = rowCheck(rowsById[id]);
      if (check) check.checked = on;
      if (sel.mode === "ids") {
        if (on) sel.ids.add(id);
        else sel.ids.delete(id);
      } else if (tile && inCapture(tile)) {
        if (on) sel.except.delete(id);
        else sel.except.add(id);
      } else if (on) sel.plus.add(id);
      else sel.plus.delete(id);
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
      if (!selecting) {
        clearSelection();
        syncTicks();
      }
      status.textContent = "";
      updateSelection();
    }

    // The selection becomes exactly the visible files fn(tile) accepts,
    // entering select mode if it is off. Returns how many that is.
    function selectWhere(fn) {
      if (!selecting) setSelecting(true);
      clearSelection();
      visibleTiles().forEach(function (t) { if (fn(t)) sel.ids.add(idOf(t)); });
      syncTicks();
      status.textContent = "";
      updateSelection();
      return sel.ids.size;
    }
    // "Select all (N)" and "Select everything new to me": the whole list
    // under the current query, loaded or not. A list without a feed holds
    // everything, so it ticks what it shows.
    function selectQuery(fresh) {
      if (!listUrl) return selectWhere(fresh ? isNewToMe : function () { return true; });
      if (!selecting) setSelecting(true);
      sel = { mode: "query", query: Object.assign({}, query), fresh: !!fresh, except: new Set(), plus: new Set() };
      syncTicks();
      status.textContent = "";
      updateSelection();
      return selectedCount();
    }
    // New to me: neither downloaded nor my own upload (D12)
    function isNewToMe(tile) {
      return tile.getAttribute("data-downloaded") !== "true" && tile.getAttribute("data-mine") !== "true";
    }
    // The query's filters on one file, as the server applies them: for live
    // adds, files just downloaded under "Hide what I've downloaded", and the
    // search of a list without a feed
    function matchesQuery(tile) {
      if (query.scope === "mine" && tile.getAttribute("data-mine") !== "true") return false;
      if (query.person && tile.getAttribute("data-uploader-id") !== query.person) return false;
      if (query.hide && !isNewToMe(tile)) return false;
      if (query.kind && FGS.fileKind(tile.getAttribute("data-kind"), tile.getAttribute("data-name")) !== query.kind) return false;
      var q = query.q.trim().toLowerCase();
      return !q || (tile.getAttribute("data-name") || "").toLowerCase().indexOf(q) >= 0;
    }

    // The viewer, over everything visible, starting at this file. Reaching
    // the last one asks for what follows it: the files listed after it since
    // (a batch the sentinel loaded meanwhile), else the next batch
    // (loadMore). Never what loadMore alone returned, which would skip a
    // batch that arrived while the viewer was open.
    function openAt(tile) {
      if (!tile) return;
      var tiles = visibleTiles();
      var last = tiles.length ? idOf(tiles[tiles.length - 1]) : "";
      function after() {
        var now = visibleTiles();
        var at = now.findIndex(function (t) { return idOf(t) === last; });
        var rest = at >= 0 ? now.slice(at + 1) : [];
        if (rest.length) last = idOf(rest[rest.length - 1]);
        return rest.map(FGS.itemFromEl);
      }
      function more() {
        var listed = after();
        if (listed.length) return Promise.resolve(listed);
        return loadMore().then(after);
      }
      var po = photosOpts();
      // While selecting, the viewer's "Selected" checkbox is the file's tick
      var pick = !selecting ? null : {
        isSelected: function (id) { return !!tilesById[id] && isSelected(tilesById[id]); },
        setSelected: function (id, on) {
          setChecked(id, on);
          status.textContent = "";
          updateSelection();
        }
      };
      FGS.openViewer(tiles.map(FGS.itemFromEl), Math.max(0, tiles.indexOf(tile)), listUrl ? more : null,
        { zipIds: po.zipIds, zipSkipsForeign: po.zipSkipsForeign }, pick);
    }

    // Board: the grid's own tiles in masonry, true proportions, no captions
    function layoutBoard() {
      if (view !== "board" || !columns) return;
      masonry(grid, visibleTiles(), columns.cols);
    }
    function clearBoard() {
      Array.prototype.forEach.call(grid.querySelectorAll(".fgs-tile"), function (t) {
        ["position", "width", "height", "left", "top"].forEach(function (p) { t.style[p] = ""; });
      });
      grid.style.height = "";
    }
    var boardTimer = null;
    function layoutLater() { window.clearTimeout(boardTimer); boardTimer = window.setTimeout(layoutBoard, 120); }
    // A new width: new ends and default stop, the remembered count clamped
    function resized() { updateColumns(); layoutLater(); }
    if ("ResizeObserver" in window) new ResizeObserver(resized).observe(grid);
    else window.addEventListener("resize", resized);

    function applyView(v) {
      if (VIEWS.indexOf(v) < 0) v = root.getAttribute("data-default-view") || "grid";
      var was = view;
      view = v;
      root.setAttribute("data-current-view", v);
      grid.classList.toggle("d-none", v === "list");
      listWrap.classList.toggle("d-none", v !== "list");
      grid.classList.toggle("fgs-board-mode", v === "board");
      if (was === "board" && v !== "board") clearBoard();
      updateColumns();
      setActive("data-layout", v);
      store("view", v);
      layoutBoard();
      updateSelection();
      // The other view's sentinel may be the one in reach now
      observeMore();
    }

    // My files / Everything: the server's filter, remembered
    function applyScope(s) {
      if (!scopeSwitch) return;
      s = s === "all" ? "all" : "mine";
      setActive("data-scope", s);
      store("scope", s);
      if (s === query.scope) return;
      query.scope = s;
      showFilters();
      reload();
    }

    // The viewer now has these files (group pages: the tick, and "Hide what
    // I've downloaded"). keepVisible leaves them shown for now, so a
    // selection being worked on (Save to Photos' prepared set) survives.
    // Under "Hide what I've downloaded" they leave the list; the cursor is a
    // position in the order, so the next batch skips nothing.
    // Files this page has downloaded (or queued): the server may list them
    // as new for a while yet, while the downloads are still on their way
    var had = new Set();
    // ofSelection: the files are the selection, acted on from the bar
    function markDownloaded(ids, keepVisible, ofSelection) {
      (ids || []).forEach(function (id) {
        had.add(String(id));
        setData(tilesById[id], "downloaded", "true");
        setData(rowsById[id], "downloaded", "true");
      });
      if (keepVisible || !query.hide) return;
      // A "Select all" under the filter has been acted on and its files
      // leave the list: it ends here, so a later batch (which may still
      // list files whose download is under way) cannot tick them again.
      // A single file taken from its menu only leaves the selection (below).
      if (ofSelection && sel.mode === "query") {
        clearSelection();
        syncTicks();
      }
      // Files of the selection not rendered yet leave the list too: they
      // matched it, and now no longer do
      var gone = 0;
      (ids || []).forEach(function (id) {
        var listed = !!tilesById[id];
        if (dropItem(id) || (!listed && listUrl)) {
          gone++;
          forget(id);
        }
      });
      if (!gone) return;
      counts.total = Math.max(0, counts.total - gone);
      // Under the filter every listed file was new to me
      counts.fresh = Math.max(0, counts.fresh - gone);
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
    // Columns (R3, V9): the slider runs from the most that fit (left) to 1
    // (right), so its value is max + 1 - columns. The count is remembered
    // as columns; columns is FGS.tileColumns for the grid's width now.
    var columns = null;
    function showColumns() {
      if (!size || !columns) return;
      size.max = String(columns.max);
      size.value = String(columns.max + 1 - columns.cols);
      size.disabled = columns.max < 2;
      size.setAttribute("aria-valuetext", columns.cols + (columns.cols === 1 ? " column" : " columns"));
      if (sizeStop) sizeStop.value = String(columns.max + 1 - columns.def);
    }
    function updateColumns() {
      if (view !== "grid" && view !== "board") return;
      var c = placeColumns(root, grid, view);
      if (!c) return; // hidden (List): its width says nothing
      columns = c;
      showColumns();
    }
    function setColumns(n) {
      if (!columns) return;
      n = Math.min(columns.max, Math.max(1, n | 0));
      store("cols-" + view, String(n));
      columns = { max: columns.max, def: columns.def, cols: n };
      grid.style.setProperty("--fgs-cols", String(n));
      showColumns();
      layoutBoard();
    }
    // A drag released within one step of the default stop lands on it
    // (FGS.snapColumns). Pointer only: the arrow keys step one by one.
    function snapOnRelease(e) {
      if (!columns || columns.max < 2) return;
      var box = size.getBoundingClientRect(), thumb = 16, span = box.width - thumb;
      if (span <= 0) return;
      var at = Math.min(1, Math.max(0, (e.clientX - box.left - thumb / 2) / span));
      var position = columns.max - at * (columns.max - 1);
      if (FGS.snapColumns(position, columns.def) === columns.def && columns.cols !== columns.def) setColumns(columns.def);
    }

    // Sticky toolbar and selection bar (R3, V7): the selection bar sits
    // under the toolbar's measured height; while stuck, both keep clear of
    // the branding's fixed buttons (the sidebar toggle at the top left, the
    // user menu at the top right) by padding past whichever they overlap.
    var FIXED_CHROME = ["fgs-sidebar-toggle", "fgs-usermenu"];
    function clearChrome(el, stuck) {
      var left = 0, right = 0;
      if (stuck) {
        var box = el.getBoundingClientRect();
        FIXED_CHROME.forEach(function (id) {
          var fixed = document.getElementById(id);
          var r = fixed ? fixed.getBoundingClientRect() : null;
          if (!r || !r.width || r.bottom <= box.top || r.top >= box.bottom || r.right <= box.left || r.left >= box.right) return;
          if (r.left + r.width / 2 < box.left + box.width / 2) left = Math.max(left, Math.ceil(r.right - box.left) + 6);
          else right = Math.max(right, Math.ceil(box.right - r.left) + 6);
        });
      }
      el.style.setProperty("--fgs-inset-l", left + "px");
      el.style.setProperty("--fgs-inset-r", right + "px");
    }
    function placeBars() {
      if (!toolbar || !root.isConnected) return;
      root.style.setProperty("--fgs-toolbar-h", Math.ceil(toolbar.getBoundingClientRect().height) + "px");
      var top = parseFloat(window.getComputedStyle(toolbar).top) || 0;
      var stuck = root.getBoundingClientRect().top < top - 1 && toolbar.getBoundingClientRect().top <= top + 1;
      toolbar.classList.toggle("fgs-stuck", stuck);
      clearChrome(toolbar, stuck);
      clearChrome(bar, stuck && !bar.classList.contains("d-none"));
    }
    var barsFrame = 0;
    function placeBarsSoon() {
      if (barsFrame) return;
      barsFrame = window.requestAnimationFrame(function () { barsFrame = 0; placeBars(); });
    }

    // Download and Save to Photos items, with the dates the File gets
    function asItems(tiles) { return tiles.map(FGS.fileItemFromEl); }

    // data: FGS.itemData of every file acted on, rendered or not (Part 3);
    // ids are theirs, tiles only the rendered ones
    function actionArg(tiles, button, bulk, menu, trigger, data) {
      data = data || tiles.map(FGS.itemData);
      return { browser: api, root: root, context: context, tiles: tiles, ids: data.map(function (d) { return d.id; }),
        data: data, items: tiles.map(FGS.itemFromEl), bulk: bulk, button: button, trigger: trigger || button,
        menu: menu || (tiles.length === 1 && data.length === 1 ? menuOf(tiles[0]) : null), status: status, linkResult: linkResult };
    }

    // The selection as FGS.itemData, in list order: the ticked files, or a
    // query selection's whole matching list from the feed (limit=all) less
    // what was unticked. Resolves to null when a reload dropped the
    // selection meanwhile.
    function resolveSelected() {
      if (sel.mode === "ids") return Promise.resolve(selected().map(FGS.itemData));
      var s = sel, gen = generation;
      var url = feedUrlFor(s.query, "") + (s.fresh ? "&new=1" : "") + "&limit=all";
      return fetch(url, { credentials: "same-origin", cache: "no-store" })
        .then(function (r) {
          if (!r.ok) throw new Error("The server answered " + r.status);
          return r.json();
        })
        .then(function (answer) {
          if (gen !== generation || sel !== s) return null;
          if (!answer || !Array.isArray(answer.Items)) throw new Error("Unexpected answer");
          var seen = {};
          // Files this page has downloaded meanwhile (a single file from its
          // menu) left a hide or new-to-me capture, though the server may
          // still list them while their download is under way
          var leftHad = s.query.hide || s.fresh;
          var data = answer.Items.map(FGS.itemData).filter(function (d) {
            seen[d.id] = true;
            return !s.except.has(d.id) && !(leftHad && had.has(d.id));
          });
          s.plus.forEach(function (id) {
            if (!seen[id] && tilesById[id]) data.push(FGS.itemData(tilesById[id]));
          });
          return data;
        });
    }
    // Every file of the list, loaded or not, in list order, as FGS.itemData
    // (the collection page's own buttons). Fetched only while some are not
    // rendered, or a filter hides some.
    function resolveAll() {
      var rendered = function () {
        return Array.prototype.filter.call(grid.children, function (t) { return t.classList.contains("fgs-tile"); }).map(FGS.itemData);
      };
      if (!listUrl || (!next && !filtered(query))) return Promise.resolve(rendered());
      var everything = { scope: query.scope ? "all" : "", person: "", hide: false, kind: "", q: "", sort: query.sort, desc: query.desc };
      return fetch(feedUrlFor(everything, "") + "&limit=all", { credentials: "same-origin", cache: "no-store" })
        .then(function (r) {
          if (!r.ok) throw new Error("The server answered " + r.status);
          return r.json();
        })
        .then(function (answer) {
          if (!answer || !Array.isArray(answer.Items)) throw new Error("Unexpected answer");
          return answer.Items.map(FGS.itemData);
        });
    }
    function filtered(q) { return q.scope === "mine" || !!q.person || !!q.hide || !!q.kind || !!q.q.trim(); }

    // A zip of these files: a POSTed form (no URL length limit), answered
    // with an attachment, so the page stays. ZipUrl ends in the parameter
    // that takes the ids ("./files/zip?ids=", "./cz?id=C&files="); that
    // parameter becomes the form field. A context with its own zip (request
    // pages: presigned) is handed the ids instead.
    function hasZip() {
      return !!fileAction(context, "zip") || /[?&][^=&?]+=$/.test(zipUrl || "");
    }
    function zipIds(ids) {
      var handler = fileAction(context, "zip");
      if (handler) {
        handler({ browser: api, root: root, context: context, tiles: [], ids: ids.slice(),
          data: ids.map(function (id) { return tilesById[id] ? FGS.itemData(tilesById[id]) : { id: id }; }),
          items: [], bulk: true, button: null, trigger: null, menu: null, status: status, linkResult: linkResult });
        return true;
      }
      var m = /^(.*?)[?&]([^=&?]+)=$/.exec(zipUrl);
      if (!m) return false;
      var form = document.createElement("form");
      form.method = "post";
      form.action = m[1];
      form.target = "_self";
      form.hidden = true;
      var field = document.createElement("input");
      field.type = "hidden";
      field.name = m[2];
      field.value = ids.join(",");
      form.appendChild(field);
      document.body.appendChild(form);
      form.submit();
      window.setTimeout(function () { if (form.parentNode) form.parentNode.removeChild(form); }, 1000);
      return true;
    }

    // Built-in selection-bar actions: the ones visitors have too. data is
    // FGS.itemData of every selected file.
    function bulkDownload(data, button) {
      // Files without a link (end-to-end encrypted on the Files tab) can only
      // be decrypted on their own download page; the zip skips them too, so
      // fetch the rest and say what was left out
      var usable = data.filter(function (d) { return d.url; });
      var skipped = data.length - usable.length;
      if (usable.length) FGS.downloadSequentially(usable, { button: button, status: status });
      if (usable.length && groupTools) markDownloaded(usable.map(function (d) { return d.id; }), false, true);
      if (skipped) {
        window.setTimeout(function () {
          status.textContent = manages
            ? skipped + " end-to-end encrypted file(s) left out: open each from its own download link."
            : skipped + " file(s) left out: they cannot be downloaded here.";
        }, usable.length * 600 + 100); // after downloadSequentially's own last message
      }
    }
    // The whole list, unfiltered and nothing unticked: the zip without
    // files= (every live member), where the page has one
    function selectionIsEverything() {
      return sel.mode === "query" && !sel.fresh && !sel.except.size && !sel.plus.size && !filtered(sel.query);
    }
    function bulkZip(data) {
      if (!zipUrl) return;
      var ids = data.map(function (d) { return d.id; });
      if (zipAllUrl && selectionIsEverything()) window.location = zipAllUrl;
      else if (!zipIds(ids)) return;
      // Ticked only once the request has gone out
      if (groupTools) markDownloaded(ids, false, true);
    }

    // A bar action on the resolved selection
    function runBulk(what, act, data) {
      // On the Files tab, other people's files from the viewer's groups
      // (foreign) can be downloaded and saved, but nothing else: those
      // actions, the uncounted owner zip included, are their uploader's or
      // the group's. (Group pages mark them too, and act on them there.)
      if (context === "files" && what !== "download" && what !== "photos") {
        var own = data.filter(function (d) { return !d.foreign; });
        if (own.length < data.length) {
          var left = data.length - own.length;
          status.textContent = left + " file(s) from your groups left out: " +
            (what === "zip" ? "zip them on their group's page." : "they are someone else's.");
          if (!own.length) return;
          data = own;
        }
      }
      var tiles = data.map(function (d) { return tilesById[d.id]; }).filter(Boolean);
      var handler = fileAction(context, what);
      if (handler) { handler(actionArg(tiles, act, true, null, null, data)); return; }
      if (what === "download") bulkDownload(data, act);
      else if (what === "zip") bulkZip(data);
      else if (what === "photos") FGS.saveToPhotos(data, act, photosOpts());
    }

    // Save to Photos' options: what was left out goes to the status, big
    // videos to the zip (where the page has one), and on group pages each
    // batch is ticked downloaded once fetched, kept shown (hidden only later,
    // or the set would be dropped). The Files tab's zip only takes the
    // viewer's own files, so big videos from their groups are pointed at the
    // group's page rather than sent to a zip that would drop them.
    function photosOpts() {
      var skipForeign = context === "files";
      return {
        status: status,
        zipSkipsForeign: skipForeign,
        zipIds: hasZip() ? function (ids) {
          if (skipForeign) ids = ids.filter(function (id) { return !(tilesById[id] && FGS.itemData(tilesById[id]).foreign); });
          if (ids.length && zipIds(ids) && groupTools) markDownloaded(ids, true);
        } : null,
        fetched: groupTools ? function (ids) { markDownloaded(ids, true); } : null
      };
    }

    // One resolution at a time; the button tapped says "Loading…" meanwhile
    var resolving = false;
    bar.addEventListener("click", function (e) {
      var act = e.target.closest("[data-act]");
      if (!act || !bar.contains(act) || resolving || act.disabled) return;
      if (!selectedCount()) return;
      var what = act.getAttribute("data-act");
      // A prepared Save to Photos shares on this tap: nothing may come
      // between the tap and the share sheet
      if (what === "photos" && FGS.photosPending(act)) { runBulk(what, act, []); return; }
      var label = null;
      if (sel.mode === "query") {
        resolving = true;
        label = Array.prototype.map.call(act.childNodes, function (n) { return n.cloneNode(true); });
        act.textContent = "Loading…";
        act.disabled = true;
      }
      var restore = function () {
        if (!label) return;
        resolving = false;
        act.textContent = "";
        label.forEach(function (n) { act.appendChild(n); });
        act.disabled = false;
      };
      resolveSelected().then(function (data) {
        restore();
        if (data && data.length) runBulk(what, act, data);
      }, function () {
        restore();
        status.textContent = "Couldn't load the selection. Try again.";
      });
    });
    if (selectAllButton) selectAllButton.addEventListener("click", function () { selectQuery(false); });
    if (selectNewButton) selectNewButton.addEventListener("click", function () { selectQuery(true); });

    var help = bar.querySelector("[data-fgs-help]");
    if (FGS.isIOS()) {
      // Safari allows one download per tap, so the zip and Save to Photos
      // are the way to take several at once
      var dl = bar.querySelector("[data-act=download]");
      var photos = bar.querySelector("[data-act=photos]");
      // Save to Photos needs navigator.canShare, like the page's own button
      var canPhotos = !!photos && !!navigator.canShare;
      if (dl && (canPhotos || bar.querySelector("[data-act=zip]"))) dl.classList.add("d-none");
      if (canPhotos) photos.classList.remove("d-none");
      // One "?" per page: where the page has its own buttons (the collection
      // link page, FGS.initDownloadButtons), that one is the "?"
      if (help && !document.querySelector("[data-fgs-downloads]")) help.classList.remove("d-none");
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
      // Select mode's expand button: open, the selection unchanged (R3)
      var expand = e.target.closest(".fgs-tile-expand");
      if (expand) { openAt(expand.closest(".fgs-tile")); return; }
      var open = e.target.closest(".fgs-tile-open");
      if (!open) return;
      var tile = open.closest(".fgs-tile");
      if (selecting) toggleSelected(tile);
      else openAt(tile);
    });
    listWrap.addEventListener("click", function (e) {
      var expand = e.target.closest(".fgs-row-expand");
      if (expand) {
        var at = expand.closest("tr[data-row-id]");
        openAt(at && tilesById[at.getAttribute("data-row-id")]);
        return;
      }
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
      var what = item.getAttribute("data-menu");
      // Save to Photos keeps its menu open (hide.bs.dropdown below), so
      // focus stays on the entry
      if (what !== "photos") trigger.focus();
      var handler = fileAction(context, what);
      if (handler) handler(actionArg([tile], item, false, menu || menuOf(tile), trigger));
      else if (what === "open") openAt(tile);
      else if (what === "download") {
        FGS.downloadSequentially(asItems([tile]));
        if (groupTools && tile.getAttribute("data-download")) markDownloaded([idOf(tile)]);
      } else if (what === "photos") {
        FGS.saveToPhotos([FGS.itemData(tile)], item, photosOpts());
      }
    });

    // The "⋯" menu's Save to Photos, decided each time a menu opens (so files
    // added live and previews that arrived later are covered): on iOS with
    // navigator.canShare, for a photo or video that has an original to fetch
    // (its own download, or the context's handler: presigned on request
    // pages), and not where Download is disabled (end-to-end encrypted)
    function canSaveFromMenu(tile, menu) {
      if (!FGS.isIOS() || !navigator.canShare) return false;
      if (!FGS.isPhotoOrVideo(tile.getAttribute("data-kind"), tile.getAttribute("data-name"))) return false;
      var dl = menu.querySelector("[data-menu=download]");
      if (dl && dl.disabled) return false;
      return !!(tile.getAttribute("data-download") || fileAction(context, "photos"));
    }
    root.addEventListener("show.bs.dropdown", function (e) {
      var menu = e.target.closest && e.target.closest("[data-fgs-file-menu]");
      if (!menu || menu.closest("[data-fgs-browser]") !== root) return;
      var entry = menu.querySelector("[data-menu=photos]");
      var tile = tilesById[menu.getAttribute("data-id")];
      if (entry && entry.parentNode) entry.parentNode.classList.toggle("d-none", !(tile && canSaveFromMenu(tile, menu)));
    });
    // A tap on Save to Photos leaves its menu open: the entry shows the
    // progress, then "Ready — tap to save" where a second tap is needed.
    // The entry's label changes under the tap, so its target may already be
    // detached: the click's path (still being dispatched) says where it was.
    root.addEventListener("hide.bs.dropdown", function (e) {
      var click = e.clickEvent;
      var menu = e.target.closest && e.target.closest("[data-fgs-file-menu]");
      var entry = menu && menu.querySelector("[data-menu=photos]");
      if (!click || !entry || !click.composedPath) return;
      if (click.composedPath().indexOf(entry) >= 0) e.preventDefault();
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
    function stopPolling() {
      polls = {};
      if (pollTimer) window.clearInterval(pollTimer);
      pollTimer = null;
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

    // lazy: a thumbnail below the first screen (every tile the browser
    // builds), as the template renders it
    function thumbImg(className, info, lazy) {
      var img = document.createElement("img");
      if (className) img.className = className;
      if (lazy) img.setAttribute("loading", "lazy");
      img.decoding = "async";
      img.alt = "";
      img.src = info.Thumb;
      if (info.Color) img.style.background = info.Color;
      return img;
    }

    // A preview (a previewInfo: the /files/previewinfo or /p/info answer, or
    // a feed item's Preview) onto a tile and its list row. The download URL
    // is the page's own and is left alone.
    function paintPreview(tile, row, info, lazy) {
      setData(tile, "preview-status", info.Status);
      setData(tile, "view", info.View);
      setData(tile, "video", info.Video);
      setData(tile, "kind", info.Kind);
      setData(tile, "w", info.Width || 0);
      setData(tile, "h", info.Height || 0);
      if (info.TakenAt) setData(tile, "taken", info.TakenAt);
      var open = tile.querySelector(".fgs-tile-open");
      if (info.Thumb) {
        open.textContent = "";
        open.appendChild(thumbImg("", info, lazy));
        if (info.Video) {
          var play = document.createElement("span");
          play.className = "fgs-play";
          var icon = document.createElement("i");
          icon.className = "bi bi-play-fill";
          play.appendChild(icon);
          open.appendChild(play);
        }
        var old = row && row.querySelector(".fgs-list-thumb");
        if (old) old.parentNode.replaceChild(thumbImg("fgs-list-thumb", info, true), old);
      } else {
        // No thumbnail (unsupported, failed, or the new one not made yet):
        // back to the type icon, e.g. an image replaced by a PDF
        var name = tile.getAttribute("data-name") || "";
        var typeIcon = fileTypeIcon(name);
        if (!open.querySelector(".fgs-typeicon")) {
          open.textContent = "";
          var span = document.createElement("span");
          span.className = "fgs-typeicon";
          var glyph = document.createElement("i");
          glyph.className = "bi " + typeIcon;
          var small = document.createElement("small");
          small.textContent = name.slice(0, 40);
          span.appendChild(glyph);
          span.appendChild(small);
          open.appendChild(span);
        }
        var thumb = row && row.querySelector(".fgs-list-thumb");
        if (thumb && !thumb.classList.contains("fgs-list-icon")) {
          var holder = document.createElement("span");
          holder.className = "fgs-list-thumb fgs-list-icon";
          holder.setAttribute("aria-hidden", "true");
          var rowGlyph = document.createElement("i");
          rowGlyph.className = "bi " + typeIcon;
          holder.appendChild(rowGlyph);
          thumb.parentNode.replaceChild(holder, thumb);
        }
      }
      var taken = row && row.querySelector("td[data-col=taken]");
      if (taken && info.TakenAt) {
        taken.setAttribute("data-order", String(info.TakenAt));
        setTimeCell(taken, info.TakenAt);
      }
    }

    // A preview that arrived later onto a listed file
    function applyPreview(id, info) {
      var tile = tilesById[id];
      if (!tile || !info) return;
      var row = rowsById[id];
      var oldKey = shootingKey(tile);
      paintPreview(tile, row, info, false);
      // Shooting order (D13): a date taken that arrives with the preview
      // moves the file to its place
      if (query.sort === "taken" && shootingKey(tile) !== oldKey) placeSorted(tile, row);
      // A replaced file's new preview is still being made (or its row does
      // not exist yet): ask for it
      if (!info.Status || info.Status === "pending") startPolling(id);
      layoutBoard();
    }

    Object.keys(tilesById).forEach(function (id) {
      if (tilesById[id].getAttribute("data-preview-status") === "pending") startPolling(id);
    });

    // A file's menu, filled in with setters only. A file reached only
    // through a group (foreign) carries no management data (I2).
    function fillMenu(menu, file, perms, foreign) {
      setData(menu, "id", file.Id);
      var trigger = menu.querySelector(".fgs-menu-btn");
      if (trigger) trigger.setAttribute("aria-label", "Actions for " + (file.Name || ""));
      if (manages && !foreign) {
        setData(menu, "url", file.UrlDownload);
        setData(menu, "downloads", file.DownloadsRemaining);
        setData(menu, "unlimited-downloads", !!file.UnlimitedDownloads);
        setData(menu, "expire", file.ExpireAt);
        setData(menu, "unlimited-time", !!file.UnlimitedTime);
        setData(menu, "password", !!file.IsPasswordProtected);
        setData(menu, "e2e", !!file.IsEndToEndEncrypted);
        setData(menu, "can-edit", perms.edit);
        setData(menu, "can-replace", perms.replace);
      } else if (foreign) {
        // The blank template carries them: a foreign file's menu must not
        ["url", "downloads", "unlimited-downloads", "expire", "unlimited-time", "password", "e2e", "can-edit", "can-replace"]
          .forEach(function (k) { menu.removeAttribute("data-" + k); });
      }
      if (manages || itemPerms) {
        setData(menu, "can-delete", perms.del);
        setData(menu, "can-remove", !!perms.remove);
      }
      // D4: the name of the live group whose terms the file follows, as the
      // template renders it; absent leaves the menu's lock as it is
      if (file.GroupLock !== undefined && file.GroupLock !== null) {
        if (file.GroupLock) menu.setAttribute("data-group-lock", String(file.GroupLock));
        else menu.removeAttribute("data-group-lock");
      }
      var e2e = !!file.IsEndToEndEncrypted;
      menu.querySelectorAll("[data-menu]").forEach(function (b) {
        var k = b.getAttribute("data-menu");
        if (k === "copy") b.setAttribute("data-clipboard-text", file.UrlDownload || "");
        if (k === "hotlink") {
          // Only files that have a hotlink offer it
          b.setAttribute("data-clipboard-text", file.UrlHotlink || "");
          if (b.parentNode) {
            b.parentNode.classList.toggle("d-none", !file.UrlHotlink);
            if (!b.parentNode.className) b.parentNode.removeAttribute("class");
          }
        }
        if (k === "download") b.disabled = e2e;
        if (k === "add-to-collection" || k === "add-to-group") b.disabled = e2e || !!foreign;
        if (k === "copy" || k === "share" || k === "qr") b.disabled = !!foreign;
        if (k === "email") {
          // admin_ui_filebrowser.js makes the link from data-url when clicked
          b.setAttribute("href", "mailto:?body=" + encodeURIComponent(file.UrlDownload || ""));
          b.classList.toggle("disabled", !!foreign);
          if (foreign) b.setAttribute("aria-disabled", "true");
          else b.removeAttribute("aria-disabled");
        }
        if (k === "remove-from-group") b.disabled = !perms.remove;
        if (k === "edit") b.disabled = !perms.edit;
        if (k === "replace") b.disabled = !perms.replace;
        if (k === "delete") b.disabled = !perms.del;
        // A prepared Save to Photos holds the old content
        if (k === "photos") FGS.resetSaveToPhotos(b);
      });
    }

    function shootingKey(tile) {
      return parseInt(tile.getAttribute("data-taken"), 10) || parseInt(tile.getAttribute("data-uploaded"), 10) || 0;
    }
    // A file's key under the current sort, as the server's listSortKey
    function sortKeyOf(tile, row) {
      var number = function (v) { return parseInt(v, 10) || 0; };
      switch (query.sort) {
        case "name": return (tile.getAttribute("data-name") || "").toLowerCase();
        case "uploaded": return number(tile.getAttribute("data-uploaded"));
        case "taken": return shootingKey(tile);
        case "size": return number(tile.getAttribute("data-bytes"));
        case "expires": {
          var menu = menuOf(tile);
          if (tile.getAttribute("data-foreign") === "true" || !menu) return 0;
          return menu.getAttribute("data-unlimited-time") === "true" ? Infinity : number(menu.getAttribute("data-expire"));
        }
        case "downloads": {
          var cell = row && row.querySelector("td[data-col=downloads]");
          return cell ? number(cell.textContent) : 0;
        }
      }
      return 0;
    }
    // The order of two files under the current sort; the id breaks ties in
    // the same direction (compareListKeys)
    function compareFiles(a, b) {
      var ka = sortKeyOf(a.tile, a.row), kb = sortKeyOf(b.tile, b.row);
      var r = ka < kb ? -1 : ka > kb ? 1 : 0;
      if (!r) {
        var ia = idOf(a.tile), ib = idOf(b.tile);
        r = ia < ib ? -1 : ia > ib ? 1 : 0;
      }
      return query.desc ? -r : r;
    }
    // Puts a file (and its row) before the first rendered file that sorts
    // after it. Past the last one while later batches follow, it is one of
    // theirs: then nothing is placed, and false says so.
    function placeSorted(tile, row) {
      var file = { tile: tile, row: row };
      var before = Array.prototype.find.call(grid.children, function (t) {
        return t !== tile && t.classList.contains("fgs-tile") &&
          compareFiles(file, { tile: t, row: rowsById[idOf(t)] }) < 0;
      }) || null;
      if (!before && next) return false;
      grid.insertBefore(tile, before);
      if (row) {
        var beforeRow = before && rowsById[idOf(before)];
        tbody.insertBefore(row, beforeRow && beforeRow.parentNode === tbody ? beforeRow : null);
      }
      return true;
    }

    // A file's tile and list row from the <template>s, filled in with
    // textContent and setters only, not inserted. file is a feed item
    // (listItemJson: the server says what the viewer may do) or a
    // FileApiOutput of a live add, with its previewInfo or nothing (the
    // permissions then follow the page's own, as the server would decide).
    function buildItem(file, preview) {
      if (!file || !file.Id || !tplTile || !tplRow) return null;
      var feed = typeof file.Mine === "boolean" && file.Preview !== undefined;
      preview = (feed ? file.Preview : preview) || {};
      var id = String(file.Id);
      var userId = parseInt(root.getAttribute("data-user-id"), 10) || 0;
      var mine, perms, foreign, download, uploader, uploaderId, bytes, photo;
      if (feed) {
        mine = file.Mine;
        foreign = !!file.Foreign;
        perms = { edit: !!file.CanEdit, del: !!file.CanDelete, replace: !!file.CanReplace, remove: !!file.CanRemove };
        download = file.Download || "";
        uploader = file.UploaderName || "";
        photo = file.UploaderPhoto || "";
        // As rendered: the uploader's id on group pages and on files reached
        // through a group
        uploaderId = groupTools || foreign ? file.UploaderId || 0 : 0;
        bytes = file.Bytes;
      } else {
        mine = file.UploaderId === undefined || file.UploaderId === null || file.UploaderId === userId;
        var flag = function (k) { return root.getAttribute("data-perm-" + k) === "true"; };
        perms = { edit: mine || flag("edit-others"), del: mine || flag("delete-others") };
        perms.replace = perms.edit && !file.IsEndToEndEncrypted && ((mine && flag("replace")) || flag("replace-others"));
        perms.remove = !!file.CanRemove;
        // A group page shows someone else's file it may not edit as theirs
        foreign = context === "group" && !perms.edit;
        download = preview.Download || (downloadBase ? downloadBase + encodeURIComponent(id)
          : (manages && !file.IsEndToEndEncrypted ? "./files/download?id=" + encodeURIComponent(id) : ""));
        // The viewer's own shown name and photo (people.Name, Part 4)
        var ownName = root.getAttribute("data-user-name") || "";
        var known = file.UploaderName !== undefined && file.UploaderName !== null;
        uploader = known ? String(file.UploaderName) : (mine ? ownName : "");
        photo = file.UploaderPhoto !== undefined && file.UploaderPhoto !== null ? String(file.UploaderPhoto)
          : (!known && mine ? root.getAttribute("data-user-photo") || "" : "");
        // As rendered: only group pages carry the uploader's id
        uploaderId = !groupTools ? 0 : file.UploaderId !== undefined && file.UploaderId !== null ? file.UploaderId : (mine ? userId : 0);
        bytes = file.SizeBytes;
      }
      var name = file.Name || "";

      var tile = tplTile.content.firstElementChild.cloneNode(true);
      setData(tile, "mine", mine);
      setData(tile, "id", id);
      setData(tile, "name", name);
      if (foreign) setData(tile, "foreign", "true");
      setData(tile, "download", download);
      setData(tile, "uploader", uploader);
      setData(tile, "uploaded", file.UploadDate || 0);
      setData(tile, "size", file.Size);
      setData(tile, "bytes", bytes || 0);
      setData(tile, "taken", (feed ? file.TakenAt : preview.TakenAt) || 0);
      setData(tile, "uploader-id", uploaderId);
      setData(tile, "downloaded", !!file.Downloaded);
      var initials = tile.querySelector(".fgs-initials");
      if (initials) fillAvatar(initials, initialsOf(uploader), photo);
      tile.querySelector(".fgs-tile-open").setAttribute("aria-label", "Open " + name);
      tile.querySelector(".fgs-tile-expand").setAttribute("aria-label", "Open " + name);
      tileCheck(tile).setAttribute("aria-label", "Select " + name);
      var small = tile.querySelector(".fgs-typeicon small");
      if (small) small.textContent = name.slice(0, 40);
      var icon = fileTypeIcon(name);
      Array.prototype.forEach.call(tile.querySelectorAll(".fgs-typeicon i"), function (i) { i.className = "bi " + icon; });
      tile.querySelector(".fgs-name").textContent = name;
      tile.querySelector(".fgs-meta-uploader").textContent = uploader ? uploader + " · " : "";
      setData(tile.querySelector("figcaption [data-ts]"), "ts", file.UploadDate || 0);
      fillMenu(menuOf(tile), file, perms, foreign);

      var row = tplRow.content.firstElementChild.cloneNode(true);
      var ids = legacyIds && !foreign;
      setData(row, "mine", mine);
      setData(row, "row-id", id);
      setData(row, "downloaded", !!file.Downloaded);
      if (ids) row.id = "row-" + id;
      rowCheck(row).setAttribute("aria-label", "Select " + name);
      row.querySelector(".fgs-row-expand").setAttribute("aria-label", "Open " + name);
      row.querySelector("td[data-col=name]").setAttribute("data-order", name);
      var nameSpan = row.querySelector(".fgs-row-name");
      nameSpan.textContent = name;
      if (ids) nameSpan.id = "cell-name-" + id;
      var cell = function (k) { return row.querySelector("td[data-col=" + k + "]"); };
      if (cell("uploader")) cell("uploader").textContent = uploader;
      cell("uploaded").setAttribute("data-order", String(file.UploadDate || 0));
      setData(cell("uploaded").querySelector("[data-ts]"), "ts", file.UploadDate || 0);
      // Shooting order: without a date taken, the upload date stands in (D13)
      cell("taken").setAttribute("data-order", String(preview.TakenAt || (sortTakenAsc ? file.UploadDate || 0 : 0)));
      cell("taken").textContent = "—";
      cell("size").setAttribute("data-order", String(bytes || 0));
      cell("size").textContent = file.Size || "";
      if (cell("expires")) {
        if (foreign) {
          cell("expires").setAttribute("data-order", "0");
          cell("expires").textContent = "—";
        } else if (file.UnlimitedTime) {
          cell("expires").setAttribute("data-order", "9223372036854775807");
          cell("expires").textContent = "Never";
        } else {
          cell("expires").setAttribute("data-order", String(file.ExpireAt || 0));
          setTimeCell(cell("expires"), file.ExpireAt);
        }
      }
      if (cell("downloads")) {
        if (ids) cell("downloads").id = "cell-downloads-" + id;
        cell("downloads").textContent = foreign ? "—" : String(file.DownloadCount || 0);
      }
      var copy = row.querySelector(".fgs-list-actions > .copyurl");
      if (copy && foreign) {
        // Someone else's file has no share link: the quick button downloads
        var quick = document.createElement("button");
        quick.type = "button";
        quick.className = "btn btn-outline-light btn-sm";
        quick.setAttribute("data-menu", "download");
        quick.title = "Download";
        quick.setAttribute("aria-label", "Download");
        var glyph = document.createElement("i");
        glyph.className = "bi bi-download";
        quick.appendChild(glyph);
        copy.parentNode.replaceChild(quick, copy);
      } else if (copy) {
        if (ids) copy.id = "url-button-" + id;
        copy.setAttribute("data-clipboard-text", file.UrlDownload || "");
      }
      Array.prototype.forEach.call(row.querySelectorAll(".fgs-list-icon i"), function (i) { i.className = "bi " + icon; });
      fillMenu(row.querySelector("[data-fgs-file-menu]"), file, perms, foreign);
      paintPreview(tile, row, preview, true);
      formatTimes(tile);
      formatTimes(row);
      return { tile: tile, row: row };
    }

    // A file the page did not render (no feed lists it): its uploader joins
    // the person filter (Part 2 follow-up)
    function addPerson(tile) {
      var who = tile.getAttribute("data-uploader-id");
      if (!personSelect || !who || who === "0") return;
      if (Array.prototype.some.call(personSelect.options, function (o) { return o.value === who; })) return;
      var option = document.createElement("option");
      option.value = who;
      option.textContent = tile.getAttribute("data-uploader") || "";
      personSelect.appendChild(option);
    }

    // Live adds (an upload on this page, a restored file): in its place in
    // the current order. One that sorts after the last rendered file while
    // later batches follow is left to them, and one the filters reject is
    // only counted. Returns the tile, or null when none was inserted.
    function addItem(file, preview) {
      var built = buildItem(file, preview);
      if (!built) return null;
      var tile = built.tile, row = built.row, id = idOf(tile);
      if (tilesById[id]) removeItem(id);
      addPerson(tile);
      var matches = matchesQuery(tile);
      if (listUrl) {
        counts.all++;
        if (matches) counts.total++;
        if (matches && groupTools && isNewToMe(tile)) counts.fresh++;
        if (!matches || !placeSorted(tile, row)) {
          refreshCount();
          return null;
        }
      } else {
        // A list without a feed holds everything; its search hides
        placeSorted(tile, row);
        tile.classList.toggle("d-none", !matches);
        row.classList.toggle("d-none", !matches);
      }
      index(tile, row);
      // Ticked on arrival where a "Select all" covers it
      syncTicks([tile]);
      var state = tile.getAttribute("data-preview-status");
      if (state !== "ready" && !file.IsEndToEndEncrypted && (!state || state === "pending")) startPolling(id);
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
        var rendered = menu.getAttribute("data-can-" + k);
        if (rendered !== null) return rendered === "true";
        var b = menu.querySelector("[data-menu=" + (k === "remove" ? "remove-from-group" : k) + "]");
        return !!b && !b.disabled;
      };
      var perms = { edit: was("edit"), del: was("delete"), replace: was("replace"), remove: was("remove") };
      var foreign = tile.getAttribute("data-foreign") === "true";
      var name = file.Name || tile.getAttribute("data-name") || "";
      setData(tile, "name", name);
      if (file.Size !== undefined) setData(tile, "size", file.Size);
      if (file.SizeBytes !== undefined) setData(tile, "bytes", file.SizeBytes);
      tile.querySelector(".fgs-tile-open").setAttribute("aria-label", "Open " + name);
      tile.querySelector(".fgs-tile-expand").setAttribute("aria-label", "Open " + name);
      tileCheck(tile).setAttribute("aria-label", "Select " + name);
      tile.querySelector(".fgs-name").textContent = name;
      var small = tile.querySelector(".fgs-typeicon small");
      if (small) small.textContent = name.slice(0, 40);
      // A new extension is a new type icon (on tiles and rows without a thumbnail)
      var icon = fileTypeIcon(name);
      Array.prototype.forEach.call(tile.querySelectorAll(".fgs-typeicon i"), function (i) { i.className = "bi " + icon; });
      fillMenu(menu, file, perms, foreign);
      if (row) {
        Array.prototype.forEach.call(row.querySelectorAll(".fgs-list-icon i"), function (i) { i.className = "bi " + icon; });
        rowCheck(row).setAttribute("aria-label", "Select " + name);
        row.querySelector(".fgs-row-expand").setAttribute("aria-label", "Open " + name);
        row.querySelector("td[data-col=name]").setAttribute("data-order", name);
        row.querySelector(".fgs-row-name").textContent = name;
        var cell = function (k) { return row.querySelector("td[data-col=" + k + "]"); };
        if (cell("size") && file.Size !== undefined) {
          cell("size").setAttribute("data-order", String(file.SizeBytes || 0));
          cell("size").textContent = file.Size;
        }
        if (cell("expires") && !foreign) {
          cell("expires").setAttribute("data-order", file.UnlimitedTime ? "9223372036854775807" : String(file.ExpireAt || 0));
          if (file.UnlimitedTime) cell("expires").textContent = "Never";
          else setTimeCell(cell("expires"), file.ExpireAt);
        }
        if (cell("downloads") && !foreign && file.DownloadCount !== undefined) cell("downloads").textContent = String(file.DownloadCount);
        var copy = row.querySelector(".copyurl");
        if (copy) copy.setAttribute("data-clipboard-text", file.UrlDownload || "");
        fillMenu(row.querySelector("[data-fgs-file-menu]"), file, perms, foreign);
      }
      return tile;
    }

    // Takes a file's tile and row out; false when it was not listed
    function dropItem(id) {
      var tile = tilesById[id], row = rowsById[id];
      if (!tile && !row) return false;
      delete tilesById[id];
      delete rowsById[id];
      delete polls[id];
      if (tile && tile.parentNode) tile.parentNode.removeChild(tile);
      if (row && row.parentNode) row.parentNode.removeChild(row);
      return true;
    }
    // A file that left the list. One not rendered (an action on a "Select
    // all") only leaves the counts.
    function removeItem(id) {
      var tile = tilesById[id];
      var fresh = tile ? groupTools && isNewToMe(tile) : false;
      if ((dropItem(id) || !tile) && listUrl) {
        counts.total = Math.max(0, counts.total - 1);
        counts.all = Math.max(0, counts.all - 1);
        if (fresh) counts.fresh = Math.max(0, counts.fresh - 1);
      }
      forget(id);
      refreshCount();
      layoutBoard();
      updateSelection();
    }

    // ---- Batches ------------------------------------------------------
    // The feed URL of the current query, after a cursor or from the start
    function feedUrl(after) { return feedUrlFor(query, after); }
    // The same for another query (a captured selection's)
    function feedUrlFor(q, after) {
      var params = [];
      if (q.scope) params.push("scope=" + q.scope);
      if (q.person) params.push("person=" + encodeURIComponent(q.person));
      if (q.hide) params.push("hide=1");
      if (q.kind) params.push("kind=" + q.kind);
      if (q.q.trim()) params.push("q=" + encodeURIComponent(q.q.trim()));
      params.push("sort=" + q.sort, "dir=" + (q.desc ? "desc" : "asc"));
      if (after) params.push("after=" + encodeURIComponent(after));
      return listUrl + params.join("&");
    }

    // The sentinels after the grid and the table: "Loading…", or the
    // failure and a Retry that runs retry
    function setMore(state, retry) {
      moreState = state;
      sentinels.forEach(function (s) {
        s.textContent = "";
        if (state === "loading") s.textContent = "Loading…";
        if (state !== "error") return;
        s.appendChild(document.createTextNode("Couldn't load more files. "));
        var again = document.createElement("button");
        again.type = "button";
        again.className = "btn btn-sm btn-outline-light";
        again.textContent = "Retry";
        again.addEventListener("click", function () { retry(); });
        s.appendChild(again);
      });
      refreshCount();
    }

    // One batch onto the end of the list, in the server's order; files
    // already listed (a live add) are skipped. Returns the new tiles.
    function wasHad(json) { return had.has(String(json.Id)); }
    function appendBatch(answer, first) {
      var added = [], skipped = 0;
      answer.Items.forEach(function (json) {
        if (!json || tilesById[String(json.Id)]) return;
        var done = wasHad(json);
        // Under "Hide what I've downloaded", never back in the list
        if (done && query.hide) { skipped++; return; }
        var built = buildItem(json);
        if (!built) return;
        if (done) {
          setData(built.tile, "downloaded", "true");
          setData(built.row, "downloaded", "true");
        }
        grid.appendChild(built.tile);
        tbody.appendChild(built.row);
        index(built.tile, built.row);
        if (built.tile.getAttribute("data-preview-status") === "pending") startPolling(String(json.Id));
        added.push(built.tile);
      });
      next = answer.Next || "";
      var total = answer.Total || 0, fresh = answer.New || 0;
      // The server still counts files whose download is under way: a first
      // batch leaves out the ones it listed, a later one keeps the count
      // this page already has (it never grows from a later batch then)
      if (skipped || (!first && query.hide && had.size)) {
        total = first ? Math.max(0, total - skipped) : Math.min(total, counts.total);
        fresh = first ? Math.max(0, fresh - skipped) : Math.min(fresh, counts.fresh);
      }
      counts.total = total;
      counts.all = answer.All || 0;
      counts.fresh = fresh;
      // A "Select all" covers what arrives
      syncTicks(added);
      // Once per batch, and the selection (with what was prepared or
      // linked from it) stays as it is
      refreshCount();
      layoutBoard();
      return added;
    }

    // Fetches one batch of the current query: the first (a reload) or the
    // one after the cursor. Resolves to the tiles it added, [] when it
    // failed or was overtaken by a reload; never rejects.
    function fetchBatch(after) {
      var gen = generation, first = !after;
      var abort = typeof AbortController === "function" ? new AbortController() : null;
      var request = { abort: function () { if (abort) abort.abort(); } };
      setMore("loading");
      request.promise = fetch(feedUrl(after), { credentials: "same-origin", cache: "no-store", signal: abort ? abort.signal : undefined })
        .then(function (r) {
          if (!r.ok) throw new Error("The server answered " + r.status);
          return r.json();
        })
        .then(function (answer) {
          if (gen !== generation) return [];
          if (!answer || !Array.isArray(answer.Items)) throw new Error("Unexpected answer");
          pending = null;
          if (first) views.classList.remove("fgs-reloading");
          setMore("");
          var added = appendBatch(answer, first);
          observeMore();
          return added;
        })
        .catch(function () {
          if (gen !== generation) return [];
          pending = null;
          // Never left invisible: the failure shows, with its Retry
          if (first) views.classList.remove("fgs-reloading");
          setMore("error", first ? reload : loadMore);
          return [];
        });
      pending = request;
      return request.promise;
    }

    // The next batch, if there is one; at most one request at a time
    function loadMore() {
      if (pending) return pending.promise;
      if (!listUrl || !next) return Promise.resolve([]);
      return fetchBatch(next);
    }

    // The list again from its first batch, under the current query: any
    // older request is dropped, and so are the files, their selection and
    // their polling. A list without a feed sorts and searches itself.
    function reload() {
      if (!listUrl) {
        reorderLocally();
        return Promise.resolve([]);
      }
      if (pending) pending.abort();
      pending = null;
      generation++;
      stopPolling();
      tilesById = {};
      rowsById = {};
      grid.textContent = "";
      tbody.textContent = "";
      grid.style.height = "";
      next = "";
      status.textContent = "";
      clearSelection();
      updateSelection();
      return fetchBatch("");
    }
    function reorderLocally() {
      var files = Object.keys(tilesById).map(function (id) { return { tile: tilesById[id], row: rowsById[id] }; });
      files.sort(compareFiles);
      files.forEach(function (f) {
        grid.appendChild(f.tile);
        if (f.row) tbody.appendChild(f.row);
        var hide = !matchesQuery(f.tile);
        f.tile.classList.toggle("d-none", hide);
        if (f.row) f.row.classList.toggle("d-none", hide);
        // A hidden file leaves the selection, rather than coming back selected
        if (hide) setChecked(idOf(f.tile), false);
      });
      refreshCount();
      layoutBoard();
      updateSelection();
    }

    // The next batch is fetched while the sentinel is about two screens
    // away; observing again after each batch asks again if it still is
    var moreObserver = null;
    function observeMore() {
      if (!listUrl || !("IntersectionObserver" in window)) return;
      if (!moreObserver) {
        moreObserver = new IntersectionObserver(function (entries) {
          var near = entries.some(function (e) { return e.isIntersecting; });
          if (near && next && !pending && moreState !== "error") loadMore();
        }, { rootMargin: "0px 0px 200% 0px" });
      }
      sentinels.forEach(function (s) {
        moreObserver.unobserve(s);
        moreObserver.observe(s);
      });
    }

    // The sort, in every view (R1): from a List header (the second click on
    // the active one turns it round) or the Sort & filter dropdown, which a
    // new key gives the header's first direction. Remembered like the view.
    function setOrder(key, desc) {
      if (SORT_KEYS.indexOf(key) < 0) return;
      if (key === query.sort && desc === query.desc) return;
      query.sort = key;
      query.desc = desc;
      store("sort", query.sort + ":" + (query.desc ? "desc" : "asc"));
      showSort();
      reload();
    }
    function showSort() {
      sortRadios.forEach(function (r) { r.checked = r.value === query.sort; });
      dirRadios.forEach(function (r) { r.checked = r.value === (query.desc ? "desc" : "asc"); });
      Array.prototype.forEach.call(table.tHead.querySelectorAll("th[data-fgs-sortkey]"), function (th) {
        var on = th.getAttribute("data-fgs-sortkey") === query.sort;
        if (on) th.setAttribute("aria-sort", query.desc ? "descending" : "ascending");
        else th.removeAttribute("aria-sort");
        var arrow = th.querySelector(".fgs-sort-arrow");
        if (arrow) arrow.textContent = on ? (query.desc ? "↓" : "↑") : "";
      });
    }
    table.tHead.addEventListener("click", function (e) {
      var button = e.target.closest("th[data-fgs-sortkey] button");
      if (!button) return;
      var key = button.closest("th").getAttribute("data-fgs-sortkey");
      setOrder(key, key === query.sort ? !query.desc : !!DESC_FIRST[key]);
    });

    // The filters of the Sort & filter dropdown: Type everywhere, My files /
    // Everything (applyScope), and the group page's person and "Hide what
    // I've downloaded". The badge counts the ones not at their default;
    // Reset puts those back (the sort and the search stay).
    function activeFilters() {
      return (query.kind ? 1 : 0) + (query.person ? 1 : 0) + (query.hide ? 1 : 0) + (scopeSwitch && query.scope === "all" ? 1 : 0);
    }
    function showFilters() {
      kindRadios.forEach(function (r) { r.checked = r.value === query.kind; });
      if (personSelect) personSelect.value = query.person;
      if (hideDownloaded) hideDownloaded.checked = query.hide;
      if (scopeSwitch) setActive("data-scope", query.scope);
      if (filterBadge) {
        var n = activeFilters();
        filterBadge.textContent = n ? String(n) : "";
        filterBadge.hidden = !n;
        var toggle = filterBadge.closest("button");
        if (toggle) toggle.setAttribute("aria-label", "Sort & filter" + (n ? " (" + n + " filter" + (n === 1 ? "" : "s") + " on)" : ""));
      }
    }
    function resetFilters() {
      if (!activeFilters()) return;
      query.kind = "";
      query.person = "";
      query.hide = false;
      store("kind", "");
      if (hideDownloaded) store("hideDownloaded", "false");
      if (scopeSwitch) {
        query.scope = "mine";
        store("scope", "mine");
      }
      showFilters();
      reload();
    }

    api = {
      root: root,
      context: context,
      addItem: addItem,
      removeItem: removeItem,
      updateItem: updateItem,
      applyPreview: applyPreview,
      pollPreview: startPolling,
      tile: function (id) { return tilesById[id] || null; },
      row: function (id) { return rowsById[id] || null; },
      selected: selected,
      // The selection (Part 3): {mode: "ids", ids} or {mode: "query", query,
      // except, plus}; its count; and its files, resolved
      selection: selectionInfo,
      selectedCount: selectedCount,
      resolveSelected: resolveSelected,
      resolveAll: resolveAll,
      zipIds: zipIds,
      visible: visibleTiles,
      setSelecting: setSelecting,
      view: function () { return view; },
      relayout: layoutBoard,
      markDownloaded: markDownloaded,
      selectWhere: selectWhere,
      // List batching (Part 3)
      loadMore: loadMore,
      reload: reload,
      // {tile, row} of a feed item, not inserted
      buildItem: function (json, preview) {
        var built = buildItem(json, preview);
        return built ? { tile: built.tile, row: built.row } : null;
      },
      // The current query ({scope, person, hide, kind, q, sort, desc}), the counts
      // of the last answer ({total, all, new}) and the cursor ("" = all in)
      query: function () { return Object.assign({}, query); },
      counts: function () { return { total: counts.total, all: counts.all, "new": counts.fresh }; },
      next: function () { return next; },
      feedUrl: feedUrl
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
      size.addEventListener("input", function () {
        if (columns) setColumns(columns.max + 1 - (parseInt(size.value, 10) || 1));
      });
      // Armed by a press on the slider, for that pointer only; a cancelled
      // press (a touch turned into a scroll) disarms it, so a later tap
      // elsewhere never snaps the slider
      var pressId = null;
      size.addEventListener("pointerdown", function (e) { pressId = e.pointerId; });
      window.addEventListener("pointerup", function (e) {
        if (pressId === null || e.pointerId !== pressId) return;
        pressId = null;
        snapOnRelease(e);
      });
      window.addEventListener("pointercancel", function (e) { if (e.pointerId === pressId) pressId = null; });
    }
    // Phones: search, shape and the slider open below the one-row toolbar
    var toolsToggle = root.querySelector("[data-fgs-tools-toggle]");
    if (toolsToggle && toolbar) toolsToggle.addEventListener("click", function () {
      var open = toolbar.classList.toggle("fgs-tools-open");
      toolsToggle.setAttribute("aria-expanded", open ? "true" : "false");
      // Lit while open, or while a search hidden behind it is filtering
      var lit = open || !!(searchBox && searchBox.value.trim());
      toolsToggle.classList.toggle("btn-light", lit);
      toolsToggle.classList.toggle("btn-outline-light", !lit);
      if (open && searchBox) searchBox.focus();
      updateColumns();
    });
    if (toolbar) {
      if ("ResizeObserver" in window) new ResizeObserver(placeBarsSoon).observe(toolbar);
      window.addEventListener("scroll", placeBarsSoon, { passive: true });
      window.addEventListener("resize", placeBarsSoon);
      placeBars();
    }
    applyShape(load("shape") || "square");
    formatTimes(views);
    applyView(initialView(root));
    // Group pages: whose files (never remembered: each group has its own
    // people; a browser may restore the last choice, showFilters undoes
    // it), and "Hide what I've downloaded" (remembered)
    if (personSelect) {
      personSelect.addEventListener("change", function () {
        query.person = personSelect.value;
        showFilters();
        reload();
      });
      // Arrow keys choose a person rather than move through the dropdown
      personSelect.addEventListener("keydown", function (e) { if (e.key !== "Escape") e.stopPropagation(); });
    }
    if (hideDownloaded) {
      hideDownloaded.addEventListener("change", function () {
        store("hideDownloaded", hideDownloaded.checked ? "true" : "false");
        // Your own uploads count as already had (D12)
        query.hide = hideDownloaded.checked;
        showFilters();
        reload();
      });
    }
    kindRadios.forEach(function (r) {
      r.addEventListener("change", function () {
        if (!r.checked || r.value === query.kind) return;
        query.kind = KINDS.indexOf(r.value) >= 0 ? r.value : "";
        store("kind", query.kind);
        showFilters();
        reload();
      });
    });
    sortRadios.forEach(function (r) {
      r.addEventListener("change", function () {
        if (r.checked && r.value !== query.sort) setOrder(r.value, !!DESC_FIRST[r.value]);
      });
    });
    dirRadios.forEach(function (r) {
      r.addEventListener("change", function () {
        if (r.checked) setOrder(query.sort, r.value === "desc");
      });
    });
    if (filterReset) filterReset.addEventListener("click", resetFilters);
    showFilters();
    if (searchBox) {
      searchBox.value = ""; // never remembered, nor restored by the browser
      var searchTimer = null;
      searchBox.addEventListener("input", function () {
        window.clearTimeout(searchTimer);
        searchTimer = window.setTimeout(function () {
          var q = searchBox.value.trim();
          if (q === query.q) return;
          query.q = q;
          reload();
        }, 300);
      });
    }
    showSort();
    if (!listUrl) reorderLocally();
    else if (reloadFirst) {
      views.classList.add("fgs-reloading");
      reload();
    } else {
      views.classList.remove("fgs-reloading");
      observeMore();
    }
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
