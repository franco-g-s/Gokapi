//go:build test

package viewas

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// recorder is a handler that notes whether the guard let a request through,
// and the request it got
type recorder struct {
	reached bool
	request *http.Request
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec.reached = true
	rec.request = r
	_, _ = w.Write([]byte("handler"))
}

// session saves a session for the user and removes it after the test
func session(t *testing.T, id string, user models.User) {
	t.Helper()
	database.SaveSession(id, models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: user.Id})
	t.Cleanup(func() { database.DeleteSession(id) })
}

func guarded(method, target, sessionId string, extra ...*http.Cookie) (*httptest.ResponseRecorder, *recorder) {
	r := httptest.NewRequest(method, target, nil)
	if sessionId != "" {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: sessionId})
	}
	for _, c := range extra {
		r.AddCookie(c)
	}
	return guardedRequest(r)
}

func guardedRequest(r *http.Request) (*httptest.ResponseRecorder, *recorder) {
	rec := &recorder{}
	w := httptest.NewRecorder()
	Guard(rec).ServeHTTP(w, r)
	return w, rec
}

func cookieOf(w *httptest.ResponseRecorder, name string) (*http.Cookie, bool) {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c, true
		}
	}
	return nil, false
}

func TestGuardPassesWhenInactive(t *testing.T) {
	admin, _, _ := people(t, 8200, "inactive")
	session(t, "viewasguardinactive", admin)
	for _, target := range []string{"/files", "/gd?id=x", "/api/files/list", "/uploadChunk"} {
		for _, method := range []string{"GET", "POST"} {
			w, rec := guarded(method, target, "viewasguardinactive")
			test.IsEqualBool(t, rec.reached, true)
			test.IsEqualInt(t, w.Code, 200)
		}
	}
}

func TestGuardFastPath(t *testing.T) {
	calls := 0
	original := peek
	peek = func(r *http.Request) (models.User, bool) {
		calls++
		return original(r)
	}
	defer func() { peek = original }()

	admin, a, _ := people(t, 8210, "fastpath")
	session(t, "viewasguardfastpath", admin)
	w, rec := guarded("GET", "/gd", "viewasguardfastpath", &http.Cookie{Name: DisplayCookie, Value: "x"})
	test.IsEqualBool(t, rec.reached, true)
	test.IsEqualInt(t, calls, 0)
	// the stale display cookie is expired without a read
	c, ok := cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, c.MaxAge < 0, true)

	test.IsNil(t, Start(admin, a.Id))
	_, rec = guarded("GET", "/gd", "viewasguardfastpath")
	test.IsEqualBool(t, rec.reached, false)
	test.IsEqualBool(t, calls > 0, true)
}

func TestGuardAllowlist(t *testing.T) {
	admin, a, _ := people(t, 8220, "allow")
	session(t, "viewasguardallow", admin)
	test.IsNil(t, Start(admin, a.Id))

	w, rec := guarded("GET", "/files", "viewasguardallow")
	test.IsEqualBool(t, rec.reached, true)
	test.IsEqualInt(t, w.Code, 200)
	w, rec = guarded("HEAD", "/p/thumb?id=x", "viewasguardallow")
	test.IsEqualBool(t, rec.reached, true)
	w, rec = guarded("GET", "/js/min/viewas.min.js", "viewasguardallow")
	test.IsEqualBool(t, rec.reached, true)

	w, rec = guarded("POST", "/files", "viewasguardallow")
	test.IsEqualBool(t, rec.reached, false)
	test.IsEqualInt(t, w.Code, http.StatusForbidden)
	test.IsEqualBool(t, strings.HasPrefix(w.Header().Get("Content-Type"), "application/json"), true)
	test.IsEqualString(t, strings.TrimSpace(w.Body.String()),
		`{"Result":"error","ErrorMessage":"View as… is read-only. Exit it to make changes or download."}`)

	// JSON endpoints refuse with JSON even on GET
	w, rec = guarded("GET", "/groups/rename", "viewasguardallow")
	test.IsEqualBool(t, rec.reached, false)
	test.IsEqualBool(t, strings.HasPrefix(w.Header().Get("Content-Type"), "application/json"), true)

	// someone else, not viewing as anybody, is untouched
	session(t, "viewasguardallowa", a)
	_, rec = guarded("POST", "/files", "viewasguardallowa")
	test.IsEqualBool(t, rec.reached, true)
	_, rec = guarded("GET", "/gd", "")
	test.IsEqualBool(t, rec.reached, true)
}

func TestAllowed(t *testing.T) {
	for _, path := range []string{"/admin", "/files", "/files/list", "/gl", "/cl", "/c", "/viewas", "/css/x.css", "/favicon-32x32.png", "/custom/x"} {
		test.IsEqualBool(t, Allowed("GET", path), true)
		test.IsEqualBool(t, Allowed("HEAD", path), true)
		test.IsEqualBool(t, Allowed("POST", path), false)
	}
	for _, path := range []string{"/viewas/start", "/viewas/end"} {
		test.IsEqualBool(t, Allowed("POST", path), true)
		test.IsEqualBool(t, Allowed("GET", path), false)
	}
	for _, path := range []string{"/", "/d", "/gd", "/gz", "/files/download", "/files/zip", "/api/", "/auth/token", "/photosprobe", "/oauth-login", "/files/replacecandidates", "/filesx", "/admin/"} {
		test.IsEqualBool(t, Allowed("GET", path), false)
		test.IsEqualBool(t, Allowed("POST", path), false)
	}
}

func TestGuardHtmlRefusal(t *testing.T) {
	admin, a, _ := people(t, 8230, "html")
	session(t, "viewasguardhtml", admin)
	test.IsNil(t, Start(admin, a.Id))
	r := httptest.NewRequest("GET", "/gd?id=x", nil)
	r.AddCookie(&http.Cookie{Name: "session_token", Value: "viewasguardhtml"})
	r.Header.Set("Accept", "text/html,application/xhtml+xml")
	w, rec := guardedRequest(r)
	test.IsEqualBool(t, rec.reached, false)
	test.IsEqualInt(t, w.Code, http.StatusForbidden)
	body := w.Body.String()
	test.IsEqualBool(t, strings.HasPrefix(w.Header().Get("Content-Type"), "text/html"), true)
	test.IsEqualBool(t, strings.Contains(body, "View as… is read-only. Exit it to make changes or download."), true)
	test.IsEqualBool(t, strings.Contains(body, "data-fgs-viewas-exit"), true)
	test.IsEqualBool(t, strings.Contains(body, `<script src="./js/min/viewas.min.js"></script>`), true)

	test.IsEqualBool(t, strings.Contains(body, `<base href="./">`), true)

	r.Header.Set("Accept", "application/json")
	w, _ = guardedRequest(r)
	test.IsEqualBool(t, strings.HasPrefix(w.Header().Get("Content-Type"), "application/json"), true)

	// Nested paths: the page's relative URLs still point at the site root
	for path, base := range map[string]string{"/dh/x/x": "../../", "/h/x": "../", "/hotlink/x": "../"} {
		r = httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "session_token", Value: "viewasguardhtml"})
		r.Header.Set("Accept", "text/html")
		w, rec = guardedRequest(r)
		test.IsEqualBool(t, rec.reached, false)
		test.IsEqualBool(t, strings.Contains(w.Body.String(), `<base href="`+base+`">`), true)
	}
}

func TestBaseHref(t *testing.T) {
	test.IsEqualString(t, baseHref("/"), "./")
	test.IsEqualString(t, baseHref("/gd"), "./")
	test.IsEqualString(t, baseHref("/h/"), "../")
	test.IsEqualString(t, baseHref("/dh/x/x"), "../../")
}

// The guard decides once and hands its decision on; nothing else is swapped
func TestGuardDecision(t *testing.T) {
	admin, a, _ := people(t, 8260, "decision")
	session(t, "viewasguarddecision", admin)
	_, rec := guarded("GET", "/files", "viewasguarddecision")
	_, _, ok := FromGuard(rec.request)
	test.IsEqualBool(t, ok, false)

	test.IsNil(t, Start(admin, a.Id))
	_, rec = guarded("GET", "/files", "viewasguarddecision")
	gotAdmin, gotTarget, ok := FromGuard(rec.request)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, gotAdmin.Id, admin.Id)
	test.IsEqualInt(t, gotTarget.Id, a.Id)
	target, ok := SwapFor(rec.request, admin)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, target.Id, a.Id)
	_, ok = SwapFor(rec.request, a) // only for the admin the guard decided on
	test.IsEqualBool(t, ok, false)

	// a request that never passed the guard is never swapped, row or not
	_, ok = SwapFor(httptest.NewRequest("GET", "/files", nil), admin)
	test.IsEqualBool(t, ok, false)
}

// A demoted admin's row is ended at their next request, which passes as theirs
func TestGuardEndsDemoted(t *testing.T) {
	admin, a, _ := people(t, 8270, "demoted")
	session(t, "viewasguarddemoted", admin)
	test.IsNil(t, Start(admin, a.Id))
	demoted := admin
	demoted.UserLevel = models.UserLevelUser
	database.SaveUser(demoted, false)

	_, rec := guarded("POST", "/files", "viewasguarddemoted")
	test.IsEqualBool(t, rec.reached, true)
	_, _, ok := FromGuard(rec.request)
	test.IsEqualBool(t, ok, false)
	_, ok = database.GetViewAs(admin.Id)
	test.IsEqualBool(t, ok, false)
	test.IsEqualBool(t, isActive(admin.Id), false)
	test.IsEqualBool(t, hasSuffixLine(logLines(t, demoted), endLine(demoted, a, "logout")), true)
}

func TestGuardLogoutEnds(t *testing.T) {
	admin, a, _ := people(t, 8240, "logout")
	session(t, "viewasguardlogout", admin)
	test.IsNil(t, Start(admin, a.Id))
	w, rec := guarded("GET", "/logout", "viewasguardlogout", &http.Cookie{Name: DisplayCookie, Value: "x"})
	test.IsEqualBool(t, rec.reached, true)
	_, ok := Target(admin.Id)
	test.IsEqualBool(t, ok, false)
	test.IsEqualBool(t, hasSuffixLine(logLines(t, admin), endLine(admin, a, "logout")), true)
	c, ok := cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, c.MaxAge < 0, true)
}

func TestGuardCookieSync(t *testing.T) {
	admin, a, _ := people(t, 8250, "cookie")
	session(t, "viewasguardcookie", admin)
	test.IsNil(t, Start(admin, a.Id))
	want := url.QueryEscape("cookie-a")

	w, _ := guarded("GET", "/files", "viewasguardcookie")
	c, ok := cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, c.Value, want)
	test.IsEqualBool(t, c.HttpOnly, false)
	test.IsEqualString(t, c.Path, "/")
	test.IsEqualBool(t, c.SameSite == http.SameSiteLaxMode, true)

	// in sync: nothing is set
	w, _ = guarded("GET", "/files", "viewasguardcookie", &http.Cookie{Name: DisplayCookie, Value: want})
	_, ok = cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, false)

	// stale: set again
	w, _ = guarded("GET", "/files", "viewasguardcookie", &http.Cookie{Name: DisplayCookie, Value: "someone"})
	c, ok = cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, c.Value, want)

	// inactive with the cookie: expired, while another admin keeps the guard on the slow path
	session(t, "viewasguardcookiea", a)
	w, rec := guarded("GET", "/files", "viewasguardcookiea", &http.Cookie{Name: DisplayCookie, Value: want})
	test.IsEqualBool(t, rec.reached, true)
	c, ok = cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, c.MaxAge < 0, true)

	End(admin.Id, "exit")
	w, _ = guarded("GET", "/files", "viewasguardcookie", &http.Cookie{Name: DisplayCookie, Value: want})
	c, ok = cookieOf(w, DisplayCookie)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, c.MaxAge < 0, true)
}
