//go:build !integration && test

package webserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	manageMenu := []string{"open", "download", "photos", "copy", "hotlink", "share", "qr", "email", "edit", "replace", "add-to-collection", "add-to-group", "delete"}
	manageBulk := []string{"download", "zip", "photos", "link", "add-to-collection", "add-to-group", "change-expiry", "delete"}
	want := map[string]struct {
		menu, bulk []string
		view       string
	}{
		ctxFiles:            {manageMenu, manageBulk, "grid"},
		ctxUpload:           {manageMenu, manageBulk, "grid"},
		ctxCollectionAdmin:  {append(append([]string{}, manageMenu...), "remove-from-collection"), append(append([]string{}, manageBulk...), "remove-from-collection"), "list"},
		ctxFileRequest:      {[]string{"open", "download", "photos", "delete"}, []string{"download", "zip", "photos", "delete"}, "list"},
		ctxCollectionPublic: {[]string{"open", "download", "photos"}, []string{"download", "zip", "photos"}, "list"},
		ctxGroup:            {[]string{"open", "download", "photos", "edit", "replace", "remove-from-group", "delete"}, []string{"download", "zip", "photos", "remove-from-group", "delete"}, "board"},
	}
	test.IsEqualInt(t, len(want), 6)
	for context, w := range want {
		b := newFileBrowser("b-"+context, context, nil, models.User{Id: 7})
		test.IsEqualString(t, b.Context, context)
		test.IsEqualString(t, b.Id, "b-"+context)
		test.IsEqualString(t, strings.Join(b.Menu, ","), strings.Join(w.menu, ","))
		test.IsEqualString(t, strings.Join(b.Bulk, ","), strings.Join(w.bulk, ","))
		test.IsEqualString(t, b.DefaultView, w.view)
		test.IsEqualBool(t, b.HasMenu("open"), true)
		test.IsEqualBool(t, b.HasMenu("edit"), context == ctxFiles || context == ctxUpload || context == ctxCollectionAdmin || context == ctxGroup)
		test.IsEqualBool(t, b.HasBulk("remove-from-collection"), context == ctxCollectionAdmin)
		// Save to Photos is in the "⋯" menu wherever Download is (R3)
		test.IsEqualBool(t, b.HasMenu("photos"), b.HasMenu("download"))
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
	// no direct download: downloadOwnFile refuses request uploads
	test.IsEqualString(t, info.Download, "")
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

// renderBrowser renders the "file_browser" partial for one context with one
// item, the way a page would
func renderBrowser(t *testing.T, b fileBrowser) string {
	t.Helper()
	var out bytes.Buffer
	test.IsNil(t, templateFolder.ExecuteTemplate(&out, "file_browser", b))
	return out.String()
}

// Every context renders exactly the "⋯" entries and selection-bar actions of
// its row of the table, and the upload page's element ids only where a page
// holds one browser (Fix round 1: several browsers per page).
func TestFileBrowserRendersOnlyItsActions(t *testing.T) {
	f := accessFile("browserrender0001", func(f *models.File) { f.SHA1 = "browserrendersha"; f.Name = "render.pdf" })
	user := models.User{Id: 7}
	allMenu := []string{"open", "download", "photos", "copy", "hotlink", "share", "qr", "email", "edit", "replace", "add-to-collection", "add-to-group", "delete", "remove-from-collection", "remove-from-group"}
	allBulk := []string{"download", "zip", "photos", "link", "add-to-collection", "add-to-group", "change-expiry", "delete", "remove-from-collection", "remove-from-group"}
	collection := models.Collection{Id: "colrender"}
	for _, context := range []string{ctxFiles, ctxUpload, ctxCollectionAdmin, ctxFileRequest, ctxCollectionPublic, ctxGroup} {
		t.Run(context, func(t *testing.T) {
			r := sessionRequest("validsession")
			item := browserItemFromFile(r, f, user, context, collection.Id, map[int]string{7: "Seven"})
			b := newFileBrowser("b-"+context, context, []browserItem{item}, user)
			if context == ctxCollectionAdmin || context == ctxCollectionPublic {
				b.WithCollection(collection)
			}
			if context == ctxGroup {
				b.WithGroup(models.Group{Id: collection.Id}, nil)
			}
			html := renderBrowser(t, b)
			test.IsEqualBool(t, strings.Contains(html, `data-context="`+context+`"`), true)
			for _, key := range allMenu {
				test.IsEqualBool(t, strings.Contains(html, `data-menu="`+key+`"`), b.HasMenu(key))
			}
			for _, key := range allBulk {
				want := b.HasBulk(key) && (key != "zip" || b.ZipUrl != "" || b.ZipByHandler)
				test.IsEqualBool(t, strings.Contains(html, `data-act="`+key+`"`), want)
			}
			// Save to Photos starts hidden: media.js shows it on iOS, for photos and videos (R3)
			photosEntry := `<li class="d-none"><button type="button" class="dropdown-item" data-menu="photos"><i class="bi bi-box-arrow-down"></i> Save to Photos</button></li>`
			test.IsEqualBool(t, strings.Contains(html, photosEntry), b.HasMenu("photos"))
			test.IsEqualBool(t, strings.Count(html, `data-menu="photos"`) == strings.Count(html, photosEntry), true)
			// Copy link only where a share link exists
			test.IsEqualBool(t, strings.Contains(html, "copyurl"), b.HasMenu("copy"))
			legacy := context == ctxFiles || context == ctxUpload
			test.IsEqualBool(t, b.LegacyIds(), legacy)
			test.IsEqualBool(t, strings.Contains(html, `data-legacy-ids="`+map[bool]string{true: "true", false: "false"}[legacy]+`"`), true)
			for _, id := range []string{`id="row-` + f.Id, `id="cell-name-` + f.Id, `id="cell-downloads-` + f.Id, `id="url-button-` + f.Id} {
				want := legacy && (id != `id="cell-downloads-`+f.Id || b.Manages()) && (id != `id="url-button-`+f.Id || b.HasMenu("copy"))
				test.IsEqualBool(t, strings.Contains(html, id), want)
			}
			// Visitors and request pages carry no management data
			if !b.Manages() {
				test.IsEqualBool(t, strings.Contains(html, "data-can-edit"), false)
				test.IsEqualBool(t, strings.Contains(html, "data-url="), false)
			}
			test.IsEqualBool(t, strings.Contains(html, `class="bi bi-file-earmark-pdf"`), true)
		})
	}
	// Collections admin: one browser per collection, a file in two of them,
	// still no id twice
	r := sessionRequest("validsession")
	item := browserItemFromFile(r, f, user, ctxCollectionAdmin, "", nil)
	page := renderBrowser(t, newFileBrowser("col-a", ctxCollectionAdmin, []browserItem{item}, user)) +
		renderBrowser(t, newFileBrowser("col-b", ctxCollectionAdmin, []browserItem{item}, user))
	ids := regexp.MustCompile(` id="([^"]+)"`).FindAllStringSubmatch(page, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		test.IsEqualBool(t, seen[m[1]], false)
		seen[m[1]] = true
	}
}

// "Copy hotlink" and "Email" (Ruling S-2) are in the managing contexts' menus
// only. A file without a hotlink keeps the entry hidden (a file added live
// may have one, and media.js shows it then); the Email link carries the
// share URL.
func TestFileBrowserHotlinkAndEmail(t *testing.T) {
	plain := accessFile("browserhotlink001", func(f *models.File) { f.SHA1 = "browserhotlinksha"; f.Name = "hot.jpg" })
	locked := accessFile("browserhotlink002", func(f *models.File) {
		f.SHA1 = "browserhotlinksha2"
		f.Name = "locked.jpg"
		f.PasswordHash = "hash"
	})
	user := models.User{Id: 7}
	shown := regexp.MustCompile(`<li><button type="button" class="dropdown-item copyurl" data-menu="hotlink" data-clipboard-text="[^"]+"`)
	hidden := regexp.MustCompile(`<li class="d-none"><button type="button" class="dropdown-item copyurl" data-menu="hotlink" data-clipboard-text=""`)
	for _, context := range []string{ctxFiles, ctxUpload, ctxCollectionAdmin, ctxFileRequest, ctxCollectionPublic} {
		t.Run(context, func(t *testing.T) {
			r := sessionRequest("validsession")
			itemPlain := browserItemFromFile(r, plain, user, context, "colhot", nil)
			itemLocked := browserItemFromFile(r, locked, user, context, "colhot", nil)
			test.IsEqualBool(t, itemPlain.File.UrlHotlink != "", true)
			test.IsEqualString(t, itemLocked.File.UrlHotlink, "")
			b := newFileBrowser("h-"+context, context, []browserItem{itemPlain, itemLocked}, user)
			html := renderBrowser(t, b)
			manages := b.Manages()
			test.IsEqualBool(t, strings.Contains(html, `data-menu="hotlink"`), manages)
			test.IsEqualBool(t, strings.Contains(html, `data-menu="email"`), manages)
			test.IsEqualBool(t, strings.Contains(html, "mailto:"), manages)
			if !manages {
				return
			}
			// Per file a menu in each view; the two <template>s hold a blank one
			// that starts hidden
			test.IsEqualInt(t, len(shown.FindAllString(html, -1)), 2)
			test.IsEqualInt(t, len(hidden.FindAllString(html, -1)), 2+2)
			test.IsEqualBool(t, strings.Contains(html, `data-clipboard-text="`+itemPlain.File.UrlHotlink+`"`), true)
			test.IsEqualBool(t, strings.Contains(html, `href="mailto:?body=`+url.QueryEscape(itemPlain.File.UrlDownload)+`"`), true)
			test.IsEqualBool(t, strings.Contains(html, `href="mailto:?body=`+url.QueryEscape(itemLocked.File.UrlDownload)+`"`), true)
			test.IsEqualBool(t, strings.Contains(html, "Copy hotlink"), true)
			test.IsEqualBool(t, strings.Contains(html, "bi-envelope"), true)
		})
	}
}

// WithCollection sets the collection and, on the public page, the counted
// zip of the selected members; admin browsers keep the owner zip
func TestFileBrowserWithCollection(t *testing.T) {
	c := models.Collection{Id: "colzip"}
	pub := newFileBrowser("p", ctxCollectionPublic, nil, models.User{})
	test.IsEqualString(t, pub.ZipUrl, "")
	pub.WithCollection(c)
	test.IsEqualString(t, pub.CollectionId, "colzip")
	test.IsEqualString(t, pub.ZipUrl, "./cz?id=colzip&files=")
	test.IsEqualString(t, pub.ZipAllUrl, "./cz?id=colzip")
	test.IsEqualString(t, pub.InfoUrl(), "./p/info?c=colzip&id=")
	adm := newFileBrowser("a", ctxCollectionAdmin, nil, models.User{Id: 7})
	adm.WithCollection(c)
	test.IsEqualString(t, adm.CollectionId, "colzip")
	test.IsEqualString(t, adm.ZipUrl, "./files/zip?ids=")
	// /files/zip has no "everything" form
	test.IsEqualString(t, adm.ZipAllUrl, "")
}

// Files added live get the same type icon as rendered ones: media.js holds
// a copy of fileTypeIconGroups
func TestFileTypeIconsMatchJs(t *testing.T) {
	source, err := staticFolderEmbedded.ReadFile("web/static/js/media.js")
	test.IsNil(t, err)
	block := regexp.MustCompile(`(?s)FILE_TYPE_ICONS_BEGIN.*?var FILE_TYPE_ICONS = (\{.*?\});.*?FILE_TYPE_ICONS_END`).FindSubmatch(source)
	test.IsEqualBool(t, block != nil, true)
	js := map[string]string{}
	test.IsNil(t, json.Unmarshal(block[1], &js))
	test.IsEqualInt(t, len(js), len(fileTypeIconGroups))
	for icon, extensions := range fileTypeIconGroups {
		test.IsEqualString(t, js[icon], extensions)
	}
}

// A group page (Part 2, G3): the group's own download and zip, previews
// through /p/info, Board first, and a managed list (edit is back, D4)
func TestGroupBrowser(t *testing.T) {
	people := []browserPerson{{Id: 7, Name: "seven"}, {Id: 8, Name: "anna"}}
	b := newFileBrowser("g", ctxGroup, nil, models.User{Id: 7})
	b.WithGroup(models.Group{Id: "G1"}, people)
	test.IsEqualString(t, b.GroupId, "G1")
	test.IsEqualString(t, b.ZipUrl, "./gz?id=G1&files=")
	test.IsEqualString(t, b.ZipAllUrl, "./gz?id=G1")
	test.IsEqualString(t, b.DownloadBase, "./gd?id=G1&file=")
	test.IsEqualString(t, b.InfoUrl(), "./p/info?id=")
	test.IsEqualString(t, b.DefaultView, "board")
	test.IsEqualBool(t, b.SortTakenAsc, true)
	test.IsEqualBool(t, b.GroupTools, true)
	test.IsEqualBool(t, b.ShowUploader, true)
	test.IsEqualInt(t, len(b.People), 2)
	test.IsEqualBool(t, b.Manages(), true)
	test.IsEqualBool(t, b.ItemPerms(), true)
	// Without groups: file requests have item permissions but no management
	fr := newFileBrowser("r", ctxFileRequest, nil, models.User{Id: 7})
	test.IsEqualBool(t, fr.ItemPerms(), true)
	test.IsEqualBool(t, fr.Manages(), false)
	test.IsEqualBool(t, newFileBrowser("c", ctxCollectionPublic, nil, models.User{}).ItemPerms(), false)
	test.IsEqualString(t, newFileBrowser("f", ctxFiles, nil, models.User{Id: 7}).DownloadBase, "")

	// The item: the group download, the default branch's permissions. Edit
	// follows the default branch (D4 revision): the owner may, others not.
	f := accessFile("groupbrowser00001", func(f *models.File) { f.SHA1 = "groupbrowsersha"; f.Name = "fjord.jpg" })
	names := map[int]string{7: "anna.mueller"}
	r := sessionRequest("validsession")
	own := browserItemFromFile(r, f, models.User{Id: 7}, ctxGroup, "G1", names)
	test.IsEqualString(t, own.DownloadUrl, "./gd?id=G1&file="+f.Id)
	test.IsEqualBool(t, own.Mine, true)
	test.IsEqualBool(t, own.CanEdit, true)
	test.IsEqualBool(t, own.CanDelete, true)
	test.IsEqualInt(t, own.UploaderId, 7)
	test.IsEqualString(t, own.Uploader, "anna.mueller")
	test.IsEqualString(t, own.Initials, "AM")
	test.IsEqualBool(t, own.CanRemove, false) // the caller decides (G10)
	// Previews are not scoped to the group: CanView admits members
	test.IsEqualBool(t, strings.Contains(own.Preview.Thumb, "&c="), false)
	other := browserItemFromFile(r, f, models.User{Id: 8}, ctxGroup, "G1", names)
	test.IsEqualBool(t, other.Mine, false)
	test.IsEqualBool(t, other.CanEdit, false)
	test.IsEqualBool(t, other.CanDelete, false)
	test.IsEqualBool(t, other.CanReplace, false)
	test.IsEqualInt(t, other.UploaderId, 7)

	test.IsEqualString(t, initialsOf("anna.mueller"), "AM")
	test.IsEqualString(t, initialsOf("ben"), "B")
	test.IsEqualString(t, initialsOf(""), "")
	test.IsEqualString(t, initialsOf("anna.maria.mueller"), "AM")
	test.IsEqualString(t, initialsOf("élodie_roux"), "ÉR")
}

// D4: a file in a live group carries the group's name on every managing
// page, and its menu tells the edit dialog to lock expiry and limits
func TestGroupBrowserLock(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	f := accessFile("groupbrowserlock1", func(f *models.File) { f.SHA1 = "groupbrowserlocksha"; f.Name = "lock.jpg" })
	free := accessFile("groupbrowserfree1", func(f *models.File) { f.SHA1 = "groupbrowserfreesha"; f.Name = "free.jpg" })
	g, err := storage.CreateGroup("Iceland trip", 30, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	res, err := storage.AddFilesToGroup(g, []string{f.Id}, owner)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(res.Added), 1)
	f, _ = database.GetMetaDataById(f.Id)

	r := sessionRequest("validsession")
	for _, context := range []string{ctxFiles, ctxUpload, ctxCollectionAdmin, ctxGroup} {
		test.IsEqualString(t, browserItemFromFile(r, f, owner, context, g.Id, nil).GroupLock, "Iceland trip")
		test.IsEqualString(t, browserItemFromFile(r, free, owner, context, g.Id, nil).GroupLock, "")
	}
	// Pages without the edit dialog never ask
	test.IsEqualString(t, browserItemFromFile(r, f, owner, ctxFileRequest, "", nil).GroupLock, "")
	test.IsEqualString(t, browserItemFromFile(httptest.NewRequest("GET", "/c", nil), f, models.User{}, ctxCollectionPublic, "col", nil).GroupLock, "")

	item := browserItemFromFile(r, f, owner, ctxGroup, g.Id, nil)
	b := newFileBrowser("g-lock", ctxGroup, []browserItem{item}, owner)
	b.WithGroup(g, nil)
	test.IsEqualBool(t, strings.Contains(renderBrowser(t, b), `data-group-lock="Iceland trip"`), true)
	files := newFileBrowser("f-lock", ctxFiles, []browserItem{browserItemFromFile(r, free, owner, ctxFiles, "", nil)}, owner)
	test.IsEqualBool(t, strings.Contains(renderBrowser(t, files), "data-group-lock"), false)
}

// The group page's markup: its filters and selections, the downloaded tick,
// shooting order, and the menu entries of its row of the action table
func TestGroupBrowserMarkup(t *testing.T) {
	f := accessFile("groupbrowsermark1", func(f *models.File) { f.SHA1 = "groupbrowsermarksha"; f.Name = "mark.jpg"; f.UserId = 8 })
	user := models.User{Id: 7}
	r := sessionRequest("validsession")
	item := browserItemFromFile(r, f, user, ctxGroup, "G1", map[int]string{8: "anna"})
	item.Downloaded = true
	item.CanRemove = true
	b := newFileBrowser("g-mark", ctxGroup, []browserItem{item}, user)
	b.WithGroup(models.Group{Id: "G1"}, []browserPerson{{Id: 8, Name: "anna"}, {Id: 7, Name: "<me>"}})
	html := renderBrowser(t, b)
	for _, want := range []string{`data-fgs-select-new`, `data-fgs-person`, `Hide what I&#39;ve downloaded`,
		`data-downloaded="true"`, `data-can-remove="true"`, `data-sort="taken-asc"`, `data-fgs-select-all`,
		`data-group-id="G1"`, `data-download-base="./gd?id=G1&amp;file="`, `data-group-tools="true"`,
		`data-upload-sink="false"`, `data-uploader-id="8"`, `<option value="8">anna</option>`,
		`<option value="7">&lt;me&gt;</option>`, `<option value="">Everyone</option>`,
		`class="fgs-initials" aria-hidden="true">A</span>`, `class="fgs-tick"`,
		`data-menu="edit"`, `data-menu="replace"`, `data-menu="remove-from-group"`, `data-act="remove-from-group"`,
		`<th data-fgs-sortkey="taken"><button type="button" class="fgs-sort">Taken`} {
		if !strings.Contains(html, want) {
			t.Errorf("group browser lacks %s", want)
		}
	}
	for _, unwanted := range []string{`data-menu="copy"`, `data-menu="share"`, `data-menu="add-to-group"`, `data-fgs-sortkey="expires"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("group browser has %s", unwanted)
		}
	}

	files := newFileBrowser("f-mark", ctxFiles, []browserItem{browserItemFromFile(r, f, user, ctxFiles, "", nil)}, user)
	html = renderBrowser(t, files)
	for _, want := range []string{`data-fgs-select-all`, `data-menu="add-to-group"`, `data-act="add-to-group"`,
		`data-sort="uploaded-desc"`, `data-group-tools="false"`, `<th data-fgs-sortkey="uploaded"><button type="button" class="fgs-sort">Uploaded`,
		`data-fgs-sortkey="expires"`, `data-fgs-sortkey="downloads"`} {
		if !strings.Contains(html, want) {
			t.Errorf("files browser lacks %s", want)
		}
	}
	for _, unwanted := range []string{`data-fgs-select-new`, `data-fgs-person`, `data-fgs-hide-downloaded`, `fgs-initials`, `data-menu="remove-from-group"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("files browser has %s", unwanted)
		}
	}
}

// Part 3, T2a: the list's batching state on the root, "Select all" over the
// whole list, a sentinel after the grid and after the table, sortable List
// headers (an arrow on the default column) and the List-only search box
func TestTemplateListAttributes(t *testing.T) {
	f := accessFile("listattrs00000001", func(f *models.File) { f.SHA1 = "listattrssha"; f.Name = "attrs.jpg" })
	user := models.User{Id: 7}
	r := sessionRequest("validsession")
	b := newFileBrowser("g-list", ctxGroup, []browserItem{browserItemFromFile(r, f, user, ctxGroup, "G1", nil)}, user)
	b.WithGroup(models.Group{Id: "G1"}, nil)
	b.ListUrl, b.Total, b.All, b.New, b.Next, b.Sort, b.Desc = "./gl?id=G1&", 230, 250, 12, "CURSOR1", "taken", false
	html := renderBrowser(t, b)
	for _, want := range []string{
		`data-list-url="./gl?id=G1&amp;"`, `data-total="230"`, `data-all="250"`, `data-new="12"`,
		`data-next="CURSOR1"`, `data-sort-key="taken"`, `data-sort-desc="false"`,
		`data-fgs-select-all>Select all (230)</button>`,
		`<input type="search" class="form-control form-control-sm" data-fgs-search placeholder="Search names" aria-label="Search names">`,
		`<th data-fgs-sortkey="name"><button type="button" class="fgs-sort">Name`,
		`<th data-fgs-sortkey="uploaded"><button type="button" class="fgs-sort">Uploaded`,
		`<th data-fgs-sortkey="taken" aria-sort="ascending"><button type="button" class="fgs-sort">Taken<span class="fgs-sort-arrow" aria-hidden="true">↑</span>`,
		`<th data-fgs-sortkey="size"><button type="button" class="fgs-sort">Size`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("list lacks %s", want)
		}
	}
	sentinel := `<div class="fgs-more" data-fgs-more aria-live="polite"></div>`
	test.IsEqualInt(t, strings.Count(html, sentinel), 2)
	// one directly after the grid, one directly after the table
	test.IsEqualBool(t, regexp.MustCompile(`</div>\s*`+regexp.QuoteMeta(sentinel)+`\s*<div class="table-responsive" data-fgs-list>`).MatchString(html), true)
	test.IsEqualBool(t, regexp.MustCompile(`</table>\s*`+regexp.QuoteMeta(sentinel)).MatchString(html), true)
	// Group pages have no Expires and Downloads columns; "Uploaded by" is not sortable
	test.IsEqualBool(t, strings.Contains(html, `data-fgs-sortkey="expires"`), false)
	// R1: one "Sort & filter" dropdown holds the sort, the direction, Type and
	// the group tools; the search stays in the toolbar, in every view
	test.IsEqualBool(t, strings.Contains(html, "fgs-list-only"), false)
	menuAt := strings.Index(html, `data-fgs-sortfilter`)
	menuEnd := strings.Index(html, `data-fgs-filter-reset`)
	if menuAt < 0 || menuEnd < menuAt {
		t.Fatalf("no Sort & filter dropdown")
	}
	menu := html[menuAt:menuEnd]
	for _, want := range []string{`data-fgs-person`, `<option value="">Everyone</option>`, `data-fgs-hide-downloaded`,
		`name="g-list-sort" value="name" data-fgs-sort-option`, `name="g-list-sort" value="uploaded" data-fgs-sort-option`,
		`name="g-list-sort" value="taken" data-fgs-sort-option checked`, `name="g-list-sort" value="size" data-fgs-sort-option`,
		`name="g-list-dir" value="asc" data-fgs-sort-dir checked`, `name="g-list-dir" value="desc" data-fgs-sort-dir>`,
		`name="g-list-kind" value="" data-fgs-kind checked`, `name="g-list-kind" value="image" data-fgs-kind`,
		`name="g-list-kind" value="video" data-fgs-kind`, `name="g-list-kind" value="other" data-fgs-kind`} {
		if !strings.Contains(menu, want) {
			t.Errorf("Sort & filter lacks %s", want)
		}
	}
	test.IsEqualBool(t, strings.Contains(menu, `value="expires"`), false)
	test.IsEqualBool(t, strings.Contains(html, `data-fgs-filter-badge`), true)
	test.IsEqualBool(t, strings.Contains(html, `Sort &amp; filter`), true)
	test.IsEqualBool(t, strings.Contains(html, `<th>Uploaded by</th>`), true)
	// No DataTables hooks are left
	test.IsEqualBool(t, strings.Contains(html, "data-fgs-sort "), false)
	test.IsEqualBool(t, strings.Contains(html, "data-fgs-nosort"), false)

	// The Files tab: newest first, its management columns sortable
	files := newFileBrowser("f-list", ctxFiles, nil, user)
	files.ListUrl, files.Total, files.All, files.Sort, files.Desc = "./files/list?src=files&", 3, 9, "uploaded", true
	html = renderBrowser(t, files)
	for _, want := range []string{`data-sort-key="uploaded"`, `data-sort-desc="true"`, `Select all (3)`,
		`<th data-fgs-sortkey="uploaded" aria-sort="descending"><button type="button" class="fgs-sort">Uploaded<span class="fgs-sort-arrow" aria-hidden="true">↓</span>`,
		`<th data-fgs-sortkey="expires"><button type="button" class="fgs-sort">Expires`,
		`<th data-fgs-sortkey="downloads"><button type="button" class="fgs-sort">Downloads`,
		`name="f-list-sort" value="uploaded" data-fgs-sort-option checked`, `name="f-list-dir" value="desc" data-fgs-sort-dir checked`,
		`name="f-list-sort" value="expires" data-fgs-sort-option`, `name="f-list-sort" value="downloads" data-fgs-sort-option`,
		`name="f-list-kind" value="video" data-fgs-kind`} {
		if !strings.Contains(html, want) {
			t.Errorf("files list lacks %s", want)
		}
	}
	// No group tools on the Files tab; My files / Everything where it shows
	test.IsEqualBool(t, strings.Contains(html, `data-fgs-person`), false)
	files.ShowScope = true
	html = renderBrowser(t, files)
	menu = html[strings.Index(html, `data-fgs-sortfilter`):strings.Index(html, `data-fgs-filter-reset`)]
	test.IsEqualBool(t, strings.Contains(menu, `data-scope="mine"`), true)
	test.IsEqualBool(t, strings.Contains(menu, `data-scope="all"`), true)
}

// R3: Save to Photos in one tap where the tap's activation survives the
// downloads, an explicit "Ready" step where it does not, the date taken as
// the file's date, and a viewer button. There is no browser harness, so the
// script's load-bearing parts are checked as text.
func TestSaveToPhotosScript(t *testing.T) {
	js, err := staticFolderEmbedded.ReadFile("web/static/js/media.js")
	test.IsNil(t, err)
	for _, want := range []string{
		"navigator.userActivation", ".isActive",
		"opts.lastModified = date", "s * 1000", `getAttribute("data-taken")`, `getAttribute("data-uploaded")`,
		`"Ready — tap to save"`, `" (batch "`, `"Next batch — tap to prepare ("`, "fgs-photos-ready",
		"var PHOTO_BATCH_FILES = 20;", "var PHOTO_BATCH_BYTES = 1.5e9;", "var VIDEO_MAX_BYTES  = 500e6;",
		"FGS.planPhotos = function (items, limits)", "FGS.photosPending",
		` video(s) are too big to save to Photos from Safari. Download them as a .zip, then save them from the Files app.`,
		`"Download those as .zip"`,
		`"NotAllowedError"`, `"AbortError"`,
		"hide.bs.dropdown", "show.bs.dropdown", "clickEvent",
		".fgs-v-photos", `what === "photos"`,
		// Final review I1: the Files tab's zip skips other people's files,
		// so their big videos are pointed at the group's page instead
		"zipSkipsForeign: skipForeign", `" video(s) from your groups: zip them on their group's page."`,
		// I2: a retry fetches only what failed, never what was saved
		"state.failedItems.push(it)", "FGS.planPhotos(again)", `"Try the " + again.length + " again"`,
		// Viewer: the note above the caption bar, with the page's zip
		`querySelector(".fgs-v-info")`, "uploaded: it.uploaded, foreign: it.foreign }], button, viewerPhotos",
	} {
		if !strings.Contains(string(js), want) {
			t.Errorf("media.js lacks %s", want)
		}
	}
	// The old second-tap label is gone
	test.IsEqualBool(t, strings.Contains(string(js), `button.textContent = "Save to Photos"`), false)

	css, err := staticFolderEmbedded.ReadFile("web/static/css/media.css")
	test.IsNil(t, err)
	for _, want := range []string{
		".fgs-menu .dropdown-menu{", "backdrop-filter:blur(", "-webkit-backdrop-filter:blur(",
		"var(--fgs-radius,14px)", "var(--fgs-line,#2e2e2b)", ".fgs-menu .dropdown-item:focus-visible",
		".btn.fgs-photos-ready", ".dropdown-item.fgs-photos-ready", "@keyframes fgs-pulse",
		"prefers-reduced-motion", ".fgs-v-photos[hidden]{display:none}",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("media.css lacks %s", want)
		}
	}

	var out bytes.Buffer
	test.IsNil(t, templateFolder.ExecuteTemplate(&out, "media_viewer", nil))
	viewer := out.String()
	download := strings.Index(viewer, "fgs-v-download")
	photos := strings.Index(viewer, `<button type="button" class="btn btn-sm btn-outline-light fgs-v-photos" hidden>Save to Photos</button>`)
	test.IsEqualBool(t, download > 0, true)
	// next to "Download original", hidden until media.js decides it applies
	test.IsEqualBool(t, photos > download, true)
}

// F1: the remembered view is in place before the first paint. Each list root
// is followed directly by an inline script that hands it to
// FGS.presetView (media.js, loaded in the head), which can never throw; the
// CSS shows only data-current-view's view, so the list no longer starts
// d-none; and the default view's button is active as rendered. The key
// choice itself is tested in build/dev/browser_view_test.mjs.
func TestFileBrowserPresetsViewBeforePaint(t *testing.T) {
	user := models.User{Id: 7}
	for _, context := range []string{ctxFiles, ctxUpload, ctxCollectionAdmin, ctxFileRequest, ctxCollectionPublic, ctxGroup} {
		t.Run(context, func(t *testing.T) {
			b := newFileBrowser("b-"+context, context, nil, user)
			if context == ctxGroup {
				b.WithGroup(models.Group{Id: "grp42"}, nil)
			}
			html := renderBrowser(t, b)
			script := `<script>try{window.FGS&&FGS.presetView&&FGS.presetView(document.getElementById("b-` + context + `"))}catch(e){}</script>`
			at := strings.Index(html, script)
			if at < 0 {
				t.Fatalf("no preset script for %s", context)
			}
			// directly after the root's closing tag, before anything else
			test.IsEqualBool(t, strings.HasSuffix(strings.TrimSpace(html[:at]), "</div>"), true)
			test.IsEqualBool(t, strings.Contains(html, `<div class="table-responsive" data-fgs-list>`), true)
			for _, v := range []string{viewGrid, viewBoard, viewList} {
				class := "btn-outline-light"
				if v == b.DefaultView {
					class = "btn-light"
				}
				test.IsEqualBool(t, strings.Contains(html, `class="btn btn-sm `+class+`" data-layout="`+v+`"`), true)
			}
		})
	}
	// Groups start on Board, keyed by their own id
	g := newFileBrowser("b-group", ctxGroup, nil, user)
	g.WithGroup(models.Group{Id: "grp42"}, nil)
	html := renderBrowser(t, g)
	test.IsEqualBool(t, strings.Contains(html, `data-current-view="board"`), true)
	test.IsEqualBool(t, strings.Contains(html, `data-group-id="grp42"`), true)

	css, err := staticFolderEmbedded.ReadFile("web/static/css/media.css")
	test.IsNil(t, err)
	for _, want := range []string{
		".fgs-browser[data-current-view=list]>.fgs-views>[data-fgs-grid]{display:none!important}",
		".fgs-browser:not([data-current-view=list])>.fgs-views>[data-fgs-list]{display:none!important}",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("media.css lacks %s", want)
		}
	}
	js, err := staticFolderEmbedded.ReadFile("web/static/js/media.js")
	test.IsNil(t, err)
	for _, want := range []string{"FGS.presetView = function (root)", "FGS.browserPrefKey = browserPrefKey"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("media.js lacks %s", want)
		}
	}
}

// F1: iOS 27 drops the date taken on import, through Save to Photos and
// through Files alike, so the help sheet promises nothing about dates
func TestHelpSheetMakesNoDatePromise(t *testing.T) {
	var out bytes.Buffer
	test.IsNil(t, templateFolder.ExecuteTemplate(&out, "media_help_sheet", nil))
	sheet := strings.ToLower(out.String())
	test.IsEqualBool(t, strings.Contains(sheet, "fgs-help-sheet"), true)
	for _, promise := range []string{"original date", "keep their", "timeline", "date and time"} {
		test.IsEqualBool(t, strings.Contains(sheet, promise), false)
	}
}

// R3 (V7–V9): the expand button that opens a file while selecting, in
// Grid/Board and in List; the viewer's "Selected" checkbox; the column
// slider with its default stop; the sticky toolbar and selection bar.
func TestSelectModePreviewAndColumnSlider(t *testing.T) {
	f := accessFile("browserexpand0001", func(f *models.File) { f.SHA1 = "browserexpandsha"; f.Name = "beach & sun.jpg" })
	user := models.User{Id: 7}
	r := sessionRequest("validsession")
	item := browserItemFromFile(r, f, user, ctxGroup, "", map[int]string{7: "Seven"})
	b := newFileBrowser("b-expand", ctxGroup, []browserItem{item}, user)
	b.WithGroup(models.Group{Id: "grp42"}, nil)
	html := renderBrowser(t, b)

	expandTile := `<button type="button" class="fgs-tile-expand" aria-label="Open beach &amp; sun.jpg" title="Open"><i class="bi bi-arrows-angle-expand"></i></button>`
	expandRow := `<button type="button" class="btn btn-sm fgs-row-expand" aria-label="Open beach &amp; sun.jpg" title="Open"><i class="bi bi-arrows-angle-expand"></i></button>`
	// the rendered file and the template each live adds clone
	test.IsEqualInt(t, strings.Count(html, expandTile), 1)
	test.IsEqualInt(t, strings.Count(html, expandRow), 1)
	test.IsEqualInt(t, strings.Count(html, `class="fgs-tile-expand"`), 2)
	test.IsEqualInt(t, strings.Count(html, `fgs-row-expand"`), 2)
	// part3_e2e parses the tiles with this prefix
	test.IsEqualBool(t, strings.Contains(html, `<figure class="fgs-tile" data-mine=`), true)

	// The slider counts columns: step 1, a default stop on its datalist
	slider := regexp.MustCompile(`<input type="range"[^>]*>`).FindString(html)
	for _, want := range []string{`step="1"`, `list="b-expand-cols-stops"`, `class="fgs-tiles-only fgs-size form-range"`, `aria-label="Icon size"`} {
		test.IsEqualBool(t, strings.Contains(slider, want), true)
	}
	test.IsEqualBool(t, strings.Contains(html, `<datalist id="b-expand-cols-stops"><option value="2"></option></datalist>`), true)
	test.IsEqualBool(t, strings.Contains(html, `class="fgs-toolbar d-flex flex-wrap gap-2 align-items-center" data-fgs-toolbar`), true)

	var out bytes.Buffer
	test.IsNil(t, templateFolder.ExecuteTemplate(&out, "media_viewer", nil))
	test.IsEqualBool(t, strings.Contains(out.String(), `<label class="fgs-v-select" hidden><input type="checkbox" class="form-check-input" data-fgs-v-select> Selected</label>`), true)

	css, err := staticFolderEmbedded.ReadFile("web/static/css/media.css")
	test.IsNil(t, err)
	for _, want := range []string{
		".fgs-toolbar{position:sticky;top:var(--fgs-viewas-h,0px)",
		".fgs-selectbar{position:sticky;top:calc(var(--fgs-viewas-h,0px) + var(--fgs-toolbar-h,0px))",
		".fgs-grid.fgs-has-cols{grid-template-columns:repeat(var(--fgs-cols),minmax(0,1fr))}",
		".fgs-browser:not(.fgs-selecting) .fgs-tile-expand,.fgs-browser:not(.fgs-selecting) .fgs-row-expand{display:none}",
		".fgs-v-select[hidden]{display:none}",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("media.css lacks %s", want)
		}
	}
	viewas, err := staticFolderEmbedded.ReadFile("web/static/js/viewas.js")
	test.IsNil(t, err)
	test.IsEqualBool(t, strings.Contains(string(viewas), "2.5rem"), false)
	js, err := staticFolderEmbedded.ReadFile("web/static/js/media.js")
	test.IsNil(t, err)
	for _, want := range []string{"FGS.tileColumns = function (width, remembered)", "FGS.snapColumns = function (position, def)",
		`".fgs-tile-expand"`, `".fgs-row-expand"`, `"--fgs-toolbar-h"`, `[data-fgs-v-select]`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("media.js lacks %s", want)
		}
	}
}
