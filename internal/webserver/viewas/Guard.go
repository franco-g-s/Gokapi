package viewas

import (
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
// are viewing as. Read-only.
func Active(r *http.Request) (admin, target models.User, ok bool) {
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

// Guard wraps the whole mux. It refuses every change and every download of an
// admin in "View as…". With nobody in "View as…" it reads nothing at all.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !anyActive() {
			clearDisplayCookieIfSent(w, r)
			next.ServeHTTP(w, r)
			return
		}
		admin, target, ok := Active(r)
		if !ok {
			clearDisplayCookieIfSent(w, r)
			next.ServeHTTP(w, r)
			return
		}
		if Allowed(r.Method, r.URL.Path) {
			syncDisplayCookie(w, r, target)
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
<title>View as…</title>
<link href="./assets/dist/css/bootstrap.min.css" rel="stylesheet">
<script src="./js/min/viewas.min.js"></script>
</head>
<body class="bg-dark text-white text-center p-5">
<p>{{ . }}</p>
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
	_ = refusalPage.Execute(w, refusal)
}
