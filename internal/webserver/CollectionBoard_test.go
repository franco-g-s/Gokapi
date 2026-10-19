//go:build test

package webserver

import (
	"os"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// everything a management page has and a visitor must never be offered
var collectionPublicExcluded = []string{
	// the bundle that would supply the actions, and what it needs
	"admin.min.", "datatables", "dropzone", "qrcode.min.js",
	// management actions: selection bar, "⋯" menu, scope switch
	`data-act="link"`, `data-act="add-to-collection"`, `data-act="change-expiry"`, `data-act="delete"`,
	`data-act="remove-from-collection"`, `data-menu="edit"`, `data-menu="replace"`, `data-menu="copy"`,
	`data-menu="share"`, `data-menu="qr"`, `data-menu="delete"`, `data-menu="add-to-collection"`,
	"data-scope", "copyurl", "data-clipboard-text",
	// management data and singletons
	"data-can-edit", "data-url=", "fgs-collect-modal", "fgs-expiry-modal", "toastnotification", "qroverlay",
	// the old board and table
	"fgs-board-item", "collectiontable", "data-fgs-layout-toggle",
}

// C5: the public collection page is the shared file list, context
// collection-public: List by default, thumbnails in the list, Select with
// Download / zip / Save to Photos, and nothing a visitor must not do.
func TestCollectionBoardMarkup(t *testing.T) {
	f := accessFile("boardfile0000000001", func(f *models.File) { f.SHA1 = "boardsha1"; f.Name = "IMG.JPG" })
	g := accessFile("boardfile0000000002", func(f *models.File) { f.SHA1 = "boardsha2"; f.Name = "notes.txt" })
	database.SavePreview(models.Preview{Sha1: "boardsha1", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Color: "#808080", Updated: 1})
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "b", FileIds: []string{f.Id, g.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{
			// the component, in its context, List first, scoped to this collection
			"data-fgs-browser", `data-context="collection-public"`, `data-default-view="list"`, `data-current-view="list"`,
			`data-collection-id="` + col.Id + `"`,
			`data-zip-url="./cz?id=` + col.Id + `&amp;files="`, `data-info-url="./p/info?c=` + col.Id + `&amp;id="`,
			// thumbnails in the list, and in the grid, scoped with &c=
			`class="fgs-list-thumb"`, "./p/thumb?id=" + f.Id, "&amp;c=" + col.Id, "fgs-list-icon",
			// counted downloads through the collection
			`data-download="./cd?id=` + col.Id + `&amp;file=` + f.Id + `"`, `data-download="./cd?id=` + col.Id + `&amp;file=` + g.Id + `"`,
			// Select mode and its three visitor actions, in table order
			"data-fgs-select-toggle", `data-layout="grid"`, `data-layout="board"`, `data-layout="list"`,
			`data-act="download"`, `data-act="zip"`, `data-act="photos"`, "fgs-selectbar",
			`data-menu="open"`, `data-menu="download"`,
			// the page's own parts: wide layout, top buttons, viewer, help sheet
			"fgs-viewer", "fgs-help-sheet", "fgs-collection-wide", "data-fgs-downloads", "data-fgs-download-all",
			"data-fgs-photos", "data-fgs-download-zip", `href="./cz?id=` + col.Id + `"`, "Download all 2 files",
			`data-bytes="1"`,
			// G2: the Back button is in the markup but hidden until the script finds a same-origin referrer
			`class="btn btn-sm btn-outline-light d-none" data-fgs-back`, `<i class="bi bi-arrow-left"></i> Back`,
			"FGS.initBackButton(",
		},
		ExcludedContent: collectionPublicExcluded,
	})
}

// G2: the Back button's rule lives in media.js: same-origin referrer, not this page
func TestCollectionBackButtonScript(t *testing.T) {
	js, err := os.ReadFile("web/static/js/media.js")
	test.IsNil(t, err)
	for _, want := range []string{"FGS.initBackButton = function", "ref.origin !== page.origin", "ref.pathname === page.pathname", "window.history.back()", "window.location.href = ref.href"} {
		test.IsEqualBool(t, strings.Contains(string(js), want), true)
	}
}

// The file count is the list's own ("Files: n"); the header no longer says it again
func TestCollectionPageShowsFileCountOnce(t *testing.T) {
	f := accessFile("boardfile0000000005", func(f *models.File) { f.SHA1 = "boardsha5" })
	g := accessFile("boardfile0000000006", func(f *models.File) { f.SHA1 = "boardsha6" })
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "count", FileIds: []string{f.Id, g.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{`data-count-label="Files: "`, `class="fgs-count`, "<h3 class=\"card-title mb-1\">count</h3>"},
		ExcludedContent: []string{"2 files &middot;"},
	})
	resp, body := filesGet(t, "", "/c?id="+col.Id)
	test.IsEqualInt(t, resp, 200)
	// Only the "Download all 2 files" button still says it
	test.IsEqualInt(t, strings.Count(string(body), "2 files"), 1)
}

// Even the collection's owner, logged in, sees the visitor's page on /c
func TestCollectionPublicHasNoManagementForOwner(t *testing.T) {
	f := accessFile("boardfile0000000003", func(f *models.File) { f.SHA1 = "boardsha3" })
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "own", FileIds: []string{f.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		Cookies:         []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`data-context="collection-public"`, `data-mine="false"`},
		ExcludedContent: collectionPublicExcluded,
	})
}

// The password page is the card alone: no list, viewer or help sheet
func TestCollectionPasswordPageHasNoBrowser(t *testing.T) {
	f := accessFile("boardfile0000000004", func(f *models.File) { f.SHA1 = "boardsha4" })
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "pw", FileIds: []string{f.Id}, Password: "pw"}, models.User{Id: 7})
	test.IsNil(t, err)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{"This collection is password protected.", `name="password"`},
		ExcludedContent: []string{"data-fgs-browser", "file_browser", "fgs-viewer", "fgs-help-sheet", "data-fgs-downloads", "boardfile0000000004", "data-fgs-back", "initBackButton"},
	})
}

// R5: a name in the Collections tab opens the collection in the same tab, the
// collection page widens on desktop, and the password page stays narrow.
func TestCollectionNamesLinkAndWidePage(t *testing.T) {
	f := accessFile("r5namefile0000000001", nil)
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "r5-linked-name", FileIds: []string{f.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	locked, err := storage.CreateCollection(storage.CollectionParameters{Name: "r5-locked", FileIds: []string{f.Id}, Password: "pw"}, models.User{Id: 7})
	test.IsNil(t, err)

	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/collections", IsHtml: true,
		Cookies:         []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`<a class="collection-name-link" href="./c?id=` + col.Id + `">r5-linked-name</a>`},
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{`class="file-card card fgs-collection-wide"`},
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + locked.Id, IsHtml: true,
		RequiredContent: []string{"This collection is password protected."},
		ExcludedContent: []string{"fgs-collection-wide"},
	})

	// Wide only from 992px up, so smaller screens keep the 450px card
	css, err := os.ReadFile("web/static/css/media.css")
	test.IsNil(t, err)
	rule := "@media (min-width:992px){.file-card.fgs-collection-wide{max-width:min(1280px,100% - 2rem)}}"
	test.IsEqualBool(t, strings.Contains(string(css), rule), true)
}
