//go:build !integration && test

package webserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/preview"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/storage/presign"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// get requests a path on the test server with an optional session cookie,
// drains the body (zips are written while streaming) and returns the status
func downloadGet(t *testing.T, token, path string) int {
	t.Helper()
	req, err := http.NewRequest("GET", "http://localhost:53843"+path, nil)
	test.IsNil(t, err)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	resp, err := http.DefaultClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	test.IsNil(t, err)
	return resp.StatusCode
}

// TestEveryDownloadRouteRecords: each route that serves an original writes one
// Downloads row per file, for the signed-in person; viewing writes none
func TestEveryDownloadRouteRecords(t *testing.T) {
	database.SaveUser(models.User{Id: 8, Name: "dlstranger", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("dlstranger", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})

	a := accessFile("dlsourcea00000000001", func(f *models.File) { f.SHA1 = "dlsourceasha"; f.HotlinkId = "dlsourcea.jpg" })
	database.SaveHotlink(a)
	b := accessFile("dlsourceb00000000001", func(f *models.File) { f.SHA1 = "dlsourcebsha"; f.Name = "b.jpg" })
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "dl", FileIds: []string{a.Id, b.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	defer database.DeleteCollection(col.Id)

	clear := func() {
		database.DeleteDownloadsOfFile(a.Id)
		database.DeleteDownloadsOfFile(b.Id)
	}
	downloaded := func(userId int, f models.File) bool { return database.GetDownloadedFileIds(userId)[f.Id] }

	// request serves the route with the session and expects the files in want
	// to be recorded for userId, and the others not
	request := func(name, token, path string, userId int, want ...models.File) {
		t.Run(name, func(t *testing.T) {
			clear()
			test.IsEqualInt(t, downloadGet(t, token, path), http.StatusOK)
			for _, f := range []models.File{a, b} {
				expected := false
				for _, w := range want {
					expected = expected || w.Id == f.Id
				}
				test.IsEqualBool(t, downloaded(userId, f), expected)
			}
		})
	}

	request("downloadFile, owner", "validsession", "/downloadFile?id="+a.Id, 7, a)
	request("downloadFile, other signed-in user", "dlstranger", "/downloadFile?id="+a.Id, 8, a)
	request("dh with name in URL", "validsession", "/dh/"+a.Id+"/a.jpg", 7, a)
	request("hotlink", "validsession", "/h/"+a.HotlinkId, 7, a)
	request("own file", "validsession", "/files/download?id="+a.Id, 7, a)
	request("own files zip, two rows", "validsession", "/files/zip?ids="+a.Id+","+b.Id, 7, a, b)
	request("collection file", "validsession", "/cd?id="+col.Id+"&file="+b.Id, 7, b)
	request("collection zip, two rows", "validsession", "/cz?id="+col.Id, 7, a, b)

	t.Run("anonymous download records nothing for anybody signed in", func(t *testing.T) {
		clear()
		test.IsEqualInt(t, downloadGet(t, "", "/downloadFile?id="+a.Id), http.StatusOK)
		test.IsEqualBool(t, downloaded(7, a), false)
		test.IsEqualBool(t, downloaded(8, a), false)
		// ...while the same request with a session for user 8 does record
		test.IsEqualInt(t, downloadGet(t, "dlstranger", "/downloadFile?id="+a.Id), http.StatusOK)
		test.IsEqualBool(t, downloaded(8, a), true)
		test.IsEqualBool(t, downloaded(7, a), false)
	})

	t.Run("presigned, one file is a file and several are a zip", func(t *testing.T) {
		clear()
		presign.Save(models.Presign{Id: "dlpresignone", FileIds: []string{a.Id}, Expiry: time.Now().Add(time.Hour).Unix()})
		test.IsEqualInt(t, downloadGet(t, "dlstranger", "/downloadPresigned?key=dlpresignone"), http.StatusOK)
		test.IsEqualBool(t, downloaded(8, a), true)
		test.IsEqualBool(t, downloaded(8, b), false)

		clear()
		presign.Save(models.Presign{Id: "dlpresigntwo", FileIds: []string{a.Id, b.Id}, Expiry: time.Now().Add(time.Hour).Unix(), Filename: "two"})
		test.IsEqualInt(t, downloadGet(t, "dlstranger", "/downloadPresigned?key=dlpresigntwo"), http.StatusOK)
		test.IsEqualBool(t, downloaded(8, a), true)
		test.IsEqualBool(t, downloaded(8, b), true)
	})

	t.Run("HEAD sends no file and records nothing", func(t *testing.T) {
		clear()
		req, err := http.NewRequest("HEAD", "http://localhost:53843/downloadFile?id="+a.Id, nil)
		test.IsNil(t, err)
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "validsession"})
		resp, err := http.DefaultClient.Do(req)
		test.IsNil(t, err)
		resp.Body.Close()
		test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
		test.IsEqualBool(t, downloaded(7, a), false)
	})

	t.Run("presigned attributes to the context user, with no session cookie", func(t *testing.T) {
		// Header and Disabled authentication have no session: requireLogin
		// puts the user in the context, and that is who downloaded
		for _, ids := range [][]string{{a.Id}, {a.Id, b.Id}} {
			clear()
			presign.Save(models.Presign{Id: "dlpresignctx", FileIds: ids, Expiry: time.Now().Add(time.Hour).Unix(), Filename: "ctx"})
			r := httptest.NewRequest("GET", "/downloadPresigned?key=dlpresignctx", nil)
			r = authentication.SetUserInRequest(r, models.User{Id: 8})
			w := httptest.NewRecorder()
			downloadPresigned(w, r)
			test.IsEqualInt(t, w.Code, http.StatusOK)
			for _, f := range []models.File{a, b} {
				want := f.Id == a.Id || len(ids) == 2
				test.IsEqualBool(t, downloaded(8, f), want)
			}
			test.IsEqualBool(t, downloaded(7, a), false)
		}
	})

	t.Run("an expired or unknown file records nothing", func(t *testing.T) {
		clear()
		gone := accessFile("dlsourcegone0000001", func(f *models.File) { f.ExpireAt = time.Now().Add(-time.Hour).Unix() })
		downloadGet(t, "validsession", "/downloadFile?id="+gone.Id)
		test.IsEqualBool(t, downloaded(7, gone), false)
	})

	t.Run("viewing never records", func(t *testing.T) {
		v := accessFile("dlsourcevideo000001", func(f *models.File) { f.SHA1 = "dlsourcevsha"; f.ContentType = "video/mp4" })
		test.IsNil(t, os.MkdirAll(preview.Dir(), 0700))
		thumb, view, poster := preview.Paths(preview.Dir(), v.SHA1)
		for _, p := range []string{thumb, view, poster} {
			test.IsNil(t, os.WriteFile(p, []byte("RIFFxxxxWEBP"), 0600))
		}
		database.SavePreview(models.Preview{Sha1: v.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 1})
		database.DeleteDownloadsOfFile(v.Id)

		for _, path := range []string{
			"/p/thumb?id=" + v.Id, "/p/view?id=" + v.Id, "/p/poster?id=" + v.Id,
			"/p/video?id=" + v.Id, "/p/info?id=" + v.Id, "/files/previewinfo?id=" + v.Id,
		} {
			// each one is served for the owner, so a missing row is not a refused request
			test.IsEqualInt(t, downloadGet(t, "validsession", path), http.StatusOK)
			test.IsEqualBool(t, downloaded(7, v), false)
		}
	})
}
