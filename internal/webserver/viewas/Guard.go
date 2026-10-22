package viewas

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
)

// DisplayCookie carries the target's display name to viewas.js, for the
// banner. It is display only: no server decision ever reads it.
const DisplayCookie = "fgs_viewas"

// refusal is the one sentence every refused request answers with
const refusal = "View as… is read-only. Exit it to make changes or download."

// peek reads the session without side effects; a var so tests can count calls
var peek = sessionmanager.PeekSession

// allowedPaths may be read (GET or HEAD) during "View as…", exactly as written
var allowedPaths = map[string]bool{
	"/admin": true, "/files": true, "/files/list": true, "/files/previewinfo": true,
	"/groups": true, "/g": true, "/gl": true, "/groups/item": true, "/groups/list": true,
	"/groups/neardupes": true, "/collections": true, "/collections/list": true,
	"/filerequests": true, "/c": true, "/cl": true, "/p/thumb": true, "/p/view": true,
	"/p/poster": true, "/p/video": true, "/p/info": true, "/join": true, "/g/join": true,
	"/users": true, "/logs": true, "/apiKeys": true, "/uploadStatus": true, "/index": true,
	"/error": true, "/viewas": true,
}

// allowedPrefixes may be read below these prefixes: static assets only
var allowedPrefixes = []string{"/js/", "/css/", "/assets/", "/custom/", "/favicon"}

// allowedPosts are the only changes "View as…" accepts: starting another and ending it
var allowedPosts = map[string]bool{"/viewas/start": true, "/viewas/end": true}

// Allowed is the allowlist of "View as…". Everything else is refused, every
// download and every change included, and so is any route added later until
// someone lists it here.
func Allowed(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead:
		if allowedPaths[path] {
			return true
		}
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
		return false
	case http.MethodPost:
		return allowedPosts[path]
	}
	return false
}

// Active returns the admin behind the request's session cookie and whom they
// are viewing as: the guard's decision if the request passed it, otherwise
// read from the session. Read-only.
func Active(r *http.Request) (admin, target models.User, ok bool) {
	if admin, target, ok = FromGuard(r); ok {
		return admin, target, true
	}
	if !anyActive() {
		return models.User{}, models.User{}, false
	}
	user, found := peek(r)
	if !found || !IsRealAdmin(user) {
		return models.User{}, models.User{}, false
	}
	t, found := Target(user.Id)
	if !found {
		return models.User{}, models.User{}, false
	}
	return user, t, true
}

// resolve is the guard's one decision: who the session's admin is viewing as.
// A row whose user is no longer a real admin is ended here, with "logout":
// they lost the right to be signed in as an admin.
func resolve(r *http.Request) (admin, target models.User, ok bool) {
	user, found := peek(r)
	if !found || !isActive(user.Id) {
		return models.User{}, models.User{}, false
	}
	if !IsRealAdmin(user) {
		End(user.Id, "logout")
		return models.User{}, models.User{}, false
	}
	t, found := Target(user.Id)
	if !found {
		return models.User{}, models.User{}, false
	}
	return user, t, true
}

// decision is what the guard decided for a request in "View as…"
type decision struct {
	admin, target models.User
}

type decisionKey struct{}

// FromGuard returns the guard's decision for this request: the admin and the
// target. Only a request that passed the guard's active branch carries one,
// and only such a request is ever swapped to the target.
func FromGuard(r *http.Request) (admin, target models.User, ok bool) {
	d, ok := r.Context().Value(decisionKey{}).(decision)
	if !ok {
		return models.User{}, models.User{}, false
	}
	return d.admin, d.target, true
}

// SwapFor returns the target the guard decided on, if this request passed the
// guard as user's "View as…". Every swap goes through here.
func SwapFor(r *http.Request, user models.User) (models.User, bool) {
	admin, target, ok := FromGuard(r)
	if !ok || admin.Id != user.Id {
		return models.User{}, false
	}
	return target, true
}

// Guard wraps the whole mux. It refuses every change and every download of an
// admin in "View as…". With nobody in "View as…" it reads nothing at all.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !anyActive() {
			clearDisplayCookieIfSent(w, r)
			next.ServeHTTP(w, r)
			return
		}
		admin, target, ok := resolve(r)
		if !ok {
			clearDisplayCookieIfSent(w, r)
			next.ServeHTTP(w, r)
			return
		}
		if Allowed(r.Method, r.URL.Path) {
			syncDisplayCookie(w, r, target)
			r = r.WithContext(context.WithValue(r.Context(), decisionKey{}, decision{admin: admin, target: target}))
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/logout" {
			End(admin.Id, "logout")
			clearDisplayCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		syncDisplayCookie(w, r, target)
		refuse(w, r)
	})
}

func displayValue(target models.User) string {
	return url.QueryEscape(displayName(target))
}

func syncDisplayCookie(w http.ResponseWriter, r *http.Request, target models.User) {
	want := displayValue(target)
	if c, err := r.Cookie(DisplayCookie); err == nil && c.Value == want {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: DisplayCookie, Value: want, Path: "/", SameSite: http.SameSiteLaxMode})
}

func clearDisplayCookieIfSent(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(DisplayCookie); err == nil {
		clearDisplayCookie(w)
	}
}

func clearDisplayCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: DisplayCookie, Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode})
}

// isJsonPath is true for the endpoints that scripts call
func isJsonPath(path string) bool {
	if path == "/auth/token" {
		return true
	}
	for _, prefix := range []string{"/api/", "/groups/", "/collections/", "/files/", "/viewas/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

var refusalPage = template.Must(template.New("viewasrefusal").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<base href="{{ .Base }}">
<title>View as…</title>
<link href="./assets/dist/css/bootstrap.min.css" rel="stylesheet">
<script src="./js/min/viewas.min.js"></script>
</head>
<body class="bg-dark text-white text-center p-5">
<p>{{ .Message }}</p>
<button type="button" class="btn btn-outline-light" data-fgs-viewas-exit>Exit</button>
</body>
</html>
`))

// refuse answers 403: JSON for scripts, a small page with an Exit button for people
func refuse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
	if !isRead || isJsonPath(r.URL.Path) || strings.Contains(r.Header.Get("Accept"), "json") {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"Result":"error","ErrorMessage":"` + refusal + `"}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(http.StatusForbidden)
	_ = refusalPage.Execute(w, struct{ Base, Message string }{baseHref(r.URL.Path), refusal})
}

// baseHref points relative URLs at the site root from any depth, so the
// refusal page's assets and Exit work under /h/x or /dh/x/x too
func baseHref(path string) string {
	depth := strings.Count(path, "/")
	if depth <= 1 {
		return "./"
	}
	return strings.Repeat("../", depth-1)
}
