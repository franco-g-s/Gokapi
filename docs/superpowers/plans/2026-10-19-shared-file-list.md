# Shared file list: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every file list on the site uses one shared component built from today's Files tab. That means the same Grid · Board · List views, thumbnails, "⋯" menu, Select mode and selection bar, viewer, Square/True shape and size slider. Only the content and the allowed actions differ per page.

**Architecture:**
- **One server-side partial** (`file_browser`), fed by a Go view model built with one constructor that encodes the per-page action table.
- **One public JS module** in `media.js` (`FGS.initFileBrowser`) that owns views, selection, the viewer, download / zip / Save to Photos and live-added items.
- **Management actions** (edit, delete, copy/share/QR, add to collection, change expiry, create link, remove from collection) live in the admin bundle and register themselves with the module. Public pages never load them.
- **Rollout:** C1 builds the component and moves the Files tab onto it. C2–C5 then move the other four pages in parallel worktrees.

**Tech Stack:** Go html/template, vanilla JS, Bootstrap 5, DataTables (list view), existing preview routes. Go runs only via `build/dev/gotest.sh` (Docker).

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`. See "One file list everywhere" (commit 3e13345), plus the earlier "Files tab", "Viewer" and "Board layout" sections.

## Global Constraints

- Everything in `docs/superpowers/plans/2026-10-19-part1-revisions.md` → Global Constraints still applies. In particular:
  - Docker-only Go;
  - always `-tags=test,awsmock`;
  - the literal `mux.HandleFunc("<path>", requireLogin(...))` form;
  - viewing never counts as downloading;
  - names only via `textContent` / attribute setters;
  - the Co-Authored-By trailer;
  - push after each commit;
  - never touch `~/apps/gokapi`, the live `gokapi` container, `gokapi-preview` or `~/src/gokapi-preview`;
  - no background processes;
  - implementers do not dispatch subagents.
- **Exact copy strings:**
  - "Grid", "Board", "List"
  - "Select", "Done"
  - "Square", "True shape"
  - "Remove from collection"
  - everything already on the Files tab
- **Per-page action table (binding, from the spec):**

| Context | ⋯ menu | Selection bar |
|---|---|---|
| `files` | Open, Download original, Copy link, Copy hotlink (only for a file that has one), Share…, QR code, Email, Edit, Replace content, Add to collection…, Delete | Download, Download as .zip, Save to Photos (iOS), Create one link, Add to collection…, Change expiry & limits, Delete |
| `upload` | as `files` | as `files` |
| `collection-admin` | as `files` + Remove from collection | as `files` + Remove from collection |
| `filerequest` | Open, Download, Delete | Download, Download as .zip, Save to Photos (iOS), Delete |
| `collection-public` | Open, Download | Download, Download as .zip, Save to Photos (iOS) |

- **Default view per context:**
  - `files` and `upload`: grid.
  - `collection-public`: list.
  - `collection-admin` and `filerequest`: list.
  - Group pages (later): board.
  - A user's choice is remembered per context in localStorage.
- **Parallel tasks C2–C5** run in separate git worktrees. They commit **source only**:
  - Restore regenerated assets before committing: `internal/webserver/web/static/js/min/*`, `css/min/*`, `templates/string_constants.tmpl`, `build/go-generate/minifyStaticContent.go`, `api/VersionNumbers.go`.
  - The controller merges the branches and regenerates once.
  - C2–C5 must not change the shared component files (`html_file_browser.tmpl`, the `FGS.initFileBrowser` part of `media.js`, `admin_ui_filebrowser.js`, `FileBrowser.go`) except for a clearly separated, minimal hook their page needs. Anything bigger is reported as NEEDS_CONTEXT.

---

### Task C1: Build the shared component; Files tab on it; Board view; live thumbnails

**Read first:**
- `internal/webserver/Files.go`
- `html_files.tmpl`
- `admin_ui_files.js`
- `media.js` (viewer, `initBoard`, `initLayoutToggle`, `saveToPhotos`, `downloadSequentially`, `itemFromEl`)
- `media.css`
- the collection page's board markup in `html_collection.tmpl`, which is the reference for Board

**Produce (these interfaces are binding for C2–C5):**

- **Go: `internal/webserver/FileBrowser.go`**
  - `type browserItem struct` with the fields:
    - `File models.FileApiOutput`
    - `Uploader string`
    - `Mine bool`
    - `TakenAt int64`
    - `Preview previewInfo`
    - `Bytes int64`
    - `CanEdit bool`
    - `CanDelete bool`
    - `DownloadUrl string`, the original-file URL for this context: `/files/download` for owners, `./cd?id=…&file=…` for public collections, presign for file requests
  - `type fileBrowser struct` with the fields:
    - `Id string`, a unique DOM id, so several browsers can coexist on one page (the collections admin has one per collection)
    - `Context string`, one of the constants `ctxFiles`, `ctxUpload`, `ctxCollectionAdmin`, `ctxFileRequest`, `ctxCollectionPublic`
    - `Items []browserItem`
    - `DefaultView string`
    - `Menu []string` and `Bulk []string`, action keys taken from the table
    - `ShowScope bool`, true only for `files` with PERM_LIST
    - `ShowUploader bool`
    - `CollectionId string`, used for `collection-admin` and `collection-public`
    - `ZipUrl string`
    - `ActiveUser models.User`
  - Constructor `newFileBrowser(id, context string, items []browserItem, user models.User) fileBrowser`. It fills `Menu`, `Bulk` and `DefaultView` from the action table. A test asserts that every context matches the table exactly.
  - Item builder `browserItemFromFile(f models.File, user models.User, context, collectionId string, uploaderNames map[int]string) browserItem`, which reuses `previewUrls` with the right download URL.
  - The `Files.go` page handler uses these. `FilesView` embeds a `Browser fileBrowser`.
- **Go: new route `/files/previewinfo`** (requireLogin, literal form), with an unauthenticated public twin `/p/info?id=&c=`.
  - Both return JSON `previewInfo` for one file, after `access.CanView`.
  - They exist so a tile whose preview is `pending` (or whose file was just uploaded) can poll every 3 s, up to 2 min, and swap the thumbnail in when it is `ready`.
  - **Viewing never counts.**
- **Template `html_file_browser.tmpl`** defines `file_browser`, rendering the whole component for one `fileBrowser`:
  - **Toolbar:** view switcher Grid · Board · List; Select/Done; Square/True shape and the size slider, shown in Grid only; the scope switch when `ShowScope`.
  - **Selection bar:** only the `Bulk` actions.
  - **Views:** grid tiles, board figures and list rows. All three are rendered from `Items`, or rendered once and laid out three ways (implementer's choice). Each carries the data-* attributes `FGS.itemFromEl` needs, plus `data-bytes` and `data-kind`.
  - **Menu:** the "⋯" menu with only the `Menu` actions. Copy all copy strings from the current Files tab.
  - **Empty state:** "No files yet".
  - **Live adds:** a hidden `<template>` per view for live-added items, used by C2.
  - **Escaping:** templates escape names; the JS uses only `textContent` and setters.
- **JS, public (in `media.js`):** `FGS.initFileBrowser(root)` owns:
  - the views (Board reuses the existing masonry code: `initBoard` / `initLayoutToggle` become internals of the component);
  - Select mode;
  - the selection bar's public actions: download (sequential on desktop), zip (`ZipUrl` + ids where the context supports it; for `collection-public` the existing `/cz?id=` with the selected ids, so add an optional `files=` filter to `/cz` that only narrows the collection's own members, with a test);
  - Save to Photos;
  - opening the viewer in visible order;
  - pending-thumbnail polling;
  - a `root.fgsBrowser.addItem(fileApiOutputLikeObject, previewInfo)` method that builds a tile, figure and row from the `<template>`s;
  - an `FGS.registerFileActions(map)` hook through which the admin bundle supplies management actions.
- **JS, admin (`admin_ui_filebrowser.js`, which replaces the action parts of `admin_ui_files.js`):**
  - Registers edit, delete, replace, copy / hotlink / share / QR / email, add to collection, change expiry & limits, create one link and remove from collection (the last is used by C3, a stub here).
  - Reuses the existing upload-page helpers, as R4 did.
  - Keeps every R4 fix: own collections only (`?mine=1`), keep omits headers, the expiry dialog can't close mid-run, focus returns after copy.
- **Files tab:** `html_files.tmpl` now just renders `{{ template "file_browser" .Browser }}`. Behaviour is unchanged apart from the added Board view and live thumbnails.
- **Collection public page:** do NOT migrate it here; C5 does that. Its existing board must keep working, so either leave the old code paths alive until C5, or migrate them inside C1 if that is simpler (say which).
- **Test serialisation:** make `build/dev/gotest.sh` take a simple cross-process lock, so parallel worktrees don't run several Go test containers at once in the 4 GiB VM. Use `mkdir /tmp/gokapi-gotest.lock` in a wait loop, plus a trap that removes it on exit, and a stale-lock timeout of 20 min.

**Tests:**
- the action-table test for every context;
- `TestFilesPage` updated for the component markup (view switcher with Board, `data-fgs-browser`, `data-context="files"`);
- `/files/previewinfo` and `/p/info` ACL tests (owner yes, stranger no, password file without cookie no, collection with cookie yes);
- the `/cz?files=` narrowing test (it cannot add non-members).

**Browser self-check is strongly recommended.** Use a throwaway instance on 127.0.0.1:53900 with fresh volumes and `build/e2e/setup_local.py`, one container at a time, removed afterwards. Check:
- Files tab Grid / Board / List;
- select mode in all three views;
- the menu and bulk actions;
- the viewer from every view;
- a pending thumbnail appearing without a reload.

- [ ] **Step 1.** RED tests.
- [ ] **Step 2.** Implement in logical commits: component and Files migration; Board view; previewinfo and polling; gotest lock. Run `node --check` and the tests GREEN.
- [ ] **Step 3.** Browser self-check, results in the report.
- [ ] **Step 4.** Push.

---

### Task C2 (parallel): Upload page on the shared component

**Files:**
- `html_admin.tmpl`: replace `#maintable` and its tbody with `{{ template "file_browser" .Browser }}` for context `upload`, with no initial items.
- `Webserver.go` (`showAdminMenu`): build an empty `fileBrowser`.
- `admin_ui_upload.js`: `addRow` becomes a call to `browser.addItem(item, previewInfo)`. The preview starts pending, so the component's polling fills it in.

**Keep working:**
- the "Uploaded now: N" counter;
- SSE download-count updates and deletions;
- `showEditedFile` in-place edit;
- delete with 10 s undo;
- the auto-collection link per batch (Task 12a);
- the "Files you upload now appear below…" note.

**Retired:** the custom branding `admin.js` bar targets `#maintable`. With the table gone it must find nothing and do nothing; check its selectors in `~/src/gokapi-preview/custom/admin.js` (read only).

**Tests:**
- the upload page renders the component with `data-context="upload"`;
- a Go test that the page has no server-rendered stored files (keep `TestUploadPageDoesNotListStoredFiles`).

Browser check: upload 3 files and they appear as tiles; then list view, the menu, Select and Create one link.

### Task C3 (parallel): Collections admin member lists on the shared component

**Files:**
- `html_collections_admin.tmpl`: each collection's member `<ul>` becomes `{{ template "file_browser" … }}` with context `collection-admin`, id `col-<id>`, default List, inside the existing expandable row.
- `Collections.go` (`showCollectionsAdmin`): build one browser per collection.
- `admin_ui_filebrowser.js`: implement the `remove-from-collection` action, single and bulk, using the existing `./collections/modify` remove-members form. Read `modifyCollection`.

The "Add files" button and its picker stay as they are.

**Tests:**
- each collection row renders a browser with that collection's members and the Remove action;
- removing via `modify` keeps working; there is an existing test pattern.

### Task C4 (parallel): File requests on the shared component, and the CanView change

**Files:**
- `html_uploadrequest.tmpl`: each request's file `<ul>` becomes the component with context `filerequest`, id `fr-<id>`, default List.
- the file-requests handler: build the items. `DownloadUrl` uses the existing presign download, through a small JS action that calls `downloadFileWithPresign(id)`, since presign is a JS flow. Delete uses the existing request-file delete.
- `access.CanView`: file-request uploads stay denied, **except** to the request owner (the file's `UserId`) and to PERM_LIST users identified through `SessionIdentity`.
  - The `/d` page behaviour is unchanged.
  - Zip of request files: `/files/zip` already refuses them (`UploadRequestId`). For the `filerequest` context, either allow owner zips of request files in a separate narrow path, or leave zip out of that context's selection bar and note it. Choose the smaller change that keeps `/files/zip`'s rules intact, and explain the choice.

**Tests:**
- CanView: the owner may view a request file's preview, a stranger may not, and anonymous may not;
- the page renders the browser per request.

### Task C5 (parallel): Collection link page (visitors) on the shared component

**Files:**
- `html_collection.tmpl`: replace the board, the table and the Board/List toggle with the component, context `collection-public`, default List.
  - Keep the header (name, size, expiry), the password gate, the wide layout (R5) and the top buttons (Download all, Download as .zip, Save to Photos, "?").
  - The selection bar adds Download / zip / Save to Photos for **selected** items. Zip uses `/cz?id=…&files=…` from C1.
- `Collections.go` (`showCollection`): build items with `DownloadUrl` `./cd?id=<c>&file=<f>` and previews scoped with `&c=`.

**Visitors never see management actions.** The admin bundle isn't loaded on this page; check that.

**Tests:**
- `TestCollectionBoardMarkup` and its siblings updated: the component, `data-context="collection-public"`, list thumbnails, no admin actions in the markup;
- the password page still renders without the component.

---

## Self-review against the spec ("One file list everywhere")

| Spec item | Task |
|---|---|
| Same component on every list | C1 (+ C2–C5 adoption) |
| Grid · Board · List everywhere; defaults per page | C1 (switcher, defaults); C2–C5 (contexts) |
| Per-page menu and selection bar | C1 (action table + test) |
| Upload: live-added items | C1 (`addItem`) + C2 |
| Collections admin: Remove from collection | C3 |
| File requests: owner previews (CanView) | C4 |
| Collection link page: visitor Select mode, list thumbnails | C5 |
| Retire the custom upload-page bar | C2 (code side); controller at deploy (branding file) |
