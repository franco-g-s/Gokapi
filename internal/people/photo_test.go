//go:build test

package people

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

var jpegBytes = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{1}, 200)...)
var pngBytes = append([]byte("\x89PNG\x0d\x0a\x1a\x0a"), bytes.Repeat([]byte{2}, 200)...)

// photoServer serves a fixed body at /ok, /png, /big, /html and /slow, and
// counts the requests. The photo client and host check are pointed at it.
func photoServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write(jpegBytes)
		case "/png":
			_, _ = w.Write(pngBytes)
		case "/big":
			_, _ = w.Write(append(jpegBytes, bytes.Repeat([]byte{3}, maxPhotoBytes)...))
		case "/html":
			_, _ = w.Write([]byte("<html><body>not a photo</body></html>"))
		case "/slow":
			time.Sleep(400 * time.Millisecond)
			_, _ = w.Write(jpegBytes)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/redirect-away":
			http.Redirect(w, r, "https://evil.example.com/x.jpg", http.StatusFound)
		}
	}))
	oldClient, oldHostOk, oldSync := photoClient, photoHostOk, fetchPhotoSync
	client := srv.Client()
	client.Timeout = 200 * time.Millisecond
	client.CheckRedirect = checkPhotoRedirect
	photoClient = client
	srvUrl, _ := url.Parse(srv.URL)
	photoHostOk = func(u *url.URL) bool { return u.Scheme == "https" && u.Host == srvUrl.Host }
	fetchPhotoSync = true
	t.Cleanup(func() {
		srv.Close()
		photoClient, photoHostOk, fetchPhotoSync = oldClient, oldHostOk, oldSync
	})
	return srv, &count
}

func TestPhotoHostCheck(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://lh3.googleusercontent.com/a/abc=s96-c": true,
		"http://lh3.googleusercontent.com/a/abc":        false,
		"https://googleusercontent.com/a":               false,
		"https://evilgoogleusercontent.com/a":           false,
		"https://lh3.googleusercontent.com.evil.com/a":  false,
		"https://example.com/a.jpg":                     false,
	} {
		u, err := url.Parse(raw)
		test.IsNil(t, err)
		if photoHostOk(u) != want {
			t.Errorf("%s: want %t", raw, want)
		}
	}
}

func TestPhotoFetchStoresJpeg(t *testing.T) {
	srv, count := photoServer(t)
	u := models.User{Id: 503, Name: "carla@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	test.IsEqualString(t, PhotoUrl(u), "")

	OnSignIn(u, map[string]any{"name": "Carla", "picture": srv.URL + "/ok"}, true)
	data, ct, updated, ok := PhotoOf(u.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, ct, "image/jpeg")
	test.IsEqualBool(t, bytes.Equal(data, jpegBytes), true)
	test.IsEqualString(t, PhotoUrl(u), "./avatar?id=503&v="+strconv.FormatInt(updated, 10))
	test.IsEqualInt(t, int(atomic.LoadInt32(count)), 1)

	// Within 24 h a sign-in does not fetch again
	OnSignIn(u, map[string]any{"name": "Carla", "picture": srv.URL + "/png"}, false)
	test.IsEqualInt(t, int(atomic.LoadInt32(count)), 1)
	_, ct, _, _ = PhotoOf(u.Id)
	test.IsEqualString(t, ct, "image/jpeg")

	// A day later it does
	p, _ := database.GetProfile(u.Id)
	p.PhotoUpdated -= 86401
	database.SaveProfile(p)
	OnSignIn(u, map[string]any{"name": "Carla", "picture": srv.URL + "/png"}, false)
	test.IsEqualInt(t, int(atomic.LoadInt32(count)), 2)
	_, ct, _, _ = PhotoOf(u.Id)
	test.IsEqualString(t, ct, "image/png")

	// "Use initials instead" hides it
	on := true
	SetPreferences(u.Id, nil, &on)
	test.IsEqualString(t, PhotoUrl(u), "")
	_, _, _, ok = PhotoOf(u.Id) // still stored, for the person's own Settings page
	test.IsEqualBool(t, ok, true)
}

func TestPhotoFetchRefusalsKeepTheOldPhoto(t *testing.T) {
	srv, count := photoServer(t)
	u := models.User{Id: 505, Name: "eve@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	test.IsNil(t, StorePhoto(u.Id, pngBytes))
	_, _, before, ok := PhotoOf(u.Id)
	test.IsEqualBool(t, ok, true)

	srvUrl, _ := url.Parse(srv.URL)
	for _, raw := range []string{
		"http://" + srvUrl.Host + "/ok", // not https
		"https://example.com/ok",        // not the allowed host
		srv.URL + "/big",                // over 512 KB
		srv.URL + "/html",               // sniffed as text/html
		srv.URL + "/slow",               // times out
		srv.URL + "/missing",            // 404
		srv.URL + "/redirect-away",      // redirect to another host
		"::not a url",
	} {
		fetchPhoto(u.Id, raw)
		data, ct, updated, ok := PhotoOf(u.Id)
		if !ok || ct != "image/png" || !bytes.Equal(data, pngBytes) || updated != before {
			t.Errorf("%s replaced the photo: %s %d", raw, ct, updated)
		}
	}
	// the non-https, wrong-host and unparsable URLs never reached the server
	test.IsEqualBool(t, atomic.LoadInt32(count) <= 5, true)
}

func TestStorePhotoValidates(t *testing.T) {
	t.Cleanup(func() { database.DeleteUser(506) })
	test.IsNotNil(t, StorePhoto(506, []byte("<html></html>")))
	test.IsNotNil(t, StorePhoto(506, nil))
	test.IsNotNil(t, StorePhoto(506, append(jpegBytes, bytes.Repeat([]byte{3}, maxPhotoBytes)...)))
	_, _, _, ok := PhotoOf(506)
	test.IsEqualBool(t, ok, false)
	test.IsNil(t, StorePhoto(506, jpegBytes))
	_, ct, _, ok := PhotoOf(506)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, ct, "image/jpeg")
}
