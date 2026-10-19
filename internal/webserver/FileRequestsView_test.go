//go:build !integration && test

package webserver

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// The File requests page lists each request's files in the shared file
// browser: one browser per request, context "filerequest", with the actions
// of the spec's table and the presigned download in place of a direct URL.
func TestFileRequestsPageRendersABrowserPerRequest(t *testing.T) {
	for _, id := range []string{"frviewone", "frviewtwo"} {
		database.SaveFileRequest(models.FileRequest{Id: id, UserId: 5, Name: "Request " + id, ApiKey: "frkey" + id, CreationDate: 1})
	}
	accessFile("frviewfile1", func(f *models.File) { f.UserId = 5; f.UploadRequestId = "frviewone"; f.Name = "first-upload.jpg" })
	accessFile("frviewfile2", func(f *models.File) { f.UserId = 5; f.UploadRequestId = "frviewtwo"; f.Name = "second-upload.jpg" })
	accessFile("frviewfile3", func(f *models.File) { f.UserId = 5; f.UploadRequestId = "frviewtwo"; f.Name = "third-upload.jpg" })

	code, body := filesGet(t, filesAdminSession().Value, "/filerequests")
	test.IsEqualInt(t, code, 200)
	page := string(body)

	for _, id := range []string{"frviewone", "frviewtwo"} {
		test.IsEqualBool(t, strings.Contains(page, `id="fr-`+id+`"`), true)
		test.IsEqualBool(t, strings.Contains(page, `id="fr-`+id+`-table"`), true)
		test.IsEqualBool(t, strings.Contains(page, `id="fr-`+id+`-selstatus"`), true)
	}
	test.IsEqualInt(t, strings.Count(page, `data-context="filerequest"`), 2)
	test.IsEqualInt(t, strings.Count(page, `data-default-view="list"`), 2)
	// request one holds one file, request two holds two; each file is listed
	// in its own request's browser only
	one := page[strings.Index(page, `id="fr-frviewone"`):strings.Index(page, `id="fr-frviewtwo"`)]
	two := page[strings.Index(page, `id="fr-frviewtwo"`):]
	test.IsEqualBool(t, strings.Contains(one, `data-id="frviewfile1"`), true)
	test.IsEqualBool(t, strings.Contains(one, `data-id="frviewfile2"`), false)
	test.IsEqualBool(t, strings.Contains(two, `data-id="frviewfile2"`), true)
	test.IsEqualBool(t, strings.Contains(two, `data-id="frviewfile3"`), true)
	test.IsEqualBool(t, strings.Contains(two, `data-id="frviewfile1"`), false)

	// The spec's actions: Open, Download, Delete; Download, zip, Save to Photos, Delete
	// in each request's own browser, not merely somewhere on the page
	for name, browser := range map[string]string{"one": one, "two": two} {
		for _, want := range []string{`data-menu="open"`, `data-menu="download"`, `data-menu="delete"`,
			`data-act="download"`, `data-act="zip"`, `data-act="photos"`, `data-act="delete"`,
			`data-info-url="./files/previewinfo?id="`, `data-zip-url=""`} {
			if !strings.Contains(browser, want) {
				t.Errorf("browser %s lacks %s", name, want)
			}
		}
	}
	// the viewer is on the page once, shared by the browsers
	test.IsEqualInt(t, strings.Count(page, `id="fgs-viewer"`), 1)
	for _, unwanted := range []string{`data-menu="edit"`, `data-menu="copy"`, `data-menu="share"`, `data-menu="add-to-collection"`,
		`data-act="link"`, `data-act="change-expiry"`, `cell-listupload-`, "deleteFileFr"} {
		test.IsEqualBool(t, strings.Contains(page, unwanted), false)
	}
	// No direct download URL: the presigned flow supplies it in JS
	test.IsEqualBool(t, strings.Contains(two, `data-download=""`), true)
	test.IsEqualBool(t, strings.Contains(two, "./files/download?id="), false)
}

func TestNewFileRequestsView(t *testing.T) {
	user := models.User{Id: 5, Name: "Admin", Permissions: models.UserPermissionAll, UserLevel: models.UserLevelSuperAdmin}
	f := accessFile("frviewmodel01", func(f *models.File) { f.UserId = 5; f.UploadRequestId = "frmodel" })
	request := models.FileRequest{Id: "frmodel", UserId: 5, Name: "Model", Files: []models.File{f}}
	admin := AdminView{FileRequests: []models.FileRequest{request}}

	view := newFileRequestsView(httptest.NewRequest("GET", "/filerequests", nil), admin, user)
	b, ok := view.Browsers["frmodel"]
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, b.Id, "fr-frmodel")
	test.IsEqualString(t, b.Context, ctxFileRequest)
	test.IsEqualString(t, b.DefaultView, viewList)
	test.IsEqualInt(t, len(b.Items), 1)
	test.IsEqualString(t, b.Items[0].DownloadUrl, "")
	test.IsEqualBool(t, b.Items[0].CanDelete, true)
	// the zip button comes from a handler, not from a zip URL
	test.IsEqualString(t, b.ZipUrl, "")
	test.IsEqualBool(t, b.ZipByHandler, true)
	// A viewer who may not delete other people's uploads cannot delete this one
	other := newFileRequestsView(httptest.NewRequest("GET", "/filerequests", nil), admin, models.User{Id: 8})
	test.IsEqualBool(t, other.Browsers["frmodel"].Items[0].CanDelete, false)
}
