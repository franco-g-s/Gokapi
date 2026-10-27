# Part 1 revisions: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply Franco's feedback from the preview build. The Files tab gets full file management in both views, a select mode and Square/True-shape tiles. Previews get smaller, the viewer gets a resolution notice, correct stacking and trackpad navigation, and collections get wider pages and clickable names.

**Architecture:** Existing Part 1 code on `feature/previews`, with small edits throughout:
- the Go handlers in `internal/webserver`;
- the generator constants and worker in `internal/preview`;
- the templates `html_files.tmpl`, `html_collection.tmpl`, `html_collections_admin.tmpl` and `html_admin.tmpl` (edit modal);
- the front-end files `media.js`, `admin_ui_files.js`, `admin_ui_upload.js` and `media.css`.

No new pages.

**Tech Stack:** Go (built and tested only in Docker via `build/dev/gotest.sh`), Go html/template, vanilla JS, Bootstrap 5, DataTables, vips/ffmpeg.

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`, Part 1, revised in commit `6dbd61c`. Sections: "Files tab", "Viewer", "Preview worker" table, and the "Default layout" note on collection pages.

## Global Constraints

Every constraint in `docs/superpowers/plans/2026-10-19-part1-previews.md` → "Global Constraints" still applies. Copied here verbatim where it matters:

- **No Go toolchain on the host.** Every `go` command runs via `build/dev/gotest.sh` (Docker, `DOCKER_HOST=unix://$HOME/.colima/default/docker.sock`).
  - Two packages are known to be killed for lack of VM memory: `internal/encryption` and `internal/configuration/setup`. Ignore those two.
  - If `internal/configuration/setup/test/` is left behind, delete it.
- **Always `-tags=test,awsmock`** (the script does this). Check for `--- PASS: <TestName>`.
- `go generate` rewrites hashed asset names. Commit the regenerated files with the change that caused them.
- **Protected routes** must be registered literally as `mux.HandleFunc("<path>", requireLogin(...))`.
- **Originals are never altered.** No download route ever serves a preview.
- **Viewing never counts as downloading.**
- **Worker errors go to stdout only, never the Status log** (the notifier emails warnings). At most one `info` line per backfill run.
- **New Go code goes in new files** where possible, and edits to upstream files stay small.
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Push after each commit. Branch: `feature/previews`.
- Never touch the live `gokapi` container, its volumes, `~/apps/gokapi`, the preview container `gokapi-preview`, or `~/src/gokapi-preview`. The controller refreshes the preview itself.
- No background processes. Implementers do not dispatch subagents.
- **Copy (exact strings):**
  - "Select", "Done"
  - "Square", "True shape"
  - "Lower-resolution preview — download the original for full quality"
  - "Turn into a group" (Part 2 only, not in this plan)
  - "Change expiry & limits", "Add to collection…", "Create one link"
- **Security:** build any DOM that contains file names with `textContent` and attribute setters only, never `innerHTML`.

---

### Task R1: Rename files (server + edit dialog)

Upstream's `/api/files/modify` cannot rename a file. Add an optional `filename` header, and a name field to the shared edit dialog.

**Files:**
- Modify: `internal/webserver/api/routing.go`. Add `FileName string \`header:"filename" supportBase64:"true"\`` to `paramFilesModify`. In its `ProcessParameter`, when `p.foundHeaders["filename"]`, set `p.FileName = helper.SanitiseFilename(p.FileName)` and reject an empty result with an error.
- Modify: `internal/webserver/api/Api.go` `apiEditFile`. If `request.FileName != ""`, set `file.Name = request.FileName`. The existing permission check (owner or `UserPermEditOtherUploads`) already covers this. Content type does not change; only the stored display/download name does.
- Modify: `openapi.json`. Document the new optional header on `/files/modify`.
- Modify: the edit modal in `html_admin.tmpl` (`admin_modal_edit`). Add a "Name" text input pre-filled with the current name. The JS that opens and saves the modal is in `admin_ui_upload.js`.
  - Send `filename` (base64 if the API wrapper base64-encodes other headers; follow how `admin_api.js` sends the `filename` header for duplicate) only when the name changed.
  - On save, the upload page updates its row in place (Task 7 behaviour) and the Files page reloads.
- Test: `internal/webserver/api/Api_test.go`, or a new `//go:build test` file in that package, covering:
  - the owner renames successfully;
  - a user without edit permission on someone else's file is refused;
  - a name with path separators is sanitised;
  - an empty name is rejected.

- [ ] **Step 1.** Write the failing API test. Run `build/dev/gotest.sh ./internal/webserver/api/ -run TestEditFileRename -v` and confirm it fails (RED).
- [ ] **Step 2.** Implement the routing and Api changes. Re-run and confirm it passes (GREEN).
- [ ] **Step 3.** Add the name field to the modal and the JS. Run `node --check` on the changed JS. Run `build/dev/gotest.sh ./internal/webserver/...`.
- [ ] **Step 4.** Commit `files: rename from the edit dialog (filename on /files/modify)` and push.

---

### Task R2: Smaller previews, regenerated once, labelled in the viewer

**Files:**
- Modify: `internal/preview/Generate.go`.
  - Set `thumbWidth = 480` and `viewMaxEdge = 1600`.
  - Change the view JPEG quality from `Q=82` to `Q=75`.
  - Add `const GeneratorVersion = 2`, with a comment saying to bump it whenever outputs change.
- Modify: `internal/preview/Worker.go`. At `Start()`, compare `GeneratorVersion` with the marker file `Dir()/.generator-version`; a missing marker counts as version 1.
  - If they differ: delete every existing preview row and its files, which the backfill then regenerates; write the new marker; log one `info` line (`"Preview generator v2: regenerating N preview(s)"`). The regeneration happens in the normal backfill order.
  - Thumbnails disappear until each file is regenerated. On the live server that takes about 90 s, which is acceptable.
  - Do it before the backfill runs. It must be idempotent and crash-safe: write the marker only after the rows have been deleted.
- Modify: `media.js` viewer. For photos (not videos), show a small label "Lower-resolution preview — download the original for full quality" in the info bar. The info bar already has "Download original". Hide the label when there is no download URL.
- Test, in `internal/preview/Worker_test.go`:
  - a stale marker (or no marker) with existing ready rows → rows removed and requeued, marker written;
  - matching marker → nothing removed.

  Also update `TestGenerate` expectations, for example the view long edge `<= 1600`.

- [ ] **Step 1.** Write the failing tests (RED): `build/dev/gotest.sh ./internal/preview/ -v`.
- [ ] **Step 2.** Implement. Run until GREEN, plus `go vet` cleanliness (the script runs vet as part of test).
- [ ] **Step 3.** Add the viewer label and `node --check`.
- [ ] **Step 4.** Commit `preview: smaller thumbnails and views, regenerate once on version change` and push.

---

### Task R3: Viewer stacking and desktop trackpad navigation

**Files:** `media.js`, `media.css`.

- **Stacking.** Gokapi's user badge is an inline-styled `div` with `z-index:9999`, styled by the live custom CSS. The viewer currently uses `z-index:3000`, so the badge covers the close button.
  - Set `.fgs-viewer` to `z-index: 10050`, and the help sheet (`.fgs-sheet`) to `10060` so it can still open above the viewer.
  - Bootstrap modals use about 1055. The viewer never opens a modal, so this is safe.
- **Horizontal scroll and trackpad.** While the viewer is open, listen for `wheel` on the viewer.
  - When `Math.abs(deltaX) > Math.abs(deltaY)`, accumulate `deltaX`. At about ±80 px, go to next/previous and reset the accumulator.
  - Then ignore further wheel input for about 350 ms so one swipe moves one item. That smooths out inertial scrolling.
  - `preventDefault` horizontal wheel events while the viewer is open, so the browser doesn't navigate back or forward.
  - Leave vertical wheel alone.
  - Don't act when the event target is inside a `<video>` element that is seeking. Simplest: ignore wheel input whose target is inside `video`.
- **Verification.** Run `node --check`. There is no JS harness; the controller verifies in the browser.

- [ ] **Step 1.** Implement both, and `node --check`.
- [ ] **Step 2.** Run `build/dev/gotest.sh ./internal/webserver/ -run TestFilesPage -v` (assets regenerate).
- [ ] **Step 3.** Commit `viewer: above the user badge, trackpad swipe between items` and push.

---

### Task R4: Files tab, select mode, per-file menu, list thumbnails, Square / True shape

This is the largest task. Read `html_files.tmpl`, `admin_ui_files.js`, `media.js` (viewer API `FGS.openViewer(items, index)`, `FGS.itemFromEl`) and the list-view action sub-templates (`admin_button_*`) first.

**Behaviour (from the spec):**

1. **Toolbar.**
   - Add a **Select** button. While select mode is on, it reads **Done**.
   - The icon-size slider shows only in grid view.
   - Add a **Square / True shape** toggle, shown only in grid view. Remember it in localStorage under `filesTileShape`.
2. **Select mode off (the default):**
   - Checkboxes are hidden in both views.
   - Clicking a grid tile opens the viewer at that item.
   - Clicking a file name in the list also opens the viewer at that item. Wrap the name in a `button` styled as a link.
   - Build the viewer item list from the visible items in the current order. In list view that means the DataTable's current order and filter.
3. **Select mode on:**
   - Checkboxes are visible.
   - Clicking a tile or a name toggles its selection instead of opening it.
   - The select bar shows: count · Download · Download as .zip · Save to Photos (iOS) · Create one link · Add to collection… · Change expiry & limits · Delete · "?" (iOS).
   - Turning select mode off clears the selection.
   - Selection works in list view too, with a checkbox column that is hidden when select mode is off.
4. **One "⋯" menu per file, identical in both views.** Items:
   - Open
   - Download original
   - Copy link
   - Share…, using `navigator.share` where available and otherwise copying the link, matching the upload page's existing share/QR helpers
   - QR code
   - Edit (the existing edit modal, now with Name from R1)
   - Replace content (the existing flow)
   - Add to collection…
   - Delete

   Rules for the menu:
   - Reuse the existing JS helpers behind the upload page's buttons (copy, QR, share, edit modal, delete with undo, replace) rather than reimplementing them. Read `admin_ui_upload.js` and the `admin_button_*` templates.
   - In the list, the "⋯" menu replaces the current button cluster in the Actions column. Keep a visible copy-link icon next to it.
   - In the grid, "⋯" sits at the tile's top-right corner. It is always visible on touch devices (`@media (hover: none)`) and appears on hover/focus on desktop.
   - The menu is keyboard-accessible: Escape closes it, and focus returns to the "⋯".
   - Use Bootstrap's dropdown component, which is already loaded on admin pages.
5. **"Add to collection…"** for one file or a selection. Open a small modal that lists your collections and offers a "New collection" field.
   - Adding to an existing collection uses the existing `./collections/modify` endpoint with its add-members form. Read `modifyCollection` in `Collections.go` for the exact fields.
   - Creating a new one uses `./collections/create`.
   - Show the resulting link with a copy button.
   - Only your own files (or PERM_LIST) can be added; the server already enforces this.
6. **"Change expiry & limits"** for a selection. A small modal with:
   - expiry: keep / date / never;
   - download limit: keep / number / unlimited.

   On apply, call the existing `/api/files/modify` for each selected file through `admin_api.js`'s existing wrapper, one request at a time, with progress text. Report failures per file. When done, reload.
7. **Bulk "Delete"** asks first with `confirm("Delete N files? …")`. Then call the existing delete API per file, and reload when finished.
8. **List view** gets a small thumbnail (40×40, `object-fit: cover`, rounded) before the name, using `Preview.Thumb`. Non-previewable files get a type icon. Load it with `loading="lazy"`.
9. **True shape mode.**
   - Tiles stay square. The image uses `object-fit: contain`, letterboxed on a neutral background (`#1c1c1a`).
   - The class goes on the grid root: `.fgs-grid.fgs-shape-true .fgs-tile-open img { object-fit: contain }`.
   - Square mode is unchanged (`cover`).

**Files:** `internal/webserver/web/templates/html_files.tmpl`, `internal/webserver/web/static/js/admin_ui_files.js`, `internal/webserver/web/static/css/media.css`. Possibly `internal/webserver/Files.go`, if the template needs more data such as the user's collections list for the "Add to collection" modal; `AdminView.Collections` may already carry it.

**Tests (Go, page markup):** extend `TestFilesPage` to require:
- `data-fgs-select-toggle`
- `fgs-shape-toggle`
- a list-view thumbnail `<img` with `/p/thumb` for a ready-preview file
- the "⋯" menu markup (`data-fgs-file-menu`)
- the bulk actions `data-act="add-to-collection"`, `data-act="change-expiry"` and `data-act="delete"`

Add a test that a file the user may not list never appears in the "Add to collection" data either.

- [ ] **Step 1.** Extend the page tests (RED). Run `build/dev/gotest.sh ./internal/webserver/ -run 'TestFiles' -v`.
- [ ] **Step 2.** Implement the template, JS and CSS. Run `node --check`. Tests GREEN.
- [ ] **Step 3.** Browser self-check is not required here; the controller does it on the preview. Do include in the report a precise list of the interactions implemented and how each maps to the behaviour list above.
- [ ] **Step 4.** Commit in 2–3 logical commits (for example select mode + open-on-click; per-file menu + list thumbnails + shape toggle; bulk add-to-collection / expiry / delete) and push each.

---

### Task R5: Collections, clickable names and wide pages

**Files:** `html_collections_admin.tmpl` (and its JS, if the table is built in JS), `html_collection.tmpl`, `media.css` (or `cover.css`, if the fixed width comes from a shared class; prefer overriding in `media.css`).

- **Clickable names.** In the Collections tab, a collection's name links to its public page (`./c?id=<id>`, the same URL its copy button uses), opening in the same tab.
- **Wide pages.**
  - On the collection page (`/c`), the card is currently fixed-width. Find the class and container that set it.
  - At ≥ 992 px, let the page use up to about 1280 px (`max-width: min(1280px, 100% - 2rem)`) for both the board and the list.
  - Below 992 px, keep the current behaviour.
  - The password page stays narrow.
- **Test.** Extend `TestCollectionBoardMarkup`, or the collections admin page test, to assert:
  - the admin list links names to `./c?id=`;
  - the collection page carries the wide-layout class, for example `fgs-collection-wide`.
- [ ] **Step 1.** RED. **Step 2.** Implement, GREEN. **Step 3.** Commit `collections: names open the collection, wide pages on desktop` and push.

---

## Self-review against the spec revision (commit 6dbd61c)

| Spec item | Task |
|---|---|
| Rename in Edit | R1 |
| Thumbnails 480 px, view 1600 px Q75, regenerate once | R2 |
| Viewer lower-resolution label | R2 |
| Viewer above the user badge | R3 |
| Trackpad / horizontal scroll navigation | R3 |
| Select mode, open-on-click, no checkboxes when off | R4 |
| "⋯" menu, identical in both views, full suite | R4 (+ R1 for the name) |
| List thumbnails; no slider in list view | R4 |
| Square / True shape | R4 |
| Bulk: Create one link, Add to collection, Change expiry & limits, Delete with confirm | R4 |
| Collection names clickable; wide collection pages | R5 |
| "Turn into a group" | Part 2 (spec only) |
