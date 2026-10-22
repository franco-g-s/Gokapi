//go:build !integration && test

package webserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// filesAdminSession is a session for user 5, the super admin, who holds
// PERM_LIST. validsession is user 7, who has no permissions at all.
func filesAdminSession() test.Cookie {
	database.SaveSession("filesadmin", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 5})
	return test.Cookie{Name: "session_token", Value: "filesadmin"}
}

func TestFilesPage(t *testing.T) {
	accessFile("filesmine", nil)
	// The admin (PERM_LIST) gets the My files / Everything switch. The page
	// renders My files, the switch's default (Part 3); Everything, with
	// other users' files including user 7's, comes from the feed.
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`id="fgs-files"`, `data-fgs-browser`, `data-context="files"`, "My files", "Everything", `data-default-view="grid"`,
			`data-layout="grid"`, `data-layout="board"`, `data-layout="list"`,
			`data-id="unlimitedDownload"`, `id="fgs-files-table"`, `href="./files"`, `id="fgs-files-selstatus"`, `id="fgs-viewer"`, `data-kind="`, `data-act="link"`, `id="fgs-files-linkresult"`,
			"<th>Uploaded by</th>"},
		ExcludedContent: []string{`data-id="filesmine"`},
	})
	code, body := filesGet(t, filesAdminSession().Value, "/files/list?src=files&scope=all&limit=all")
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualBool(t, bytes.Contains(body, []byte(`"Id":"filesmine"`)), true)
	test.IsEqualBool(t, bytes.Contains(body, []byte(`"Bytes":1,`)), true)
	// A user without PERM_LIST gets neither the switch nor anyone else's files
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/files",
		IsHtml:          true,
		Cookies:         []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`id="fgs-files"`, `data-context="files"`, `data-default-view="grid"`, `data-id="filesmine"`},
		ExcludedContent: []string{"My files", "Everything", "unlimitedDownload"},
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/files", IsHtml: true,
		RequiredContent: []string{"Login"}, // not logged in -> login page
	})
}

// The Files tab lists exactly what the upload table used to list: no expired,
// exhausted, pending-deletion or file-request files.
func TestFilesPageFiltersLikeUploadTable(t *testing.T) {
	accessFile("filesexpired", func(f *models.File) { f.ExpireAt = time.Now().Add(-time.Hour).Unix() })
	accessFile("filesexhausted", func(f *models.File) { f.UnlimitedDownloads = false; f.DownloadsRemaining = 0 })
	accessFile("filespending", func(f *models.File) { f.PendingDeletion = time.Now().Add(time.Hour).Unix() })
	accessFile("filesrequest", func(f *models.File) { f.UploadRequestId = "somerequest" })
	accessFile("filesneverexp", func(f *models.File) { f.ExpireAt = 0; f.UnlimitedTime = true })
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/files",
		IsHtml:          true,
		Cookies:         []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`data-id="filesneverexp"`},
		ExcludedContent: []string{"filesexpired", "filesexhausted", "filespending", "filesrequest"},
	})
}

// The upload page no longer renders stored files; they live on the Files tab
func TestUploadPageDoesNotListStoredFiles(t *testing.T) {
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/admin",
		IsHtml:          true,
		Cookies:         []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`data-context="upload"`, `<a href="./files">Files</a>`},
		ExcludedContent: []string{`id="row-unlimitedDownload"`, `data-id="unlimitedDownload"`, `data-row-id="unlimitedDownload"`},
	})
}

// The upload page's "uploaded now" list is the shared file list, empty until
// something is uploaded, with the dialogs its actions need. The old table is
// gone, so the custom branding's #maintable bar finds nothing.
func TestUploadPageUsesFileBrowser(t *testing.T) {
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/admin",
		IsHtml:  true,
		Cookies: []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`id="fgs-upload"`, `data-fgs-browser`, `data-context="upload"`,
			`data-default-view="grid"`, `data-count-label="Uploaded now: "`, `data-zip-url="./files/zip?ids="`,
			`data-act="link"`, `data-act="add-to-collection"`, `data-act="change-expiry"`,
			`id="fgs-collect-modal"`, `id="fgs-expiry-modal"`, `id="fgs-viewer"`,
			`template data-fgs-tpl="tile"`, `id="modaledit"`, `id="toastnotificationUndo"`,
			"Files you upload now appear below."},
		ExcludedContent: []string{`id="maintable"`, `id="downloadtable"`, "Downloads remaining"},
	})
}

func filesZip(t *testing.T, token, ids string) (int, string, []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://localhost:53843/files/zip?ids="+ids, nil)
	test.IsNil(t, err)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	resp, err := http.DefaultClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	test.IsNil(t, err)
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

func zipEntries(t *testing.T, body []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	test.IsNil(t, err)
	names := make([]string, 0)
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func TestFilesZipOnlyOwnUnlessPermList(t *testing.T) {
	accessFile("fileszipown", func(f *models.File) { f.Name = "own.jpg" })

	// User 7 (no PERM_LIST) asking for user 5's file gets nothing
	code, _, body := filesZip(t, "validsession", "unlimitedDownload")
	test.IsEqualInt(t, code, http.StatusGone)
	test.IsEqualBool(t, bytes.Contains(body, []byte("Nothing left to download")), true)

	// ...but their own file is served, and a foreign ID in the same request is dropped
	code, ct, body := filesZip(t, "validsession", "fileszipown,unlimitedDownload")
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, ct, "application/zip")
	names := zipEntries(t, body)
	test.IsEqualInt(t, len(names), 1)
	test.IsEqualString(t, names[0], "own.jpg")

	// The admin holds PERM_LIST and may zip anyone's files
	code, ct, body = filesZip(t, filesAdminSession().Value, "fileszipown,unlimitedDownload")
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, ct, "application/zip")
	test.IsEqualInt(t, len(zipEntries(t, body)), 2)

	// Not logged in: refused like every other non-UI protected call
	resp, err := http.Get("http://localhost:53843/files/zip?ids=fileszipown")
	test.IsNil(t, err)
	resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, http.StatusUnauthorized)
}

func filesGet(t *testing.T, token, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://localhost:53843"+url, nil)
	test.IsNil(t, err)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	resp, err := http.DefaultClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	test.IsNil(t, err)
	return resp.StatusCode, body
}

func downloadState(t *testing.T, id string) (int, int) {
	t.Helper()
	f, ok := database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	return database.GetDownloadsRemaining(id), f.DownloadCount
}

// Ruling 18: the owner (or a PERM_LIST admin) fetching from the Files tab
// does not spend the download budget of the links they shared.
func TestFilesDownloadsAreNotCounted(t *testing.T) {
	limited := accessFile("filesdlcapped", func(f *models.File) {
		f.Name = "capped.jpg"
		f.SHA1 = "filesdlcappedsha"
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 2
	})
	pw := accessFile("filesdlpassword", func(f *models.File) { f.SHA1 = "filesdlpwsha"; f.PasswordHash = "x" })
	e2e := accessFile("filesdle2e", func(f *models.File) { f.SHA1 = "filesdle2esha"; f.Encryption.IsEndToEndEncrypted = true })

	for i := 0; i < 3; i++ {
		code, body := filesGet(t, "validsession", "/files/download?id="+limited.Id)
		test.IsEqualInt(t, code, http.StatusOK)
		test.IsEqualInt(t, len(body), 4096)
		code, _, body = filesZip(t, "validsession", limited.Id)
		test.IsEqualInt(t, code, http.StatusOK)
		test.IsEqualInt(t, len(zipEntries(t, body)), 1)
	}
	remaining, count := downloadState(t, limited.Id)
	test.IsEqualInt(t, remaining, 2)
	test.IsEqualInt(t, count, 0)

	// The owner needs no password for their own file
	code, _ := filesGet(t, "validsession", "/files/download?id="+pw.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	// The admin holds PERM_LIST and may fetch user 7's files, also uncounted
	code, _ = filesGet(t, filesAdminSession().Value, "/files/download?id="+limited.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	remaining, count = downloadState(t, limited.Id)
	test.IsEqualInt(t, remaining, 2)
	test.IsEqualInt(t, count, 0)

	// Someone else's file without PERM_LIST, an end-to-end encrypted file and
	// an unknown id all look the same: 404
	code, body := filesGet(t, "validsession", "/files/download?id=unlimitedDownload")
	test.IsEqualInt(t, code, http.StatusNotFound)
	test.IsEqualBool(t, bytes.Contains(body, []byte("def")), false)
	code, _ = filesGet(t, "validsession", "/files/download?id="+e2e.Id)
	test.IsEqualInt(t, code, http.StatusNotFound)
	code, _ = filesGet(t, "validsession", "/files/download?id=doesnotexist")
	test.IsEqualInt(t, code, http.StatusNotFound)
	// Not logged in: refused like every other non-UI protected call
	code, _ = filesGet(t, "", "/files/download?id="+limited.Id)
	test.IsEqualInt(t, code, http.StatusUnauthorized)

	// The tiles point at the uncounted route, password-protected ones included;
	// only end-to-end encrypted files have no direct link
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`data-download="./files/download?id=filesdlcapped"`,
			`data-download="./files/download?id=filesdlpassword"`},
		ExcludedContent: []string{`data-download="./files/download?id=filesdle2e"`, `data-download="./downloadFile`},
	})
}

// The public collection zip still counts, exactly like /cd.
func TestCollectionZipStillCounts(t *testing.T) {
	limited := accessFile("czcounted", func(f *models.File) {
		f.SHA1 = "czcountedsha"
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 2
	})
	collection, err := storage.CreateCollection(storage.CollectionParameters{FileIds: []string{limited.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	code, _ := filesGet(t, "", "/cz?id="+collection.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	remaining, count := downloadState(t, limited.Id)
	test.IsEqualInt(t, remaining, 1)
	test.IsEqualInt(t, count, 1)
}

// Ruling 21: "Create one link" posts the selection to /collections/create,
// which keeps only files the user may list and reports errors as JSON.
func TestFilesCreateOneLink(t *testing.T) {
	own := accessFile("fileslinkown", nil)
	e2e := accessFile("fileslinke2e", func(f *models.File) { f.SHA1 = "fileslinke2esha"; f.Encryption.IsEndToEndEncrypted = true })
	post := func(ids ...string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"name": "2 files", "fileIds": ids})
		req, err := http.NewRequest("POST", "http://localhost:53843/collections/create", bytes.NewReader(body))
		test.IsNil(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "validsession"})
		resp, err := http.DefaultClient.Do(req)
		test.IsNil(t, err)
		defer resp.Body.Close()
		result := map[string]any{}
		test.IsNil(t, json.NewDecoder(resp.Body).Decode(&result))
		return resp.StatusCode, result
	}

	// Another user's file is dropped from the link, not added to it
	code, result := post(own.Id, "unlimitedDownload")
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, result["Result"].(string), "OK")
	test.IsEqualInt(t, int(result["Files"].(float64)), 1)
	test.IsEqualBool(t, strings.Contains(result["Url"].(string), "c?id="), true)

	// Nothing listable, or an end-to-end encrypted file: an error to show
	code, result = post("unlimitedDownload")
	test.IsEqualInt(t, code, http.StatusBadRequest)
	test.IsEqualString(t, result["Result"].(string), "error")
	code, result = post(own.Id, e2e.Id)
	test.IsEqualInt(t, code, http.StatusBadRequest)
	test.IsEqualBool(t, result["ErrorMessage"].(string) != "", true)
}

// R4: select mode. Checkboxes appear only after "Select", in both views, and
// the icon-size slider belongs to the grid.
func TestFilesPageSelectMode(t *testing.T) {
	accessFile("filesselect", func(f *models.File) { f.Name = "select.jpg" })
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`data-fgs-select-toggle`, `>Select</button>`, `class="fgs-grid-only`,
			`class="form-check-input fgs-row-check"`, `<th class="fgs-col-check" data-fgs-nosort>`,
			// the name opens the viewer: a button styled as a link
			`<button type="button" class="btn btn-link fgs-name-open"`, `<span class="fgs-row-name" id="cell-name-filesselect">select.jpg</span>`},
	})
}

// R4: one "⋯" menu per file in both views, list thumbnails, Square / True shape.
func TestFilesPageFileMenuAndThumbs(t *testing.T) {
	f := accessFile("filesthumb", func(f *models.File) { f.SHA1 = "filesthumbsha"; f.Name = "thumb.jpg" })
	database.SavePreview(models.Preview{Sha1: "filesthumbsha", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Color: "#808080", Updated: 1})
	accessFile("filesnothumb", func(f *models.File) { f.SHA1 = "filesnothumbsha"; f.Name = "notes.pdf" })
	page := test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`fgs-shape-toggle`, `data-shape="square"`, `data-shape="true"`, ">Square<", ">True shape<",
			`<img class="fgs-list-thumb" loading="lazy" decoding="async" src="./p/thumb?id=` + f.Id,
			// a file without a preview gets a type icon instead
			`<span class="fgs-list-thumb fgs-list-icon" aria-hidden="true"><i class="bi bi-file-earmark-pdf"></i></span>`,
			`data-fgs-file-menu`, `data-menu="open"`, `data-menu="download"`, `data-menu="copy"`, `data-menu="share"`,
			`data-menu="qr"`, `data-menu="edit"`, `data-menu="replace"`, `data-menu="add-to-collection"`, `data-menu="delete"`,
			`data-menu="hotlink"`, `data-menu="email"`, `href="mailto:?body=`, "registerChangeHandler();",
			"Download original", "Copy link", "Copy hotlink", "Share…", "QR code", "Email", "Replace content", "Add to collection…",
			// the visible copy icon next to the list's menu, with the upload page's id
			`id="url-button-filesthumb"`,
			// user 7 owns the file but holds no permissions: edit yes, replace no
			`data-can-edit="true"`, `data-can-delete="true"`, `data-can-replace="false"`},
	}
	test.HttpPageResult(t, page)
	// Two menus per file, one per view, both from the same sub-template
	resp, body := filesGet(t, "validsession", "/files")
	test.IsEqualInt(t, resp, http.StatusOK)
	test.IsEqualInt(t, bytes.Count(body, []byte(`data-fgs-file-menu data-id="filesthumb"`)), 2)
}

// R4: files without a preview get an icon for their type, by extension
func TestFileTypeIcon(t *testing.T) {
	for name, icon := range map[string]string{
		"notes.pdf": "bi-file-earmark-pdf", "IMG_1.HEIC": "bi-file-earmark-image", "clip.mov": "bi-file-earmark-play",
		"song.mp3": "bi-file-earmark-music", "backup.tar.gz": "bi-file-earmark-zip", "readme.txt": "bi-file-earmark-text",
		"cv.docx": "bi-file-earmark-word", "sums.xlsx": "bi-file-earmark-spreadsheet", "talk.pptx": "bi-file-earmark-slides",
		"main.go": "bi-file-earmark-code", "noextension": "bi-file-earmark", "weird.xyz": "bi-file-earmark",
	} {
		test.IsEqualString(t, fileTypeIcon(name), icon)
	}
}

// R4: the bulk actions in select mode.
func TestFilesPageBulkActions(t *testing.T) {
	accessFile("filesbulk", nil)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`data-act="download"`, `data-act="zip"`, `data-act="photos"`, `data-act="link"`,
			`data-act="add-to-collection"`, `data-act="change-expiry"`, `data-act="delete"`,
			"Create one link", "Add to collection…", "Change expiry &amp; limits",
			`id="fgs-collect-modal"`, `id="fgs-expiry-modal"`, `data-server-url="`},
	})
}

// R4: "Add to collection…" posts addFileIds to /collections/modify. A file the
// user may not list is dropped there as well, so it never joins the
// collection. Characterization: the server already enforced this.
func TestFilesAddToCollectionOnlyListable(t *testing.T) {
	own := accessFile("filesaddown", nil)
	other := accessFile("filesaddown2", nil)
	collection, err := storage.CreateCollection(storage.CollectionParameters{Name: "mine", FileIds: []string{own.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	modify := func(ids ...string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"id": collection.Id, "addFileIds": ids})
		req, err := http.NewRequest("POST", "http://localhost:53843/collections/modify", bytes.NewReader(body))
		test.IsNil(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "validsession"})
		resp, err := http.DefaultClient.Do(req)
		test.IsNil(t, err)
		defer resp.Body.Close()
		result := map[string]any{}
		test.IsNil(t, json.NewDecoder(resp.Body).Decode(&result))
		return resp.StatusCode, result
	}
	code, result := modify("unlimitedDownload", other.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualInt(t, int(result["Files"].(float64)), 2)
	saved, ok := database.GetCollection(collection.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, strings.Contains(strings.Join(saved.FileIds, ","), "unlimitedDownload"), false)
	// ...and the page it is picked from never offered it in the first place
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/files", IsHtml: true,
		Cookies:         []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`data-id="filesaddown"`},
		ExcludedContent: []string{"unlimitedDownload"},
	})
}

// R4 fix 1: the Files tab's "Add to collection…" picker asks for ?mine=1 and
// gets only the caller's own collections, even with PERM_LIST. Without the
// parameter the list is unchanged.
func TestCollectionsListMine(t *testing.T) {
	friendFile := accessFile("fileslistfriend", nil) // user 7's
	friends, err := storage.CreateCollection(storage.CollectionParameters{Name: "friend's", FileIds: []string{friendFile.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	mine, err := storage.CreateCollection(storage.CollectionParameters{Name: "admin's", FileIds: []string{"unlimitedDownload"}}, models.User{Id: 5})
	test.IsNil(t, err)
	list := func(url string) string {
		code, body := filesGet(t, filesAdminSession().Value, url)
		test.IsEqualInt(t, code, http.StatusOK)
		return string(body)
	}
	all := list("/collections/list")
	test.IsEqualBool(t, strings.Contains(all, friends.Id), true)
	test.IsEqualBool(t, strings.Contains(all, mine.Id), true)
	own := list("/collections/list?mine=1")
	test.IsEqualBool(t, strings.Contains(own, friends.Id), false)
	test.IsEqualBool(t, strings.Contains(own, mine.Id), true)
}

// The Files tab is the shared component, context "files"
func TestFilesPageIsFileBrowser(t *testing.T) {
	accessFile("filesbrowser01", func(f *models.File) { f.Name = "browser.jpg" })
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`data-fgs-browser`, `data-context="files"`, `data-default-view="grid"`,
			`data-layout="grid"`, `data-layout="board"`, `data-layout="list"`, ">Grid<", ">Board<", ">List<",
			`data-zip-url="./files/zip?ids="`, `data-info-url="./files/previewinfo?id="`,
			`data-fgs-tpl="tile"`, `data-fgs-tpl="row"`, "No files yet",
			`id="fgs-files-selstatus"`, `id="fgs-files-linkresult"`, `data-preview-status="`},
		ExcludedContent: []string{`data-menu="remove-from-collection"`, `data-act="remove-from-collection"`, "initFilesPage"},
	})
}

// Live adds need the templates outside the table, so DataTables never sees them
func TestFileBrowserTemplatesOutsideTable(t *testing.T) {
	_, body := filesGet(t, "validsession", "/files")
	tableEnd := bytes.Index(body, []byte("</table>"))
	tpl := bytes.Index(body, []byte(`data-fgs-tpl="row"`))
	test.IsEqualBool(t, tableEnd > 0 && tpl > tableEnd, true)
}
