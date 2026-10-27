/* FGS - turns Gokapi's top nav row into a collapsible left sidebar.
 *
 * Loaded on every admin view (Upload, File Requests, Users, API, Status) via
 * the "customjs" template, at the end of <body>, so the DOM already exists.
 *
 * NOTE: this file is served publicly at /custom/admin.v<N>.js - never put
 * anything secret in here.
 *
 * Bump custom/version.txt after editing, or the 2-day cache will serve the old
 * copy. Gokapi only notices the file exists at startup, so restart after
 * creating it for the first time.
 */
(function () {
  "use strict";

  function build() {
    var nav = document.querySelector("nav.nav-masthead");
    if (!nav || document.getElementById("fgs-sidebar")) return;

    var links = Array.prototype.slice.call(nav.querySelectorAll("a.nav-link"));
    if (!links.length) return;

    // --- sidebar -----------------------------------------------------------
    var aside = document.createElement("aside");
    aside.id = "fgs-sidebar";

    var list = document.createElement("nav");
    list.className = "fgs-sb-nav";

    // Icon per destination, matched on the href Gokapi renders.
    var icons = {
      admin: "bi-cloud-arrow-up",
      collections: "bi-collection",
      files: "bi-images",
      groups: "bi-people-fill",
      filerequests: "bi-inbox",
      users: "bi-person-gear",
      apiKeys: "bi-key",
      logs: "bi-activity",
      logout: "bi-box-arrow-right"
    };
    // Logout is rendered as href="#" with an onclick, so it cannot be matched
    // on its href like the others.
    var iconsByLabel = {
      "logout": "bi-box-arrow-right",
      "collections": "bi-collection",
      "files": "bi-images",
      "groups": "bi-people-fill",
      "upload": "bi-cloud-arrow-up",
      "file requests": "bi-inbox",
      "users": "bi-person-gear",
      "api": "bi-key",
      "status": "bi-activity"
    };

    links.forEach(function (a) {
      var href = (a.getAttribute("href") || "").replace(/^\.\//, "");
      var item = document.createElement("a");
      item.className = "fgs-sb-link" + (a.classList.contains("active") ? " active" : "");
      item.href = a.getAttribute("href");

      var text = a.textContent.trim();
      var i = document.createElement("i");
      i.className = "bi " + (icons[href] || iconsByLabel[text.toLowerCase()] || "bi-dot");
      item.appendChild(i);

      var label = document.createElement("span");
      label.textContent = text;
      item.appendChild(label);

      // Links that do not navigate (Logout is href="#" + onclick) must have the
      // click forwarded to the original element, or the copy does nothing.
      if (href === "#" || href === "") {
        item.addEventListener("click", function (e) {
          e.preventDefault();
          a.click();
        });
      }
      if (href === "logout" || text.toLowerCase() === "logout") {
        item.classList.add("fgs-sb-logout");
      }
      list.appendChild(item);
    });

    aside.appendChild(list);
    document.body.appendChild(aside);

    // --- toggle button -----------------------------------------------------
    var btn = document.createElement("button");
    btn.id = "fgs-sidebar-toggle";
    btn.type = "button";
    btn.setAttribute("aria-controls", "fgs-sidebar");
    btn.appendChild(document.createElement("i"));
    document.body.appendChild(btn);

    // --- backdrop (small screens) -----------------------------------------
    var backdrop = document.createElement("div");
    backdrop.id = "fgs-sidebar-backdrop";
    document.body.appendChild(backdrop);

    // Hide the original nav row now that it has been mirrored.
    nav.classList.add("fgs-nav-replaced");

    function apply(open) {
      document.body.classList.toggle("fgs-sb-open", open);
      btn.querySelector("i").className = "bi " + (open ? "bi-x-lg" : "bi-list");
      btn.setAttribute("aria-expanded", open ? "true" : "false");
      btn.setAttribute("aria-label", open ? "Close menu" : "Open menu");
    }

    // Always start collapsed - the sidebar floats over the UI, so it is opened
    // deliberately rather than persisting across page loads.
    document.body.classList.add("fgs-sb-no-anim");
    apply(false);
    window.setTimeout(function () {
      document.body.classList.remove("fgs-sb-no-anim");
    }, 60);

    btn.addEventListener("click", function (e) {
      e.stopPropagation();      // don't let the outside-click handler see this
      apply(!document.body.classList.contains("fgs-sb-open"));
    });

    backdrop.addEventListener("click", function () { apply(false); });

    // Clicking anywhere outside the sidebar closes it, at any width.
    document.addEventListener("click", function (e) {
      if (!document.body.classList.contains("fgs-sb-open")) return;
      if (aside.contains(e.target) || btn.contains(e.target)) return;
      apply(false);
    });

    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && document.body.classList.contains("fgs-sb-open")) {
        apply(false);
      }
    });
  }

  /* ---------------------------------------------------------------------
   * Upload limit line.
   * It used to be a ::before on #uploadstatus, but uploadProgress.css gives
   * .uploads-container `visibility: hidden` until an upload starts - so the
   * text only appeared mid-upload. Inject a real, always-visible element
   * straight after the dropzone instead. The wording itself still lives in
   * custom.css (#fgs-uploadlimit::before) so set-max-filesize.sh can rewrite it.
   * ------------------------------------------------------------------- */
  function uploadLimit() {
    var dz = document.getElementById("uploaddropzone");
    if (!dz || document.getElementById("fgs-uploadlimit")) return;
    var note = document.createElement("div");
    note.id = "fgs-uploadlimit";
    dz.parentNode.insertBefore(note, dz.nextSibling);
  }

  /* ---------------------------------------------------------------------
   * Password suggestion: a word-based passphrase, easy to read out or type.
   * ------------------------------------------------------------------- */
  var WORDS = ("amber anchor apple arrow autumn bamboo beacon birch bishop " +
    "bottle branch bridge bronze butter cactus candle canvas carbon castle " +
    "cedar cherry cinder circle citrus cliff cobalt comet copper coral " +
    "cotton crater crimson crystal dahlia daisy delta desert diamond domino " +
    "dragon ember empire falcon fern fiddle forest fossil garnet ginger " +
    "glacier granite harbor harvest hazel hollow indigo island ivory jasmine " +
    "jungle juniper kernel lagoon lantern laurel lemon lilac linen lotus " +
    "lunar maple marble meadow mellow meteor mint mirror monsoon mosaic " +
    "nectar nickel nomad north nutmeg oasis olive onyx opal orbit orchard " +
    "otter paddle pebble pepper pewter pigeon pine pillar planet pocket " +
    "poplar prairie prism pumpkin quartz quiver radish raven ribbon river " +
    "rocket rosemary saffron sage sailor salmon sandy sapphire scarlet " +
    "shadow shelter silver socket solar spruce squash stellar stone summit " +
    "sunset tangle teak thistle thunder timber topaz torch tulip tundra " +
    "umber valley velvet vessel village violet walnut willow window winter " +
    "zephyr zinc").split(" ");

  function randomInt(max) {
    if (window.crypto && window.crypto.getRandomValues) {
      var a = new Uint32Array(1);
      // rejection-sample to keep the distribution even
      var limit = Math.floor(4294967296 / max) * max;
      do { window.crypto.getRandomValues(a); } while (a[0] >= limit);
      return a[0] % max;
    }
    return Math.floor(Math.random() * max);
  }

  function makePassphrase() {
    var parts = [];
    for (var i = 0; i < 3; i++) parts.push(WORDS[randomInt(WORDS.length)]);
    return parts.join("-") + "-" + (randomInt(90) + 10);
  }

  function passwordSuggest() {
    var input = document.getElementById("password");
    var toggle = document.getElementById("enablePassword");
    if (!input || document.getElementById("fgs-suggest")) return;

    var btn = document.createElement("button");
    btn.id = "fgs-suggest";
    btn.type = "button";
    btn.textContent = "Suggest a password";

    btn.addEventListener("click", function () {
      if (input.disabled) return;
      input.value = makePassphrase();
      // let Gokapi's own handlers see the change
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.dispatchEvent(new Event("change", { bubbles: true }));
      input.focus();
      input.select();
    });

    input.parentNode.insertBefore(btn, input.nextSibling);

    function sync() {
      var on = toggle ? toggle.checked : !input.disabled;
      btn.disabled = !on;
      btn.classList.toggle("is-off", !on);
    }
    if (toggle) toggle.addEventListener("change", sync);
    sync();
  }

  function init() {
    build();
    uploadLimit();
    passwordSuggest();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
