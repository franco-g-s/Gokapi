//go:build test

package people

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

var jpegBytes = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{1}, 200)...)
var pngBytes = append([]byte("\x89PNG\x0d\x0a\x1a\x0a"), bytes.Repeat([]byte{2}, 200)...)

type photoServers struct {
	srv       *httptest.Server
	count     *int32 // requests to srv
	evilCount *int32 // requests to the server outside the allowlist
}

// photoServer serves fixed bodies on an allowed host, and a second server
// outside the allowlist that /redirect-away points at. The production
// photoClient is used as it is, CheckRedirect included: the tests replace
// only photoHostOk, the transport (to trust the test certificate) and the
// timeout (so the slow case takes 200 ms rather than 5 s).
func photoServer(t *testing.T) photoServers {
	t.Helper()
	var count, evilCount int32
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&evilCount, 1)
		_, _ = w.Write(jpegBytes)
	}))
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
			http.Redirect(w, r, evil.URL+"/x.jpg", http.StatusFound)
		case "/redirect-ok":
			http.Redirect(w, r, "/png", http.StatusFound)
		}
	}))
	oldTransport, oldTimeout, oldHostOk, oldSync := photoClient.Transport, photoClient.Timeout, photoHostOk, fetchPhotoSync
	photoClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}}
	photoClient.Timeout = 200 * time.Millisecond
	srvUrl, _ := url.Parse(srv.URL)
	photoHostOk = func(u *url.URL) bool { return u.Scheme == "https" && u.Host == srvUrl.Host }
	fetchPhotoSync = true
	t.Cleanup(func() {
		srv.Close()
		evil.Close()
		photoClient.Transport, photoClient.Timeout, photoHostOk, fetchPhotoSync = oldTransport, oldTimeout, oldHostOk, oldSync
	})
	return photoServers{srv: srv, count: &count, evilCount: &evilCount}
}

func hits(c *int32) int { return int(atomic.LoadInt32(c)) }

// ageAttempt pretends the last fetch attempt was over a day ago
func ageAttempt(userId int) {
	p, _ := database.GetProfile(userId)
	p.PhotoTried -= 86401
	database.SaveProfile(p)
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
	test.IsEqualBool(t, photoClient.CheckRedirect != nil, true)
	test.IsEqualBool(t, photoClient.Timeout == 5*time.Second, true)
}

func TestPhotoFetchStoresJpeg(t *testing.T) {
	ps := photoServer(t)
	u := models.User{Id: 503, Name: "carla@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	test.IsEqualString(t, PhotoUrl(u), "")

	OnSignIn(u, map[string]any{"name": "Carla", "picture": ps.srv.URL + "/ok"}, true)
	data, ct, updated, ok := PhotoOf(u.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, ct, "image/jpeg")
	test.IsEqualBool(t, bytes.Equal(data, jpegBytes), true)
	test.IsEqualString(t, PhotoUrl(u), "./avatar?id=503&v="+strconv.FormatInt(updated, 10))
	test.IsEqualInt(t, hits(ps.count), 1)

	// Within 24 h a sign-in does not fetch again
	OnSignIn(u, map[string]any{"name": "Carla", "picture": ps.srv.URL + "/png"}, false)
	test.IsEqualInt(t, hits(ps.count), 1)
	_, ct, _, _ = PhotoOf(u.Id)
	test.IsEqualString(t, ct, "image/jpeg")

	// A day later it does; a redirect within the allowed host is followed
	ageAttempt(u.Id)
	OnSignIn(u, map[string]any{"name": "Carla", "picture": ps.srv.URL + "/redirect-ok"}, false)
	test.IsEqualInt(t, hits(ps.count), 3) // the redirect and /png
	_, ct, _, _ = PhotoOf(u.Id)
	test.IsEqualString(t, ct, "image/png")

	// "Use initials instead" hides it
	on := true
	SetPreferences(u.Id, nil, &on)
	test.IsEqualString(t, PhotoUrl(u), "")
	_, _, _, ok = PhotoOf(u.Id) // still stored, for the person's own Settings page
	test.IsEqualBool(t, ok, true)
}

// Fix round 1: a picture that keeps failing is fetched at most once a day
func TestPhotoFailingFetchIsTriedOnceADay(t *testing.T) {
	ps := photoServer(t)
	u := models.User{Id: 508, Name: "gina@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	claims := map[string]any{"picture": ps.srv.URL + "/html"}
	OnSignIn(u, claims, true)
	OnSignIn(u, claims, false)
	OnSignIn(u, claims, false)
	test.IsEqualInt(t, hits(ps.count), 1)
	_, _, _, ok := PhotoOf(u.Id)
	test.IsEqualBool(t, ok, false)
	ageAttempt(u.Id)
	OnSignIn(u, claims, false)
	test.IsEqualInt(t, hits(ps.count), 2)
}

// Fix round 1: concurrent sign-ins start one fetch
func TestPhotoConcurrentSignInsFetchOnce(t *testing.T) {
	ps := photoServer(t)
	u := models.User{Id: 509, Name: "hugo@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			OnSignIn(u, map[string]any{"name": "Hugo", "picture": ps.srv.URL + "/ok"}, false)
		}()
	}
	wg.Wait()
	test.IsEqualInt(t, hits(ps.count), 1)
	_, _, _, ok := PhotoOf(u.Id)
	test.IsEqualBool(t, ok, true)
}

func TestPhotoFetchRefusalsKeepTheOldPhoto(t *testing.T) {
	ps := photoServer(t)
	u := models.User{Id: 505, Name: "eve@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	test.IsNil(t, StorePhoto(u.Id, pngBytes))
	_, _, before, ok := PhotoOf(u.Id)
	test.IsEqualBool(t, ok, true)

	srvUrl, _ := url.Parse(ps.srv.URL)
	for _, raw := range []string{
		"http://" + srvUrl.Host + "/ok", // not https: never sent
		"https://example.com/ok",        // not the allowed host: never sent
		ps.srv.URL + "/big",             // over 512 KB
		ps.srv.URL + "/html",            // sniffed as text/html
		ps.srv.URL + "/slow",            // times out
		ps.srv.URL + "/missing",         // 404
		ps.srv.URL + "/redirect-away",   // redirect outside the allowlist: not followed
		"::not a url",                   // never sent
	} {
		fetchPhoto(u.Id, raw)
		data, ct, updated, ok := PhotoOf(u.Id)
		if !ok || ct != "image/png" || !bytes.Equal(data, pngBytes) || updated != before {
			t.Errorf("%s replaced the photo: %s %d", raw, ct, updated)
		}
	}
	test.IsEqualInt(t, hits(ps.count), 5)     // big, html, slow, missing, redirect-away
	test.IsEqualInt(t, hits(ps.evilCount), 0) // the production CheckRedirect stopped it
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

type panickingTransport struct{}

func (panickingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("database is locked")
}

// M6: a panic inside the background photo fetch (a DB error in StorePhoto
// panics through helper.Check) is recovered with one line, not a crash
func TestPhotoFetchPanicIsRecovered(t *testing.T) {
	oldTransport := photoClient.Transport
	photoClient.Transport = panickingTransport{}
	defer func() { photoClient.Transport = oldTransport }()
	r, w, err := os.Pipe()
	test.IsNil(t, err)
	oldStdout := os.Stdout
	os.Stdout = w
	func() {
		defer func() { os.Stdout = oldStdout }()
		fetchPhoto(9312, "https://lh3.googleusercontent.com/a/photo")
	}()
	w.Close()
	out, _ := io.ReadAll(r)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	test.IsEqualInt(t, len(lines), 1)
	test.IsEqualBool(t, strings.Contains(lines[0], "people: photo fetch for user 9312 failed") &&
		strings.Contains(lines[0], "database is locked"), true)
}
