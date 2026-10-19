//go:build !integration && test

package webserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/preview"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/access"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// accessFile saves a file owned by user 7 and writes its blob to disk, because
// storage.GetFile refuses files whose blob is missing and every allow-case
// would otherwise fail for the wrong reason.
func accessFile(id string, mutate func(*models.File)) models.File {
	f := models.File{Id: id, Name: "a.jpg", SHA1: "accesssha", UserId: 7, SizeBytes: 1,
		ExpireAt: time.Now().Add(time.Hour).Unix(), UnlimitedDownloads: true}
	if mutate != nil {
		mutate(&f)
	}
	database.SaveMetaData(f)
	if err := os.WriteFile(filepath.Join(configuration.Get().DataDir, f.SHA1), make([]byte, 4096), 0600); err != nil {
		panic(err)
	}
	return f
}

func sessionRequest(token string) *http.Request {
	r := httptest.NewRequest("GET", "/p/thumb", nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	return r
}

func TestCanView(t *testing.T) {
	// user 7 (no permissions) is the uploader; user 5 is the super admin
	// (PERM_LIST); user 8 is logged in but unrelated
	database.SaveUser(models.User{Id: 8, Name: "Stranger", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("accessstranger", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})
	database.SaveSession("accessadmin", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 5})

	anon := sessionRequest("")
	uploader := sessionRequest("validsession")
	admin := sessionRequest("accessadmin")
	stranger := sessionRequest("accessstranger")
	expiredSession := sessionRequest("expiredsession")
	bogusSession := sessionRequest("nosuchsession")

	plain := accessFile("accessplain00000001", nil)
	pw := accessFile("accesspw00000000001", func(f *models.File) { f.PasswordHash = "x" })
	req := accessFile("accessreq0000000001", func(f *models.File) { f.UploadRequestId = "r" })
	e2e := accessFile("accesse2e0000000001", func(f *models.File) { f.Encryption.IsEndToEndEncrypted = true })
	gone := accessFile("accessexp0000000001", func(f *models.File) { f.ExpireAt = time.Now().Add(-time.Hour).Unix() })

	t.Run("link holder", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(anon, plain, ""), true)
		test.IsEqualBool(t, access.CanView(stranger, plain, ""), true)
	})
	t.Run("password without cookie", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(anon, pw, ""), false)
		test.IsEqualBool(t, access.CanView(stranger, pw, ""), false)
		test.IsEqualBool(t, access.CanView(expiredSession, pw, ""), false)
		test.IsEqualBool(t, access.CanView(bogusSession, pw, ""), false)
	})
	t.Run("stale model cannot bypass the stored password", func(t *testing.T) {
		stale := pw
		stale.PasswordHash = ""
		test.IsEqualBool(t, access.CanView(anon, stale, ""), false)
	})
	t.Run("uploader and admin", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(uploader, pw, ""), true)
		test.IsEqualBool(t, access.CanView(admin, pw, ""), true)
	})
	t.Run("file request upload is never viewable", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(anon, req, ""), false)
		test.IsEqualBool(t, access.CanView(uploader, req, ""), false)
		test.IsEqualBool(t, access.CanView(admin, req, ""), false)
	})
	t.Run("end-to-end encrypted is never viewable", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(anon, e2e, ""), false)
		test.IsEqualBool(t, access.CanView(uploader, e2e, ""), false)
		test.IsEqualBool(t, access.CanView(admin, e2e, ""), false)
	})
	t.Run("expired", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(anon, gone, ""), false)
		test.IsEqualBool(t, access.CanView(admin, gone, ""), false)
	})
	t.Run("unknown file", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(admin, models.File{Id: "doesnotexist0000000"}, ""), false)
	})
	t.Run("file password cookie", func(t *testing.T) {
		r := sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "p" + pw.Id, Value: downloadPasswordToken.Generate(pw.Id)})
		test.IsEqualBool(t, access.CanView(r, pw, ""), true)
		// token minted for a different file
		r = sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "p" + pw.Id, Value: downloadPasswordToken.Generate(plain.Id)})
		test.IsEqualBool(t, access.CanView(r, pw, ""), false)
		// token that has already lapsed
		r = sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "p" + pw.Id, Value: downloadPasswordToken.GenerateWithTtl(pw.Id, -time.Hour)})
		test.IsEqualBool(t, access.CanView(r, pw, ""), false)
	})

	t.Run("collections", func(t *testing.T) {
		locked, err := storage.CreateCollection(storage.CollectionParameters{Name: "c", FileIds: []string{pw.Id}, Password: "pw"}, models.User{Id: 7})
		test.IsNil(t, err)
		open, err := storage.CreateCollection(storage.CollectionParameters{Name: "o", FileIds: []string{plain.Id}}, models.User{Id: 7})
		test.IsNil(t, err)
		openWithPw, err := storage.CreateCollection(storage.CollectionParameters{Name: "op", FileIds: []string{pw.Id}}, models.User{Id: 7})
		test.IsNil(t, err)

		test.IsEqualBool(t, access.CanView(anon, pw, locked.Id), false) // locked collection
		// unlocked collection
		r := sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "pc" + locked.Id, Value: downloadPasswordToken.GenerateWithTtl(locked.Id, time.Hour)})
		test.IsEqualBool(t, access.CanView(r, pw, locked.Id), true)
		// a cookie for another collection does not unlock this one
		r = sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "pc" + locked.Id, Value: downloadPasswordToken.GenerateWithTtl(open.Id, time.Hour)})
		test.IsEqualBool(t, access.CanView(r, pw, locked.Id), false)
		// the unlock cookie is a collection token, not a file token
		r = sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "pc" + locked.Id, Value: downloadPasswordToken.GenerateWithTtl(locked.Id, time.Hour)})
		test.IsEqualBool(t, access.CanView(r, pw, ""), false)
		// an expired unlock token
		r = sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "pc" + locked.Id, Value: downloadPasswordToken.GenerateWithTtl(locked.Id, -time.Hour)})
		test.IsEqualBool(t, access.CanView(r, pw, locked.Id), false)

		// file not in the given collection falls through to the file's own rule
		test.IsEqualBool(t, access.CanView(anon, pw, open.Id), false)
		test.IsEqualBool(t, access.CanView(anon, plain, locked.Id), true)
		// a collection that does not exist grants nothing
		test.IsEqualBool(t, access.CanView(anon, pw, "nosuchcollection"), false)
		// an unprotected collection page shows its members, same as /c
		test.IsEqualBool(t, access.CanView(anon, pw, openWithPw.Id), true)
		// a file whose own link is open, listed in a locked collection, is not viewable through that collection
		both, err := storage.CreateCollection(storage.CollectionParameters{Name: "b", FileIds: []string{plain.Id}, Password: "pw"}, models.User{Id: 7})
		test.IsNil(t, err)
		test.IsEqualBool(t, access.CanView(anon, plain, both.Id), false)
		test.IsEqualBool(t, access.CanView(anon, plain, ""), true)
	})
}

// A session that is due for renewal must neither panic nor be consumed by an
// access check: IsAuthenticated would delete it and lose the replacement cookie.
func TestCanViewDoesNotConsumeSession(t *testing.T) {
	f := accessFile("accesssession0000001", func(f *models.File) { f.PasswordHash = "x" })
	r := sessionRequest("needsRenewal")
	test.IsEqualBool(t, access.CanView(r, f, ""), true)
	_, ok := database.GetSession("needsRenewal")
	test.IsEqualBool(t, ok, true)
	// and a lapsed session is neither accepted nor deleted by a read
	test.IsEqualBool(t, access.CanView(sessionRequest("expiredsession"), f, ""), false)
}

func TestCollectionCookieLasts12Hours(t *testing.T) {
	w := httptest.NewRecorder()
	writeCollectionPwCookie(w, models.Collection{Id: "ttlcollection"})
	cookies := w.Result().Cookies()
	test.IsEqualInt(t, len(cookies), 1)
	remaining := time.Until(cookies[0].Expires)
	test.IsEqualBool(t, remaining > 11*time.Hour+55*time.Minute && remaining <= 12*time.Hour, true)

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])
	test.IsEqualBool(t, access.ValidCollectionPwCookie(r, "ttlcollection"), true)
	test.IsEqualBool(t, isValidCollectionPwCookie(r, models.Collection{Id: "ttlcollection"}), true)
	// and the token itself outlives the old five minutes
	test.IsEqualBool(t, downloadPasswordToken.IsValid(cookies[0].Value, "ttlcollection"), true)
}

func TestPreviewRoutes(t *testing.T) {
	f := accessFile("previewroute0000001", func(f *models.File) { f.SHA1 = "routesha1"; f.ContentType = "video/mp4" })
	test.IsNil(t, os.MkdirAll(preview.Dir(), 0700))
	thumb, _, _ := preview.Paths(preview.Dir(), "routesha1")
	test.IsNil(t, os.WriteFile(thumb, []byte("RIFFxxxxWEBP"), 0600))
	database.SavePreview(models.Preview{Sha1: "routesha1", Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 1})

	get := func(h http.HandlerFunc, url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("GET", url, nil))
		return w
	}

	w := get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id+"&v=routesha1")
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, max-age=31536000, immutable")

	// immutable only with the matching version
	w = get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id+"&v=stale")
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, no-cache")
	w = get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, no-cache")

	// no poster was written
	w = get(servePreviewFile("poster"), "/p/poster?id="+f.Id+"&v=routesha1")
	test.IsEqualInt(t, w.Code, 404)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "no-store")

	// unknown id
	w = get(servePreviewFile("thumb"), "/p/thumb?id=nosuchfile&v=routesha1")
	test.IsEqualInt(t, w.Code, 404)

	// video: range request is 206 and is not a download
	before, _ := database.GetMetaDataById(f.Id)
	w = httptest.NewRecorder()
	rq := httptest.NewRequest("GET", "/p/video?id="+f.Id, nil)
	rq.Header.Set("Range", "bytes=0-99")
	streamVideo(w, rq)
	test.IsEqualInt(t, w.Code, 206)
	test.IsEqualInt(t, w.Body.Len(), 100)
	test.IsEqualString(t, w.Header().Get("Content-Type"), "video/mp4")
	after, _ := database.GetMetaDataById(f.Id)
	test.IsEqualInt(t, after.DownloadCount, before.DownloadCount)
	test.IsEqualBool(t, after.DownloadsRemaining == before.DownloadsRemaining, true)

	// the routes are registered on the real server
	resp, err := http.Get("http://localhost:53843/p/thumb?id=" + f.Id + "&v=routesha1")
	test.IsNil(t, err)
	resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, 200)
	resp, err = http.Get("http://localhost:53843/p/video?id=" + f.Id)
	test.IsNil(t, err)
	resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, 200)
}

func TestPreviewRoutesDeny(t *testing.T) {
	pwFile := accessFile("previewpw0000000001", func(f *models.File) {
		f.SHA1 = "routepwsha1"
		f.PasswordHash = "x"
		f.ContentType = "video/mp4"
	})
	thumb, _, _ := preview.Paths(preview.Dir(), "routepwsha1")
	test.IsNil(t, os.MkdirAll(preview.Dir(), 0700))
	test.IsNil(t, os.WriteFile(thumb, []byte("RIFFxxxxWEBP"), 0600))
	database.SavePreview(models.Preview{Sha1: "routepwsha1", Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 1})

	for _, c := range []struct {
		name    string
		handler http.HandlerFunc
		url     string
	}{
		{"thumb", servePreviewFile("thumb"), "/p/thumb?id=" + pwFile.Id + "&v=routepwsha1"},
		{"view", servePreviewFile("view"), "/p/view?id=" + pwFile.Id + "&v=routepwsha1"},
		{"video", streamVideo, "/p/video?id=" + pwFile.Id},
	} {
		w := httptest.NewRecorder()
		c.handler(w, httptest.NewRequest("GET", c.url, nil))
		test.IsEqualInt(t, w.Code, 404)
		test.IsEqualBool(t, w.Body.Len() < 100, true)
	}

	// with the file password cookie the same requests work
	rq := httptest.NewRequest("GET", "/p/thumb?id="+pwFile.Id+"&v=routepwsha1", nil)
	rq.AddCookie(&http.Cookie{Name: "p" + pwFile.Id, Value: downloadPasswordToken.Generate(pwFile.Id)})
	w := httptest.NewRecorder()
	servePreviewFile("thumb")(w, rq)
	test.IsEqualInt(t, w.Code, 200)

	// pending preview: 404 and never cached
	pending := accessFile("previewpending00001", func(f *models.File) { f.SHA1 = "routependsha" })
	database.SavePreview(models.Preview{Sha1: "routependsha", Status: models.PreviewPending, Updated: 1})
	w = httptest.NewRecorder()
	servePreviewFile("thumb")(w, httptest.NewRequest("GET", "/p/thumb?id="+pending.Id+"&v=routependsha", nil))
	test.IsEqualInt(t, w.Code, 404)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "no-store")

	// a file with no preview row, and a non-video, are not streamed
	noRow := accessFile("previewnorow0000001", func(f *models.File) { f.SHA1 = "routenorowsha"; f.ContentType = "text/html" })
	w = httptest.NewRecorder()
	streamVideo(w, httptest.NewRequest("GET", "/p/video?id="+noRow.Id, nil))
	test.IsEqualInt(t, w.Code, 404)
}

func TestPreviewUrls(t *testing.T) {
	f := accessFile("previewurls000000001", func(f *models.File) { f.SHA1 = "urlssha1abcdefghijkl" })
	info := previewUrls(f, "", "d?id=x")
	test.IsEqualString(t, info.Status, "")
	test.IsEqualString(t, info.Download, "d?id=x")
	test.IsEqualString(t, info.Thumb, "")

	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewPending})
	info = previewUrls(f, "", "d?id=x")
	test.IsEqualString(t, info.Status, models.PreviewPending)
	test.IsEqualString(t, info.Thumb, "")

	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 2, Color: "#000000", TakenAt: 5})
	info = previewUrls(f, "colid", "d?id=x")
	test.IsEqualString(t, info.Thumb, "./p/thumb?id="+f.Id+"&v=urlssha1abcd&c=colid")
	test.IsEqualString(t, info.View, "./p/view?id="+f.Id+"&v=urlssha1abcd&c=colid")
	test.IsEqualString(t, info.Video, "")
	test.IsEqualInt(t, info.Width, 3)
	test.IsEqualInt64(t, info.TakenAt, 5)

	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindVideo})
	info = previewUrls(f, "", "d?id=x")
	test.IsEqualString(t, info.Video, "./p/video?id="+f.Id+"&v=urlssha1abcd")
}
