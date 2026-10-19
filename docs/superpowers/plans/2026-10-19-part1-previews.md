# Part 1 — Previews, Files tab, viewer, boards, zip: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make photos and videos previewable everywhere on share.franco.international. Thumbnails become file icons, there is a full-screen viewer, collection pages get a board layout, and "Download all" works on iPhone.

**Architecture:** One background worker in the Go server runs `vips` and `ffmpeg` as separate processes. It writes a thumbnail, a viewer-sized preview and the metadata for each file fingerprint (SHA1). Results go to `data/previews/` and a new `Previews` SQLite table. Every new route asks one function, `access.CanView`, whether the request may see a file. The front end is server-rendered Go templates plus small vanilla-JS modules. The board and the viewer are shared between the Files tab and the public collection page.

**Tech Stack:**
- Go 1.25 (module), built in `golang:1.26.2-alpine`.
- SQLite via the existing provider.
- `vips-tools` and `vips-heif` (libvips 8.17, verified on Alpine 3.23), plus `ffmpeg` (8.0, includes `ffprobe`).
- Bootstrap 5, jQuery and DataTables, as already used by the admin UI.
- Vanilla JS, no new frameworks.

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`. This plan is Part 1. Read the spec's "Ground rules" and "Part 1" before starting.

## Global Constraints

- **No Go toolchain on the host.** Every `go` command runs through `build/dev/gotest.sh` (Task 0), which uses `golang:1.26.2-alpine` with `vips-tools vips-heif ffmpeg exiftool` installed and the module cache in a named volume.
- Docker on the mini needs `DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock`. The default socket is Docker Desktop, which does not have the gokapi container.
- **Always pass `-tags=test,awsmock`** (the script does this). Without the tags Go prints `no test files` and exits 0, which looks like a pass. Every "run the test" step greps for its test's name with `-v` and checks for `--- PASS: <TestName>`.
- **Tests that need `vips` or `ffmpeg` must fail, not skip, when the tools are missing.**
- `go generate ./...` rewrites the hashed asset names in `string_constants.tmpl`, `api/VersionNumbers.go` and `js/min/*`. Commit those changes together with the change that caused them.
- **Protected routes** must be registered in exactly this form: `mux.HandleFunc("<path>", requireLogin(...))`. `build/go-generate/updateProtectedUrls.go` finds them with a regex.
- **Originals are never altered.** No download route ever serves a preview. Thumbnails and previews are produced only by the worker.
- **No `immutable` caching of anything whose fingerprint is not in the URL.** Preview URLs carry `&v=<first 12 hex chars of SHA1>`.
- **Viewing must never count as downloading.**
  - Preview and video-streaming routes never call `ServeFile`, `LogDownload` or `IncreaseDownloadCount`.
  - Only explicit downloads by a link holder count: `/cd`, `/downloadFile`, `/cz`.
  - The owner (or a `PERM_LIST` admin) fetching from the Files tab does **not** count (Ruling 18): `/files/download` and `/files/zip` serve uncounted, like the upload page's presigned download. They would otherwise spend the download budget of the very links being managed.
- **Worker errors go to stdout only, never the Status log.** Today's notifier (`~/apps/gokapi/gokapi-notify.py`) emails every `warning` line, and every unknown category, immediately. At most one summary log line is written per backfill run, using the existing `info` category, which the notifier ignores.
- **Timestamps are stored as UTC Unix seconds.**
- **New Go code goes in new files** wherever possible, so rebasing onto upstream stays cheap. Edits to existing upstream files stay small and are listed explicitly.
- Commits use the repo's normal author. Each commit message ends with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Push after every commit: `git push`. The branch is `feature/previews`.
- Copy and labels used in the UI:
  - "Download all", "Download as .zip", "Save to Photos", "Download original".
  - Help sheet title: "Downloaded to Files? Here's how to get them into Photos".
  - Files tab switch: "My files" / "Everything".
  - Collection toggle: "Board" / "List".

---

## File structure

**New Go files:**

| File | Responsibility |
|---|---|
| `internal/models/Preview.go` | `Preview` struct and status constants |
| `internal/configuration/database/provider/sqlite/previews.go` | SQLite CRUD for `Previews` |
| `internal/configuration/database/provider/redis/previews.go` | Redis CRUD for `Previews` (parity) |
| `internal/preview/Generate.go` | Pure generator: input path → outputs and metadata, via exec |
| `internal/preview/Generate_test.go` | Fixture-driven tests: HEIC, rotated JPEG, iPhone MOV, corrupt file |
| `internal/preview/Worker.go` | Queue, backfill, enqueue hooks, delete-by-SHA1 |
| `internal/preview/Worker_test.go` | Worker tests |
| `internal/webserver/access/Access.go` | `CanView` |
| `internal/webserver/access/Access_test.go` | Grant and deny matrix |
| `internal/webserver/Previews.go` | `/p/thumb`, `/p/view`, `/p/poster`, `/p/video` |
| `internal/webserver/Zip.go` | `/cz` (collection zip) and `/files/zip` (multi-select zip) |
| `internal/storage/Zip.go` | `ServeZip`: filters, counts, then calls the existing `ServeFilesAsZip` |
| `internal/webserver/Files.go` | `/files` page and view model |

**New front-end files:**

| File | Responsibility |
|---|---|
| `internal/webserver/web/templates/html_files.tmpl` | Files tab (grid and list) |
| `internal/webserver/web/templates/html_media_shared.tmpl` | Shared partials: help sheet, viewer shell, device buttons |
| `internal/webserver/web/static/js/media.js` | Shared front end: device detection, viewer, board, zip and Photos buttons. Minified to `js/min/media.min.<hash>.js` |
| `internal/webserver/web/static/css/media.css` | Board, grid, viewer and help-sheet styles. Bundled into `gokapi.min.<hash>.css` automatically |
| `build/dev/gotest.sh` | Runs go generate and go test in the Alpine toolchain container |

**Existing files modified (kept small):**

- `Dockerfile`: runtime `apk add`.
- `cmd/gokapi/Main.go`: start the worker.
- `internal/models/FileList.go`: no change.
- `internal/configuration/database/dbabstraction/DbAbstraction.go`, `internal/configuration/database/Database.go`: interface and facade.
- `internal/configuration/database/provider/sqlite/Sqlite.go`: schema 18.
- `internal/storage/FileServing.go`:
  - hook in `CleanUp`;
  - hook in `ReplaceFile`;
  - change one line in `ServeFilesAsZip` so it uses `ZipEntryTime`.
- `internal/webserver/api/Api.go`, `internal/webserver/fileupload/FileUpload.go`: enqueue after upload.
- `internal/webserver/Webserver.go`:
  - routes;
  - `ViewFiles`;
  - `DownloadView.PreviewUrl`.
- `internal/webserver/Collections.go`: view fields, collection-password TTL, zip and board.
- `internal/webserver/authentication/downloadPasswordToken/DownloadPasswordToken.go`: `GenerateWithTtl`.
- Templates:
  - `html_header.tmpl`: Files nav link and `media.min.js`.
  - `html_collection.tmpl`, `html_download.tmpl`, `html_admin.tmpl`.
  - `html_collections_admin.tmpl`: logout fix.
- `internal/webserver/web/static/js/admin_ui_upload.js`: the auto-collection default.
- `build/go-generate/minifyStaticContent.go`: add the `media.js` entry.
- Deployment, outside the repo: `~/apps/gokapi/custom/admin.js` (sidebar icon) and `version.txt`.

---

### Task 0: Toolchain container and runtime image

**Files:**
- Create: `build/dev/gotest.sh`
- Modify: `Dockerfile` (runtime stage `apk add` line)

**Interfaces:**
- Produces: `build/dev/gotest.sh [go test args…]`. It runs `go generate ./...` and then `go test -tags=test,awsmock -count=1 <args>` in the container. With no args it runs `./...`.

- [ ] **Step 1: Create the branch.**

```bash
cd ~/src/Gokapi && git fetch -q && git checkout -b feature/previews design/group-sharing
```

- [ ] **Step 2: Write `build/dev/gotest.sh`.**

```sh
#!/bin/sh
# Runs go generate + go test inside the same Alpine toolchain the Dockerfile
# builds with, plus the preview tools. There is no Go on the mini itself.
set -eu
cd "$(dirname "$0")/../.."
export DOCKER_HOST="${DOCKER_HOST:-unix:///Users/franco/.colima/default/docker.sock}"
if [ "$#" -eq 0 ]; then set -- ./...; fi
exec docker run --rm \
  -v "$PWD":/src -w /src \
  -v gokapi-gomod:/go/pkg/mod -v gokapi-gocache:/root/.cache/go-build \
  golang:1.26.2-alpine sh -c '
    apk add -q --no-cache git vips-tools vips-heif ffmpeg exiftool >/dev/null &&
    go generate ./... &&
    go test -tags=test,awsmock -count=1 -parallel 8 "$@"' sh "$@"
```

Then: `chmod +x build/dev/gotest.sh`

- [ ] **Step 3: Get a baseline.** Run: `build/dev/gotest.sh ./internal/storage/... 2>&1 | tail -5`

Expected: `ok  github.com/forceu/gokapi/internal/storage`. If it fails before any change, stop and report: the baseline is broken.

- [ ] **Step 4: Add the tools to the runtime image.** In `Dockerfile`, runtime stage, change:

```
RUN apk update && apk add --no-cache su-exec tini ca-certificates curl tzdata
```

to:

```
RUN apk update && apk add --no-cache su-exec tini ca-certificates curl tzdata vips-tools vips-heif ffmpeg
```

- [ ] **Step 5: Build the image and smoke-test the tools.**

```bash
export DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock
docker build -q -t gokapi-fgs:previews-dev . && \
docker run --rm --entrypoint sh gokapi-fgs:previews-dev -c 'vips --version; vips -l foreign | grep -c heif; ffprobe -version | head -1'
```

Expected:
- `vips-8.17.x`;
- a count of 1 or more;
- `ffprobe version 8.x`.

- [ ] **Step 6: Commit.**

```bash
git add build/dev/gotest.sh Dockerfile
git commit -m "build: preview tools in the runtime image, containerised test runner"
git push -u origin feature/previews
```

---

### Task 1: Zip for collections, device-specific buttons, phone help sheet

This task is the deployable checkpoint. It fixes the live iPhone "Download all" bug before any preview work exists.

**Files:**
- Create: `internal/storage/Zip.go`, `internal/storage/Zip_test.go`, `internal/webserver/Zip.go`, `internal/webserver/web/templates/html_media_shared.tmpl`, `internal/webserver/web/static/js/media.js`, `internal/webserver/web/static/css/media.css`
- Modify:
  - `internal/storage/FileServing.go`: in `ServeFilesAsZip`, `Modified:` → `ZipEntryTime(file)`.
  - `internal/webserver/Webserver.go`: route `/cz`.
  - `internal/webserver/Collections.go`: nothing in this task.
  - `internal/webserver/web/templates/html_collection.tmpl`: buttons and help.
  - `internal/webserver/web/templates/html_header.tmpl`: load `media.min.js` on public and admin pages.
  - `build/go-generate/minifyStaticContent.go`: the `media.js` minify entry.

**Interfaces:**
- Produces:
  - `storage.ServeZip(files []models.File, name string, w http.ResponseWriter, r *http.Request) int`. It returns how many files were included.
  - `storage.ZipEntryTime(file models.File) time.Time`. It is a package-level `var` holding a function, so Task 3 can swap in taken-at.
  - Route `GET /cz?id=<collectionId>`, public. It honours the same `pc<id>` cookie as `/cd`.
  - In `media.js`:
    - `window.FGS.isIOS()` (`bool`);
    - `window.FGS.initDownloadButtons(rootEl)`;
    - `window.FGS.openHelpSheet()`.

- [ ] **Step 1: Write the failing test for `ServeZip`.** It must skip expired files, skip file-request files, decrement limited files and keep originals byte-identical.

`internal/storage/Zip_test.go`:

```go
//go:build test

package storage

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func zipTestFile(t *testing.T, id, content string, mutate func(*models.File)) models.File {
	t.Helper()
	sha := "ziptest" + id
	err := os.WriteFile(configuration.Get().DataDir+"/"+sha, []byte(content), 0600)
	test.IsNil(t, err)
	f := models.File{Id: id, Name: id + ".jpg", SHA1: sha, SizeBytes: int64(len(content)),
		Size: "1 B", UploadDate: 1755181925, ExpireAt: time.Now().Add(time.Hour).Unix(),
		UnlimitedDownloads: true, UnlimitedTime: false}
	if mutate != nil {
		mutate(&f)
	}
	database.SaveMetaData(f)
	return f
}

func TestServeZipFiltersAndCounts(t *testing.T) {
	ok := zipTestFile(t, "zipok00000000000001", "ORIGINAL-BYTES", nil)
	limited := zipTestFile(t, "ziplim0000000000001", "LIMITED", func(f *models.File) {
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 1
	})
	expired := zipTestFile(t, "zipexp0000000000001", "EXPIRED", func(f *models.File) {
		f.ExpireAt = time.Now().Add(-time.Hour).Unix()
	})
	request := zipTestFile(t, "zipreq0000000000001", "REQUEST", func(f *models.File) {
		f.UploadRequestId = "somerequest"
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/cz", nil)
	n := ServeZip([]models.File{ok, limited, expired, request}, "trip", w, r)
	test.IsEqualInt(t, n, 2)

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	test.IsNil(t, err)
	test.IsEqualInt(t, len(zr.File), 2)
	rc, _ := zr.File[0].Open()
	got, _ := io.ReadAll(rc)
	test.IsEqualString(t, string(got), "ORIGINAL-BYTES")
	test.IsEqualInt(t, int(zr.File[0].Method), int(zip.Store))
	test.IsEqualInt(t, database.GetDownloadsRemaining(limited.Id), 0)

	// A second zip no longer includes the exhausted file
	w2 := httptest.NewRecorder()
	test.IsEqualInt(t, ServeZip([]models.File{limited}, "trip", w2, r), 0)
}
```

Check the existing storage test setup before relying on it. `TestMain` in `internal/storage/FileServing_test.go` creates the test configuration, and `configuration.Get().DataDir` is `test/data`. If `TestMain` does something else, adapt only the setup lines.

- [ ] **Step 2: Run it.** `build/dev/gotest.sh ./internal/storage/ -run TestServeZipFiltersAndCounts -v 2>&1 | grep -E "TestServeZip|undefined"`

Expected: a compile failure, `undefined: ServeZip`.

- [ ] **Step 3: Implement `internal/storage/Zip.go`.**

```go
package storage

import (
	"net/http"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper/apimutex"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/sse"
)

// ZipEntryTime is the modified time stamped on each zip entry. It is a variable
// so the preview package can substitute the capture date once it is known,
// without storage importing preview. Default: the upload date.
var ZipEntryTime = func(file models.File) time.Time {
	return time.Unix(file.UploadDate, 0)
}

// ServeZip streams the downloadable subset of files as a store-only zip and
// returns how many were included. Each file is re-resolved, so expired,
// exhausted, file-request and end-to-end encrypted files are skipped, and
// limited files are counted exactly as a single download through /cd would be.
func ServeZip(files []models.File, name string, w http.ResponseWriter, r *http.Request) int {
	included := make([]models.File, 0, len(files))
	now := time.Now().Unix()
	for _, f := range files {
		current, ok := GetFile(f.Id)
		if !ok || current.UploadRequestId != "" || current.Encryption.IsEndToEndEncrypted {
			continue
		}
		apimutex.Lock(apimutex.TypeMetaData, current.Id)
		if !current.UnlimitedDownloads {
			current.DownloadsRemaining = database.GetDownloadsRemaining(current.Id)
		}
		if IsExpiredFile(current, now) {
			apimutex.Unlock(apimutex.TypeMetaData, current.Id)
			continue
		}
		current.DownloadsRemaining--
		current.DownloadCount++
		database.IncreaseDownloadCount(current.Id, !current.UnlimitedDownloads)
		apimutex.Unlock(apimutex.TypeMetaData, current.Id)
		go sse.PublishDownloadCount(current)
		included = append(included, current)
	}
	if len(included) == 0 {
		http.Error(w, "Nothing left to download", http.StatusGone)
		return 0
	}
	ServeFilesAsZip(included, name, w, r)
	return len(included)
}
```

Check the import paths for `apimutex` and `sse` against the imports already in `FileServing.go`, and use exactly the same ones. In `FileServing.go`'s `ServeFilesAsZip`, change only this line:

```go
			Modified: time.Unix(file.UploadDate, 0),
```

to:

```go
			Modified: ZipEntryTime(file),
```

- [ ] **Step 4: Run the test.** It is the same command as Step 2.

Expected: `--- PASS: TestServeZipFiltersAndCounts`.

- [ ] **Step 5: Add the `/cz` handler.**

`internal/webserver/Zip.go`:

```go
package webserver

import (
	"net/http"

	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/errorHandling"
)

// downloadCollectionZip serves every live member of a collection as one zip.
// Same gate as /cd: a password-protected collection needs its pc cookie.
func downloadCollectionZip(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	collection, ok := storage.GetCollection(id)
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	if collection.IsPasswordProtected() && !isValidCollectionPwCookie(r, collection) {
		redirect(w, "c?id="+collection.Id)
		return
	}
	storage.ServeZip(collection.Files, collection.Name, w, r)
}
```

Check that `errorHandling.TypeFileNotFound` exists, and use whatever `showCollection` uses for a missing collection. In `Webserver.go`, after `mux.HandleFunc("/cd", downloadCollectionFile)`, add:

```go
	mux.HandleFunc("/cz", downloadCollectionZip)
```

- [ ] **Step 6: Write a failing webserver test for `/cz`.** Add it to `internal/webserver/Webserver_test.go`, or create `internal/webserver/Zip_test.go` with `//go:build test`.

```go
func TestCollectionZipRequiresPassword(t *testing.T) {
	// Password-protected collection without cookie redirects to the password page
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/cz?id=" + createTestCollection(t, "secret"),
		IsHtml:          true,
		RequiredContent: []string{"Password"},
	})
}
```

Write `createTestCollection(t *testing.T, password string) string` in the same file. It creates a collection via `storage.CreateCollection(storage.CollectionParameters{Name: "t", FileIds: []string{<an id from testconfiguration with no password and not e2e>}, Password: password}, models.User{Id: 5})` and returns the ID. Pick the file ID by reading `internal/test/testconfiguration/TestConfiguration.go`: choose one with `PasswordHash == ""` and no encryption.

Run: `build/dev/gotest.sh ./internal/webserver/ -run TestCollectionZipRequiresPassword -v 2>&1 | grep -E "^(---|ok|FAIL)"`

Expected: PASS once Step 5 is in. If the redirect is not followed, adjust `RequiredContent` to the redirect target instead.

- [ ] **Step 7: Create `media.js` with device detection, download buttons and the help sheet.**

`internal/webserver/web/static/js/media.js`:

```js
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
      document.getElementById("fgs-help-sheet").classList.remove("open");
    }
  });
})();
```

- [ ] **Step 8: Create the shared partials.**

`internal/webserver/web/templates/html_media_shared.tmpl`:

```
{{ define "media_help_sheet" }}
<div id="fgs-help-sheet" class="fgs-sheet" role="dialog" aria-modal="true" aria-labelledby="fgs-help-title">
  <div class="fgs-sheet-card">
    <h5 id="fgs-help-title">Downloaded to Files? Here's how to get them into Photos</h5>
    <ol>
      <li>Open the <b>Files</b> app and go to <b>Downloads</b>.</li>
      <li>If it's a zip, tap it once to unzip it into a folder, then open the folder.</li>
      <li>Tap <b>⋯</b> → <b>Select</b>, then select the photos and videos you want.</li>
      <li>Tap <b>Share</b> → <b>Save N Images</b> (or <b>Save N Videos</b>).</li>
    </ol>
    <p class="small text-muted mb-3">They keep their original date and time, so they land in the right place in your Photos timeline.</p>
    <button type="button" class="btn btn-light w-100" data-fgs-help-close>Got it</button>
  </div>
</div>
{{ end }}

{{ define "media_download_buttons" }}
<div class="text-center mt-4" data-fgs-downloads>
  <button id="downloadAllButton" class="btn btn-light" type="button" data-fgs-download-all>
    Download all {{ .Count }} files
  </button>
  <a class="btn btn-outline-light ms-2" href="{{ .ZipUrl }}" data-fgs-download-zip>Download as .zip</a>
  <button type="button" class="btn btn-link text-muted d-none" data-fgs-help aria-label="How to save to Photos">
    <i class="bi bi-question-circle"></i>
  </button>
  <p id="downloadAllStatus" class="text-muted small mt-2 mb-0"></p>
</div>
{{ end }}
```

`.Count` and `.ZipUrl` come from a small struct. In `Collections.go` add:

```go
type mediaButtons struct {
	Count  int
	ZipUrl string
}
```

Add a field `Buttons mediaButtons` to `CollectionView`, and fill it in `showCollection` where `FileCount` is set:

```go
view.Buttons = mediaButtons{Count: len(collection.Files), ZipUrl: "./cz?id=" + collection.Id}
```

- [ ] **Step 9: Use the partials in `html_collection.tmpl`.** Replace the existing block:

```
            <div class="text-center mt-4">
                <button id="downloadAllButton" ...>
                    Download all {{ .FileCount }} files
                </button>
                <p id="downloadAllStatus" class="text-muted small mt-2 mb-0"></p>
            </div>
```

with:

```
            {{ template "media_download_buttons" .Buttons }}
            {{ template "media_help_sheet" }}
```

Keep the existing inline "Download all" script as it is. It finds `#downloadAllButton` and `#downloadAllStatus`, and both IDs are preserved. Before `{{ template "footer" }}` add:

```
<script src="./js/min/media.min.{{ template "js_media_version" }}.js"></script>
<script>FGS.initDownloadButtons(document.querySelector("[data-fgs-downloads]"));</script>
```

- [ ] **Step 10: Wire up minification and the version constant.** In `build/go-generate/minifyStaticContent.go` `getPaths()`, add an entry modelled exactly on the existing `all_public.js` entry. The differences:
  - source `js/media.js`;
  - output `js/min/media.min.<jsMediaVersion>.js`.

In `build/go-generate/updateVersionNumbers.go`, add `js_media_version` next to `js_admin_version`, following the same pattern: the template define in `string_constants.tmpl` plus the Go constant. Paste the relevant snippets from both files into your report so the reviewer can check they mirror the existing entries.

`media.css` is picked up automatically by the `css/*.css` → `gokapi.min.*.css` bundle. Add:

```css
.fgs-sheet{position:fixed;inset:0;display:none;align-items:flex-end;justify-content:center;background:rgba(0,0,0,.55);z-index:2000}
.fgs-sheet.open{display:flex}
.fgs-sheet-card{background:#1c1c1a;color:#f2f1ee;border:1px solid #2e2e2b;border-radius:14px 14px 0 0;max-width:32rem;width:100%;padding:1.5rem}
.fgs-sheet-card ol{padding-left:1.2rem}
```

- [ ] **Step 11: Generate, test and check the page.**

```bash
build/dev/gotest.sh ./internal/webserver/ ./internal/storage/ 2>&1 | tail -3
```

Expected: both packages `ok`, and `git status` shows regenerated `string_constants.tmpl` and `js/min/media.min.*.js`.

Add one assertion to the `/c` page test, or create one in `Zip_test.go`: GET `/c?id=<unprotected test collection>` must contain `Download as .zip`, `./cz?id=` and `fgs-help-sheet`.

- [ ] **Step 12: Commit.**

```bash
git add -A internal build
git commit -m "collections: zip download and iPhone-first buttons

iPhone Safari allows one download per tap, so 'Download all' delivered
only the first file. The server streams a store-only zip instead; on iOS
it is the primary button, with a help sheet for getting the files from
Files into Photos. Desktop keeps one-by-one downloads."
git push
```

---

### Task 2: `Previews` table and model

**Files:**
- Create: `internal/models/Preview.go`, `internal/configuration/database/provider/sqlite/previews.go`, `internal/configuration/database/provider/sqlite/previews_test.go`, `internal/configuration/database/provider/redis/previews.go`
- Modify: `dbabstraction/DbAbstraction.go` (interface), `database/Database.go` (facade), `provider/sqlite/Sqlite.go` (version 18, `Upgrade`, `createNewDatabase`)

**Interfaces:**
- Produces:
  - `models.Preview{Sha1 string; Status string; Kind string; Width, Height int; TakenAt int64; DurationMs int64; Color string; PHash uint64; Attempts int; Updated int64}`
  - Constants `models.PreviewPending = "pending"`, `PreviewReady = "ready"`, `PreviewFailed = "failed"`, `PreviewUnsupported = "unsupported"`
  - Constants `models.PreviewKindImage = "image"`, `PreviewKindVideo = "video"`
  - `func (p Preview) Version() string`, which returns `p.Sha1[:12]` (or the whole string if shorter)
  - `database.GetPreview(sha1 string) (models.Preview, bool)`
  - `database.SavePreview(p models.Preview)`
  - `database.DeletePreview(sha1 string)`
  - `database.GetAllPreviewSha1s() map[string]string`, mapping SHA1 to status

- [ ] **Step 1: Write the model.**

```go
package models

// Preview holds what the preview worker learned about one stored blob. It is
// keyed by SHA1 rather than file ID, because identical uploads share storage
// and therefore share their preview.
type Preview struct {
	Sha1       string `json:"sha1" redis:"sha1"`
	Status     string `json:"status" redis:"status"`
	Kind       string `json:"kind" redis:"kind"`
	Width      int    `json:"width" redis:"width"`   // after rotation
	Height     int    `json:"height" redis:"height"` // after rotation
	TakenAt    int64  `json:"takenAt" redis:"takenat"` // UTC unix seconds, 0 if unknown
	DurationMs int64  `json:"durationMs" redis:"durationms"`
	Color      string `json:"color" redis:"color"` // "#rrggbb" average colour
	PHash      uint64 `json:"-" redis:"phash"`     // 64-bit dHash, used by Part 2
	Attempts   int    `json:"-" redis:"attempts"`
	Updated    int64  `json:"-" redis:"updated"`
}

const (
	PreviewPending     = "pending"
	PreviewReady       = "ready"
	PreviewFailed      = "failed"
	PreviewUnsupported = "unsupported"
	PreviewKindImage   = "image"
	PreviewKindVideo   = "video"
)

// Version is the cache-busting token placed in preview URLs.
func (p Preview) Version() string {
	if len(p.Sha1) < 12 {
		return p.Sha1
	}
	return p.Sha1[:12]
}
```

- [ ] **Step 2: Write the failing SQLite test** `provider/sqlite/previews_test.go`. Model it on the existing `Sqlite_test.go` setup: use the same helper the other tests in that file use to open a fresh provider.

```go
func TestPreviewsRoundTrip(t *testing.T) {
	p := models.Preview{Sha1: "abc123def4567890", Status: models.PreviewReady, Kind: models.PreviewKindImage,
		Width: 640, Height: 853, TakenAt: 1755174725, Color: "#787878", PHash: 0xF0F0F0F0F0F0F0F0, Updated: 1}
	dbInstance.SavePreview(p)
	got, ok := dbInstance.GetPreview("abc123def4567890")
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, got.Height, 853)
	test.IsEqualBool(t, got.PHash == p.PHash, true)
	test.IsEqualString(t, dbInstance.GetAllPreviewSha1s()["abc123def4567890"], models.PreviewReady)
	dbInstance.DeletePreview("abc123def4567890")
	_, ok = dbInstance.GetPreview("abc123def4567890")
	test.IsEqualBool(t, ok, false)
}
```

Use whatever variable name the existing sqlite tests use for the provider. Read `Sqlite_test.go` first, and use it in place of `dbInstance` if it differs.

- [ ] **Step 3: Run it.** `build/dev/gotest.sh ./internal/configuration/database/provider/sqlite/ -run TestPreviewsRoundTrip -v 2>&1 | grep -E "Preview|undefined"`

Expected: `undefined`.

- [ ] **Step 4: Implement the schema.** In `Sqlite.go`:
  - set `const DatabaseSchemeVersion = 18`;
  - add this block in `Upgrade` after the `< 17` block;
  - append the same `CREATE TABLE` to the multi-statement string in `createNewDatabase()`.

```go
	// < v2.3.0-fgs: preview metadata, keyed by blob hash
	if currentDbVersion < 18 {
		err := p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Previews" (
			"Sha1"	TEXT NOT NULL UNIQUE,
			"Status"	TEXT NOT NULL,
			"Kind"	TEXT NOT NULL DEFAULT '',
			"Width"	INTEGER NOT NULL DEFAULT 0,
			"Height"	INTEGER NOT NULL DEFAULT 0,
			"TakenAt"	INTEGER NOT NULL DEFAULT 0,
			"DurationMs"	INTEGER NOT NULL DEFAULT 0,
			"Color"	TEXT NOT NULL DEFAULT '',
			"PHash"	INTEGER NOT NULL DEFAULT 0,
			"Attempts"	INTEGER NOT NULL DEFAULT 0,
			"Updated"	INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY("Sha1")
		) WITHOUT ROWID;`)
		helper.Check(err)
	}
```

`PHash` is stored as `int64(uint64)` and read back with the same cast.

`provider/sqlite/previews.go`:

```go
package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

const previewColumns = `Sha1, Status, Kind, Width, Height, TakenAt, DurationMs, Color, PHash, Attempts, Updated`

func scanPreview(scan func(dest ...any) error) (models.Preview, error) {
	var p models.Preview
	var phash int64
	err := scan(&p.Sha1, &p.Status, &p.Kind, &p.Width, &p.Height, &p.TakenAt, &p.DurationMs, &p.Color, &phash, &p.Attempts, &p.Updated)
	p.PHash = uint64(phash)
	return p, err
}

// GetPreview returns the preview row for a blob hash
func (p DatabaseProvider) GetPreview(sha1 string) (models.Preview, bool) {
	row := p.sqliteDb.QueryRow("SELECT "+previewColumns+" FROM Previews WHERE Sha1 = ?", sha1)
	result, err := scanPreview(row.Scan)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			helper.Check(err)
		}
		return models.Preview{}, false
	}
	return result, true
}

// SavePreview inserts or replaces a preview row
func (p DatabaseProvider) SavePreview(pr models.Preview) {
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO Previews (`+previewColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pr.Sha1, pr.Status, pr.Kind, pr.Width, pr.Height, pr.TakenAt, pr.DurationMs, pr.Color, int64(pr.PHash), pr.Attempts, pr.Updated)
	helper.Check(err)
}

// DeletePreview removes a preview row
func (p DatabaseProvider) DeletePreview(sha1 string) {
	_, err := p.sqliteDb.Exec("DELETE FROM Previews WHERE Sha1 = ?", sha1)
	helper.Check(err)
}

// GetAllPreviewSha1s maps every known blob hash to its preview status
func (p DatabaseProvider) GetAllPreviewSha1s() map[string]string {
	result := make(map[string]string)
	rows, err := p.sqliteDb.Query("SELECT Sha1, Status FROM Previews")
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var sha, status string
		helper.Check(rows.Scan(&sha, &status))
		result[sha] = status
	}
	return result
}
```

Add the four method signatures to the `Database` interface in `DbAbstraction.go`, next to the collections block. Add the facades to `Database.go`:

```go
// GetPreview returns the preview metadata for a blob hash
func GetPreview(sha1 string) (models.Preview, bool) { return db.GetPreview(sha1) }

// SavePreview stores preview metadata
func SavePreview(p models.Preview) { db.SavePreview(p) }

// DeletePreview removes preview metadata
func DeletePreview(sha1 string) { db.DeletePreview(sha1) }

// GetAllPreviewSha1s maps blob hashes to preview status
func GetAllPreviewSha1s() map[string]string { return db.GetAllPreviewSha1s() }
```

Implement the Redis version in `provider/redis/previews.go`, following `provider/redis/collections.go` exactly:
- prefix `"prev:"`;
- `getHashMap`, `getAllHashesWithPrefix`, `buildArgs(...).AddFlat(pr)` + `setHashMap` and `deleteKey`;
- `PHash` stored through its `redis:"phash"` tag.

Previews are regenerable, so `database.Migrate` intentionally does not copy them. Add a one-line comment saying so next to the collections note in `Migrate`.

- [ ] **Step 5: Run the sqlite and redis provider tests.**

```bash
build/dev/gotest.sh ./internal/configuration/database/... -v 2>&1 | grep -E "^(--- FAIL|ok|FAIL)|TestPreviewsRoundTrip"
```

Expected: `--- PASS: TestPreviewsRoundTrip`, all packages `ok`, and the existing scheme-version assertions still pass.

- [ ] **Step 6: Commit.**

```bash
git add -A internal
git commit -m "database: Previews table (schema 18), keyed by blob hash"
git push
```

---

### Task 3: Preview generator

A pure function: input file path in, output files and metadata out. It has no database and no queue.

**Files:**
- Create: `internal/preview/Generate.go`, `internal/preview/Generate_test.go`

**Interfaces:**
- Consumes: `models.Preview` and its constants (Task 2).
- Produces:
  - `preview.Kind(contentType, name string) string`, which returns `models.PreviewKindImage`, `models.PreviewKindVideo` or `""`.
  - `preview.Generate(ctx context.Context, src, outDir, sha1, kind string) (models.Preview, error)`. It writes `outDir/<sha1>-thumb.webp` and `outDir/<sha1>-view.jpg`, plus `outDir/<sha1>-poster.webp` for video. It returns a preview with `Status=ready`, or an error.
  - `preview.Paths(dataDir, sha1 string) (thumb, view, poster string)`.

**Decisions fixed here, per review:**
- Decode once. Make the ~2000 px rotated view first, then derive the thumbnail and a 9×8 PNG from it.
- Width and height come from the **generated view**, which is already rotated. Never swap on the EXIF orientation tag, because HEIC rotation is in the container.
- Taken-at for photos: EXIF `DateTimeOriginal`.
  - With `OffsetTimeOriginal`, use that offset.
  - Without one, interpret it in the server's local zone (`time.Local`, which is Europe/Zurich from `TZ`).
  - Convert to UTC.
- Taken-at for videos: prefer the ffprobe format tag `com.apple.quicktime.creationdate`, which carries an offset. Fall back to `creation_time`, which is UTC.
- Every exec gets a `context.WithTimeout` of 60 s for images and 120 s for videos, plus `VIPS_CONCURRENCY=1`, and `-threads 1` for ffmpeg. These stay inside `pids_limit: 200`.

- [ ] **Step 1: Write the failing tests with generated fixtures.**

`internal/preview/Generate_test.go`:

```go
//go:build test

package preview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// Fixtures are generated with the same tools the server uses, so nobody's
// real photos are committed. Missing tools are a hard failure, not a skip:
// a green run without vips would prove nothing.
func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func fixtures(t *testing.T) string {
	dir := t.TempDir()
	run(t, "vips", "black", filepath.Join(dir, "b.v"), "400", "300", "--bands", "3")
	run(t, "vips", "linear", filepath.Join(dir, "b.v"), filepath.Join(dir, "rot.jpg"), "1", "120")
	// Landscape pixels, orientation 6 => displays portrait 300x400
	run(t, "exiftool", "-q", "-overwrite_original", "-Orientation#=6",
		"-DateTimeOriginal=2026:08:14 14:32:05", "-OffsetTimeOriginal=+02:00", filepath.Join(dir, "rot.jpg"))
	run(t, "vips", "copy", filepath.Join(dir, "rot.jpg"), filepath.Join(dir, "photo.heic[compression=hevc]"))
	run(t, "exiftool", "-q", "-overwrite_original", "-DateTimeOriginal=2026:08:14 14:32:05",
		"-OffsetTimeOriginal=+02:00", filepath.Join(dir, "photo.heic"))
	run(t, "ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=10",
		"-metadata", "com.apple.quicktime.creationdate=2026-08-14T14:33:00+0200",
		"-movflags", "use_metadata_tags", filepath.Join(dir, "clip.mov"))
	test.IsNil(t, os.WriteFile(filepath.Join(dir, "corrupt.jpg"), []byte("not an image at all"), 0600))
	return dir
}

func TestGenerate(t *testing.T) {
	dir := fixtures(t)
	out := t.TempDir()
	ctx := context.Background()
	want := time.Date(2026, 8, 14, 12, 32, 5, 0, time.UTC).Unix()

	jpg, err := Generate(ctx, filepath.Join(dir, "rot.jpg"), out, "sharot", models.PreviewKindImage)
	test.IsNil(t, err)
	test.IsEqualString(t, jpg.Status, models.PreviewReady)
	test.IsEqualBool(t, jpg.Height > jpg.Width, true) // rotated to portrait
	test.IsEqualInt(t, int(jpg.TakenAt), int(want))
	test.IsEqualBool(t, len(jpg.Color) == 7, true)
	thumb, view, _ := Paths(out, "sharot")
	test.FileExists(t, thumb)
	test.FileExists(t, view)

	heic, err := Generate(ctx, filepath.Join(dir, "photo.heic"), out, "shaheic", models.PreviewKindImage)
	test.IsNil(t, err)
	test.IsEqualBool(t, heic.Height > heic.Width, true) // not double-rotated
	test.IsEqualInt(t, int(heic.TakenAt), int(want))

	mov, err := Generate(ctx, filepath.Join(dir, "clip.mov"), out, "shamov", models.PreviewKindVideo)
	test.IsNil(t, err)
	test.IsEqualInt(t, int(mov.TakenAt), int(time.Date(2026, 8, 14, 12, 33, 0, 0, time.UTC).Unix()))
	test.IsEqualInt(t, int(mov.DurationMs), 2000)
	test.IsEqualInt(t, mov.Width, 320)
	_, _, poster := Paths(out, "shamov")
	test.FileExists(t, poster)

	_, err = Generate(ctx, filepath.Join(dir, "corrupt.jpg"), out, "shabad", models.PreviewKindImage)
	test.IsNotNil(t, err)
}

func TestKind(t *testing.T) {
	test.IsEqualString(t, Kind("image/heic", "IMG_1.HEIC"), models.PreviewKindImage)
	test.IsEqualString(t, Kind("application/octet-stream", "IMG_1.HEIC"), models.PreviewKindImage)
	test.IsEqualString(t, Kind("video/quicktime", "IMG_2.MOV"), models.PreviewKindVideo)
	test.IsEqualString(t, Kind("image/svg+xml", "x.svg"), "")
	test.IsEqualString(t, Kind("application/pdf", "x.pdf"), "")
}
```

If `test.FileExists`, `test.IsNotNil` or `test.IsEqualBool` do not exist in `internal/test/TestHelper.go`, use the nearest existing helper, or `if err == nil { t.Fatal(...) }`. Do not add helpers to the shared package for this.

- [ ] **Step 2: Run it.** `build/dev/gotest.sh ./internal/preview/ -v 2>&1 | grep -E "TestGenerate|TestKind|undefined|no test files"`

Expected: `undefined: Generate`. If the output says `no test files`, the build tag is wrong.

- [ ] **Step 3: Implement `internal/preview/Generate.go`.**

```go
// Package preview turns stored uploads into display-only thumbnails and
// previews by running vips and ffmpeg as subprocesses. Originals are only ever
// read, never modified.
package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/models"
)

const (
	thumbWidth  = 640
	viewMaxEdge = 2000
)

var imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".heic": true, ".heif": true, ".png": true, ".webp": true, ".gif": true}
var videoExt = map[string]bool{".mov": true, ".mp4": true, ".m4v": true}

// Kind decides whether a file gets a preview, from its extension first
// (browsers often send HEIC as application/octet-stream) then its MIME type.
func Kind(contentType, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case imageExt[ext]:
		return models.PreviewKindImage
	case videoExt[ext]:
		return models.PreviewKindVideo
	case contentType == "image/svg+xml":
		return ""
	case strings.HasPrefix(contentType, "image/"):
		return models.PreviewKindImage
	case strings.HasPrefix(contentType, "video/"):
		return models.PreviewKindVideo
	}
	return ""
}

// Paths returns where the outputs for a blob live
func Paths(dir, sha1 string) (thumb, view, poster string) {
	return filepath.Join(dir, sha1+"-thumb.webp"), filepath.Join(dir, sha1+"-view.jpg"), filepath.Join(dir, sha1+"-poster.webp")
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "VIPS_CONCURRENCY=1")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// Generate produces the display outputs for one blob and returns its metadata.
func Generate(ctx context.Context, src, outDir, sha1, kind string) (models.Preview, error) {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return models.Preview{}, err
	}
	switch kind {
	case models.PreviewKindImage:
		return generateImage(ctx, src, outDir, sha1)
	case models.PreviewKindVideo:
		return generateVideo(ctx, src, outDir, sha1)
	}
	return models.Preview{}, errors.New("unsupported kind")
}

func generateImage(parent context.Context, src, outDir, sha1 string) (models.Preview, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	thumb, view, _ := Paths(outDir, sha1)
	// [0] = first page/frame only (GIF, multi-image HEIC). Thumbnail autorotates.
	_, err := command(ctx, "vips", "thumbnail", src+"[0]", view+"[Q=82,strip]",
		strconv.Itoa(viewMaxEdge), "--height", strconv.Itoa(viewMaxEdge), "--size", "down")
	if err != nil {
		return models.Preview{}, err
	}
	p := models.Preview{Sha1: sha1, Kind: models.PreviewKindImage, TakenAt: imageTakenAt(ctx, src)}
	return finish(ctx, p, view, thumb, outDir)
}

func generateVideo(parent context.Context, src, outDir, sha1 string) (models.Preview, error) {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	thumb, view, poster := Paths(outDir, sha1)
	probe, err := command(ctx, "ffprobe", "-v", "error", "-print_format", "json",
		"-show_entries", "format=duration:format_tags:stream=codec_type,width,height:stream_side_data=rotation", src)
	if err != nil {
		return models.Preview{}, err
	}
	p := models.Preview{Sha1: sha1, Kind: models.PreviewKindVideo}
	p.TakenAt, p.DurationMs = parseProbe(probe)
	// One frame at 1s (or the first frame for shorter clips), autorotated by ffmpeg.
	seek := "1"
	if p.DurationMs < 1500 {
		seek = "0"
	}
	_, err = command(ctx, "ffmpeg", "-loglevel", "error", "-threads", "1", "-y", "-ss", seek, "-i", src,
		"-frames:v", "1", "-vf", fmt.Sprintf("scale='min(%d,iw)':-2", viewMaxEdge), view)
	if err != nil {
		return models.Preview{}, err
	}
	p, err = finish(ctx, p, view, thumb, outDir)
	if err != nil {
		return p, err
	}
	// The poster is the thumbnail of the frame; the board shows it with a play badge.
	if err := copyFile(thumb, poster); err != nil {
		return models.Preview{}, err
	}
	return p, nil
}

// finish derives dimensions, thumbnail, colour and hash from the rotated view.
func finish(ctx context.Context, p models.Preview, view, thumb, outDir string) (models.Preview, error) {
	w, h, err := dimensions(ctx, view)
	if err != nil {
		return models.Preview{}, err
	}
	p.Width, p.Height = w, h
	if _, err = command(ctx, "vips", "thumbnail", view, thumb+"[Q=75,strip]", strconv.Itoa(thumbWidth),
		"--height", "100000", "--size", "down"); err != nil {
		return models.Preview{}, err
	}
	tiny := filepath.Join(outDir, p.Sha1+"-tiny.png")
	defer os.Remove(tiny)
	if _, err = command(ctx, "vips", "thumbnail", view, tiny, "9", "--height", "8", "--size", "force"); err != nil {
		return models.Preview{}, err
	}
	p.Color, p.PHash, err = colourAndHash(tiny)
	if err != nil {
		return models.Preview{}, err
	}
	p.Status = models.PreviewReady
	p.Updated = time.Now().Unix()
	return p, nil
}

func dimensions(ctx context.Context, path string) (int, int, error) {
	out, err := command(ctx, "vipsheader", "-f", "width", path)
	if err != nil {
		return 0, 0, err
	}
	w, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	out, err = command(ctx, "vipsheader", "-f", "height", path)
	if err != nil {
		return 0, 0, err
	}
	h, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if w == 0 || h == 0 {
		return 0, 0, errors.New("zero dimensions")
	}
	return w, h, nil
}

// colourAndHash reads the 9x8 image: average colour, and a 64-bit difference
// hash (each bit: is this pixel brighter than its right neighbour).
func colourAndHash(path string) (string, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return "", 0, err
	}
	b := img.Bounds()
	var rs, gs, bs, n uint64
	luma := func(x, y int) uint32 {
		r, g, bl, _ := img.At(x, y).RGBA()
		return (299*r + 587*g + 114*bl) / 1000
	}
	var hash uint64
	bit := 0
	for y := b.Min.Y; y < b.Max.Y && y < b.Min.Y+8; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			rs, gs, bs, n = rs+uint64(r>>8), gs+uint64(g>>8), bs+uint64(bl>>8), n+1
			if x < b.Min.X+8 && x+1 < b.Max.X {
				if luma(x, y) > luma(x+1, y) {
					hash |= 1 << uint(bit)
				}
				bit++
			}
		}
	}
	if n == 0 {
		return "", 0, errors.New("empty image")
	}
	return fmt.Sprintf("#%02x%02x%02x", rs/n, gs/n, bs/n), hash, nil
}

var _ image.Image // keep image import explicit for readers of colourAndHash

// imageTakenAt reads EXIF capture time. Missing data is 0, never a guess.
func imageTakenAt(ctx context.Context, src string) int64 {
	field := func(name string) string {
		out, err := command(ctx, "vipsheader", "-f", name, src)
		if err != nil {
			return ""
		}
		// vips prints "2026:08:14 14:32:05 (2026:08:14 14:32:05, ASCII, ...)"
		s := strings.TrimSpace(string(out))
		if i := strings.Index(s, " ("); i > 0 {
			s = s[:i]
		}
		return s
	}
	raw := field("exif-ifd2-DateTimeOriginal")
	if raw == "" {
		return 0
	}
	loc := time.Local
	if off := field("exif-ifd2-OffsetTimeOriginal"); off != "" {
		if t, err := time.Parse("-07:00", off); err == nil {
			loc = t.Location()
		}
	}
	t, err := time.ParseInLocation("2006:01:02 15:04:05", raw, loc)
	if err != nil {
		return 0
	}
	return t.UTC().Unix()
}

func parseProbe(raw []byte) (takenAt, durationMs int64) {
	var probe struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return 0, 0
	}
	if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
		durationMs = int64(d*1000 + 0.5)
	}
	if s := probe.Format.Tags["com.apple.quicktime.creationdate"]; s != "" {
		if t, err := time.Parse("2006-01-02T15:04:05-0700", s); err == nil {
			return t.UTC().Unix(), durationMs
		}
	}
	if s := probe.Format.Tags["creation_time"]; s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC().Unix(), durationMs
		}
	}
	return 0, durationMs
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0600)
}
```

Notes for the implementer:
- Remove the `var _ image.Image` line and the `image` import if `go vet` reports the import unused once `png` is used.
- For video `Width`/`Height`, the test expects 320×240. That comes from the generated view frame, which is already rotated, so it is correct for portrait iPhone video too.

- [ ] **Step 4: Run the tests.** Same command as Step 2.

Expected: `--- PASS: TestGenerate` and `--- PASS: TestKind`.

If the HEIC assertions fail, look at the dimensions first. vips with libheif already applies `irot`, and the fixture was converted from a JPEG with orientation 6. If HEIC's taken-at fails because exiftool cannot write HEIC EXIF, remove only the HEIC taken-at assertion and note it in the report. Do not weaken the JPEG assertion.

- [ ] **Step 5: Commit.**

```bash
git add internal/preview
git commit -m "preview: generator for photo and video thumbnails via vips/ffmpeg

Decode once to a rotated view, derive the thumbnail from it, and read the
capture time with its offset so mixed photo/video boards sort correctly."
git push
```

---

### Task 4: Worker, backfill and lifecycle hooks

**Files:**
- Create: `internal/preview/Worker.go`, `internal/preview/Worker_test.go`
- Modify:
  - `internal/webserver/api/Api.go`: after a successful `CompleteChunk` in `doBlockingPartCompleteChunk`.
  - `internal/webserver/fileupload/FileUpload.go`: after a successful `storage.NewFile` in `ProcessCompleteFile`.
  - `internal/storage/FileServing.go`:
    - `CleanUp`: call the delete hook where `deleteFile` is true. **Do not gate it on `fileExists`.**
    - `ReplaceFile`: enqueue the new SHA1.
  - `cmd/gokapi/Main.go`: `preview.Start()` after `storage.CleanUp(true)`; `preview.Stop()` in `shutdown()`.

**Interfaces:**
- Consumes: `preview.Generate`, `preview.Kind`, `preview.Paths` (Task 3), and the `database.*Preview*` functions (Task 2).
- Produces:
  - `preview.Start()`. It starts one goroutine and enqueues the backfill. It is idempotent.
  - `preview.Stop()`.
  - `preview.Enqueue(file models.File)`, which never blocks.
  - `preview.Dir() string`, which returns `<DataDir>/previews`.
  - `storage.OnBlobDeleted func(sha1 string)`, a hook variable set by `preview.Start`. This avoids an import cycle, because preview imports storage.
  - `storage.OnBlobAdded func(file models.File)`, also set by `preview.Start`. It is called by `ReplaceFile`.

- [ ] **Step 1: Write the failing worker test.**

```go
//go:build test

package preview

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	code := m.Run()
	testconfiguration.Delete()
	os.Exit(code)
}

func waitStatus(t *testing.T, sha string) models.Preview {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := database.GetPreview(sha); ok && p.Status != models.PreviewPending {
			return p
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("preview for %s never left pending", sha)
	return models.Preview{}
}

func TestWorkerProcessesAndDedupes(t *testing.T) {
	dir := fixtures(t)
	data := configuration.Get().DataDir
	raw, _ := os.ReadFile(filepath.Join(dir, "rot.jpg"))
	test.IsNil(t, os.WriteFile(filepath.Join(data, "workersha1"), raw, 0600))
	f := models.File{Id: "workerfile000000001", Name: "IMG_1.JPG", SHA1: "workersha1", ContentType: "image/jpeg"}

	Start()
	defer Stop()
	Enqueue(f)
	Enqueue(f) // same blob twice: processed once, no error
	p := waitStatus(t, "workersha1")
	test.IsEqualString(t, p.Status, models.PreviewReady)
	thumb, _, _ := Paths(Dir(), "workersha1")
	test.FileExists(t, thumb)

	test.IsNil(t, os.WriteFile(filepath.Join(data, "badsha1"), []byte("junk"), 0600))
	Enqueue(models.File{Id: "workerfile000000002", Name: "bad.jpg", SHA1: "badsha1"})
	test.IsEqualString(t, waitStatus(t, "badsha1").Status, models.PreviewFailed)

	Enqueue(models.File{Id: "workerfile000000003", Name: "doc.pdf", SHA1: "pdfsha1", ContentType: "application/pdf"})
	test.IsEqualString(t, waitStatus(t, "pdfsha1").Status, models.PreviewUnsupported)

	removeBlob("workersha1")
	_, ok := database.GetPreview("workersha1")
	test.IsEqualBool(t, ok, false)
	_, err := os.Stat(thumb)
	test.IsEqualBool(t, os.IsNotExist(err), true)
}
```

- [ ] **Step 2: Run it and confirm it fails.** `build/dev/gotest.sh ./internal/preview/ -run TestWorker -v 2>&1 | grep -E "TestWorker|undefined"`

- [ ] **Step 3: Implement `Worker.go`.**

```go
package preview

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
)

const maxAttempts = 3

var (
	queue    chan models.File
	stopCh   chan struct{}
	startMu  sync.Mutex
	running  bool
	inFlight sync.Map // sha1 -> struct{}, so duplicates in the queue are dropped
)

// Dir is where preview outputs are stored
func Dir() string { return filepath.Join(configuration.Get().DataDir, "previews") }

// Start launches the single worker and queues every blob without a preview.
func Start() {
	startMu.Lock()
	defer startMu.Unlock()
	if running {
		return
	}
	queue = make(chan models.File, 10000)
	stopCh = make(chan struct{})
	running = true
	storage.OnBlobDeleted = removeBlob
	storage.OnBlobAdded = Enqueue
	go loop(queue, stopCh)
	go backfill()
}

// Stop ends the worker after its current job
func Stop() {
	startMu.Lock()
	defer startMu.Unlock()
	if !running {
		return
	}
	close(stopCh)
	running = false
}

// Enqueue schedules a file's blob for preview generation. Never blocks.
func Enqueue(file models.File) {
	if !running || file.SHA1 == "" || file.Encryption.IsEndToEndEncrypted || !file.IsLocalStorage() {
		return
	}
	if p, ok := database.GetPreview(file.SHA1); ok && p.Status != models.PreviewPending {
		return
	}
	if _, loaded := inFlight.LoadOrStore(file.SHA1, struct{}{}); loaded {
		return
	}
	database.SavePreview(models.Preview{Sha1: file.SHA1, Status: models.PreviewPending, Updated: time.Now().Unix()})
	select {
	case queue <- file:
	default:
		inFlight.Delete(file.SHA1) // queue full; the next backfill picks it up
	}
}

func backfill() {
	known := database.GetAllPreviewSha1s()
	seen := make(map[string]bool)
	queued := 0
	for _, f := range database.GetAllMetadata() {
		if seen[f.SHA1] {
			continue
		}
		seen[f.SHA1] = true
		if status, ok := known[f.SHA1]; ok && status != models.PreviewPending {
			continue
		}
		Enqueue(f)
		queued++
	}
	if queued > 0 {
		// The only Status-log line the worker writes: one per startup, in the
		// "info" category, which the current notifier ignores.
		logging.LogInfo(fmt.Sprintf("Preview backfill queued %d file(s)", queued))
	}
}

func loop(q chan models.File, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case f := <-q:
			process(f)
			inFlight.Delete(f.SHA1)
		}
	}
}

func process(f models.File) {
	kind := Kind(f.ContentType, f.Name)
	prev, _ := database.GetPreview(f.SHA1)
	if kind == "" {
		database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewUnsupported, Updated: time.Now().Unix()})
		return
	}
	src, cleanup, err := readablePath(f)
	if err == nil {
		var p models.Preview
		p, err = Generate(context.Background(), src, Dir(), f.SHA1, kind)
		cleanup()
		if err == nil {
			database.SavePreview(p)
			return
		}
	}
	// stdout only: see Global Constraints (the notifier emails warning lines)
	fmt.Printf("preview: %s (%s): %v\n", f.Id, f.Name, err)
	attempts := prev.Attempts + 1
	status := models.PreviewFailed
	if attempts < maxAttempts {
		status = models.PreviewPending // retried on next startup
	}
	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: status, Kind: kind, Attempts: attempts, Updated: time.Now().Unix()})
}

// readablePath returns a plain-bytes path for the blob. Unencrypted local files
// are read in place; server-side encrypted ones are decrypted to a temp file
// that cleanup removes.
func readablePath(f models.File) (string, func(), error) {
	path := filepath.Join(configuration.Get().DataDir, f.SHA1)
	if !f.Encryption.IsEncrypted {
		return path, func() {}, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return "", func() {}, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(Dir(), "dec-*"+filepath.Ext(f.Name))
	if err != nil {
		return "", func() {}, err
	}
	err = encryption.DecryptReader(f.Encryption, in, tmp)
	_ = tmp.Close()
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp.Name(), cleanup, nil
}

func removeBlob(sha1 string) {
	thumb, view, poster := Paths(Dir(), sha1)
	for _, p := range []string{thumb, view, poster} {
		_ = os.Remove(p)
	}
	database.DeletePreview(sha1)
}

var _ = io.EOF
```

Notes for the implementer:
- Remove the `var _ = io.EOF` line and the `io` import if they are unused.
- `os.CreateTemp(Dir(), …)` needs the directory to exist. Call `os.MkdirAll(Dir(), 0700)` in `Start()`.
- `logging.LogInfo` does not exist. Add it to `internal/logging/Logging.go`, next to `LogCollectionCreate`:

```go
// LogInfo adds an informational log entry. Non-Blocking
func LogInfo(text string) {
	createLogEntry(categoryInfo, text, false)
}
```

Check that the category constant is named `categoryInfo`; the category list is at `Logging.go:23-28`.

- [ ] **Step 4: Add the hooks in existing files.** Keep each edit to a few lines.

`internal/storage/FileServing.go`, top level:

```go
// OnBlobDeleted is called when the last file using a blob is removed. Set by
// the preview package; nil-safe.
var OnBlobDeleted func(sha1 string)

// OnBlobAdded is called when a file's blob changes in place (ReplaceFile).
var OnBlobAdded func(file models.File)
```

In `CleanUp`, inside the branch where `deleteFile` is decided true (around `:827`), call it outside the `fileExists` condition:

```go
		if deleteFile && OnBlobDeleted != nil {
			OnBlobDeleted(element.SHA1)
		}
```

In `ReplaceFile`, after the metadata with the new SHA1 is saved:

```go
	if OnBlobAdded != nil {
		OnBlobAdded(file)
	}
```

Use the variable that holds the updated file at that point.

`internal/webserver/api/Api.go`, in `doBlockingPartCompleteChunk` after `CompleteChunk` succeeds, beside `logging.LogUpload`:

```go
	preview.Enqueue(file)
```

`internal/webserver/fileupload/FileUpload.go`, in `ProcessCompleteFile`, after the successful `NewFile`, beside `logging.LogUpload`:

```go
	preview.Enqueue(result)
```

Use the actual variable name there.

`cmd/gokapi/Main.go`: after `storage.CleanUp(true)` add `preview.Start()`. In `shutdown()` add `preview.Stop()`.

Check for an import cycle by running `build/dev/gotest.sh ./cmd/... ./internal/webserver/... -run XXX_NONE` (it compiles only). If `preview` → `storage` → … → `preview` cycles, the hook variables are what break the cycle, so only `api`, `fileupload` and `main` may import `preview`.

- [ ] **Step 5: Run the tests.**

```bash
build/dev/gotest.sh ./internal/preview/ ./internal/storage/ ./internal/webserver/... 2>&1 | grep -E "^(--- FAIL|ok|FAIL)|TestWorker"
```

Expected: `--- PASS: TestWorkerProcessesAndDedupes`, every package `ok`.

- [ ] **Step 6: Commit.**

```bash
git add -A internal cmd
git commit -m "preview: single background worker, backfill, and delete/replace hooks

Errors go to stdout only: the notifier emails every warning line, and a
failed backfill must not become dozens of emails."
git push
```

---

### Task 5: `CanView`, preview routes, video streaming, collection-unlock TTL

**Files:**
- Create: `internal/webserver/access/Access.go`, `internal/webserver/access/Access_test.go`, `internal/webserver/Previews.go`
- Modify:
  - `downloadPasswordToken/DownloadPasswordToken.go`: add `GenerateWithTtl`.
  - `internal/webserver/Collections.go`: in `writeCollectionPwCookie`, use 12 h for both the token and the cookie.
  - `internal/webserver/Webserver.go`: routes.
  - `internal/storage/Zip.go`: set `ZipEntryTime` to taken-at. Do this here, because `storage` cannot import `preview`. It is set from `preview.Start()` instead (Step 6).

**Interfaces:**
- Consumes: `models.Preview`, `preview.Paths`, `preview.Dir()`, `database.GetPreview`.
- Produces:
  - `access.CanView(r *http.Request, file models.File, collectionId string) bool`.
  - Routes:
    - `GET /p/thumb?id=<fileId>&v=<ver>[&c=<collectionId>]`
    - `GET /p/view?…`
    - `GET /p/poster?…`
    - `GET /p/video?id=<fileId>[&c=<collectionId>]`
  - `webserver.previewUrls(file models.File, collectionId string) previewInfo`, where:

```go
type previewInfo struct {
	Status   string // "ready", "pending", "failed", "unsupported", or "" if no row
	Kind     string
	Width    int
	Height   int
	Color    string
	TakenAt  int64
	Thumb    string // "" unless ready
	View     string
	Video    string // only for video
	Download string // the original, via the route the caller passes in
}
```

**The CanView rule** (spec, "Access", tightened per review):
1. The file must resolve through `storage.GetFile(id)`, so it is not expired, not exhausted and not pending deletion.
2. Deny if `UploadRequestId != ""` or the file is end-to-end encrypted.
3. Allow if the request has a logged-in user (`authentication.IsAuthenticated`) who is the uploader, or who has `UserPermListOtherUploads`.
4. If `collectionId != ""`: allow if the collection resolves, contains the file, and either has no password or has a valid `pc<id>` cookie.
5. Otherwise allow if the file has no password, or the request has a valid `p<fileId>` cookie. This is "holders of the file's link", the same as `/d`.

- [ ] **Step 1: Write the failing access tests.** Build them as a table, one row per grant and one per denial. Use `internal/webserver/Webserver_test.go`'s `TestMain` environment by placing the test in package `webserver`, with `access_test.go` calling `access.CanView` directly.

```go
//go:build test

package webserver

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/access"
)

func accessFile(id string, mutate func(*models.File)) models.File {
	f := models.File{Id: id, Name: "a.jpg", SHA1: "accesssha", UserId: 7, SizeBytes: 1,
		ExpireAt: time.Now().Add(time.Hour).Unix(), UnlimitedDownloads: true}
	if mutate != nil {
		mutate(&f)
	}
	database.SaveMetaData(f)
	return f
}

func TestCanView(t *testing.T) {
	anon := httptest.NewRequest("GET", "/p/thumb", nil)
	admin := httptest.NewRequest("GET", "/p/thumb", nil)
	admin.AddCookie(&http.Cookie{Name: "session_token", Value: "validsession"})

	plain := accessFile("accessplain00000001", nil)
	pw := accessFile("accesspw00000000001", func(f *models.File) { f.PasswordHash = "x" })
	req := accessFile("accessreq0000000001", func(f *models.File) { f.UploadRequestId = "r" })
	e2e := accessFile("accesse2e0000000001", func(f *models.File) { f.Encryption.IsEndToEndEncrypted = true })
	gone := accessFile("accessexp0000000001", func(f *models.File) { f.ExpireAt = time.Now().Add(-time.Hour).Unix() })

	test.IsEqualBool(t, access.CanView(anon, plain, ""), true)  // link holder
	test.IsEqualBool(t, access.CanView(anon, pw, ""), false)    // no pw cookie
	test.IsEqualBool(t, access.CanView(admin, pw, ""), true)    // admin with PERM_LIST
	test.IsEqualBool(t, access.CanView(admin, req, ""), false)  // file request upload
	test.IsEqualBool(t, access.CanView(admin, e2e, ""), false)  // end-to-end encrypted
	test.IsEqualBool(t, access.CanView(admin, gone, ""), false) // expired

	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "c", FileIds: []string{pw.Id}, Password: "pw"}, models.User{Id: 7})
	test.IsNil(t, err)
	test.IsEqualBool(t, access.CanView(anon, pw, col.Id), false) // locked collection
	other, _ := storage.CreateCollection(storage.CollectionParameters{Name: "o", FileIds: []string{plain.Id}}, models.User{Id: 7})
	test.IsEqualBool(t, access.CanView(anon, pw, other.Id), false) // file not in that collection
}
```

Add `"net/http"` to the imports. `validsession` belongs to the super-admin in `testconfiguration`; check that, and pick a session belonging to a user with `PERM_LIST` if it does not.

- [ ] **Step 2: Run it and confirm the failure.** `build/dev/gotest.sh ./internal/webserver/ -run TestCanView -v 2>&1 | grep -E "TestCanView|undefined"`

- [ ] **Step 3: Implement `access/Access.go`.** It needs the pc and p cookie checks, which live unexported in package `webserver`. To avoid exporting internals:
  - move the two validity checks into `access` as exported functions `ValidFilePwCookie(r, fileId string) bool` and `ValidCollectionPwCookie(r, collectionId string) bool`;
  - re-implement `webserver.isValidPwCookie` and `isValidCollectionPwCookie` as one-line calls to them.

```go
// Package access answers one question for every new route: may this request
// see this file? Keeping it in one place is what stops a preview, a board feed
// or a zip from leaking something its download page would refuse.
package access

import (
	"net/http"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// CanView reports whether the request may see the file, optionally in the
// context of a collection page.
func CanView(r *http.Request, file models.File, collectionId string) bool {
	current, ok := storage.GetFile(file.Id)
	if !ok || current.UploadRequestId != "" || current.Encryption.IsEndToEndEncrypted {
		return false
	}
	if user, loggedIn, err := authentication.IsAuthenticated(nil, r); err == nil && loggedIn {
		if user.Id == current.UserId || user.HasPermission(models.UserPermListOtherUploads) {
			return true
		}
	}
	if collectionId != "" {
		collection, ok := storage.GetCollection(collectionId)
		if ok {
			if _, member := storage.GetCollectionFile(collection, current.Id); member {
				return !collection.IsPasswordProtected() || ValidCollectionPwCookie(r, collection.Id)
			}
		}
	}
	return current.PasswordHash == "" || ValidFilePwCookie(r, current.Id)
}

// ValidFilePwCookie checks the cookie set after entering a file's password
func ValidFilePwCookie(r *http.Request, fileId string) bool {
	c, err := r.Cookie("p" + fileId)
	return err == nil && downloadPasswordToken.IsValid(c.Value, fileId)
}

// ValidCollectionPwCookie checks the cookie set after entering a collection's password
func ValidCollectionPwCookie(r *http.Request, collectionId string) bool {
	c, err := r.Cookie("pc" + collectionId)
	return err == nil && downloadPasswordToken.IsValid(c.Value, collectionId)
}
```

Before relying on this, check three things:
- `authentication.IsAuthenticated` accepts a nil `ResponseWriter`. If it writes to `w`, pass `httptest.NewRecorder()`, or add a read-only variant `authentication.UserFromSessionCookie(r) (models.User, bool)` next to it, and use that.
- The existing pw-cookie functions use exactly these cookie names and token calls. They are at `Webserver.go:1168` and `Collections.go:313`.
- `access` importing `storage` does not create a cycle.

If a deny case is unexpectedly allowed in the zero-collection fallthrough (step 5 of the rule), that is intended. A file inside a locked collection whose own link has no password is reachable by its file ID through `/d` today, and that file ID is printed on the collection page only after unlocking.

- [ ] **Step 4: Run the test.** Expected: `--- PASS: TestCanView`.

- [ ] **Step 5: Extend the collection unlock to 12 h.** In `DownloadPasswordToken.go`:

```go
// GenerateWithTtl is Generate with a caller-chosen lifetime
func GenerateWithTtl(fileId string, lifetime time.Duration) string {
	token := helper.GenerateRandomString(60)
	mutex.Lock()
	tokens[token] = pwToken{FileId: fileId, Expiry: time.Now().Add(lifetime).Unix()}
	mutex.Unlock()
	cleanupOnce.Do(func() { go cleanup(true) })
	return token
}
```

Make `Generate` call `GenerateWithTtl(fileId, ttl)`. In `writeCollectionPwCookie`, use `GenerateWithTtl(collection.Id, 12*time.Hour)` and set the cookie `Expires` to `time.Now().Add(12 * time.Hour)`. The file-password cookie is unchanged.

- [ ] **Step 6: Add the preview routes in `internal/webserver/Previews.go`.**

```go
package webserver

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/preview"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/access"
)

type previewInfo struct {
	Status, Kind, Color, Thumb, View, Video, Download string
	Width, Height                                     int
	TakenAt                                           int64
}

// previewUrls builds the display URLs for a file. collectionId scopes access
// checks to a collection page; download is the original-file URL to show.
func previewUrls(file models.File, collectionId, download string) previewInfo {
	info := previewInfo{Download: download}
	p, ok := database.GetPreview(file.SHA1)
	if !ok {
		return info
	}
	info.Status, info.Kind, info.Color = p.Status, p.Kind, p.Color
	info.Width, info.Height, info.TakenAt = p.Width, p.Height, p.TakenAt
	if p.Status != models.PreviewReady {
		return info
	}
	q := "?id=" + file.Id + "&v=" + p.Version()
	if collectionId != "" {
		q += "&c=" + collectionId
	}
	info.Thumb, info.View = "./p/thumb"+q, "./p/view"+q
	if p.Kind == models.PreviewKindVideo {
		info.Video = "./p/video" + q
	}
	return info
}

func servePreviewFile(which string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, ok := storage.GetFile(r.URL.Query().Get("id"))
		if !ok || !access.CanView(r, file, r.URL.Query().Get("c")) {
			http.NotFound(w, r)
			return
		}
		p, ok := database.GetPreview(file.SHA1)
		if !ok || p.Status != models.PreviewReady {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		thumb, view, poster := preview.Paths(preview.Dir(), file.SHA1)
		path := map[string]string{"thumb": thumb, "view": view, "poster": poster}[which]
		f, err := os.Open(path)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		if r.URL.Query().Get("v") == p.Version() {
			w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "private, no-cache")
		}
		http.ServeContent(w, r, filepath.Base(path), time.Unix(p.Updated, 0), f)
	}
}

// streamVideo plays the original inline for the viewer. It is deliberately
// not a download: no counter, no download log, no Downloads row. A <video>
// element issues many range requests per play.
func streamVideo(w http.ResponseWriter, r *http.Request) {
	file, ok := storage.GetFile(r.URL.Query().Get("id"))
	if !ok || !access.CanView(r, file, r.URL.Query().Get("c")) || !file.IsLocalStorage() || file.Encryption.IsEncrypted {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(configuration.Get().DataDir, file.SHA1))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	ct := file.ContentType
	if ct == "" || ct == "application/octet-stream" || ct == "video/quicktime" {
		ct = "video/mp4" // Chrome refuses video/quicktime for HEVC/H.264 MOVs it can otherwise play
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, "", time.Unix(file.UploadDate, 0), f)
}
```

Routes in `Webserver.go`, after `/cz`:

```go
	mux.HandleFunc("/p/thumb", servePreviewFile("thumb"))
	mux.HandleFunc("/p/view", servePreviewFile("view"))
	mux.HandleFunc("/p/poster", servePreviewFile("poster"))
	mux.HandleFunc("/p/video", streamVideo)
```

These routes are public, gated by `CanView`, so they are not `requireLogin`.

`ZipEntryTime`: in `preview.Start()` add:

```go
	storage.ZipEntryTime = func(f models.File) time.Time {
		if p, ok := database.GetPreview(f.SHA1); ok && p.TakenAt > 0 {
			return time.Unix(p.TakenAt, 0)
		}
		return time.Unix(f.UploadDate, 0)
	}
```

- [ ] **Step 7: Write the HTTP tests for the routes.** Add them to the same `access_test.go`:
  - A ready preview with the correct `v` gets the `immutable` header.
  - A wrong `v` gets `no-cache`.
  - A pending preview returns 404 with `no-store`.
  - `/p/video` on a password file without a cookie returns 404.
  - Range requests on `/p/video` return 206 and **do not** change `DownloadCount` (read it before and after with `database.GetMetaDataById`).

```go
func TestPreviewRoutes(t *testing.T) {
	f := accessFile("previewroute0000001", func(f *models.File) { f.SHA1 = "routesha1"; f.ContentType = "video/mp4" })
	test.IsNil(t, os.MkdirAll(preview.Dir(), 0700))
	thumb, _, _ := preview.Paths(preview.Dir(), "routesha1")
	test.IsNil(t, os.WriteFile(thumb, []byte("RIFFxxxxWEBP"), 0600))
	test.IsNil(t, os.WriteFile(filepath.Join(configuration.Get().DataDir, "routesha1"), make([]byte, 4096), 0600))
	database.SavePreview(models.Preview{Sha1: "routesha1", Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 1})

	w := httptest.NewRecorder()
	servePreviewFile("thumb")(w, httptest.NewRequest("GET", "/p/thumb?id="+f.Id+"&v=routesha1", nil))
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, max-age=31536000, immutable")

	w = httptest.NewRecorder()
	servePreviewFile("thumb")(w, httptest.NewRequest("GET", "/p/thumb?id="+f.Id+"&v=stale", nil))
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, no-cache")

	before, _ := database.GetMetaDataById(f.Id)
	w = httptest.NewRecorder()
	rq := httptest.NewRequest("GET", "/p/video?id="+f.Id, nil)
	rq.Header.Set("Range", "bytes=0-99")
	streamVideo(w, rq)
	test.IsEqualInt(t, w.Code, 206)
	after, _ := database.GetMetaDataById(f.Id)
	test.IsEqualInt(t, after.DownloadCount, before.DownloadCount)
}
```

`v=routesha1` is the full SHA1, because it is shorter than 12 characters and `Version()` returns it whole. Add the `os`, `path/filepath`, `configuration` and `preview` imports.

Run: `build/dev/gotest.sh ./internal/webserver/ -run 'TestCanView|TestPreviewRoutes' -v 2>&1 | grep -E "^---"`

Expected: two PASS lines.

- [ ] **Step 8: Commit.**

```bash
git add -A internal
git commit -m "previews: one access rule, preview routes, inline video that never counts

CanView mirrors exactly what /d and /c allow. Video streams through its own
route so a <video> element's range requests are not logged or counted as
downloads. Collection unlock lasts 12h so long boards keep loading."
git push
```

---

### Task 6: Preview on the single-file download page

**Files:**
- Modify:
  - `internal/webserver/Webserver.go`: `DownloadView` gets `Preview previewInfo`, filled in `showDownload`.
  - `internal/webserver/web/templates/html_download.tmpl`: the icon block.

- [ ] **Step 1: Write the failing test.** GET `/d?id=<plain test file with a ready preview>` must contain `./p/thumb?id=`. GET for a password file before entering the password must not.

```go
func TestDownloadPageShowsPreview(t *testing.T) {
	f := accessFile("dlpreview0000000001", func(f *models.File) { f.SHA1 = "dlsha1"; f.Name = "IMG.JPG" })
	database.SavePreview(models.Preview{Sha1: "dlsha1", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Updated: 1})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/d?id=" + f.Id, IsHtml: true,
		RequiredContent: []string{"./p/view?id=" + f.Id},
	})
}
```

- [ ] **Step 2: Implement it.** In `showDownload`, where the non-password `DownloadView` is built and not end-to-end encrypted:

```go
	view.Preview = previewUrls(file, "", "./downloadFile?id="+file.Id)
```

In `html_download.tmpl`, replace the inner `{{ else }}` icon branch at lines 13-16 with:

```
                {{ else if .Preview.View }}
                    <img src="{{ .Preview.View }}" alt="" class="fgs-download-preview"
                         width="{{ .Preview.Width }}" height="{{ .Preview.Height }}"
                         style="background:{{ .Preview.Color }}">
                {{ else }}
                    <svg …existing file icon, unchanged…></svg>
                {{ end }}
```

`media.css`:

```css
.fgs-download-preview{max-width:100%;height:auto;max-height:60vh;border-radius:10px;margin-bottom:1rem}
```

- [ ] **Step 3: Run the test.** Expected: PASS. Run the existing download-page tests too, `-run TestDownload`, and confirm they still pass.

- [ ] **Step 4: Commit.** Message: `download page: show the preview instead of a generic icon`, then push.

---

### Task 7: Files tab

**Files:**
- Create: `internal/webserver/Files.go`, `internal/webserver/web/templates/html_files.tmpl`, `internal/webserver/web/static/js/admin_ui_files.js`
- Modify:
  - `Webserver.go`: add `ViewFiles = 6` beside the other view IDs. In `convertGlobalConfig`, extend `case ViewMain, ViewCollections:` to include `ViewFiles`. Add the routes.
  - `html_header.tmpl`: a Files nav link after Upload.
  - `html_admin.tmpl`: the upload table stops rendering stored files and shows only this session's uploads via `addRow`, plus a link to Files.
  - `internal/webserver/Zip.go`: `/files/zip`.

`admin_ui_files.js` is bundled automatically by the `js/admin_*.js` glob.

**Interfaces:**
- Consumes: `previewUrls`, `storage.ServeZip`, `newAdminButtonContext`, and the templates `admin_button_*` and `admin_modal_edit`.
- Produces:
  - Route `GET /files`, `requireLogin(showFiles, true, false)`.
  - Route `GET /files/zip?ids=a,b,c`, `requireLogin(downloadFilesZip, false, false)`. Each ID is checked as owner or `PERM_LIST`.
  - Template `files`, with view model `FilesView{AdminView; Rows []fileRow; CanSeeEveryone bool}`.

```go
type fileRow struct {
	File     models.FileApiOutput
	Uploader string
	Mine     bool
	TakenAt  int64
	Preview  previewInfo
}
```

- [ ] **Step 1: Write the failing page test.**

```go
func TestFilesPage(t *testing.T) {
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/files",
		IsHtml:          true,
		Cookies:         []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`id="fgs-files"`, "My files", "Everything", `data-view="grid"`},
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/files", IsHtml: true,
		RequiredContent: []string{"Login"}, // not logged in -> login page
	})
}

func TestFilesZipOnlyOwnUnlessPermList(t *testing.T) {
	// A user without PERM_LIST asking for someone else's file gets nothing.
	// Use a session for a non-admin test user from testconfiguration.
}
```

Fill in the second test with a concrete session and file ID from `testconfiguration`. If no non-admin session exists, create one with `database.SaveSession` as the existing users tests do. Assert a 410 response with `Nothing left to download`.

- [ ] **Step 2: Implement `Files.go`.**

```go
package webserver

import (
	"net/http"
	"sort"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type fileRow struct {
	File     models.FileApiOutput
	Uploader string
	Mine     bool
	TakenAt  int64
	Preview  previewInfo
}

// FilesView is the Files tab: the upload table's actions over a Finder-like grid
type FilesView struct {
	AdminView
	Rows           []fileRow
	CanSeeEveryone bool
}

func showFiles(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	config := configuration.Get()
	view := FilesView{CanSeeEveryone: user.HasPermissionListOtherUploads()}
	view.AdminView = *(&AdminView{}).convertGlobalConfig(ViewFiles, user)
	names := map[int]string{}
	for _, u := range database.GetAllUsers() {
		names[u.Id] = u.Name
	}
	for _, f := range database.GetAllMetadata() {
		if f.UserId != user.Id && !view.CanSeeEveryone {
			continue
		}
		if f.IsPendingDeletion() || f.UploadRequestId != "" {
			continue
		}
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		row := fileRow{File: out, Uploader: names[f.UserId], Mine: f.UserId == user.Id}
		row.Preview = previewUrls(f, "", out.UrlDownload)
		row.TakenAt = row.Preview.TakenAt
		view.Rows = append(view.Rows, row)
	}
	sort.SliceStable(view.Rows, func(i, j int) bool { return view.Rows[i].File.UploadDate > view.Rows[j].File.UploadDate })
	helper.CheckIgnoreTimeout(templateFolder.ExecuteTemplate(w, "files", view))
}
```

Use whatever `showCollectionsAdmin` does for executing the template and handling errors; copy its exact calls rather than `helper.CheckIgnoreTimeout` if that differs. Also check that expired files are filtered the way the upload page does.

`downloadFilesZip` in `Zip.go`:

```go
func downloadFilesZip(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	files := make([]models.File, 0)
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		f, ok := storage.GetFile(id)
		if !ok || (f.UserId != user.Id && !user.HasPermissionListOtherUploads()) {
			continue
		}
		files = append(files, f)
	}
	storage.ServeZip(files, "Files", w, r)
}
```

Routes, in exactly the form `updateProtectedUrls` expects:

```go
	mux.HandleFunc("/files", requireLogin(showFiles, true, false))
	mux.HandleFunc("/files/zip", requireLogin(downloadFilesZip, false, false))
```

- [ ] **Step 3: Write the template `html_files.tmpl`.** Build it on the existing skeleton from `html_logs.tmpl`, with the header, `admin.min.js`, `pagename`, `customjs` and `footer true`.

```
{{ define "files" }}{{ template "header" . }}
<div class="row"><div class="col"><div id="container" class="card" style="width:100%">
<div class="card-body" id="fgs-files" data-view="grid">
  <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
    <h3 class="card-title mb-0 me-auto">Files</h3>
    {{ if .CanSeeEveryone }}
    <div class="btn-group" role="group" aria-label="Whose files">
      <button class="btn btn-sm btn-light" data-scope="mine">My files</button>
      <button class="btn btn-sm btn-outline-light" data-scope="all">Everything</button>
    </div>
    {{ end }}
    <div class="btn-group" role="group" aria-label="Layout">
      <button class="btn btn-sm btn-light" data-layout="grid" aria-label="Grid"><i class="bi bi-grid-3x3-gap"></i></button>
      <button class="btn btn-sm btn-outline-light" data-layout="list" aria-label="List"><i class="bi bi-list-ul"></i></button>
    </div>
    <input type="range" min="96" max="320" value="160" id="fgs-icon-size" aria-label="Icon size" class="form-range" style="width:8rem">
  </div>

  <div class="fgs-selectbar d-none" id="fgs-selectbar">
    <span id="fgs-selcount"></span>
    <button class="btn btn-sm btn-light" data-act="download">Download</button>
    <button class="btn btn-sm btn-outline-light" data-act="zip">Download as .zip</button>
    <button class="btn btn-sm btn-outline-light d-none" data-act="photos">Save to Photos</button>
    <button class="btn btn-sm btn-link text-muted d-none" data-fgs-help aria-label="How to save to Photos"><i class="bi bi-question-circle"></i></button>
  </div>

  <div class="fgs-grid" id="fgs-grid">
    {{ range .Rows }}
    <figure class="fgs-tile" data-mine="{{ .Mine }}" data-id="{{ .File.Id }}"
            data-name="{{ .File.Name }}" data-download="{{ .Preview.Download }}"
            data-view="{{ .Preview.View }}" data-video="{{ .Preview.Video }}"
            data-uploader="{{ .Uploader }}" data-taken="{{ .TakenAt }}" data-uploaded="{{ .File.UploadDate }}"
            data-size="{{ .File.Size }}" data-w="{{ .Preview.Width }}" data-h="{{ .Preview.Height }}">
      <button class="fgs-tile-open" type="button" aria-label="Open {{ .File.Name }}">
        {{ if .Preview.Thumb }}
          <img loading="lazy" decoding="async" src="{{ .Preview.Thumb }}" alt="" style="background:{{ .Preview.Color }}">
          {{ if .Preview.Video }}<span class="fgs-play"><i class="bi bi-play-fill"></i></span>{{ end }}
        {{ else }}
          <span class="fgs-typeicon"><i class="bi bi-file-earmark"></i><small>{{ .File.Name | printf "%.40s" }}</small></span>
        {{ end }}
      </button>
      <input type="checkbox" class="form-check-input fgs-tile-check" aria-label="Select {{ .File.Name }}">
      <figcaption><span class="fgs-name">{{ .File.Name }}</span>
        <span class="fgs-meta">{{ .Uploader }} · <span data-ts="{{ .File.UploadDate }}"></span></span></figcaption>
    </figure>
    {{ end }}
  </div>

  <div class="table-responsive d-none" id="fgs-list">
    <table class="table table-dark align-middle" id="fgs-filetable">
      <thead><tr><th>Name</th><th>Uploaded by</th><th>Uploaded</th><th>Taken</th><th>Size</th><th>Expires</th><th>Downloads</th><th>Actions</th></tr></thead>
      <tbody>
      {{ range .Rows }}
        <tr data-mine="{{ .Mine }}" id="row-{{ .File.Id }}">
          <td>{{ .File.Name }}</td><td>{{ .Uploader }}</td>
          <td data-order="{{ .File.UploadDate }}"><span data-ts="{{ .File.UploadDate }}"></span></td>
          <td data-order="{{ .TakenAt }}">{{ if .TakenAt }}<span data-ts="{{ .TakenAt }}"></span>{{ else }}—{{ end }}</td>
          <td data-order="{{ .File.SizeBytes }}">{{ .File.Size }}</td>
          <td>{{ if .File.UnlimitedTime }}Never{{ else }}{{ .File.ExpireAtString }}{{ end }}</td>
          <td>{{ .File.DownloadCount }}</td>
          <td><div class="btn-group" role="group">
            {{ template "admin_button_copyurl" (newAdminButtonContext .File $.ActiveUser) }}
            {{ template "admin_button_download" (newAdminButtonContext .File $.ActiveUser) }}
            {{ template "admin_button_edit" (newAdminButtonContext .File $.ActiveUser) }}
            {{ template "admin_button_delete" (newAdminButtonContext .File $.ActiveUser) }}
          </div></td>
        </tr>
      {{ end }}
      </tbody>
    </table>
  </div>
  {{ template "media_viewer" }}
  {{ template "media_help_sheet" }}
</div></div></div></div>
{{ template "admin_modal_edit" . }}
<script src="./js/min/admin.min.{{ template "js_admin_version"}}.js"></script>
<script src="./js/min/media.min.{{ template "js_media_version" }}.js"></script>
<script>initFilesPage();</script>
{{ template "pagename" "Files"}}
{{ template "customjs" .}}
{{ template "footer" true}}{{ end }}
```

Before writing this, read `html_admin.tmpl:106-157` and copy the exact sub-template calls and argument shapes the upload table uses, because the four action buttons must match. Check how `admin_modal_edit` is invoked there, and whether it needs `.` or another context. If a button's JS handler assumes `#maintable`, it is in `admin_ui_upload.js`. Add the minimum change for it to also work in `#fgs-filetable`, and list that change in the report.

`media_viewer` is defined in Task 8. Until then, add an empty `{{ define "media_viewer" }}{{ end }}` to `html_media_shared.tmpl`; Task 8 replaces it.

- [ ] **Step 4: Write `admin_ui_files.js`.**

```js
// Files tab: grid/list, My files / Everything, icon size, multi-select.
// Remembers choices in localStorage, like the upload page's defaults.
function initFilesPage() {
  var root = document.getElementById("fgs-files");
  if (!root) return;
  var grid = document.getElementById("fgs-grid");
  var list = document.getElementById("fgs-list");
  var bar = document.getElementById("fgs-selectbar");

  document.querySelectorAll("#fgs-files [data-ts]").forEach(function (el) {
    var ts = parseInt(el.getAttribute("data-ts"), 10);
    if (ts) el.textContent = new Date(ts * 1000).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
  });

  function setActive(attr, value) {
    root.querySelectorAll("[" + attr + "]").forEach(function (b) {
      var on = b.getAttribute(attr) === value;
      b.classList.toggle("btn-light", on);
      b.classList.toggle("btn-outline-light", !on);
    });
  }

  function applyLayout(layout) {
    root.setAttribute("data-view", layout);
    grid.classList.toggle("d-none", layout !== "grid");
    list.classList.toggle("d-none", layout !== "list");
    setActive("data-layout", layout);
    localStorage.setItem("filesLayout", layout);
  }

  function applyScope(scope) {
    root.querySelectorAll("[data-mine]").forEach(function (el) {
      el.classList.toggle("d-none", scope === "mine" && el.getAttribute("data-mine") !== "true");
    });
    setActive("data-scope", scope);
    localStorage.setItem("filesScope", scope);
  }

  root.querySelectorAll("[data-layout]").forEach(function (b) {
    b.addEventListener("click", function () { applyLayout(b.getAttribute("data-layout")); });
  });
  root.querySelectorAll("[data-scope]").forEach(function (b) {
    b.addEventListener("click", function () { applyScope(b.getAttribute("data-scope")); });
  });

  var size = document.getElementById("fgs-icon-size");
  size.value = localStorage.getItem("filesIconSize") || "160";
  function applySize() {
    grid.style.setProperty("--fgs-tile", size.value + "px");
    localStorage.setItem("filesIconSize", size.value);
  }
  size.addEventListener("input", applySize);
  applySize();

  function selected() {
    return Array.prototype.slice.call(grid.querySelectorAll(".fgs-tile-check:checked"))
      .map(function (c) { return c.closest(".fgs-tile"); })
      .filter(function (t) { return !t.classList.contains("d-none"); });
  }
  grid.addEventListener("change", function () {
    var n = selected().length;
    bar.classList.toggle("d-none", n === 0);
    document.getElementById("fgs-selcount").textContent = n + " selected";
  });
  bar.addEventListener("click", function (e) {
    var act = e.target.closest("[data-act]");
    if (!act) return;
    var tiles = selected();
    if (act.getAttribute("data-act") === "zip") {
      window.location = "./files/zip?ids=" + tiles.map(function (t) { return t.getAttribute("data-id"); }).join(",");
    } else if (act.getAttribute("data-act") === "download") {
      FGS.downloadSequentially(tiles.map(function (t) { return { url: t.getAttribute("data-download"), name: t.getAttribute("data-name") }; }));
    } else if (act.getAttribute("data-act") === "photos") {
      FGS.saveToPhotos(tiles.map(function (t) { return { url: t.getAttribute("data-download"), name: t.getAttribute("data-name") }; }), act);
    }
  });
  if (window.FGS && FGS.isIOS()) {
    bar.querySelector("[data-act=download]").classList.add("d-none");
    bar.querySelector("[data-act=photos]").classList.remove("d-none");
    bar.querySelector("[data-fgs-help]").classList.remove("d-none");
  }
  bar.querySelector("[data-fgs-help]").addEventListener("click", function () { FGS.openHelpSheet(); });

  grid.addEventListener("click", function (e) {
    var open = e.target.closest(".fgs-tile-open");
    if (!open || !window.FGS || !FGS.openViewer) return;
    var tiles = Array.prototype.slice.call(grid.querySelectorAll(".fgs-tile:not(.d-none)"));
    FGS.openViewer(tiles.map(FGS.itemFromEl), tiles.indexOf(open.closest(".fgs-tile")));
  });

  if ($ && $.fn.DataTable) $("#fgs-filetable").DataTable({ order: [[2, "desc"]], pageLength: 100 });
  applyLayout(localStorage.getItem("filesLayout") || "grid");
  applyScope(localStorage.getItem("filesScope") || "mine");
}
```

Add `FGS.downloadSequentially` to `media.js`. Lift it from the collection page's existing inline "Download all" loop (temporary `<a download>`, 600 ms spacing) so both pages share one implementation, and make the collection page's inline script call it. `FGS.saveToPhotos` arrives in Task 10. Until then, guard the call with `if (FGS.saveToPhotos)`.

Add the grid styles to `media.css`:

```css
.fgs-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(var(--fgs-tile,160px),1fr));gap:.75rem}
.fgs-tile{position:relative;margin:0}
.fgs-tile-open{display:block;width:100%;aspect-ratio:1;padding:0;border:0;border-radius:10px;overflow:hidden;background:#232321}
.fgs-tile-open img{width:100%;height:100%;object-fit:cover;display:block}
.fgs-tile-check{position:absolute;top:.4rem;left:.4rem}
.fgs-play{position:absolute;right:.4rem;bottom:2.6rem;background:rgba(0,0,0,.55);border-radius:50%;padding:.15rem .4rem}
.fgs-typeicon{display:flex;flex-direction:column;align-items:center;justify-content:center;height:100%;font-size:2.4rem;color:#9a9992}
.fgs-typeicon small{font-size:.7rem;padding:0 .4rem;word-break:break-all}
.fgs-tile figcaption{font-size:.8rem;line-height:1.3;margin-top:.35rem}
.fgs-name{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.fgs-meta{color:#9a9992}
.fgs-selectbar{position:sticky;top:0;z-index:5;display:flex;gap:.5rem;align-items:center;padding:.5rem;background:#1c1c1a;border:1px solid #2e2e2b;border-radius:10px;margin-bottom:.75rem}
```

- [ ] **Step 5: Add the nav link and trim the upload page.**
  - **`html_header.tmpl`:** after the Upload nav item at `:80-84`, add a Files item that copies the exact markup of the Upload item. Use `href="./files"`, label `Files`, and active when `eq .ActiveView 6`.
  - **`html_admin.tmpl`:** empty the server-rendered `{{ range .Items }}` rows inside `tbody#downloadtable`, keeping the table, its headers and its JS-built rows. Above the table add:

```
<p class="small text-muted mt-2 mb-1">Files you upload now appear below. Everything you have shared is in <a href="./files">Files</a>.</p>
```

Read `admin_ui_upload.js` first for anything that counts or reads server-rendered rows on load, such as DataTable init or "no files" placeholders, and make sure an empty tbody is handled. The DataTable must still initialise with zero rows.

- [ ] **Step 6: Run the tests.**

```bash
build/dev/gotest.sh ./internal/webserver/... 2>&1 | grep -E "^(--- FAIL|ok|FAIL)"
```

Expected: every package `ok`, including `TestFilesPage`, the new zip test, and the existing upload-page tests. If an existing upload-page test asserted on server-rendered rows, update it to assert on the Files page instead, and say so in the report.

- [ ] **Step 7: Commit.** Message: `files: a Files tab with Finder-style grid and list, separate from upload`, then push.

---

### Task 8: Viewer

**Files:**
- Modify: `media.js` (viewer), `media.css`, `html_media_shared.tmpl` (replace the empty `media_viewer`).

**Interfaces:**
- Produces:
  - `FGS.openViewer(items, index)`, where each item is `{id, name, view, video, download, uploader, taken, uploaded, size, w, h}`.
  - `FGS.itemFromEl(el)`, which reads those fields from `data-*` attributes.

- [ ] **Step 1: Write the viewer markup.**

```
{{ define "media_viewer" }}
<div id="fgs-viewer" class="fgs-viewer" role="dialog" aria-modal="true" aria-label="Viewer" hidden>
  <button class="fgs-v-close" type="button" aria-label="Close"><i class="bi bi-x-lg"></i></button>
  <button class="fgs-v-prev" type="button" aria-label="Previous"><i class="bi bi-chevron-left"></i></button>
  <div class="fgs-v-stage"></div>
  <button class="fgs-v-next" type="button" aria-label="Next"><i class="bi bi-chevron-right"></i></button>
  <div class="fgs-v-info">
    <div class="fgs-v-name"></div>
    <div class="fgs-v-meta"></div>
    <a class="btn btn-sm btn-light fgs-v-download" download>Download original</a>
  </div>
</div>
{{ end }}
```

- [ ] **Step 2: Write the viewer JS** (append inside the `media.js` IIFE).

```js
  FGS.itemFromEl = function (el) {
    var d = function (k) { return el.getAttribute("data-" + k) || ""; };
    return { id: d("id"), name: d("name"), view: d("view"), video: d("video"), download: d("download"),
      uploader: d("uploader"), taken: parseInt(d("taken"), 10) || 0, uploaded: parseInt(d("uploaded"), 10) || 0,
      size: d("size"), w: parseInt(d("w"), 10) || 0, h: parseInt(d("h"), 10) || 0 };
  };

  var v = null, items = [], at = 0, touchX = null;
  function fmt(ts) { return ts ? new Date(ts * 1000).toLocaleString([], { dateStyle: "medium", timeStyle: "short" }) : ""; }

  function render() {
    var it = items[at], stage = v.querySelector(".fgs-v-stage");
    stage.innerHTML = "";
    if (it.video) {
      var vid = document.createElement("video");
      vid.controls = true; vid.playsInline = true; vid.preload = "metadata";
      vid.poster = it.view; vid.src = it.video;
      vid.addEventListener("error", function () {
        // Browser cannot play this codec (e.g. HEVC in Firefox): poster + download.
        stage.innerHTML = "";
        var img = document.createElement("img"); img.src = it.view; img.alt = "";
        var note = document.createElement("p"); note.className = "fgs-v-note";
        note.textContent = "This browser can't play this video. Download the original to watch it.";
        stage.appendChild(img); stage.appendChild(note);
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
    var meta = [it.uploader, it.taken ? "Taken " + fmt(it.taken) : "Uploaded " + fmt(it.uploaded), it.size].filter(Boolean);
    v.querySelector(".fgs-v-meta").textContent = meta.join(" · ");
    var dl = v.querySelector(".fgs-v-download"); dl.href = it.download; dl.setAttribute("download", it.name);
    v.querySelector(".fgs-v-prev").disabled = at === 0;
    v.querySelector(".fgs-v-next").disabled = at === items.length - 1;
    // Warm the neighbour so arrowing feels instant
    [at - 1, at + 1].forEach(function (i) { if (items[i] && items[i].view) { (new Image()).src = items[i].view; } });
  }
  function go(delta) { var n = at + delta; if (n >= 0 && n < items.length) { at = n; render(); } }
  function close() { v.hidden = true; v.querySelector(".fgs-v-stage").innerHTML = ""; document.body.style.overflow = ""; }

  FGS.openViewer = function (list, index) {
    v = v || document.getElementById("fgs-viewer");
    if (!v) return;
    items = list; at = Math.max(0, index);
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
    if (e.key === "Escape") close();
    if (e.key === "ArrowLeft") go(-1);
    if (e.key === "ArrowRight") go(1);
  });
  document.addEventListener("touchstart", function (e) { if (v && !v.hidden) touchX = e.touches[0].clientX; }, { passive: true });
  document.addEventListener("touchend", function (e) {
    if (!v || v.hidden || touchX === null) return;
    var dx = e.changedTouches[0].clientX - touchX; touchX = null;
    if (Math.abs(dx) > 50) go(dx < 0 ? 1 : -1);
  }, { passive: true });
```

- [ ] **Step 3: Add the viewer styles to `media.css`.**

```css
.fgs-viewer{position:fixed;inset:0;z-index:3000;background:rgba(10,10,9,.96);display:flex;align-items:center;justify-content:center}
.fgs-viewer[hidden]{display:none}
.fgs-v-stage{max-width:100vw;max-height:calc(100vh - 7rem);display:flex;flex-direction:column;align-items:center}
.fgs-v-stage img,.fgs-v-stage video{max-width:100vw;max-height:calc(100vh - 7rem);object-fit:contain}
.fgs-v-close,.fgs-v-prev,.fgs-v-next{position:absolute;background:none;border:0;color:#f2f1ee;font-size:1.6rem;padding:1rem}
.fgs-v-close{top:0;right:0}.fgs-v-prev{left:0}.fgs-v-next{right:0}
.fgs-v-prev:disabled,.fgs-v-next:disabled{opacity:.2}
.fgs-v-info{position:absolute;bottom:0;left:0;right:0;padding:.75rem 1rem;display:flex;gap:1rem;align-items:center;flex-wrap:wrap;background:linear-gradient(transparent,rgba(0,0,0,.7))}
.fgs-v-name{font-weight:600}.fgs-v-meta{color:#9a9992;margin-right:auto}
.fgs-v-note{color:#9a9992;margin-top:1rem}
```

- [ ] **Step 4: Run Go tests and generate.** Run `build/dev/gotest.sh ./internal/webserver/ -run TestFilesPage -v 2>&1 | grep -E "^---"`. Add `fgs-viewer` to `TestFilesPage`'s `RequiredContent` first.

Expected: PASS, and a regenerated `media.min.*.js`.

- [ ] **Step 5: Commit.** Message: `viewer: full-screen photo and video viewer with swipe and keys`, then push.

---

### Task 9: Board layout on collection pages

**Files:**
- Modify:
  - `internal/webserver/Collections.go`: `CollectionView.Items []collectionItem`, filled in `showCollection` with preview info scoped to the collection.
  - `html_collection.tmpl`: a Board/List toggle (default List), the board container and the viewer.
  - `media.js`: `FGS.initBoard(el)`.
  - `media.css`.

**Interfaces:**
- Produces:
  - `collectionItem{File models.File; Preview previewInfo}`, where `Preview.Download` is `./cd?id=<c>&file=<f>`.
  - `FGS.initBoard(container)`. It lays out `.fgs-board-item` children in shortest-column order from `data-w`/`data-h`, loads the first 12 images eagerly at high priority and the rest through an IntersectionObserver. It relayouts on resize.

- [ ] **Step 1: Write the failing test.** GET `/c?id=<unprotected collection with a ready-preview member>` must contain `data-layout="list"` (the default), `fgs-board`, `./p/thumb?id=<file>&v=` and `&c=<collection>`.

```go
func TestCollectionBoardMarkup(t *testing.T) {
	f := accessFile("boardfile0000000001", func(f *models.File) { f.SHA1 = "boardsha1"; f.Name = "IMG.JPG" })
	database.SavePreview(models.Preview{Sha1: "boardsha1", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Color: "#808080", Updated: 1})
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "b", FileIds: []string{f.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{`data-layout-default="list"`, "fgs-board", "./p/thumb?id=" + f.Id, "&amp;c=" + col.Id, "fgs-viewer"},
	})
}
```

- [ ] **Step 2: Fill in `Items` in `showCollection`.** Do this where `CollectionView` is built for the unlocked, download view.

```go
type collectionItem struct {
	File    models.File
	Preview previewInfo
}
```

```go
	for _, f := range collection.Files {
		view.Items = append(view.Items, collectionItem{File: f,
			Preview: previewUrls(f, collection.Id, "./cd?id="+collection.Id+"&file="+f.Id)})
	}
```

- [ ] **Step 3: Update the template.** Above the table, add the toggle:

```
<div class="d-flex justify-content-end mb-2" data-fgs-layout-toggle data-layout-default="list">
  <div class="btn-group" role="group" aria-label="Layout">
    <button class="btn btn-sm btn-outline-light" data-layout="board">Board</button>
    <button class="btn btn-sm btn-light" data-layout="list">List</button>
  </div>
</div>
<div class="fgs-board d-none" id="fgs-board">
  {{ range .Items }}
  <figure class="fgs-board-item" data-id="{{ .File.Id }}" data-name="{{ .File.Name }}"
          data-download="{{ .Preview.Download }}" data-view="{{ .Preview.View }}" data-video="{{ .Preview.Video }}"
          data-taken="{{ .Preview.TakenAt }}" data-uploaded="{{ .File.UploadDate }}" data-size="{{ .File.Size }}"
          data-w="{{ .Preview.Width }}" data-h="{{ .Preview.Height }}" style="background:{{ .Preview.Color }}">
    {{ if .Preview.Thumb }}<img data-src="{{ .Preview.Thumb }}" alt="" decoding="async">
      {{ if .Preview.Video }}<span class="fgs-play"><i class="bi bi-play-fill"></i></span>{{ end }}
    {{ else }}<span class="fgs-typeicon"><i class="bi bi-file-earmark"></i><small>{{ .File.Name }}</small></span>{{ end }}
  </figure>
  {{ end }}
</div>
```

Wrap the existing table container in `id="fgs-collection-list"`. Each table row's filename cell becomes clickable, opening the viewer at that index. Add `{{ template "media_viewer" }}`. In the init script, after `initDownloadButtons`:

```js
FGS.initLayoutToggle(document.querySelector("[data-fgs-layout-toggle]"),
  document.getElementById("fgs-board"), document.getElementById("fgs-collection-list"), "collectionLayout");
```

- [ ] **Step 4: Write the board and layout-toggle JS in `media.js`.**

```js
  // Masonry by shortest column. Heights come from stored dimensions, so the
  // whole board is placed before any image arrives and nothing jumps.
  FGS.initBoard = function (board) {
    if (!board || board.dataset.ready) return;
    board.dataset.ready = "1";
    var tiles = Array.prototype.slice.call(board.querySelectorAll(".fgs-board-item"));
    function layout() {
      var width = board.clientWidth, gap = 8;
      var cols = Math.max(2, Math.round(width / 260));
      var colW = (width - gap * (cols - 1)) / cols, heights = [];
      for (var i = 0; i < cols; i++) heights.push(0);
      tiles.forEach(function (t) {
        var w = parseInt(t.dataset.w, 10), h = parseInt(t.dataset.h, 10);
        var tileH = (w && h) ? colW * h / w : colW;
        var c = heights.indexOf(Math.min.apply(null, heights));
        t.style.cssText += ";position:absolute;width:" + colW + "px;height:" + tileH + "px;left:" + (c * (colW + gap)) + "px;top:" + heights[c] + "px";
        heights[c] += tileH + gap;
      });
      board.style.height = Math.max.apply(null, heights) + "px";
    }
    function load(img, eager) {
      if (eager) img.setAttribute("fetchpriority", "high");
      img.src = img.dataset.src;
    }
    var imgs = tiles.map(function (t) { return t.querySelector("img[data-src]"); });
    imgs.slice(0, 12).forEach(function (img) { if (img) load(img, true); });
    if ("IntersectionObserver" in window) {
      var io = new IntersectionObserver(function (entries) {
        entries.forEach(function (e) { if (e.isIntersecting) { load(e.target, false); io.unobserve(e.target); } });
      }, { rootMargin: "1500px 0px" });
      imgs.slice(12).forEach(function (img) { if (img) io.observe(img); });
    } else {
      imgs.slice(12).forEach(function (img) { if (img) load(img, false); });
    }
    board.addEventListener("click", function (e) {
      var t = e.target.closest(".fgs-board-item");
      if (t) FGS.openViewer(tiles.map(FGS.itemFromEl), tiles.indexOf(t));
    });
    layout();
    var timer; window.addEventListener("resize", function () { clearTimeout(timer); timer = setTimeout(layout, 120); });
  };

  // Board/List toggle, remembered per page type.
  FGS.initLayoutToggle = function (toggle, board, list, key) {
    if (!toggle) return;
    function apply(layout) {
      board.classList.toggle("d-none", layout !== "board");
      list.classList.toggle("d-none", layout !== "list");
      toggle.querySelectorAll("[data-layout]").forEach(function (b) {
        var on = b.dataset.layout === layout;
        b.classList.toggle("btn-light", on); b.classList.toggle("btn-outline-light", !on);
      });
      if (layout === "board") FGS.initBoard(board);
      localStorage.setItem(key, layout);
    }
    toggle.addEventListener("click", function (e) { var b = e.target.closest("[data-layout]"); if (b) apply(b.dataset.layout); });
    apply(localStorage.getItem(key) || toggle.dataset.layoutDefault);
  };
```

`media.css`:

```css
.fgs-board{position:relative}
.fgs-board-item{margin:0;border-radius:8px;overflow:hidden;cursor:zoom-in}
.fgs-board-item img{width:100%;height:100%;object-fit:cover;display:block}
```

- [ ] **Step 5: Run the tests.** `build/dev/gotest.sh ./internal/webserver/ -run 'TestCollectionBoardMarkup|TestCollection' -v 2>&1 | grep -E "^---"`

Expected: all PASS. Then run `python3 build/e2e/collections_e2e.py` against a local instance; see Task 12, Step 1 for how to start one. It must stay green. Do this in Task 12 if no instance is available yet, and say so in the report.

- [ ] **Step 6: Commit.** Message: `collections: board layout with lazy thumbnails and the viewer`, then push.

---

### Task 10: Save to Photos (two-tap)

`navigator.share` needs a fresh tap. Fetching large originals after one tap and then calling `share()` is likely to be rejected by Safari, so there are two explicit steps: **Prepare N** (fetch, with progress), then **Save to Photos** (share). Batching limits and the big-video threshold are refined in Part 3.

**Files:**
- Modify:
  - `media.js`: `FGS.saveToPhotos`.
  - `html_media_shared.tmpl`: add a Save to Photos button to `media_download_buttons`, hidden by default.
  - `html_collection.tmpl`: nothing beyond the partial.

**Interfaces:**
- Produces: `FGS.saveToPhotos(items, buttonEl)`, where `items` is `[{url, name}]`. On the first tap it prepares; on the second it shares.

- [ ] **Step 1: Write the JS.**

```js
  var PHOTO_BATCH = 20;           // refined on-device in Part 3
  var VIDEO_MAX_BYTES = 500e6;    // larger videos go to the zip instead (spec, Part 3)

  FGS.saveToPhotos = function (list, button) {
    if (!navigator.canShare) { FGS.openHelpSheet(); return; }
    var state = button._fgs || (button._fgs = { files: null, next: 0 });
    if (!state.files) {
      button.disabled = true;
      var done = 0, files = [], skipped = 0;
      button.textContent = "Preparing 0/" + list.length + "…";
      list.reduce(function (p, it) {
        return p.then(function () {
          return fetch(it.url, { credentials: "same-origin" }).then(function (r) {
            var len = parseInt(r.headers.get("Content-Length") || "0", 10);
            if (/\.(mov|mp4|m4v)$/i.test(it.name) && len > VIDEO_MAX_BYTES) { skipped++; return null; }
            return r.blob();
          }).then(function (b) {
            if (b) files.push(new File([b], it.name, { type: b.type || "application/octet-stream" }));
            done++; button.textContent = "Preparing " + done + "/" + list.length + "…";
          });
        });
      }, Promise.resolve()).then(function () {
        state.files = files; state.next = 0; button.disabled = false;
        button.textContent = "Save to Photos" + (files.length > PHOTO_BATCH ? " (1/" + Math.ceil(files.length / PHOTO_BATCH) + ")" : "");
        if (skipped) alert(skipped + " large video(s) were left out — use Download as .zip for those.");
      });
      return;
    }
    var batch = state.files.slice(state.next, state.next + PHOTO_BATCH);
    if (!navigator.canShare({ files: batch })) { FGS.openHelpSheet(); return; }
    navigator.share({ files: batch }).then(function () {
      state.next += PHOTO_BATCH;
      if (state.next >= state.files.length) { button.textContent = "Saved ✓"; button._fgs = null; return; }
      button.textContent = "Save to Photos (" + (state.next / PHOTO_BATCH + 1) + "/" + Math.ceil(state.files.length / PHOTO_BATCH) + ")";
    }).catch(function () { /* user cancelled the sheet: keep state, they can tap again */ });
  };
```

**Counting check:** fetching originals through `/cd` counts as a download, which is correct, because saving to Photos is a download. Through `/p/video` it would not count. Always pass the download URL here, never `view` or `video`.

- [ ] **Step 2: Wire it into the collection buttons.** In `media_download_buttons`, add before the zip link:

```
  <button type="button" class="btn btn-light d-none" data-fgs-photos>Save to Photos</button>
```

In `FGS.initDownloadButtons`, inside the `isIOS()` branch:

```js
      var photos = root.querySelector("[data-fgs-photos]");
      if (photos && navigator.canShare) {
        photos.classList.remove("d-none");
        if (zip) zip.classList.replace("btn-light", "btn-outline-light");
        photos.addEventListener("click", function () {
          var links = Array.prototype.slice.call(document.querySelectorAll("#collectiontable a[data-fileid]"));
          FGS.saveToPhotos(links.map(function (a) { return { url: a.href, name: a.dataset.filename }; }), photos);
        });
      }
```

- [ ] **Step 3: Generate and test.** `build/dev/gotest.sh ./internal/webserver/ 2>&1 | tail -2`

Expected: `ok`. In the `/c` page test, add the assertion that `data-fgs-photos` is present.

- [ ] **Step 4: Commit.** Message: `save to Photos on iOS: prepare, then share in batches`, then push.

---

### Task 11: Upload default, sidebar icon, logout on /collections

**Files:**
- Modify:
  - `internal/webserver/web/static/js/admin_ui_upload.js:1123` (`setAutoLinkDefault`).
  - `internal/webserver/web/templates/html_collections_admin.tmpl`: logout. Fix it only if it is broken; check first.
  - Deployment, outside git: `~/apps/gokapi/custom/admin.js` and `~/apps/gokapi/custom/version.txt`.

- [ ] **Step 1: Default the toggle to on.** Change `getLocalStorageWithDefault("defaultAutoCollection","false")` to `getLocalStorageWithDefault("defaultAutoCollection","true")`. People who have explicitly saved "off" keep it off.

- [ ] **Step 2: Check logout on `/collections`.** With a local instance from Task 12, Step 1, open `/collections`, click Logout, and confirm the page shows the login screen. If logout fails or `doLogout is not defined` appears in the console, add before `{{ template "customjs" . }}` in `html_collections_admin.tmpl`:

```
<script src="./js/min/admin.min.{{ template "js_admin_version"}}.js"></script>
```

Then check that no function is now defined twice: the collections page has inline JS, so search for name clashes with `admin.min.js`.

- [ ] **Step 3: Add the sidebar icon.** Deployment directory, not in git.

```bash
cp ~/apps/gokapi/custom/admin.js ~/apps/gokapi/custom/admin.js.bak-2026-10-19
```

Edit `~/apps/gokapi/custom/admin.js`. In `icons` add `files: "bi-images",`, and in `iconsByLabel` add `"files": "bi-images",`. Increment the number in `~/apps/gokapi/custom/version.txt` by one.

- [ ] **Step 4: Run the tests and commit (repo changes only).** `build/dev/gotest.sh ./internal/webserver/... 2>&1 | tail -3`, then:

```bash
git add -A internal
git commit -m "upload: combine each upload into one link by default"
git push
```

---

### Task 12: Verify on a real browser and a copy of real data

This task changes nothing deployed. It produces evidence for Franco.

- [ ] **Step 1: Start a local instance with the deployment's hardening.** Use port 53900 with fresh volumes and the real compose limits.

```bash
export DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock
cd ~/src/Gokapi && docker build -q -t gokapi-fgs:previews-dev .
docker run -d --name gokapi-previews-test -p 127.0.0.1:53900:53842 \
  --security-opt no-new-privileges:true --cap-drop ALL --pids-limit 200 --memory 1g --cpus 1.5 \
  -e TZ=Europe/Zurich -e GOKAPI_LOG_STDOUT=true -v gokapi-previews-test-data:/app/data -v gokapi-previews-test-config:/app/config \
  gokapi-fgs:previews-dev
python3 build/e2e/setup_local.py && docker restart gokapi-previews-test && python3 build/e2e/collections_e2e.py
```

Read `setup_local.py`'s header for the port and environment it expects, and adjust the run flags if it differs. Expected: `collections_e2e.py` finishes green.

- [ ] **Step 2: Browser walkthrough with the Playwright MCP**, on desktop width and on an iPhone viewport (390×844, iOS user agent):
  - Upload a JPEG, an HEIC (generated with vips as in Task 3) and a MOV.
  - The "Combine…" toggle is on by default.
  - The copy-link / share / QR flow on the upload page still works.
  - In Files: the grid shows thumbnails within about 10 s; the list view, the My files / Everything switch and the icon size all work.
  - The viewer opens, arrows through the items and plays the MOV.
  - The `DownloadCount` of the MOV is unchanged after playing it.
  - The collection page defaults to List; switching to Board lays out without jumping; the viewer works from the board.
  - On the iPhone viewport: Save to Photos and Download as .zip are visible, "Download all" is hidden, and the "?" opens the help sheet.
  - The zip downloads, and its entries are byte-identical to the uploads. Check with `unzip -p` against the sources: `cmp`.
  - Logout works from every tab.

Save screenshots to `~/src/Gokapi/build/e2e/screens-part1/`. That directory is not committed; add it to `.gitignore`.

- [ ] **Step 3: Backfill dry run on a read-only copy of the real data.** No photos leave the mini, and none are committed.

The live volumes are `gokapi_gokapi_data` and `gokapi_gokapi_config`, with the compose project prefix. Copy **both** first. **Never mount a live volume into a second container.**

```bash
export DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock
docker run --rm -v gokapi_gokapi_data:/from:ro -v gokapi-previews-realcopy:/to alpine sh -c 'cp -a /from/. /to/'
docker run --rm -v gokapi_gokapi_config:/from:ro -v gokapi-previews-realconfig:/to alpine sh -c 'cp -a /from/. /to/'
docker run -d --name gokapi-previews-real -p 127.0.0.1:53901:53842 \
  --security-opt no-new-privileges:true --cap-drop ALL --pids-limit 200 --memory 1g --cpus 1.5 \
  -e TZ=Europe/Zurich -e GOKAPI_LOG_STDOUT=true \
  -v gokapi-previews-realcopy:/app/data -v gokapi-previews-realconfig:/app/config gokapi-fgs:previews-dev
```

Then wait in the foreground; the house rule allows no background jobs. Every 30 s, run `docker logs --tail 5 gokapi-previews-real` and `docker stats --no-stream gokapi-previews-real` (note the peak memory). Stop polling when no new `preview:` lines have appeared for 60 s. Then:

```bash
docker stop gokapi-previews-real && docker rm gokapi-previews-real
docker run --rm -v gokapi-previews-realcopy:/d alpine sh -c 'apk add -q sqlite >/dev/null; sqlite3 /d/gokapi.sqlite "SELECT Status, COUNT(*) FROM Previews GROUP BY Status;"; du -sh /d/previews /d'
docker logs gokapi-previews-real 2>&1 | grep "preview:"   # run before rm if you need the failure reasons
```

Expected:
- Counts per status, with the majority `ready`, `unsupported` for non-media, and `failed` listed with reasons from the logs.
- The size of `previews/` should be about 1–2 % of `du -sh /d`.
- Peak memory (`docker stats --no-stream` during the run) is well under 1 GiB.

Record the numbers for Franco. Then clean up:

```bash
docker volume rm gokapi-previews-realcopy gokapi-previews-realconfig
```

- [ ] **Step 4: Tear down the test instance.**

```bash
docker rm -f gokapi-previews-test; docker volume rm gokapi-previews-test-data gokapi-previews-test-config
```

---

### Task 13: Deploy (requires Franco's explicit go-ahead in the conversation)

**Do not start this task without a "yes, deploy" from Franco in the current conversation.** Schema 17→18 is one-way.

- [ ] **Step 1: Back up the database.**

```bash
export DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock
mkdir -p ~/apps/gokapi/backups
docker exec gokapi sh -c 'cat /app/data/gokapi.sqlite' > ~/apps/gokapi/backups/gokapi-2026-10-19-pre-previews.sqlite
ls -la ~/apps/gokapi/backups/
```

Check the file size is about what `docker exec gokapi ls -la /app/data/gokapi.sqlite` shows. WAL mode means a `-wal` file may hold recent writes. Prefer this instead:

```bash
docker exec gokapi sh -c 'apk add -q sqlite 2>/dev/null; sqlite3 /app/data/gokapi.sqlite ".backup /app/data/backup-pre-previews.sqlite"'
```

If `apk` is unavailable in the hardened container, run the backup from a sidecar that mounts the volume instead.

- [ ] **Step 2: Build under a new tag.** `gokapi-fgs:collections` stays available for rollback.

```bash
cd ~/src/Gokapi && git log --oneline -1 && docker build -q -t gokapi-fgs:previews .
cp ~/apps/gokapi/docker-compose.yml ~/apps/gokapi/docker-compose.yml.bak.20261019
sed -i '' 's/image: gokapi-fgs:collections/image: gokapi-fgs:previews/' ~/apps/gokapi/docker-compose.yml
cd ~/apps/gokapi && docker compose up -d && sleep 5 && docker logs --tail 30 gokapi
```

- [ ] **Step 3: Smoke test** `https://share.franco.international`:
  - log in;
  - Files tab with thumbnails;
  - an existing collection link on the iPhone viewport (Playwright);
  - the notifier log `~/apps/gokapi/notify.log` shows no new burst of warning emails.

- [ ] **Step 4: Rollback, if needed.**

```bash
cd ~/apps/gokapi && cp docker-compose.yml.bak.20261019 docker-compose.yml
docker compose up -d
```

Then restore the database from Step 1 into the volume while the container is stopped. The old binary refuses schema 18, so the database restore is required, not optional.

- [ ] **Step 5: Open a PR** from `feature/previews` to `feature/collections` with `gh pr create`. Link it with the t3-code `link_pull_request` tool. The body summarises the parts shipped, the backfill numbers from Task 12 and the rollback steps, and ends with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

---

## Self-review against the spec (Part 1)

| Spec requirement | Task |
|---|---|
| Worker: single, exec vips/ffmpeg, timeouts, inside container limits | 3, 4, 12 |
| Thumbnail ~640 WebP, preview ~2000 JPEG, poster | 3 |
| Rotated dimensions, taken-at, duration, colour, perceptual hash | 3 |
| `data/previews/`, `Previews` table by SHA1, statuses | 2, 4 |
| Backfill of existing files | 4, 12 |
| Delete with the last blob | 4 |
| Formats and type icons; e2e gets no preview | 3, 7 |
| Failure never blocks upload; failures do not email | 4 |
| `CanView` with all five grant paths | 5 |
| Preview routes by file ID with version, caching rules | 5 |
| `/d` page preview | 6 |
| Files tab: grid, list, columns, My files / Everything, actions, multi-select | 7 |
| Upload page goes back to uploading only | 7 |
| Viewer: swipe/keys, video from original, unplayable fallback, info, Download original | 8 |
| Board: layout first, first 12 high priority, lazy rest; collection default List | 9 |
| Continuous scroll in batches of ~100 | Part 2 (collections are bounded in size; group boards are where batching applies). Noted, not a gap. |
| Zip: store-only, streaming, taken-at times, `CanView` | 1, 5, 7 |
| Device buttons: desktop vs iPhone | 1, 10 |
| Phone help sheet | 1 |
| Upload button keeps originals | Not separately tasked. Verified 2026-10-19: neither `admin_ui_upload.js` nor `html_admin.tmpl` sets `acceptedFiles` or `accept`, so the iOS picker is not asked to convert. Task 12 confirms an uploaded HEIC stays HEIC (fingerprint check). |
| Combine toggle default on | 11 |
| Testing: `CanView` matrix, fixtures, zip byte-equality, Playwright | 1, 3, 5, 12 |
