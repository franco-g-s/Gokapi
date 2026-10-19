//go:build !integration && test

package webserver

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
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
	// The admin (PERM_LIST) gets the My files / Everything switch and sees
	// other users' files, including user 7's.
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/files",
		IsHtml:  true,
		Cookies: []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`id="fgs-files"`, "My files", "Everything", `data-view="grid"`,
			`data-id="unlimitedDownload"`, `data-id="filesmine"`, `id="fgs-filetable"`, `href="./files"`},
	})
	// A user without PERM_LIST gets neither the switch nor anyone else's files
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/files",
		IsHtml:          true,
		Cookies:         []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{`id="fgs-files"`, `data-view="grid"`, `data-id="filesmine"`},
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
		RequiredContent: []string{`id="downloadtable"`, "Downloads remaining", `<a href="./files">Files</a>`},
		ExcludedContent: []string{`id="row-unlimitedDownload"`},
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
