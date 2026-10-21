# Part 3 — List batching, Save to Photos batching, "View as…": Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every shared file list loads in batches of about 100 while the server answers its filters, counts and "Select all" over the full set. Save to Photos on iPhone works through any selection, one share sheet per measured batch, and sends big videos to the zip. The admin can look at the site as any other person, read-only, with every start and end on the Status log.

**Architecture:**
- **Lists:** each page's item list becomes one Go function, a `listSource`, used by both the page and a JSON feed.
  - The page renders the first batch. The feed answers later batches through a keyset cursor, with filters, search and sort applied on the server.
  - The client appends batches through the existing `addItem` template path. A selection is either explicit ids or "everything matching the query, except these". It is resolved to item data on the server before any action runs.
- **Save to Photos:** this builds on the list work. The selection is resolved, split into measured batches, and fetched one batch at a time, so only one batch is ever held in memory.
- **View as…:** state is per admin, in a `ViewAs` table created `IF NOT EXISTS`.
  - The user is swapped where a session becomes a user (`requireLogin`, `SessionIdentity`).
  - A deny-by-default guard around the whole mux lets only listed reads through.
  - A display-only cookie drives the banner and hides controls that would only be refused.

**Tech Stack:** Go (tested only through `build/dev/gotest.sh` in Docker on Colima), SQLite, Go html/template, vanilla JS, Bootstrap 5, Python e2e scripts (urllib), Playwright MCP for the browser walkthrough.

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`. Read:
- "Build order" (the batching note);
- "One file list everywhere";
- "Board layout", items 1–4;
- "Download all, and zip";
- "Part 3 — Save to Photos, and 'View as…'" (lines 484–524).

Rulings from Parts 1 and 2 still bind: D1–D20 in `2026-10-20-part2-groups.md`, and every "Rulings during execution" section in `2026-10-19-*.md`, `2026-10-20-part2-groups.md` and `2026-10-21-part2-revisions.md`.

---

## Decisions (Franco answered "defaults" on 2026-10-21)

| # | Gap | Decision | Task |
|---|---|---|---|
| P1 | Bulk actions on items not yet loaded | **Every** bulk action acts on the full selection: Download, zip, Save to Photos, Delete, Change expiry & limits, Add to collection, Add to group, Remove from collection, Remove from group, Create one link. The client resolves the selection to item data through the feed before the action runs. "Replace content" gets its own server list (`/files/replacecandidates`), because it used to read names from the page. | T1, T2 |
| P2 | List view sort and search | Sort moves to the server. Name, Uploaded, Taken and Size can always be sorted; Expires and Downloads can be sorted where those columns show. The search box stays, as a case-insensitive name search on the server. DataTables leaves the shared list. | T1, T2 |
| P3 | Log category for "View as…" | `[auth]`. The notifier emails each start and end immediately; an email Franco did not trigger is a break-in signal. | V1 |
| P4 | When "View as…" ends | Exit, logout, 1 hour without a request ("timeout", which also covers an expired session), switching to another person ("switched"), and a server restart ("restart"). Each writes an end line. Only non-admin users can be viewed as, and never yourself. | V1 |
| P5 | Measuring Save to Photos | It cannot be measured in Playwright. An admin-only probe page (P-task) goes into an early preview, and Franco runs it on his iPhone. T3 builds the logic with named constants; the controller sets the measured values in one commit. Until then the defaults are the current values: 20 files, 500 MB per video, 1.5 GB per batch. | P, T3 |
| P6 | Part 2 follow-ups | Fold all three in: the superfluous WriteHeader (F), converted collections in "Shared with me" (F), and a live-added uploader missing from the person filter (T2). | F, T2 |
| P7 | Branding | The sidebar switch is a "View as…" nav link that the repo's header renders for real admins only. The branding `admin.js` already turns nav links into sidebar items, and it only needs an icon entry at deploy. The Users page gets its own "View as…" buttons in the repo. | V1, deploy |
| P8 | Who "View as…" is keyed to | **The admin's user id, not the session.** Every session of that admin is in "View as…" at once, so a second tab or the phone cannot slip past it, and Gokapi's 12-hour session renewal (a new session id) cannot drop it. Starting it deletes the admin's temporary API tokens (`IsSystemKey`, 5-minute lifetime), and `/auth/token` is refused while it is on. Real API keys (scripts) are not tied to a session and are unaffected; `/api/` accepts only keys, never the session cookie. | V1 |
| P9 | Default order of collection and file-request lists | Uploaded, newest first, in every view. Today a collection's Grid follows the collection's own order, while its List sorts by Uploaded; one server order now serves all three views. | T1 |
| P10 | Upload page | Lists only this visit's uploads, added live. Not batched; no feed. | – |
| P11 | Photo dates | No copy promises the date taken; Franco's iOS control test is still pending. The help sheet stays as it is now. | T3 |

---

## Global Constraints

Everything in `docs/superpowers/plans/2026-10-20-part2-groups.md` → Global Constraints still applies. The parts that matter, verbatim or updated:

- **No Go toolchain on the host.** Every `go` command runs through `build/dev/gotest.sh`.
  - Set `DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock`; the script defaults to it.
  - It always passes `-tags=test,awsmock`, and serialises runs across worktrees with a lock.
  - `internal/encryption` and `internal/configuration/setup` are killed by the VM's ~4 GiB of memory. That is known: ignore them. If `internal/configuration/setup/test/` is left behind, delete it.
  - Check for `--- PASS: <TestName>`. Run one package with, for example, `build/dev/gotest.sh ./internal/webserver/ -run TestListQuery -v`.
- **Protected routes** are registered literally in `internal/webserver/Webserver.go` as `mux.HandleFunc("<path>", requireLogin(...))`, which `build/go-generate/updateProtectedUrls.go` reads.
  - Each task adds **one contiguous block** of routes, with a comment naming the task (`// Part 3, T1: list feeds`), directly after the `/p/info` line.
- **Mutations are POST + JSON only**, and answer 405 to any other method (D20). Feeds and pages are GET and **never write**: no `Downloads` row, no `CollectionSeen`, no counters.
- **Viewing never counts as downloading.**
  - `/p/*`, `/files/previewinfo`, `/p/info`, `/groups/item` and the new feeds (`/files/list`, `/gl`, `/cl`, `/files/replacecandidates`) never write a `Downloads` row.
  - The zip routes stay counted exactly as today.
- **Originals are never altered.** No download route ever serves a preview.
- **One access rule.** The feeds use exactly the page's access check and `browserItemFromFile`, so `makeForeign`, `GroupLock`, the `/c` password gate and `collectionClosed` all hold for them.
- **Logging:** new code logs to stdout (`fmt.Println`) only, with **one exception**: the two "View as…" lines, in the existing `auth` category (P3).
  - Never `logging.LogWarn`, and no new category: `~/apps/gokapi/gokapi-notify.py` emails every `[warning]` and every unknown category immediately.
  - At most one `[info]` line per startup.
- **Schema stays at version 17.** The new `ViewAs` table is created with `CREATE TABLE IF NOT EXISTS` in `ensureViewAsTable()`, chained after `ensureGroupTables()`, so rollback stays an image swap. There are no `Upgrade` steps. Quote table names.
- **Names shown to members** come from `displayName(u)` (D5). The admin's own pages keep full names. The "View as…" banner uses `displayName(target)`.
- **Security:** build any DOM that contains a file name, group name or person name with `textContent` and attribute setters only, never `innerHTML`.
- **New code goes in new files** where possible; edits to upstream files stay small.
- **Commits** end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Push after each commit.
- **Branches and worktrees:**
  - The branch is `feature/part3`, cut from `feature/collections` at `39b428b`.
  - Parallel tasks use branches `p3/<task>` in worktrees under `~/src/Gokapi-wt/p3-<task>`, created by the controller.
  - Colima mounts only paths under `~`, never `/tmp`.
- **Parallel tasks commit source only.** Before committing, restore the regenerated files:

  ```bash
  git checkout -- internal/webserver/web/static/js/min internal/webserver/web/static/css/min \
    internal/webserver/web/templates/string_constants.tmpl build/go-generate/minifyStaticContent.go \
    internal/webserver/api/VersionNumbers.go internal/webserver/ProtectedUrls.go docs/setup.rst 2>/dev/null || true
  ```

  A new source JS or CSS file is committed; its `min` output is not. The controller regenerates the minified assets once per merge and git-adds any new min file.
- **Shared component ownership:**
  - **T1** owns the Go side: `FileBrowser.go` and the page handlers' list code.
  - **T2** owns the client side: `html_file_browser.tmpl`, `media.js` except the Save to Photos section, `admin_ui_filebrowser.js`, `media.css`, and the action handlers in `admin_ui_group.js` and `admin_ui_filerequest.js`.
  - **T3** owns only the Save to Photos section of `media.js`: lines 116–342 today, `PHOTO_BATCH` through `FGS.saveToPhotos`. It runs after T2.
  - Any other task that needs a change in those files reports NEEDS_CONTEXT instead of making it.
- **Never touch** the live `gokapi` container, its volumes (`gokapi_gokapi_data`, `gokapi_gokapi_config`) or anything under `~/apps/gokapi`. Only the controller makes previews and deploys, and only after Franco's explicit yes for the deploy.
- No background processes. Implementers do not dispatch subagents.
- **Exact copy strings:**
  - Lists:
    - `Files: N`;
    - `Select all (N)`;
    - `Select everything new to me`;
    - `N selected`;
    - `Search names` (placeholder);
    - `Loading…`;
    - `Couldn't load more files.` followed by a `Retry` button;
    - `No matching files`.
  - Save to Photos:
    - `Preparing N/M…`;
    - `Ready — tap to save`;
    - `Ready — tap to save (batch K of B)`;
    - `Saved ✓`;
    - `Next batch — tap to prepare (K of B)`;
    - `N video(s) are too big to save to Photos from Safari. Download them as a .zip, then save them from the Files app.`;
    - `Download those as .zip`.
  - View as…:
    - nav link and buttons `View as…`;
    - banner `Viewing as anna · read-only · ` followed by an `Exit` button;
    - refusal `View as… is read-only. Exit it to make changes or download.`;
    - picker heading `View the site as…`.
  - Log lines, category `auth`:
    - `<admin name> (user #<id>) started viewing as <target name> (user #<id>)`;
    - `<admin name> (user #<id>) stopped viewing as <target name> (user #<id>): <reason>`, where the reason is one of `exit`, `logout`, `timeout`, `switched`, `restart`.

---

## Waves, worktrees, dependencies

| Wave | Tasks | Runs | Depends on |
|---|---|---|---|
| 1 | **T1** List feeds and server filters · **V1** View as… · **P** Photos probe · **F** Part 2 follow-ups | parallel worktrees | – |
| 1b | Controller merges P first and starts the **early preview with the probe**, so Franco can measure on his iPhone while the rest is built. Then it merges T1, V1 and F. | controller | P |
| 2 | **T2** Client batching and the selection model | worktree `p3-client` from `feature/part3` after wave 1 | T1 |
| 3 | **T3** Save to Photos batching | worktree `p3-photos` after T2 merges | T2; the constants also need Franco's numbers |
| 4 | **E** Part 3 e2e script, existing e2e green, Playwright walkthrough → whole-branch review → preview → deploy | controller | all |

---

## File map

| File | Task | Responsibility |
|---|---|---|
| `internal/webserver/FileList.go` (new) | T1 | `listQuery`, `listSource`, filtering, sorting, keyset cursor, `listItemJson`, `writeList` |
| `internal/webserver/FileListFeeds.go` (new) | T1 | `/files/list`, `/gl`, `/cl`, `/files/replacecandidates` |
| `FileBrowser.go` | T1 | `fileBrowser` gains list fields and `WithList` |
| `Files.go`, `GroupPage.go`, `Collections.go`, `FileRequestsView.go` | T1 | page item code moved into `…Source` functions; pages render the first batch |
| `Zip.go`, `GroupPage.go` (`/gz`) | T1 | zip ids accepted by POST form as well as GET |
| `html_file_browser.tmpl`, `media.js` (`initFileBrowser`, viewer, `initDownloadButtons`), `admin_ui_filebrowser.js`, `admin_ui_group.js`, `admin_ui_filerequest.js`, `admin_ui_upload.js` (`getAllAvailableFiles`), `media.css` | T2 | batches, server filters, sort and search, selection model, resolving, live adds, viewer paging |
| `media.js` (Save to Photos section) | T3 | measured batches, per-batch fetch, big videos to the zip |
| `internal/webserver/PhotosProbe.go`, `templates/html_photos_probe.tmpl`, `static/js/photos_probe.js` (all new) | P | admin-only measurement page |
| `internal/models/ViewAs.go` (new) | V1 | `ViewAs` row |
| `…/provider/sqlite/viewas.go`, `…/provider/redis/viewas.go` (new); `dbabstraction`, `database/Database.go`, `Sqlite.go` (one chained call) | V1 | table and CRUD; Redis unsupported |
| `internal/logging/ViewAs.go` (new) | V1 | the two `auth` lines |
| `internal/webserver/viewas/ViewAs.go`, `Guard.go` (new package) | V1 | state, timeout, startup sweep, allowlist, guard, display cookie |
| `internal/webserver/ViewAsPages.go` (new), `templates/html_viewas.tmpl` (new), `static/js/viewas.js` (new) | V1 | picker page, start and end endpoints, banner, read-only hiding |
| `Webserver.go` (requireLogin swap, `Handler: viewas.Guard(mux)`, logout, nav constant), `authentication/SessionIdentity.go` (swap), `Collections.go` (one condition), `html_header.tmpl` (nav link, script), `html_users.tmpl`, `admin_ui_users.js` | V1 | integration points, each a few lines |
| `cmd/gokapi/Main.go` | V1 | `viewas.SweepAtStartup()` and `viewas.StartTicker()` after the database opens |
| `SharedWithMe.go`, the `/join` handler (`GroupLink.go`) | F | the two follow-ups |
| `build/e2e/part3_e2e.py` (new) | E | batching, filters, cursor and View as… end to end |

---

## Spec test list → task

| Spec or brief item | Task |
|---|---|
| Batches of ~100, fetched near the bottom (Board layout 3) | T1 (server), T2 (client), E |
| Person filter, "Hide what I've downloaded", "Select all (N)", "Select everything new to me" answered over the full set (Board layout 4) | T1, T2, E |
| Zip, download and Save to Photos on ids the page has not rendered | T1 (POST zip, resolve), T2, T3, E |
| Save to Photos batches sized to what Safari accepts; big videos to the zip with an explanation; sizes measured | P (measure), T3 |
| "View as…" read-only; the server rejects every change and every download | V1 (Go route-enumeration test), E |
| Every start and end on the Status log | V1, E |
| Previews and the viewer work as normal under "View as…" | V1 (allowlist), E, walkthrough |
| Existing e2e (groups 91, collections 56) stay green | E |

---

### Task T1: List feeds and server-side filters (worktree `p3-list`)

**Files:**
- Create: `internal/webserver/FileList.go`, `internal/webserver/FileList_test.go`, `internal/webserver/FileListFeeds.go`, `internal/webserver/FileListFeeds_test.go`
- Modify: `FileBrowser.go`, `Files.go`, `GroupPage.go`, `Collections.go`, `FileRequestsView.go`, `Zip.go`, `Webserver.go` (one route block), existing tests in `FileBrowser_test.go`, `GroupPage_test.go`, `Collections_test.go`, `Files_test.go`, `CollectionZipFilter_test.go`, `Zip_test.go` where they assert all items render.

**Interfaces — Produces (binding for T2, T3, E):**

```go
// FileList.go
const listBatch = 100

type listQuery struct {
	Person  int    // person=<user id>; 0 = everyone (group pages only)
	HideHad bool   // hide=1: only items new to the viewer (D12)
	NewOnly bool   // new=1: the same predicate, for "Select everything new to me"
	Scope   string // scope=mine|all (Files tab switch); "" = the source's default
	Search  string // q=: case-insensitive substring of the file name
	Sort    string // sort=name|uploaded|taken|size|expires|downloads; "" = the source's default
	Desc    bool   // dir=desc; ignored when Sort is ""
	After   string // after=<cursor> from the previous answer's Next
	Limit   int    // limit=<n> (1..500); limit=all → -1 (resolving a selection); default listBatch
}

func parseListQuery(r *http.Request) (listQuery, error) // 400 on an unknown sort or bad limit

type listSource struct {
	Items       []browserItem // the whole list, unfiltered
	DefaultSort string        // "uploaded" (desc) or "taken" (asc, group pages, D13)
	DefaultDesc bool
	ViewerId    int  // for Mine / new-to-me
	HasScope    bool // the Files tab's My files / Everything switch applies
	DefaultScope string // "mine" where HasScope (the client's default), else ""
	GroupTools  bool // person, hide and new apply, and New is counted
}

// matching filters and sorts; total order (key, id) so the cursor is exact
func (s listSource) matching(q listQuery) []browserItem
// pageAfter returns up to q.Limit items after q.After, and the cursor of the
// last one returned ("" when nothing follows)
func (s listSource) pageAfter(matching []browserItem, q listQuery) ([]browserItem, string, error)

type listItemJson struct {
	models.FileApiOutput        // already blanked for Foreign items (makeForeign)
	UploaderId   int
	UploaderName string
	Mine         bool
	Downloaded   bool
	CanEdit      bool
	CanDelete    bool
	CanReplace   bool
	CanRemove    bool
	Foreign      bool
	GroupLock    string
	Download     string     // browserItem.DownloadUrl
	Bytes        int64
	TakenAt      int64
	Preview      previewInfo
}
func listItemOf(item browserItem) listItemJson

type listAnswer struct {
	Result string // "OK"
	Total  int    // items matching the query's filters (not the cursor)
	All    int    // the whole list
	New    int    // GroupTools: matching items new to the viewer; else 0
	Items  []listItemJson
	Next   string // "" = this was the last batch
}
func writeList(w http.ResponseWriter, r *http.Request, s listSource) // parse, page, JSON; never cached

// FileBrowser.go: fileBrowser gains
	ListUrl string // the feed with its fixed parameters and a trailing "&", e.g. "./gl?id=G&"; "" = not batched
	Total   int    // matching the default query
	All     int
	New     int
	Next    string // cursor after the rendered first batch
	Sort    string // the default sort and direction, for the List headers
	Desc    bool
// WithList renders the first batch of s under its default query and fills
// the fields above. Items becomes that batch.
func (b *fileBrowser) WithList(s listSource, listUrl string)

// sources (one per page, shared with its feed)
func filesTabSource(r *http.Request, user models.User) (listSource, filesTabMeta)
type filesTabMeta struct{ InGroups, OnlyPerson bool; Person models.User }
func fileRequestSource(r *http.Request, user models.User, request models.FileRequest) listSource
func collectionAdminSource(r *http.Request, manager models.User, c models.Collection) listSource
func collectionPublicSource(r *http.Request, c models.Collection) listSource
func groupSource(r *http.Request, g models.Group, user models.User) (listSource, []models.File, map[int]string)
```

**Feeds** (`FileListFeeds.go`; GET only, 405 otherwise; `addNoCacheHeader`; never write). Each runs exactly its page's access check:

| Route | Login | Parameters | Access (as the page) |
|---|---|---|---|
| `/files/list` | `requireLogin` | `src=files` (+`user=<id>` for the per-person view) | as `/files` |
| `/files/list` | `requireLogin` | `src=request&id=<request id>` | as `/filerequests`: the request is in the viewer's `AdminView.FileRequests` |
| `/files/list` | `requireLogin` | `src=collection&id=<collection id>` | as `/c`'s management list: owner, or admin (`IsAdmin && Id!=0`) with PERM_LIST |
| `/cl` | none | `id=<collection id>` | `collectionUnlocked && !collectionClosed`; otherwise a 404 JSON |
| `/gl` | `requireLogin` | `id=<group id>` | `CanSeeGroup`; otherwise a 404 JSON |
| `/files/replacecandidates` | `requireLogin` | – | `[{Id, Name}]`: listed (`isListedFile`), non-e2e, non-request files the viewer owns, or anyone's with PERM_LIST. Sorted by name. |

Filtering semantics, in `matching`:
- `Scope=mine` keeps `Mine`. It applies only where `HasScope`; `""` uses `DefaultScope`.
- `Person>0` keeps `UploaderId == Person`, on GroupTools sources only.
- `HideHad` / `NewOnly` keep `!Downloaded && !Mine` (D12), on GroupTools sources only.
- `Search` keeps `strings.Contains(strings.ToLower(name), strings.ToLower(q))` after `strings.TrimSpace`.
- `New` counts the items that pass every filter except `HideHad` / `NewOnly` and are new to the viewer.

Sort keys:
- `name`: lower-case name.
- `uploaded`: `UploadDate`.
- `taken`: `TakenAt`, falling back to `UploadDate` (the existing `shootingKey`).
- `size`: `Bytes`.
- `expires`: `UnlimitedTime` → `math.MaxInt64`, else `ExpireAt`; Foreign items → 0.
- `downloads`: `DownloadCount`.

Ties break on `File.Id`, in the same direction. The cursor is `base64.RawURLEncoding` of the JSON `{"s":sort,"d":desc,"k":"<key as string>","i":"<id>"}`. A cursor whose sort or direction differs from the query is a 400. `pageAfter` returns the items strictly after `(k, i)` in the sorted order.

This cursor is what keeps "Hide what I've downloaded" exact while the reader scrolls. An item downloaded since the last batch drops out of `matching`, and the items after the cursor are still the right ones; an offset would skip one.

**Zip by POST:**
- `/files/zip` (`ids`), `/cz` and `/gz` (`files`) read the id list with `r.ParseForm()` and `r.Form`, so POST `application/x-www-form-urlencoded` works exactly like GET.
- `selectedMembers` uses `r.Form.Has("files")` instead of `r.URL.Query().Has`.
- Counting, `CanView`, the password gate and the 410 on an empty selection are unchanged.
- Without the URL length limit, the client always resolves to ids (T2). `ZipAllUrl` stays for the unfiltered select-all.

**Pages:**
- Each handler builds its source and calls `b.WithList(source, url)` instead of passing all items to `newFileBrowser`.
- `/c` for visitors also keeps `mediaButtons.Count` as the whole collection.
- The group page still computes near-copies from `groupSource`'s full file list.
- The person filter's `People` is unchanged.

- [ ] **Step 1: Failing tests for `FileList.go`** in `FileList_test.go`, on hand-built `[]browserItem` (no database):
  - `TestListQueryParse`: defaults (Limit 100); `limit=all` → -1; `limit=0`, `limit=501` and `sort=bogus` → error.
  - `TestListMatchingFilters`: one item each for mine, downloaded, new, and another person's. Person, hide, new, scope and search each keep exactly the expected ids. Person and hide are ignored when `!GroupTools`.
  - `TestListSortEveryKey`: every key, both directions, ties broken by id.
  - `TestListCursorWalk`: 250 items, limit 100. Three pages; the union equals the sorted list exactly once; `Next` is `""` on the last page.
  - `TestListCursorStableUnderHide`: 250 items, `HideHad`. Take page 1, mark items 3 and 7 of page 1 as `Downloaded`, request page 2 with page 1's cursor. Page 2 starts with the item that followed page 1's last, and nothing is skipped.
  - `TestListCursorMismatch`: a cursor made under `sort=name`, used with `sort=size`, → error.
  - `TestListCounts`: `Total`, `All` and `New` under a person filter plus hide.

  Run: `build/dev/gotest.sh ./internal/webserver/ -run 'TestList' -v`. Expected: FAIL (undefined).
- [ ] **Step 2: Implement `FileList.go`** to the interfaces above. Run the tests again; expected: PASS.
- [ ] **Step 3: Failing tests for the sources, feeds and pages** in `FileListFeeds_test.go`, using the existing test fixtures and helpers in `GroupPage_test.go` and `Files_test.go`:
  - `TestPagesRenderFirstBatch`: a group of 130 files → `/g` renders exactly 100 `fgs-tile`s, with `data-total="130"` from the template (T2 adds the attribute; until then assert `view.Browser.Total == 130` through `groupSource` and `WithList` directly).
  - `TestGroupFeedSecondBatch`: `/gl?id=G&after=<Next>` returns the remaining 30 in shooting order, `Total 130`, `Next ""`.
  - `TestFeedAccessMatchesPage`: per feed, a stranger gets 404 or 401 exactly where the page denies. Covers a non-member on `/gl`, a wrong password cookie on `/cl`, a closed collection on `/cl`, a non-owner without PERM_LIST on `src=collection`, and someone else's request on `src=request`.
  - `TestFeedForeignBlank`: a member's `/gl` item for another member's file has an empty `UrlDownload` and `HotlinkId`, a zero `ExpireAt`, and `Foreign: true`.
  - `TestFeedNeverWrites`: `/gl`, `/cl` and `/files/list` leave `GetDownloadedFileIds` and `GetCollectionsSeen` unchanged.
  - `TestFeedMethod`: POST to each feed → 405.
  - `TestFilesFeedScope`: a user without PERM_LIST in a group. `scope=mine` lists only their own; `scope=all` adds the group's files as Foreign.
  - `TestReplaceCandidates`: own listed files only, no e2e, no request uploads; PERM_LIST sees everyone's.
  - `TestZipPostIds`: POST `/gz` with form `files=a,b` serves a zip of those two. POST `/files/zip` with `ids=` the same. GET behaviour is unchanged (the existing tests stay green).
  - `TestParityJsonVsTemplate`: render one tile through the `file_browser_tile` template and the same item through `listItemOf`. Assert the JSON carries every value the tile renders: id, name, download, bytes, taken, uploaded, uploader, uploader-id, downloaded, mine, foreign, kind, w, h, thumb, view, video, color and preview status. Use a table of attribute → JSON field.

  Run: `build/dev/gotest.sh ./internal/webserver/ -run 'TestPagesRenderFirstBatch|TestGroupFeed|TestFeed|TestFilesFeed|TestReplaceCandidates|TestZipPost|TestParity' -v`. Expected: FAIL.
- [ ] **Step 4: Implement:**
  - move each page's item code into its `…Source` function;
  - add `WithList`;
  - add the feeds and their route block (`// Part 3, T1: list feeds`);
  - make zip accept POST.

  Update the existing tests that counted every tile on a page of up to 100 items; they should still pass unchanged. Run the full package: `build/dev/gotest.sh ./internal/webserver/...`. Expected: PASS.
- [ ] **Step 5: Full suite**, `build/dev/gotest.sh`. Expected: everything passes except the two known OOM packages.
- [ ] **Step 6: Commit** (source only; restore the generated files first):

  ```bash
  git add internal/webserver/FileList*.go internal/webserver/FileListFeeds*.go internal/webserver/*.go
  git commit -m "Part 3, T1: list feeds with server-side filters, sort and keyset cursor; pages render the first 100"
  git push -u origin p3/list
  ```

---

### Task T2: Client batching and the selection model (worktree `p3-client`, after T1 merges)

**Files:** `html_file_browser.tmpl`, `media.js` (`initFileBrowser`, the viewer, `FGS.initDownloadButtons`, `collectionDownloads`), `admin_ui_filebrowser.js`, `admin_ui_group.js` (action handlers only), `admin_ui_filerequest.js` (its photos and zip handlers only), `admin_ui_upload.js` (`getAllAvailableFiles` only), `media.css`.

**Interfaces:**
- Consumes: T1's feeds, `listAnswer`, `listItemJson`, the fileBrowser fields `ListUrl`, `Total`, `All`, `New`, `Next`, `Sort` and `Desc`, `/files/replacecandidates`, and zip by POST.
- Produces (binding for T3 and V1's CSS):

```js
// on the browser api (root.fgsBrowser)
api.selection()        // {mode: "ids", ids: [..]} | {mode: "query", query: {..}, except: [..]}
api.selectedCount()    // number shown as "N selected"
api.resolveSelected()  // Promise<itemData[]>: rendered data, or the feed with limit=all minus except
api.resolveAll()       // Promise<itemData[]>: every item of the list (collection page buttons)
api.zipIds(ids)        // POSTs ids to the page's zip (or the context's zip handler)
api.buildItem(json)    // {tile, row}, not inserted: the parity check uses it
api.loadMore()         // Promise; resolves when the next batch (if any) is in
FGS.itemData(x)        // tile element or listItemJson → {id, name, url, bytes, taken, uploaded,
                       //   kind, mine, foreign, downloaded, canEdit, canDelete, canReplace,
                       //   canRemove, e2e, groupLock, downloads, unlimitedDownloads, expire,
                       //   unlimitedTime, password, fileUrl}
// actionArg gains: data (itemData[] of the full selection, resolved), and keeps
// tiles (only the rendered ones), ids (all ids), resolve() is already done
// before a handler runs
```

**Template:**
- The root gets:
  - `data-list-url="{{ .ListUrl }}"`;
  - `data-total="{{ .Total }}"`;
  - `data-all="{{ .All }}"`;
  - `data-new="{{ .New }}"`;
  - `data-next="{{ .Next }}"`;
  - `data-sort-key="{{ .Sort }}"`;
  - `data-sort-desc="{{ .Desc }}"`.
- `Select all ({{ .Total }})`.
- After the grid and after the table, a sentinel `<div class="fgs-more" data-fgs-more aria-live="polite"></div>`. It holds `Loading…`, or `Couldn't load more files.` with a `Retry` button.
- Every sortable `<th>` gets `data-fgs-sortkey="name|uploaded|taken|size|expires|downloads"` and holds a `<button type="button">` with the column title, plus an arrow on the active column.
- The toolbar gets `<input type="search" class="form-control form-control-sm fgs-list-only" data-fgs-search placeholder="Search names" aria-label="Search names">`.

**`media.js` behaviour:**
1. **Remove DataTables** from `initFileBrowser`. The tbody is in server order.
   - The `dataTable` code paths go away: `visibleTiles` in List reads the tbody rows; `addItem`, `removeItem` and `updateItem` touch the DOM only.
   - `admin_ui_filerequest.js:654`'s `columns.adjust` guard becomes a no-op for these tables. Leave its `isDataTable` check in place.
2. **Query state:** `{scope, person, hide, q, sort, desc}`.
   - Initial values come from the root's defaults, then the remembered prefs (`scope` and `hideDownloaded`, as today).
   - If a remembered value differs from what the server rendered, add `fgs-reloading` to `.fgs-views` before the first paint and call `reload()`. CSS gives `.fgs-reloading` `visibility:hidden`, so the wrong batch never flashes.
   - The server's Files default scope is `mine` (T1), the same as the client's, so a first visit needs no reload.
3. **`loadMore()`:**
   - Fetch `ListUrl + queryParams + "after=" + next`, then append each item through the same code as `addItem` without sorted insertion, so items appear in server order.
   - Update `next`, `total`, `all` and `new`.
   - An IntersectionObserver on the sentinel, with `rootMargin: "0px 0px 200% 0px"` (about two screens), calls it; re-observe after each batch.
   - If the board is in view, relayout masonry once per batch.
   - Thumbnails of appended items use `loading="lazy"`; the first screen keeps `fetchpriority="high"` from the server.
   - On a failed fetch, show `Couldn't load more files.` and `Retry`.
   - At most one request in flight. A reload aborts any older request (`AbortController`) and ignores its answer.
4. **`reload()`** runs on any filter, scope, search (debounced 300 ms) or sort change.
   - It clears the tiles, rows, selection, polling state and `tilesById` / `rowsById`, then loads the first batch.
   - "Files: N" shows `total`; `Select all (N)` shows `total`; the empty state shows `No matching files` when `all > 0`.
5. **Selection model:**
   - **`ids` mode**, from taps on rendered items: an explicit set.
   - **`query` mode**:
     - "Select all (N)" captures the current query plus an empty `except`;
     - "Select everything new to me" captures the current query with `new=1`;
     - unticking an item in query mode adds it to `except`;
     - items appended later that match are ticked on arrival (with `new=1`, ticked only if new to me per D12: `!downloaded && !mine`).
   - "N selected" is `total` (or `new` for the new-to-me capture) minus `except.length`.
   - Any reload, or leaving select mode, clears the selection, as filters do today.
6. **`resolveSelected()`:**
   - In `ids` mode it returns `FGS.itemData` of those tiles.
   - In `query` mode it fetches `ListUrl + query + new + "limit=all"` and drops the `except` ids. While it runs, the action button shows `Loading…`.
   - Every bulk handler receives `a.data` (all items), `a.ids` and `a.tiles` (the rendered subset). On the Files tab, the existing filter on Foreign items for anything but download and photos applies to `a.data`.
7. **Bulk actions on resolved data:**
   - **Download** runs `downloadSequentially(data)`.
   - **Zip:**
     - with a query selection, no filters and an empty `except`, use `ZipAllUrl` where it exists;
     - otherwise use `api.zipIds(ids)`, which POSTs a hidden form (`ids` for `/files/zip`, `files` for `/cz` and `/gz`) with `target` = the page itself. The response is an attachment, so the page stays.
     - The 8,000-character URL limit goes away.
   - **Mark downloaded:** with the group tools, mark the resolved ids downloaded as today.
   - `admin_ui_filebrowser.js` handlers (delete, bulk delete, change expiry, add to collection, add to group, create one link) and the remove handlers in `admin_ui_group.js` and `admin_ui_collection` read `FGS.itemData` fields from `a.data` instead of `fileData(tile)` and menu attributes.
   - `removeItem` / `updateItem` still act only on rendered items. An id not rendered just adjusts `total` / `all`.
8. **Live adds:**
   - If `next !== ""` and the new item sorts after the last rendered item under the current sort, do not insert it. Increase `all`, and `total` if it matches the filters.
   - Otherwise insert as today: sorted position for taken-asc, top for uploaded-desc.
   - **Follow-up 3:** a live add on a group page whose uploader is not in the person `<select>` adds an `<option value=id>name</option>` (`textContent`).
9. **List sort:**
   - Header buttons set `sort` and `desc`. A second click on the active column flips the direction; the default direction is `desc` for uploaded, size, expires and downloads, and `asc` for name and taken.
   - Then `reload()`.
   - Remember sort per browser key, like `view`.
10. **Search:** the box is visible in List only. Switching to Grid or Board with a search active clears it and reloads, as the DataTables search did.
11. **Viewer:** `FGS.openViewer(list, index, more)`. `more()` returns a Promise of further items (`api.loadMore()` mapped through `FGS.itemFromEl`). It is called when the reader reaches the last item, and the viewer appends what arrives.
12. **Collection page buttons:** `FGS.initDownloadButtons` uses `browser.resolveAll()` instead of `collectionDownloads()`'s DOM read; "Download all" and Save to Photos get every member. `collectionDownloads` is deleted.
13. **Replace content:** `getAllAvailableFiles()` in `admin_ui_upload.js` fetches `./files/replacecandidates` and becomes asynchronous. `showEditModal` fills the select when it resolves. The rest of the edit modal is unchanged.

- [ ] **Step 1:** Template attributes, sentinel, sort headers and search box. Update the Go template tests in `FileBrowser_test.go` that assert markup, and add `TestTemplateListAttributes`. Run `build/dev/gotest.sh ./internal/webserver/ -run 'TestTemplate|TestFileBrowser' -v`. Expected: PASS.
- [ ] **Step 2:** DataTables removal, query state, `loadMore` and `reload`. Check by hand against a throwaway container (see E for how to start one) with a group of 250 tiny images, from `build/e2e/part3_e2e.py --seed-only` once E has written it, or from a 10-line upload loop. Scroll the Board, Grid and List: the count reads `Files: 250`, every item appears once, in order.
- [ ] **Step 3:** Selection model, `resolveSelected`, zip POST, and the handlers moved onto `a.data`. Check: Select all (250) → zip → 250 entries; untick one → zip → 249; person filter + Select all → only that person's.
- [ ] **Step 4:** Live adds, the person option, sort, search, viewer paging, collection buttons and Replace content.
- [ ] **Step 5:**
  - Run `build/dev/gotest.sh` for the full suite.
  - Run the existing e2e scripts, which must stay green: groups 91/91, collections 56/56.

    The existing scripts parse server-rendered tiles of lists under 100 items, so they should pass unchanged. If one fails because it counts tiles past 100, report it rather than editing the script; E owns the scripts.
- [ ] **Step 6: Commit** (source only), push `p3/client`.

---

### Task T3: Save to Photos batching (worktree `p3-photos`, after T2 merges)

**Files:** `media.js`, the Save to Photos section only.

**Interfaces:**
- Consumes: `api.resolveSelected()`, `api.zipIds(ids)`, `FGS.itemData`, and the measured constants from P (or the defaults).
- Produces: `FGS.saveToPhotos(items, button, opts)`, where `opts = {zipIds: function(ids)}`, and `FGS.planPhotos(items, limits)`.

```js
// Measured on Franco's iPhone (model, iOS version, date) — the controller fills these in
var PHOTO_BATCH_FILES = 20;      // files per share sheet
var PHOTO_BATCH_BYTES = 1.5e9;   // bytes per share sheet
var VIDEO_MAX_BYTES  = 500e6;    // larger videos go to the zip

// Pure: no fetch, no DOM. Returns
// {batches: [[item]], bigVideos: [item], noUrl: [item], notMedia: [item]}
// in input order; a batch closes when adding the next item would pass
// PHOTO_BATCH_FILES or PHOTO_BATCH_BYTES (a single item over the byte limit
// that is not a video gets a batch of its own).
FGS.planPhotos = function (items, limits) { ... };
```

**Flow:**
1. The first tap resolves the selection (T2) and calls `planPhotos`.
2. If there are `bigVideos`, show in the selection status (`aria-live`): `N video(s) are too big to save to Photos from Safari. Download them as a .zip, then save them from the Files app.` followed by a `Download those as .zip` button, which calls `opts.zipIds(bigVideo ids)`.
3. **Fetch the batch.** Fetch batch K only: never more than one batch in memory, and drop the Files after their share resolves. The label shows `Preparing n/m…`.
   - Then, if the activation is still alive, open the share sheet; else show `Ready — tap to save` (`Ready — tap to save (batch K of B)` when B > 1).
   - Keep the existing `lastModified` date logic. It is not promised in any copy (P11).
4. After a batch's share resolves, show `Next batch — tap to prepare (K+1 of B)` while batches remain, else `Saved ✓`.
   - An `AbortError` or `NotAllowedError` keeps the prepared batch, as today.
   - A selection change cancels everything, as today (`FGS.resetSaveToPhotos`).
5. **Never fetch before deciding.** Sizes come from `bytes`, so a counted `/cd` or `/gd` request is never made for a file that will be left out. This is the existing rule, kept.
6. **Group pages:** mark each batch's ids downloaded when it is fetched, kept visible (`markDownloaded(ids, true)`), as today.
7. The "⋯" menu's single-file Save to Photos goes through the same function with one item.

- [ ] **Step 1:** Write `planPhotos` and verify it with a Playwright `browser_evaluate` against the test container, using the cases below:
  - 45 photos of 2 MB, limits 20 files / 1.5 GB → batches of 20, 20, 5;
  - 3 photos of 600 MB → 2 + 1;
  - a 700 MB `.mov` → `bigVideos`;
  - an item without a url → `noUrl`;
  - a `.pdf` → `notMedia`.

  Record the evaluate output in the task report.
- [ ] **Step 2:** The per-batch flow. In Playwright on a 390×844 viewport:
  - set an iPhone user agent with `browser_run_code_unsafe` / context options;
  - stub `navigator.canShare = () => true` and `navigator.share = files => { window.__shared.push(files.files.length); return Promise.resolve(); }`, and stub `navigator.userActivation` as active;
  - select 45 photos and tap through.

  Expected: `__shared` is `[20, 20, 5]`, and only one batch of Blobs exists at a time (check that each batch is fetched only after the previous share resolved: log fetch URLs with timestamps).
- [ ] **Step 3:** Big videos: a fixture `.mov` with `data-bytes` above `VIDEO_MAX_BYTES` (fake the size in the item; no large file needed). Expected: the explanation and the button show, and the button POSTs the zip with exactly that id.
- [ ] **Step 4:** Run the full suite and the existing e2e. Commit (source only), push `p3/photos`.

---

### Task P: Photos probe (worktree `p3-probe`, wave 1, for Franco's iPhone)

**Files:** create `internal/webserver/PhotosProbe.go`, `templates/html_photos_probe.tmpl`, `static/js/photos_probe.js`, `PhotosProbe_test.go`; modify `Webserver.go` (one route block, `// Part 3, P: photos probe`).

**What it is:**
- `GET /photosprobe` (`requireLogin`) for a real admin only (`IsAdmin && Id != 0`); 404 for anyone else. Not in the nav.
- It renders the admin's own listed photos and videos as JSON in the page: `[{id, name, bytes, kind}]`, sorted by size descending, with download URLs `./files/download?id=` (uncounted).
- It never writes anything.
- It is **removed before the PR** (a revert commit by the controller), once Franco's numbers are in.

**`photos_probe.js` controls:**
- **Photos:**
  - inputs: count `N` (default 20) and a byte cap in MB (default 1500);
  - "Try": fetches the first N photos whose total stays under the cap, shows `fetched N files, X MB in T s`, then opens the share sheet if the activation is alive, else shows a "Share" button.
- **One video:** a `<select>` of the videos with their sizes, and "Try": the same flow for that one file.
- **Results log:**
  - each attempt appends one line: kind, N, MB, fetch seconds, and the share result (`resolved`, or the error `name: message`);
  - a "Copy log" button copies all lines.
- **Instructions at the top**, in plain text:
  1. Start with Photos, N = 20. Raise N (30, 50, 80) until the sheet fails, or "Save N Images" saves fewer than N. Check the Photos app's count each time.
  2. Then raise the MB cap with large photos until Safari reloads the page or the share fails.
  3. Then try videos, from small to large, until one fails or the tab reloads.
  4. Copy the log and send it.
- A tab reload is itself a result: the log is kept in `sessionStorage` so it survives one.

- [ ] **Step 1:** `TestPhotosProbeAccess`: a non-admin → 404; an admin → 200; the page lists only the admin's own photos and videos, and writes nothing.
- [ ] **Step 2:** Implement. Check in Playwright (desktop) that the page renders and that "Try", with stubbed `navigator.share`, logs a line.
- [ ] **Step 3:** Commit (source only), push `p3/probe`.

**Controller, wave 1b:**
1. Merge P.
2. Build `gokapi-fgs:part3-probe` and start the early preview described under "Controller".
3. Send Franco the URL and the instructions.
4. Turn his log into the three constants: take the largest values that succeeded every time, with a safety margin of about 25%. Record the measurement (device, iOS version, raw log lines) in the Rulings.

---

### Task V1: "View as…" (worktree `p3-viewas`, wave 1)

**Files:**
- Create:
  - `internal/models/ViewAs.go`;
  - `internal/configuration/database/provider/sqlite/viewas.go`, `…/redis/viewas.go`;
  - `internal/logging/ViewAs.go`;
  - `internal/webserver/viewas/ViewAs.go`, `Guard.go`, `ViewAs_test.go`, `Guard_test.go`;
  - `internal/webserver/ViewAsPages.go`, `ViewAsPages_test.go`, `ViewAsRoutes_test.go`;
  - `templates/html_viewas.tmpl`;
  - `static/js/viewas.js`.
- Modify:
  - `dbabstraction/DbAbstraction.go`, `database/Database.go`, `sqlite/Sqlite.go` (chain `ensureViewAsTable()`);
  - `Webserver.go`: `Handler: viewas.Guard(mux)`, the `requireLogin` swap, the logout end, the route block, `ViewViewAs`;
  - `authentication/SessionIdentity.go` (the swap);
  - `Collections.go` (the CollectionSeen condition);
  - `html_header.tmpl` (nav link, script tag in both branches);
  - `html_users.tmpl`, `admin_ui_users.js`;
  - `cmd/gokapi/Main.go`.

**Interfaces — Produces:**

```go
// models/ViewAs.go
type ViewAs struct {
	AdminId  int
	TargetId int
	Started  int64
	LastSeen int64
}

// database (sqlite; redis: SupportsGroups() is false → every call is a no-op, Get returns false)
GetViewAs(adminId int) (models.ViewAs, bool)
GetAllViewAs() []models.ViewAs
SaveViewAs(v models.ViewAs)
DeleteViewAs(adminId int)
// table:
// CREATE TABLE IF NOT EXISTS "ViewAs" ("AdminId" INTEGER PRIMARY KEY, "TargetId" INTEGER NOT NULL,
//   "Started" INTEGER NOT NULL, "LastSeen" INTEGER NOT NULL)

// logging/ViewAs.go — category auth (P3)
func LogViewAsStart(admin, target models.User)
func LogViewAsEnd(admin, target models.User, reason string)

// webserver/viewas (imports database, logging, models, sessionmanager; never authentication)
const Idle = time.Hour
var ErrNotAdmin, ErrBadTarget, ErrUnsupported error
func IsRealAdmin(u models.User) bool                 // u.IsAdmin() && u.Id != 0
func Start(admin models.User, targetId int) error    // ends a current one with "switched" first; deletes admin's IsSystemKey keys
func End(adminId int, reason string)                 // logs the end only if one was active
func Target(adminId int) (models.User, bool)         // active and not idle past Idle; read-only
func Touch(adminId int)                              // LastSeen = now, written at most once a minute
func SweepAtStartup()                                // ends every row with "restart"
func StartTicker()                                   // every minute: End(..., "timeout") for rows idle > Idle
func Allowed(method, path string) bool               // the allowlist below
func Guard(next http.Handler) http.Handler
func Active(r *http.Request) (admin, target models.User, ok bool) // from the session cookie, read-only
func MarkSwapped(r *http.Request, real models.User) *http.Request
func RealUser(r *http.Request) (models.User, bool)   // the admin behind a swapped request
const DisplayCookie = "fgs_viewas"
```

**The swap:**
- `requireLogin`: after the `ResetPassword` check, which therefore always runs against the real admin, add:

  ```go
  if target, ok := viewas.Target(user.Id); ok {
  	viewas.Touch(user.Id)
  	r = viewas.MarkSwapped(r, user)
  	user = target
  }
  ```

  It goes before `SetUserInRequest`.
- `SessionIdentity`: after `PeekSession`, `if t, ok := viewas.Target(u.Id); ok { return t, true }`. Everything that reads the session then sees the target: `CanView`, `/c`'s owner bar, `tagDownload`, `/join`.
- The real-admin checks in `/viewas`, `/viewas/start` and `/viewas/end` use `viewas.RealUser(r)`, falling back to `GetUserFromRequest`.

**The guard** wraps the whole mux (`Handler: viewas.Guard(mux)`). For each request:
1. `Active(r)`; if not active, clear `fgs_viewas` if it was sent, and pass the request through.
2. If active and `Allowed(r.Method, r.URL.Path)`, set `fgs_viewas` if it is missing or stale, and pass the request through.
3. `/logout` (any method): `End(admin, "logout")`, clear the cookie, pass through.
4. Anything else is a **403**:
   - JSON `{"Result":"error","ErrorMessage":"View as… is read-only. Exit it to make changes or download."}` when the method is not GET or HEAD, or the path is a JSON endpoint (`/api/`, `/groups/`, `/collections/`, `/files/` sub-paths, `/viewas/`, `/auth/token`), or `Accept` contains `json`;
   - otherwise a small HTML page with the same sentence and an `Exit` button (a form POST to `./viewas/end`, then a redirect to `./admin`). Build it with `html/template`.
   - The request never reaches the handler.

**`Allowed`:**
- GET or HEAD on these exact paths: `/admin`, `/files`, `/files/list`, `/files/previewinfo`, `/groups`, `/g`, `/gl`, `/groups/item`, `/groups/list`, `/groups/neardupes`, `/collections`, `/collections/list`, `/filerequests`, `/c`, `/cl`, `/p/thumb`, `/p/view`, `/p/poster`, `/p/video`, `/p/info`, `/join`, `/g/join`, `/users`, `/logs`, `/apiKeys`, `/uploadStatus`, `/index`, `/error`, `/viewas`.
- GET or HEAD on these prefixes: `/js/`, `/css/`, `/assets/`, `/custom/`, `/favicon`.
- POST on `/viewas/start` and `/viewas/end` only.
- Everything else is refused, including every download (`/d`, `/downloadFile`, `/h/`, `/hotlink/`, `/dh/`, `/downloadPresigned`, `/files/download`, `/files/zip`, `/cd`, `/cz`, `/gd`, `/gz`) and `/api/`, `/auth/token`, `/uploadChunk`, `/changePassword`, `/e2eSetup`, `/oauth-*` and `/photosprobe`.

**The display cookie** `fgs_viewas`:
- Not HttpOnly; `SameSite=Lax`; `Path=/`.
- Its value is `url.QueryEscape(displayName(target))`.
- It is **display only**: no server decision ever reads it. The guard keeps it in sync on every response, so a second device gets it on its next request.

**`viewas.js`** is loaded synchronously in `<head>` by `html_header.tmpl`, in both the admin and the public branch:
- If the cookie is set:
  - it adds `fgs-viewas` to `<html>` at once;
  - on `DOMContentLoaded` it inserts a fixed top banner: `Viewing as <name> · read-only · ` and an `Exit` button, all with `textContent`. Exit is a POST to `./viewas/end`, then `location.href = "./admin"`.
- It injects one `<style>` that, under `.fgs-viewas`, hides controls the server would refuse:
  - the selection bar's `[data-act]`;
  - the "⋯" entries other than `[data-menu=open]`;
  - the list's quick download and copy buttons;
  - `[data-fgs-download-all]`, `[data-fgs-download-zip]` and `[data-fgs-photos]`;
  - the viewer's download and Save to Photos buttons (find their selectors in `media.js`'s viewer);
  - the upload drop zone and upload buttons;
  - the group page's management buttons.

  These are hidden as a courtesy; the server is the enforcement.

**Pages and endpoints** (`ViewAsPages.go`, route block `// Part 3, V1: view as`):
- `GET /viewas` (`requireLogin`, `true`): 404 unless the real user is an admin. It renders `html_viewas.tmpl` with the AdminView frame:
  - heading `View the site as…`;
  - every non-admin user other than yourself, as full name plus a `View as…` button.
- `POST /viewas/start`, JSON `{"userId": n}`:
  - 404 unless real admin;
  - 400 for yourself, an admin, or an unknown user;
  - 501 on Redis;
  - on success `{"Result":"OK"}`. The client then navigates to `./files`.
- `POST /viewas/end`: `End(real admin, "exit")` → `{"Result":"OK"}`.
- Nav: in `html_header.tmpl`, before Logout:

  ```
  {{ if and .ActiveUser.IsAdmin (ne .ActiveUser.Id 0) }}<a class="nav-link {{ if eq .ActiveView 8 }}active{{ end }}" href="./viewas">View as…</a>{{ end }}
  ```

  `ViewViewAs = 8`. Under "View as…" `ActiveUser` is the target, so the link disappears, and the banner's Exit is the way out.
- Users page: in each row of a non-admin user other than the viewer, a `View as…` button (`btn-outline-light btn-sm`) wired in `admin_ui_users.js` to the same POST.
- `Collections.go`: the `SaveCollectionSeen` condition gains `&& !viewasActive(r)`, where `viewasActive(r)` is `_, _, ok := viewas.Active(r); ok`.
- `cmd/gokapi/Main.go`: after the database is open and before the webserver starts, `viewas.SweepAtStartup()` and `viewas.StartTicker()`.

- [ ] **Step 1: Failing store and state tests** (`viewas/ViewAs_test.go`, on the test SQLite database):
  - `TestStartRules`: non-admin → `ErrNotAdmin`; yourself, an admin or an unknown id → `ErrBadTarget`.
  - `TestStartDeletesSystemKeys`: the admin's `IsSystemKey` keys are gone after Start; a non-system key remains.
  - `TestTargetIdle`: `LastSeen` 61 minutes ago → `Target` false.
  - `TestTickerEndsIdle`: run the ticker body once → the row is gone, and an `auth` end line with reason `timeout` is in the log.
  - `TestSweepAtStartup`: two rows → both gone, two end lines `restart`.
  - `TestSwitchLogsEnd`: Start(B) while viewing as A → an end line `switched` for A, then a start line for B.
  - `TestEndLogsOnce`: a second End is silent.

  Read the log lines with `logging.GetAll()`, after `logging.Init` on a temp dir as the logging tests do.
- [ ] **Step 2:** Implement the model, the table, the logging and `viewas/ViewAs.go`. Tests PASS.
- [ ] **Step 3: Failing guard tests** (`viewas/Guard_test.go`):
  - `TestGuardPassesWhenInactive`.
  - `TestGuardAllowlist`: allowed GET passes; allowed path with POST → 403 JSON.
  - `TestGuardHtmlRefusal`: GET `/gd` with `Accept: text/html` → 403 HTML containing the sentence and a form to `./viewas/end`.
  - `TestGuardLogoutEnds`.
  - `TestGuardCookieSync`: active and no cookie → `Set-Cookie: fgs_viewas=…`; inactive with cookie → expiring `Set-Cookie`.
- [ ] **Step 4:** Implement `Guard.go`. Tests PASS.
- [ ] **Step 5: Failing integration tests** (`ViewAsPages_test.go`, package `webserver`):
  - `TestRequireLoginSwaps`: an admin session viewing as B → a handler behind `requireLogin` sees B, and `RealUser` is the admin.
  - `TestResetPasswordUsesRealAdmin`: B has `ResetPassword=true` under internal auth; the admin viewing as B gets `/files`, not a redirect to `changePassword`.
  - `TestSessionIdentitySwaps`: `CanView` on B's private file is true through the swap, and false for a file only the admin may see.
  - `TestCollectionSeenSkipped`: GET `/c` of A's collection while viewing as B → no `CollectionSeen` row for B.
  - `TestStartEndpoint`: every rule in the route spec, plus 405 on GET.
  - `TestNavLinkOnlyForRealAdmin`: `/files` as admin shows `View as…`; viewing as B does not.
  - `TestAuthTokenRefused`: `/auth/token` → 403 while active.
- [ ] **Step 6: The spec's test** (`ViewAsRoutes_test.go`):
  - Read `Webserver.go`'s source with `os.ReadFile` and collect every `mux.HandleFunc("<path>"` and `mux.Handle("<path>"`, plus `/oauth-login`, `/oauth-callback` and `/api/`.
  - Replace `{id}` and `{filename}` with `x`.
  - For each path and each of GET and POST, send the request through `viewas.Guard` (wrapping a handler that records whether it was reached) with an admin session viewing as B.
  - Assert reached == `Allowed(method, path)`.
  - A hand-written `mustRefuse` list covers every download route and every mutation route by name:
    - downloads: `/d`, `/downloadFile`, `/h/`, `/hotlink/`, `/dh/x/x`, `/downloadPresigned`, `/files/download`, `/files/zip`, `/cd`, `/cz`, `/gd`, `/gz`;
    - mutations and the rest: `/api/`, `/auth/token`, `/uploadChunk`, `/changePassword`, `/collections/create`, `/collections/modify`, `/collections/delete`, every `/groups/*` POST route, `/forgotpw`, `/publicUpload`, `/e2eSetup`.

    For each of those, `Allowed` is false for both GET and POST, so a route added to the allowlist later fails here.
  - A route added to `Webserver.go` later is refused until someone allowlists it, and this test lists it.
- [ ] **Step 7:** Implement the pages, nav, Users button, `viewas.js` and the `Main.go` hooks. Run the full suite. Then check in Playwright against a throwaway container, desktop:
  1. Start as B from the Users page; the banner shows.
  2. Files, Groups and a group page show B's items, and the viewer opens.
  3. The selection bar and download controls are hidden.
  4. Exit works.
  5. The Status log shows the start and end lines.
- [ ] **Step 8: Commit** (source; `viewas.js` is committed, its min is not), push `p3/viewas`.

---

### Task F: Part 2 follow-ups (worktree `p3-followups`, wave 1)

**Files:** `SharedWithMe.go`, `SharedWithMe_test.go`, the `/join` handler and its test (`GroupLink.go`, `GroupLink_test.go` or `GroupsTab.go`, wherever `showJoin` lives), and `Webserver.go` only if `redirect` itself is at fault.

1. **"Shared with me" lists converted collections.** In `sharedWithMe`, skip a collection for which `storage.CollectionLinkOf(c.Id)` reports `linked`, as `/collections/list` already does.
   - [ ] `TestSharedWithMeHidesConverted`: a seen collection that is then turned into a group is gone from the list. FAIL, implement, PASS.
2. **The "superfluous WriteHeader" line** after `/join?t=bogus`.
   - [ ] Write `TestJoinBogusTokenWritesHeaderOnce`. It uses a `ResponseWriter` wrapper that counts `WriteHeader` calls; GET `/join?t=bogus`, with and without a session, must count exactly 1.
   - [ ] Find the second write (a status written before `redirect`, or a template executed after it), and fix it at the source, with the smallest change.
   - [ ] The test passes.
3. [ ] Run the full suite, then commit and push `p3/followups`.

---

### Task E: Part 3 end to end (controller, after T3)

**Files:** create `build/e2e/part3_e2e.py`, in the style of `groups_e2e.py`: one `Client` per person, no followed redirects, `check()` lines, exit 1 on any FAIL.

1. **Setup:**
   - the admin creates users A and B (as `groups_e2e.py` does);
   - A creates a group and uploads **230** generated PNGs of distinct flat colours (pure stdlib `zlib` + `struct` PNG writer, 8×8, so they are never near-copies), with alternating names;
   - B joins and uploads 20;
   - `--seed-only` stops here (T2 uses it).
2. **Batching:**
   - `/g` as B renders exactly 100 tiles, `data-total="250"`, a non-empty `data-next`;
   - `/gl` walks the rest in 2 more batches, and the union is 250 unique ids in shooting order.
3. **Server/template parity:** for the 100 rendered tiles, each attribute equals the matching `/gl?limit=100` JSON field.
4. **Filters:**
   - `person=<A>` gives Total 230;
   - `hide=1` for B gives Total 230 (own uploads count as had, D12);
   - B downloads 5 of A's items through `/gd`, after which `hide=1` gives 225;
   - the cursor walk under `hide=1` with those downloads made between batch 1 and batch 2 skips nothing.
5. **Select all and zip:**
   - POST `/gz` with all 250 ids → a zip with 250 entries, byte-equal to the originals for a sample of 10;
   - `limit=all&person=<A>` lists 230 ids.
6. **Sort and search:**
   - `sort=name&dir=asc` is ordered by lower-cased name;
   - `q=` with a distinctive name substring returns exactly the matching names.
7. **Feed access:** a user in no group gets 404 on `/gl`; a POST to `/gl` gets 405.
8. **View as…:**
   1. The admin POSTs `/viewas/start {userId: B}`.
   2. `/groups` shows B's group, and `/g?id=` is 200.
   3. `/gd`, `/gz`, POST `/groups/leave`, POST `/collections/create`, `/auth/token` and `/uploadChunk` all give 403.
   4. `/p/thumb` of a group file is 200.
   5. GET `/c` of A's collection as B leaves B's "Shared with me" unchanged (checked after Exit, as B).
   6. POST `/viewas/end`.
   7. `/logs` contains the start line and the end line `: exit`.
9. Run all three scripts against a fresh throwaway container. **Expected:** `groups_e2e.py` 91/91, `collections_e2e.py` 56/56, `part3_e2e.py` all PASS.

   Fix the existing scripts only where Part 3 legitimately changed a page, and record each change in the Rulings.

**Throwaway container:**

```bash
docker volume create p3e2e_data && docker volume create p3e2e_config
docker run -d --name p3e2e -e GOKAPI_PORT=53900 -p 127.0.0.1:53900:53900 \
  -v p3e2e_data:/app/data -v p3e2e_config:/app/config gokapi-fgs:part3-test
python3 build/e2e/setup_local.py && docker restart p3e2e
python3 build/e2e/groups_e2e.py && python3 build/e2e/collections_e2e.py && python3 build/e2e/part3_e2e.py
docker rm -f p3e2e && docker volume rm p3e2e_data p3e2e_config
```

**Playwright walkthrough** (desktop, then a 390×844 iPhone viewport), on the seeded throwaway container:
1. Group page Board: scroll to the end; the count reads 250; nothing jumps.
2. List: sort by Name, then Size; search.
3. Person filter + Select all + zip.
4. "Select everything new to me".
5. The viewer runs past item 100.
6. Builder parity: for one id, `api.buildItem(json)` matches the server tile's attribute set exactly.
7. Files tab with My files / Everything.
8. A collection page as a visitor; Download all resolves every member (count the queued downloads with a stub).
9. Save to Photos with the stubbed share (T3 Step 2 again, end to end).
10. View as… (V1 Step 7 again).
11. No console errors.

---

## Controller: merges, review, preview, deploy

1. **Worktrees** for wave 1:

   ```bash
   for t in list viewas probe followups; do
     git -C ~/src/Gokapi worktree add ~/src/Gokapi-wt/p3-$t -b p3/$t feature/part3
   done
   ```

2. **Review** each task as it finishes (spec compliance, then code quality), and fix rounds until approved.
3. **Merge order** into `feature/part3`: P (then the early preview), T1, V1, F. Next T2, then T3. After each merge:
   - regenerate the minified assets once (`build/dev/gotest.sh` runs `go generate`);
   - `git add` any new min file (`viewas.min.js`, `photos_probe.min.js`, and the versioned media min);
   - commit `build: regenerate static assets after <task>`;
   - run the full suite.
4. **The early preview** (after P), for the probe; it is reused later for the full preview. The live volumes are only ever read:

   ```bash
   docker volume create gokapi_p3_data && docker volume create gokapi_p3_config
   docker run --rm -v gokapi_gokapi_data:/from:ro -v gokapi_p3_data:/to alpine cp -a /from/. /to/
   docker run --rm -v gokapi_gokapi_config:/from:ro -v gokapi_p3_config:/to alpine cp -a /from/. /to/
   ```

   - In the copy's `config.json`, set `Authentication.Method` to 0 and `ServerUrl` to `https://francos-mac-mini.tail2dc5de.ts.net:8443/`.
   - Start once with `/app/run.sh --deployment-password <pw>`, then recreate the container without the flag.
   - Create `anna` and `ben` through `/api/user/create` and `resetPassword`.
   - Copy the branding to `~/src/gokapi-p3-custom/`, never with a secret in it, and mount it.
   - Run on `127.0.0.1:53910` with the compose hardening:
     - `no-new-privileges`, `cap_drop ALL`, `pids_limit 200`, `mem_limit 1g`, `cpus 1.5`;
     - `TZ`;
     - the `GOKAPI_*` env.
   - `tailscale serve --bg --https=8443 http://127.0.0.1:53910`.
   - Send Franco the probe URL and its four steps.
5. **Set the constants** from Franco's log (T3's three lines) in one commit, `photos: measured batch size and video limit`. Then revert P (`git revert` of P's merge) so the probe never reaches the PR.
6. **E**, then the **whole-branch review** (superpowers:requesting-code-review) against `feature/collections`. Fix it, then run E again.
7. **Full preview:** rebuild as `gokapi-fgs:part3`, and recreate the preview container on the same copied volumes; recopy if the copy is stale. Run the Playwright walkthrough on the preview (desktop and 390×844), then hand it to Franco.
8. **Deploy, only after Franco's explicit yes:**
   1. Tag `gokapi-fgs:part3-<date>`.
   2. Back up `docker-compose.yml`, `custom/admin.js` and `custom/version.txt` as `.bak.<date>`.
   3. Stop `gokapi`. Back up the DB with a sidecar `sqlite3 .backup` into `~/apps/gokapi/backups/backup-pre-part3-<date>.sqlite`, and run `PRAGMA integrity_check` on the backup.
   4. In `custom/admin.js` add `viewas: "bi-eye"` to `icons` and `"view as…": "bi-eye"` to `iconsByLabel`, and bump `version.txt` (25 → 26). Copy only those two files; swap the image in compose; `docker compose up -d`.
   5. **Read-only smoke tests** on `https://share.franco.international/`:
      - `/files`, `/groups`, a group page;
      - `/gl?id=` (JSON, 100 items);
      - a `/c` link and `/cl`;
      - `/viewas` (renders, no start).

      Then check `log.txt` for `[warning]` lines.
   6. Open the PR `feature/part3` → `feature/collections`, and link it (`link_pull_request`).
   7. Remove the preview: the container, its volumes (`gokapi_p3_data`, `gokapi_p3_config`, which hold a copy of the live DB), the tailscale serve entry, `~/src/gokapi-p3-custom`, the throwaway e2e containers and volumes, the worktrees under `~/src/Gokapi-wt/p3-*`, and the Playwright files in `~/atlas/.playwright-mcp` from this session.
9. **Memory:** update `~/atlas/memory/gokapi-share-redesign.md`: Part 3 live, the rollback path, and what remains (Part 4, the photo-date test, the OAuth secret).

**Rollback after deploy:** restore `docker-compose.yml.bak.<date>` (image `gokapi-fgs:groups-2026-10-21`) and `custom/*.bak.<date>`, then run `docker compose up -d`. The `ViewAs` table is ignored by the old image. The DB backup is the one from step 8.3.

---

## Rulings during execution

(Filled in by the controller while executing: each decision taken where this plan was silent, why, and what it costs if wrong. The measured Save to Photos values and the deploy record go here too.)
