//go:build !integration && test

package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// The per-page action table from the spec ("One file list everywhere"):
// every context gets exactly these "⋯" entries and selection-bar actions, in
// this order, and its default view.
func TestFileBrowserActionTable(t *testing.T) {
	manageMenu := []string{"open", "download", "copy", "share", "qr", "edit", "replace", "add-to-collection", "delete"}
	manageBulk := []string{"download", "zip", "photos", "link", "add-to-collection", "change-expiry", "delete"}
	want := map[string]struct {
		menu, bulk []string
		view       string
	}{
		ctxFiles:            {manageMenu, manageBulk, "grid"},
		ctxUpload:           {manageMenu, manageBulk, "grid"},
		ctxCollectionAdmin:  {append(append([]string{}, manageMenu...), "remove-from-collection"), append(append([]string{}, manageBulk...), "remove-from-collection"), "list"},
		ctxFileRequest:      {[]string{"open", "download", "delete"}, []string{"download", "zip", "photos", "delete"}, "list"},
		ctxCollectionPublic: {[]string{"open", "download"}, []string{"download", "zip", "photos"}, "list"},
	}
	test.IsEqualInt(t, len(want), 5)
	for context, w := range want {
		b := newFileBrowser("b-"+context, context, nil, models.User{Id: 7})
		test.IsEqualString(t, b.Context, context)
		test.IsEqualString(t, b.Id, "b-"+context)
		test.IsEqualString(t, strings.Join(b.Menu, ","), strings.Join(w.menu, ","))
		test.IsEqualString(t, strings.Join(b.Bulk, ","), strings.Join(w.bulk, ","))
		test.IsEqualString(t, b.DefaultView, w.view)
		test.IsEqualBool(t, b.HasMenu("open"), true)
		test.IsEqualBool(t, b.HasMenu("edit"), context == ctxFiles || context == ctxUpload || context == ctxCollectionAdmin)
		test.IsEqualBool(t, b.HasBulk("remove-from-collection"), context == ctxCollectionAdmin)
	}
	// The scope switch is only for the Files tab, and only with PERM_LIST
	admin := models.User{Id: 5, Permissions: models.UserPermissionAll}
	test.IsEqualBool(t, newFileBrowser("x", ctxFiles, nil, admin).ShowScope, true)
	test.IsEqualBool(t, newFileBrowser("x", ctxFiles, nil, models.User{Id: 7}).ShowScope, false)
	test.IsEqualBool(t, newFileBrowser("x", ctxCollectionAdmin, nil, admin).ShowScope, false)
	test.IsEqualBool(t, newFileBrowser("x", ctxUpload, nil, admin).ShowScope, false)
}

// The item builder: per-context download URL, permissions and previews
func TestBrowserItemFromFile(t *testing.T) {
	f := accessFile("browseritem000001", func(f *models.File) { f.SHA1 = "browseritemsha"; f.Name = "pic.jpg" })
	database.SavePreview(models.Preview{Sha1: "browseritemsha", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 4, Height: 3, Color: "#123456", TakenAt: 99, Updated: 1})
	names := map[int]string{7: "Seven"}
	r := sessionRequest("validsession")

	own := browserItemFromFile(r, f, models.User{Id: 7}, ctxFiles, "", names)
	test.IsEqualString(t, own.DownloadUrl, "./files/download?id="+f.Id)
	test.IsEqualString(t, own.Preview.Download, own.DownloadUrl)
	test.IsEqualString(t, own.Uploader, "Seven")
	test.IsEqualBool(t, own.Mine, true)
	test.IsEqualBool(t, own.CanEdit, true)
	test.IsEqualBool(t, own.CanDelete, true)
	test.IsEqualInt(t, int(own.TakenAt), 99)
	test.IsEqualInt(t, int(own.Bytes), 1)
	test.IsEqualString(t, own.File.Id, f.Id)
	test.IsEqualBool(t, strings.HasPrefix(own.Preview.Thumb, "./p/thumb?id="+f.Id), true)

	// Someone else's file without any permission: listed (PERM_LIST pages
	// only) but neither editable nor deletable
	other := browserItemFromFile(r, f, models.User{Id: 8}, ctxFiles, "", names)
	test.IsEqualBool(t, other.Mine, false)
	test.IsEqualBool(t, other.CanEdit, false)
	test.IsEqualBool(t, other.CanDelete, false)

	// A visitor on a collection page: the counted collection download,
	// previews scoped to the collection, and never any management
	pub := browserItemFromFile(httptest.NewRequest("GET", "/c", nil), f, models.User{}, ctxCollectionPublic, "col123", names)
	test.IsEqualString(t, pub.DownloadUrl, "./cd?id=col123&file="+f.Id)
	test.IsEqualBool(t, strings.Contains(pub.Preview.Thumb, "&c=col123"), true)
	test.IsEqualBool(t, pub.CanEdit || pub.CanDelete || pub.Mine, false)
	test.IsEqualString(t, pub.Uploader, "")

	// End-to-end encrypted: no direct download on the Files tab
	e2e := accessFile("browseriteme2e001", func(f *models.File) { f.SHA1 = "browseriteme2esha"; f.Encryption.IsEndToEndEncrypted = true })
	test.IsEqualString(t, browserItemFromFile(r, e2e, models.User{Id: 7}, ctxFiles, "", names).DownloadUrl, "")
}

func previewInfoGet(t *testing.T, url string, cookies ...*http.Cookie) (int, previewInfo, http.Header) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://localhost:53843"+url, nil)
	test.IsNil(t, err)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	var info previewInfo
	if resp.StatusCode == http.StatusOK {
		test.IsNil(t, json.NewDecoder(resp.Body).Decode(&info))
	}
	return resp.StatusCode, info, resp.Header
}

// /files/previewinfo: the owner and PERM_LIST users only, after CanView.
// /p/info: whoever CanView lets see the file. Neither counts as a download.
func TestPreviewInfoRoutes(t *testing.T) {
	database.SaveUser(models.User{Id: 8, Name: "Stranger", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("infostranger", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})
	owner := &http.Cookie{Name: "session_token", Value: "validsession"}
	stranger := &http.Cookie{Name: "session_token", Value: "infostranger"}
	admin := &http.Cookie{Name: "session_token", Value: filesAdminSession().Value}

	f := accessFile("previewinfo000001", func(f *models.File) {
		f.SHA1 = "previewinfosha"
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 3
	})
	database.SavePreview(models.Preview{Sha1: "previewinfosha", Status: models.PreviewPending, Kind: models.PreviewKindImage, Updated: 1})
	pw := accessFile("previewinfopw0001", func(f *models.File) { f.SHA1 = "previewinfopwsha"; f.PasswordHash = "x" })
	database.SavePreview(models.Preview{Sha1: "previewinfopwsha", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 2, Height: 1, Updated: 1})

	// Owner: yes, and the pending status is what a tile polls for
	code, info, header := previewInfoGet(t, "/files/previewinfo?id="+f.Id, owner)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, info.Status, models.PreviewPending)
	test.IsEqualString(t, info.Thumb, "")
	test.IsEqualBool(t, strings.Contains(header.Get("Cache-Control"), "no-cache") || strings.Contains(header.Get("Cache-Control"), "no-store"), true)
	// ...becoming ready: the thumbnail URL arrives
	database.SavePreview(models.Preview{Sha1: "previewinfosha", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 4, Height: 3, Color: "#000000", Updated: 2})
	code, info, _ = previewInfoGet(t, "/files/previewinfo?id="+f.Id, owner)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, info.Status, models.PreviewReady)
	test.IsEqualBool(t, strings.HasPrefix(info.Thumb, "./p/thumb?id="+f.Id), true)
	test.IsEqualInt(t, info.Width, 4)
	test.IsEqualString(t, info.Download, "./files/download?id="+f.Id)
	// PERM_LIST admin: yes
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id="+f.Id, admin)
	test.IsEqualInt(t, code, http.StatusOK)
	// A logged-in stranger: no, although the file's own link is open
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id="+f.Id, stranger)
	test.IsEqualInt(t, code, http.StatusNotFound)
	// Not logged in: refused like every other protected call
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id="+f.Id)
	test.IsEqualInt(t, code, http.StatusUnauthorized)
	// Unknown id
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id=nosuchfile", owner)
	test.IsEqualInt(t, code, http.StatusNotFound)

	// /p/info for a plain file: anyone holding its link
	code, info, header = previewInfoGet(t, "/p/info?id="+f.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, info.Status, models.PreviewReady)
	test.IsEqualString(t, info.Download, "")
	test.IsEqualBool(t, strings.Contains(header.Get("Cache-Control"), "no-cache") || strings.Contains(header.Get("Cache-Control"), "no-store"), true)
	// A password-protected file without its cookie: no, for anyone but the owner
	code, _, _ = previewInfoGet(t, "/p/info?id="+pw.Id)
	test.IsEqualInt(t, code, http.StatusNotFound)
	code, _, _ = previewInfoGet(t, "/p/info?id="+pw.Id, stranger)
	test.IsEqualInt(t, code, http.StatusNotFound)
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id="+pw.Id, owner)
	test.IsEqualInt(t, code, http.StatusOK)
	// ...through a locked collection with its cookie: yes, scoped to it
	locked, err := storage.CreateCollection(storage.CollectionParameters{Name: "info", FileIds: []string{pw.Id}, Password: "pw"}, models.User{Id: 7})
	test.IsNil(t, err)
	code, _, _ = previewInfoGet(t, "/p/info?id="+pw.Id+"&c="+locked.Id)
	test.IsEqualInt(t, code, http.StatusNotFound)
	code, info, _ = previewInfoGet(t, "/p/info?id="+pw.Id+"&c="+locked.Id,
		&http.Cookie{Name: "pc" + locked.Id, Value: downloadPasswordToken.GenerateWithTtl(locked.Id, time.Hour)})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(info.Thumb, "&c="+locked.Id), true)
	// A file-request upload: never
	req := accessFile("previewinforeq001", func(f *models.File) { f.SHA1 = "previewinforeqsha"; f.UploadRequestId = "r" })
	code, _, _ = previewInfoGet(t, "/p/info?id="+req.Id)
	test.IsEqualInt(t, code, http.StatusNotFound)
	// ...unless it is the owner (or a PERM_LIST user) asking, which is how the
	// File requests page polls for the previews of what was uploaded to it
	database.SavePreview(models.Preview{Sha1: "previewinforeqsha", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 2, Height: 1, Updated: 1})
	code, info, _ = previewInfoGet(t, "/files/previewinfo?id="+req.Id, owner)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualBool(t, strings.HasPrefix(info.Thumb, "./p/thumb?id="+req.Id), true)
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id="+req.Id, admin)
	test.IsEqualInt(t, code, http.StatusOK)
	code, _, _ = previewInfoGet(t, "/files/previewinfo?id="+req.Id, stranger)
	test.IsEqualInt(t, code, http.StatusNotFound)
	code, _, _ = previewInfoGet(t, "/p/info?id="+req.Id, owner)
	test.IsEqualInt(t, code, http.StatusOK)
	code, _, _ = previewInfoGet(t, "/p/info?id="+req.Id, stranger)
	test.IsEqualInt(t, code, http.StatusNotFound)

	// Viewing never counts
	remaining, count := downloadState(t, f.Id)
	test.IsEqualInt(t, remaining, 3)
	test.IsEqualInt(t, count, 0)
}
