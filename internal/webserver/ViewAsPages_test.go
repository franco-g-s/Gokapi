//go:build !integration && test

package webserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/access"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/viewas"
)

// viewAsFixture is a fresh admin with a session, and three people: b and c,
// whom the admin may view as, and another admin. Ids start at base, so tests
// running in parallel against the shared server never share a person.
type viewAsFixture struct {
	admin, b, c, otherAdmin models.User
	adminSession            string
	bSession                string
}

func newViewAsFixture(t *testing.T, base int) viewAsFixture {
	t.Helper()
	prefix := "viewas" + strconv.Itoa(base)
	fx := viewAsFixture{
		admin:        models.User{Id: base, Name: prefix + "-admin@example.com", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll},
		b:            models.User{Id: base + 1, Name: prefix + "-b@example.com", UserLevel: models.UserLevelUser},
		c:            models.User{Id: base + 2, Name: prefix + "-c@example.com", UserLevel: models.UserLevelUser},
		otherAdmin:   models.User{Id: base + 3, Name: prefix + "-admin2@example.com", UserLevel: models.UserLevelAdmin},
		adminSession: prefix + "adminsession",
		bSession:     prefix + "bsession",
	}
	for _, u := range []models.User{fx.admin, fx.b, fx.c, fx.otherAdmin} {
		database.SaveUser(u, false)
	}
	database.SaveSession(fx.adminSession, models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: fx.admin.Id})
	database.SaveSession(fx.bSession, models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: fx.b.Id})
	t.Cleanup(func() {
		viewas.End(fx.admin.Id, "exit")
		database.DeleteSession(fx.adminSession)
		database.DeleteSession(fx.bSession)
		for _, u := range []models.User{fx.admin, fx.b, fx.c, fx.otherAdmin} {
			database.DeleteUser(u.Id)
		}
	})
	return fx
}

// asSession runs a handler behind the guard, as the server does, for a
// request carrying the session cookie
func asSession(handler http.HandlerFunc, method, target, session, contentType, body string) *httptest.ResponseRecorder {
	return asSessionDirect(viewas.Guard(handler).ServeHTTP, method, target, session, contentType, body)
}

// asSessionDirect runs a handler without the guard
func asSessionDirect(handler http.HandlerFunc, method, target, session, contentType, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, reader)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if session != "" {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: session})
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// throughGuard returns the request as the guard hands it on
func throughGuard(r *http.Request) *http.Request {
	var passed *http.Request
	viewas.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { passed = r })).
		ServeHTTP(httptest.NewRecorder(), r)
	return passed
}

func TestRequireLoginSwaps(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9100)
	var seen, real models.User
	var swapped bool
	handler := requireLogin(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = authentication.GetUserFromRequest(r)
		real, swapped = viewas.RealUser(r)
	}, false, false)

	asSession(handler, "GET", "/files", fx.adminSession, "", "")
	test.IsEqualInt(t, seen.Id, fx.admin.Id)
	test.IsEqualBool(t, swapped, false)

	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	asSession(handler, "GET", "/files", fx.adminSession, "", "")
	test.IsEqualInt(t, seen.Id, fx.b.Id)
	test.IsEqualBool(t, swapped, true)
	test.IsEqualInt(t, real.Id, fx.admin.Id)

	// a request that did not pass the guard is never swapped, row or not
	asSessionDirect(handler, "GET", "/files", fx.adminSession, "", "")
	test.IsEqualInt(t, seen.Id, fx.admin.Id)
	test.IsEqualBool(t, swapped, false)
}

// C1: a demoted admin keeps no "View as…": not swapped, row ended with a line
func TestDemotedAdminNotSwapped(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9180)
	var seen models.User
	handler := requireLogin(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = authentication.GetUserFromRequest(r)
	}, false, false)
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	demoted := fx.admin
	demoted.UserLevel = models.UserLevelUser
	database.SaveUser(demoted, false)

	w := asSession(handler, "POST", "/files", fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualInt(t, seen.Id, fx.admin.Id)
	_, ok := database.GetViewAs(fx.admin.Id)
	test.IsEqualBool(t, ok, false)
	content, _ := logging.GetAll()
	test.IsEqualBool(t, strings.Contains(content, demoted.Name+" (user #"+strconv.Itoa(demoted.Id)+") stopped viewing as "+
		fx.b.Name+" (user #"+strconv.Itoa(fx.b.Id)+"): logout"), true)
}

// I1: /join shows the target's membership state
func TestJoinSwaps(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9190)
	g, err := storage.CreateGroup("View as join", 30, fx.c)
	test.IsNil(t, err)
	_, err = storage.JoinGroupByToken(g.InviteToken, fx.b)
	test.IsNil(t, err)
	t.Cleanup(func() { database.DeleteGroup(g.Id) })

	// the admin is no member: the join card
	w := asSession(showJoin, "GET", "/join?t="+g.InviteToken, fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), `action="./groups/join"`), true)

	// as b, a member: on to the group
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	w = asSession(showJoin, "GET", "/join?t="+g.InviteToken, fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusTemporaryRedirect)
	test.IsEqualBool(t, strings.Contains(w.Header().Get("Location"), g.Id), true)
}

func TestResetPasswordUsesRealAdmin(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9110)
	fx.b.ResetPassword = true
	database.SaveUser(fx.b, false)
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	w := asSession(requireLogin(showFiles, true, false), "GET", "/files", fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(w.Header().Get("Location"), "changePassword"), false)
}

func TestSessionIdentitySwaps(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9120)
	bFile := accessFile("viewasidentityb0001", func(f *models.File) {
		f.UserId = fx.b.Id
		f.PasswordHash = "x"
		f.SHA1 = "viewasidentitysha1"
	})
	adminFile := accessFile("viewasidentityadm01", func(f *models.File) {
		f.UserId = fx.admin.Id
		f.PasswordHash = "x"
		f.SHA1 = "viewasidentitysha2"
	})
	t.Cleanup(func() {
		database.DeleteMetaData(bFile.Id)
		database.DeleteMetaData(adminFile.Id)
	})
	r := throughGuard(sessionRequest(fx.adminSession))
	// the admin (PERM_LIST) sees both
	test.IsEqualBool(t, access.CanView(r, bFile, ""), true)
	test.IsEqualBool(t, access.CanView(r, adminFile, ""), true)

	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	// without the guard's decision, no swap
	direct := sessionRequest(fx.adminSession)
	test.IsEqualBool(t, access.CanView(direct, adminFile, ""), true)
	r = throughGuard(sessionRequest(fx.adminSession))
	identity, ok := authentication.SessionIdentity(r)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, identity.Id, fx.b.Id)
	test.IsEqualBool(t, access.CanView(r, bFile, ""), true)
	test.IsEqualBool(t, access.CanView(r, adminFile, ""), false)
}

func TestCollectionSeenSkipped(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9130)
	f := accessFile("viewasseencoll00001", func(f *models.File) {
		f.UserId = fx.c.Id
		f.SHA1 = "viewasseensha1"
	})
	collection, err := storage.CreateCollection(storage.CollectionParameters{Name: "View as seen", FileIds: []string{f.Id}}, fx.c)
	test.IsNil(t, err)
	t.Cleanup(func() {
		database.DeleteCollection(collection.Id)
		database.DeleteMetaData(f.Id)
	})

	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	body := asSession(showCollection, "GET", "/c?id="+collection.Id, fx.adminSession, "", "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, "View as seen"), true)
	test.IsEqualBool(t, sharedSeen(fx.b.Id, collection.Id), false)
	test.IsEqualBool(t, sharedSeen(fx.admin.Id, collection.Id), false)

	// B's own visit does record it: the condition above is what stopped it
	openCollection(collection.Id, fx.bSession)
	test.IsEqualBool(t, sharedSeen(fx.b.Id, collection.Id), true)
}

func TestStartEndpoint(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9140)
	start := requireLogin(viewAsStart, false, false)
	end := requireLogin(viewAsEnd, false, false)
	page := requireLogin(showViewAs, true, false)
	const jsonType = "application/json"
	body := func(id int) string { return `{"userId":` + strconv.Itoa(id) + `}` }

	// not a real admin: the endpoints do not exist
	test.IsEqualInt(t, asSession(start, "POST", "/viewas/start", fx.bSession, jsonType, body(fx.c.Id)).Code, http.StatusNotFound)
	test.IsEqualInt(t, asSession(end, "POST", "/viewas/end", fx.bSession, jsonType, `{}`).Code, http.StatusNotFound)
	test.IsEqualInt(t, asSession(page, "GET", "/viewas", fx.bSession, "", "").Code, http.StatusNotFound)

	// POST + JSON only
	test.IsEqualInt(t, asSession(start, "GET", "/viewas/start", fx.adminSession, "", "").Code, http.StatusMethodNotAllowed)
	test.IsEqualInt(t, asSession(end, "GET", "/viewas/end", fx.adminSession, "", "").Code, http.StatusMethodNotAllowed)
	test.IsEqualInt(t, asSession(start, "POST", "/viewas/start", fx.adminSession, "application/x-www-form-urlencoded", "userId=1").Code, http.StatusUnsupportedMediaType)
	test.IsEqualInt(t, asSession(end, "POST", "/viewas/end", fx.adminSession, "application/x-www-form-urlencoded", "").Code, http.StatusUnsupportedMediaType)

	// yourself, an admin, an unknown user, a malformed body
	for _, b := range []string{body(fx.admin.Id), body(fx.otherAdmin.Id), body(9149), `{"userId":"x"}`} {
		test.IsEqualInt(t, asSession(start, "POST", "/viewas/start", fx.adminSession, jsonType, b).Code, http.StatusBadRequest)
	}
	_, ok := viewas.Target(fx.admin.Id)
	test.IsEqualBool(t, ok, false)

	// the picker lists b and c, not yourself or another admin
	w := asSession(page, "GET", "/viewas", fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	html := w.Body.String()
	test.IsEqualBool(t, strings.Contains(html, "View the site as…"), true)
	test.IsEqualBool(t, strings.Contains(html, fx.b.Name), true)
	test.IsEqualBool(t, strings.Contains(html, fx.c.Name), true)
	test.IsEqualBool(t, strings.Contains(html, `data-fgs-viewas-user="`+strconv.Itoa(fx.b.Id)+`"`), true)
	test.IsEqualBool(t, strings.Contains(html, fx.otherAdmin.Name), false)
	test.IsEqualBool(t, strings.Contains(html, `data-fgs-viewas-user="`+strconv.Itoa(fx.admin.Id)+`"`), false)

	w = asSession(start, "POST", "/viewas/start", fx.adminSession, jsonType, body(fx.b.Id))
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualString(t, strings.TrimSpace(w.Body.String()), `{"Result":"OK"}`)
	target, ok := viewas.Target(fx.admin.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, target.Id, fx.b.Id)

	// while viewing as b, the real admin still reaches the picker and can switch
	test.IsEqualInt(t, asSession(page, "GET", "/viewas", fx.adminSession, "", "").Code, http.StatusOK)
	test.IsEqualInt(t, asSession(start, "POST", "/viewas/start", fx.adminSession, jsonType, body(fx.c.Id)).Code, http.StatusOK)
	target, _ = viewas.Target(fx.admin.Id)
	test.IsEqualInt(t, target.Id, fx.c.Id)

	w = asSession(end, "POST", "/viewas/end", fx.adminSession, jsonType, `{}`)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualString(t, strings.TrimSpace(w.Body.String()), `{"Result":"OK"}`)
	_, ok = viewas.Target(fx.admin.Id)
	test.IsEqualBool(t, ok, false)
}

func TestNavLinkOnlyForRealAdmin(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9150)
	const link = `href="./viewas">View as…</a>`
	files := requireLogin(showFiles, true, false)
	w := asSession(files, "GET", "/files", fx.adminSession, "", "")
	test.IsEqualBool(t, strings.Contains(w.Body.String(), link), true)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), "js/min/viewas.min.js"), true)
	test.IsEqualBool(t, strings.Contains(asSession(files, "GET", "/files", fx.bSession, "", "").Body.String(), link), false)

	// the Users page offers it for b, never for yourself or another admin
	users := asSession(requireLogin(showUserAdmin, true, false), "GET", "/users", fx.adminSession, "", "").Body.String()
	test.IsEqualBool(t, strings.Contains(users, `data-fgs-viewas-user="`+strconv.Itoa(fx.b.Id)+`"`), true)
	test.IsEqualBool(t, strings.Contains(users, `data-fgs-viewas-user="`+strconv.Itoa(fx.admin.Id)+`"`), false)
	test.IsEqualBool(t, strings.Contains(users, `data-fgs-viewas-user="`+strconv.Itoa(fx.otherAdmin.Id)+`"`), false)

	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	w = asSession(files, "GET", "/files", fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), link), false)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), "js/min/viewas.min.js"), true)
}

// Through the real server, so the guard is what wraps the mux
func TestAuthTokenRefused(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9160)
	get := func() *http.Response {
		req, err := http.NewRequest("GET", "http://localhost:53843/auth/token", nil)
		test.IsNil(t, err)
		req.Header.Set("permission", "PERM_VIEW")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: fx.adminSession})
		resp, err := http.DefaultClient.Do(req)
		test.IsNil(t, err)
		return resp
	}
	resp := get()
	_ = resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)

	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	resp = get()
	defer resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, http.StatusForbidden)
	content, _ := io.ReadAll(resp.Body)
	test.IsEqualBool(t, strings.Contains(string(content), "View as… is read-only. Exit it to make changes or download."), true)
}

// Logging out ends it, also when the request does not pass the guard
func TestLogoutEndsViewAs(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9170)
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	asSessionDirect(doLogout, "GET", "/logout", fx.adminSession, "", "")
	_, ok := viewas.Target(fx.admin.Id)
	test.IsEqualBool(t, ok, false)
}
