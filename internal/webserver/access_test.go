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
	"github.com/forceu/gokapi/internal/webserver/authentication"
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
	t.Run("file request upload is viewable by its owner and PERM_LIST only", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(anon, req, ""), false)
		test.IsEqualBool(t, access.CanView(stranger, req, ""), false)
		test.IsEqualBool(t, access.CanView(expiredSession, req, ""), false)
		test.IsEqualBool(t, access.CanView(bogusSession, req, ""), false)
		test.IsEqualBool(t, access.CanView(uploader, req, ""), true)
		test.IsEqualBool(t, access.CanView(admin, req, ""), true)
	})
	t.Run("a file password cookie never opens a file request upload", func(t *testing.T) {
		reqPw := accessFile("accessreqpw00000001", func(f *models.File) { f.UploadRequestId = "r"; f.PasswordHash = "x" })
		r := sessionRequest("")
		r.AddCookie(&http.Cookie{Name: "p" + reqPw.Id, Value: downloadPasswordToken.Generate(reqPw.Id)})
		test.IsEqualBool(t, access.CanView(r, reqPw, ""), false)
		test.IsEqualBool(t, access.CanView(uploader, reqPw, ""), true)
		test.IsEqualBool(t, access.CanView(stranger, reqPw, ""), false)
	})
	t.Run("a file request upload that is end-to-end encrypted stays hidden from its owner", func(t *testing.T) {
		reqE2e := accessFile("accessreqe2e00000001", func(f *models.File) {
			f.UploadRequestId = "r"
			f.Encryption.IsEndToEndEncrypted = true
		})
		test.IsEqualBool(t, access.CanView(uploader, reqE2e, ""), false)
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

// withAuthMode runs fn with the given authentication configuration and restores the real one.
func withAuthMode(t *testing.T, cfg models.AuthenticationConfig, fn func()) {
	t.Helper()
	original := configuration.Get().Authentication
	authentication.Init(cfg)
	defer authentication.Init(original)
	fn()
}

// Only a real session cookie (Internal / OAuth2) may widen access. Disabled
// mode names the super admin for every anonymous request, and Header mode
// trusts a request header; neither may let a public image route past a password.
func TestCanViewOnlyTrustsSessions(t *testing.T) {
	database.SaveSession("accessadmin", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 5})
	pw := accessFile("accessmode000000001", func(f *models.File) { f.PasswordHash = "x" })
	base := configuration.Get().Authentication

	t.Run("internal session widens", func(t *testing.T) {
		test.IsEqualBool(t, access.CanView(sessionRequest("accessadmin"), pw, ""), true)
		test.IsEqualBool(t, access.CanView(sessionRequest(""), pw, ""), false)
	})
	t.Run("oauth2 session widens", func(t *testing.T) {
		cfg := base
		cfg.Method = models.AuthenticationOAuth2
		cfg.OAuthProvider, cfg.OAuthClientId, cfg.OAuthClientSecret, cfg.OAuthRecheckInterval = "p", "id", "secret", 12
		withAuthMode(t, cfg, func() {
			test.IsEqualBool(t, access.CanView(sessionRequest("accessadmin"), pw, ""), true)
			test.IsEqualBool(t, access.CanView(sessionRequest(""), pw, ""), false)
		})
	})
	t.Run("disabled auth: anonymous gets nothing extra", func(t *testing.T) {
		cfg := base
		cfg.Method = models.AuthenticationDisabled
		withAuthMode(t, cfg, func() {
			test.IsEqualBool(t, access.CanView(sessionRequest(""), pw, ""), false)
			// not even a valid session cookie matters: there is no login in this mode
			test.IsEqualBool(t, access.CanView(sessionRequest("accessadmin"), pw, ""), false)
			// a link-holder file is of course still viewable
			plain := accessFile("accessmode000000002", nil)
			test.IsEqualBool(t, access.CanView(sessionRequest(""), plain, ""), true)
		})
	})
	t.Run("header auth: a header naming an admin does not widen and creates no user", func(t *testing.T) {
		cfg := base
		cfg.Method = models.AuthenticationHeader
		cfg.HeaderKey = "X-Remote-User"
		withAuthMode(t, cfg, func() {
			r := sessionRequest("")
			r.Header.Set("X-Remote-User", "Test") // user 5, the super admin
			test.IsEqualBool(t, access.CanView(r, pw, ""), false)
			r = sessionRequest("accessadmin")
			r.Header.Set("X-Remote-User", "headerspoofuser")
			test.IsEqualBool(t, access.CanView(r, pw, ""), false)
			_, created := database.GetUserByName("headerspoofuser")
			test.IsEqualBool(t, created, false)
		})
	})
}

// An access check must be a pure read: no session renewal or deletion, no
// last-online update.
func TestCanViewWritesNothing(t *testing.T) {
	f := accessFile("accesswrites00000001", func(f *models.File) { f.PasswordHash = "x" })
	database.SaveUser(models.User{Id: 9, Name: "Writes", Permissions: models.UserPermissionAll, UserLevel: models.UserLevelUser, LastOnline: 1234}, false)
	database.SaveSession("accesswritessession", models.Session{RenewAt: 0, ValidUntil: 2147483646, UserId: 9}) // due for renewal
	sessionsBefore := len(database.GetAllMetadata())
	test.IsEqualBool(t, access.CanView(sessionRequest("accesswritessession"), f, ""), true)
	time.Sleep(100 * time.Millisecond) // useSession would update last-online in a goroutine
	user, _ := database.GetUser(9)
	test.IsEqualInt64(t, user.LastOnline, 1234)
	session, ok := database.GetSession("accesswritessession")
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt64(t, session.RenewAt, 0)
	test.IsEqualInt(t, len(database.GetAllMetadata()), sessionsBefore)
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
	ver := models.Preview{Sha1: "routesha1", Updated: 1}.Version()

	get := func(h http.HandlerFunc, url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("GET", url, nil))
		return w
	}

	w := get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id+"&v="+ver)
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, max-age=31536000, immutable")

	// the token follows the row: regenerating (a new Updated) retires cached URLs
	oldVer := ver
	database.SavePreview(models.Preview{Sha1: "routesha1", Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 2})
	w = get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id+"&v="+oldVer)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, no-cache")
	ver = models.Preview{Sha1: "routesha1", Updated: 2}.Version()
	w = get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id+"&v="+ver)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, max-age=31536000, immutable")

	// immutable only with the matching version
	w = get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id+"&v=stale")
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, no-cache")
	w = get(servePreviewFile("thumb"), "/p/thumb?id="+f.Id)
	test.IsEqualString(t, w.Header().Get("Cache-Control"), "private, no-cache")

	// no poster was written
	w = get(servePreviewFile("poster"), "/p/poster?id="+f.Id+"&v="+ver)
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
	resp, err := http.Get("http://localhost:53843/p/thumb?id=" + f.Id + "&v="+ver)
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
	anonRq := sessionRequest("")
	f := accessFile("previewurls000000001", func(f *models.File) { f.SHA1 = "urlssha1abcdefghijkl" })
	info := previewUrls(anonRq, f, "", "d?id=x")
	test.IsEqualString(t, info.Status, "")
	test.IsEqualString(t, info.Download, "d?id=x")
	test.IsEqualString(t, info.Thumb, "")

	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewPending})
	info = previewUrls(anonRq, f, "", "d?id=x")
	test.IsEqualString(t, info.Status, models.PreviewPending)
	test.IsEqualString(t, info.Thumb, "")

	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 2, Color: "#000000", TakenAt: 5, Updated: 72})
	info = previewUrls(anonRq, f, "colid", "d?id=x")
	test.IsEqualString(t, info.Thumb, "./p/thumb?id="+f.Id+"&v=urlssha1abcd-20&c=colid") // 72 = "20" in base 36
	test.IsEqualString(t, info.View, "./p/view?id="+f.Id+"&v=urlssha1abcd-20&c=colid")
	test.IsEqualString(t, info.Kind, models.PreviewKindImage)
	test.IsEqualString(t, info.Video, "")
	test.IsEqualInt(t, info.Width, 3)
	test.IsEqualInt64(t, info.TakenAt, 5)

	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 72})
	info = previewUrls(anonRq, f, "", "d?id=x")
	test.IsEqualString(t, info.Video, "./p/video?id="+f.Id+"&v=urlssha1abcd-20")

	// regenerated (new Updated): the URL changes, so browsers refetch
	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 73})
	test.IsEqualString(t, previewUrls(anonRq, f, "", "d?id=x").Thumb, "./p/thumb?id="+f.Id+"&v=urlssha1abcd-21")
}

// Ruling 19: a capped video streams only for its uploader or a PERM_LIST
// user, and only they are offered the video URL. Ruling 22: an encrypted
// video is never offered, because /p/video refuses it.
func TestVideoStreamingRespectsDownloadCap(t *testing.T) {
	database.SaveUser(models.User{Id: 8, Name: "Stranger", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("accessstranger", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})
	database.SaveSession("accessadmin", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 5})

	capped := accessFile("videocapped00000001", func(f *models.File) {
		f.SHA1 = "videocappedsha"
		f.ContentType = "video/mp4"
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 3
	})
	open := accessFile("videoopen0000000001", func(f *models.File) { f.SHA1 = "videoopensha"; f.ContentType = "video/mp4" })
	encrypted := accessFile("videoencrypted00001", func(f *models.File) {
		f.SHA1 = "videoencsha"
		f.ContentType = "video/mp4"
		f.Encryption.IsEncrypted = true
	})
	for _, f := range []models.File{capped, open, encrypted} {
		database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindVideo, Updated: 1})
	}

	stream := func(token, id string) int {
		r := httptest.NewRequest("GET", "/p/video?id="+id, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "session_token", Value: token})
		}
		w := httptest.NewRecorder()
		streamVideo(w, r)
		return w.Code
	}
	offered := func(token string, f models.File) bool {
		return previewUrls(sessionRequest(token), f, "", "d?id=x").Video != ""
	}

	// The uploader (validsession is user 7) and the PERM_LIST admin may play it
	for _, token := range []string{"validsession", "accessadmin"} {
		test.IsEqualInt(t, stream(token, capped.Id), http.StatusOK)
		test.IsEqualBool(t, offered(token, capped), true)
	}
	// A link holder, logged in or not, may not - and is not offered the URL,
	// but still gets the still and the download
	for _, token := range []string{"", "accessstranger"} {
		test.IsEqualInt(t, stream(token, capped.Id), http.StatusNotFound)
		info := previewUrls(sessionRequest(token), capped, "", "d?id=x")
		test.IsEqualString(t, info.Video, "")
		test.IsEqualBool(t, info.View != "", true)
		test.IsEqualString(t, info.Download, "d?id=x")
	}
	// Without a cap, anyone who can see it can play it
	test.IsEqualInt(t, stream("", open.Id), http.StatusOK)
	test.IsEqualBool(t, offered("", open), true)
	// Streaming never touched the cap
	test.IsEqualInt(t, database.GetDownloadsRemaining(capped.Id), 3)

	// Encrypted: refused and never offered, not even to the uploader
	test.IsEqualInt(t, stream("validsession", encrypted.Id), http.StatusNotFound)
	test.IsEqualBool(t, offered("validsession", encrypted), false)
}

// A member of a live group sees the files in it, whatever their password;
// nobody else gains anything from the group (spec, "Access")
func TestCanViewGroupMembers(t *testing.T) {
	database.SaveUser(models.User{Id: 8, Name: "Stranger", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveUser(models.User{Id: 10, Name: "member@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("accessstranger", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})
	database.SaveSession("accessgroupmember", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 10})
	defer database.DeleteSession("accessgroupmember")

	f := accessFile("accessgroup00000001", func(f *models.File) { f.SHA1 = "accessgroupsha"; f.PasswordHash = "x" })
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	g, err := storage.CreateGroup("Access", 30, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, err = storage.JoinGroupByToken(g.InviteToken, models.User{Id: 10, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	res, err := storage.AddFilesToGroup(g, []string{f.Id}, owner)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(res.Added), 1)

	member := sessionRequest("accessgroupmember")
	test.IsEqualBool(t, access.CanView(member, f, ""), true)
	test.IsEqualBool(t, access.CanView(sessionRequest("accessstranger"), f, ""), false)
	test.IsEqualBool(t, access.CanView(sessionRequest(""), f, ""), false)

	// The group closed: membership grants nothing any more
	live := g
	g.Expires = time.Now().Unix() - 1
	database.SaveGroup(g)
	test.IsEqualBool(t, access.CanView(member, f, ""), false)
	// Open again, so the next check fails for its own reason
	database.SaveGroup(live)
	test.IsEqualBool(t, access.CanView(member, f, ""), true)

	// The file left the group
	database.RemoveFileFromGroups(f.Id)
	test.IsEqualBool(t, access.CanView(member, f, ""), false)
}
