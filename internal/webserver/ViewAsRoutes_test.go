//go:build !integration && test

package webserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/viewas"
)

// routeRegistration matches mux.HandleFunc("<path>" and mux.Handle("<path>"
var routeRegistration = regexp.MustCompile(`mux\.Handle(?:Func)?\("([^"]+)"`)

// allRoutes reads every route registered anywhere in internal/webserver's
// own non-test files, plus /api/, with {id} and {filename} replaced by x
func allRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	test.IsNil(t, err)
	seen := map[string]bool{"/api/": true}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, err := os.ReadFile(file)
		test.IsNil(t, err)
		for _, match := range routeRegistration.FindAllStringSubmatch(string(content), -1) {
			path := strings.NewReplacer("{id}", "x", "{filename}", "x").Replace(match[1])
			seen[path] = true
		}
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// The spec's test: with an admin viewing as B, a request reaches its handler
// exactly when the allowlist says so. A route added anywhere in
// internal/webserver later is refused until someone allowlists it.
func TestViewAsRoutes(t *testing.T) {
	fx := newViewAsFixture(t, 9200)
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))

	routes := allRoutes(t)
	// the scan really found the routes, conditional and custom ones included
	for _, want := range []string{"/files", "/gd", "/d/x/x", "/dh/x/x", "/oauth-login", "/oauth-callback", "/custom/", "/api/", "/logout"} {
		found := false
		for _, route := range routes {
			found = found || route == want
		}
		if !found {
			t.Errorf("route %s was not found in internal/webserver", want)
		}
	}

	send := func(method, path string) (bool, int) {
		reached := false
		handler := viewas.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(&http.Cookie{Name: "session_token", Value: fx.adminSession})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return reached, w.Code
	}

	for _, path := range routes {
		if path == "/logout" {
			continue // ends it: checked last
		}
		for _, method := range []string{"GET", "POST"} {
			reached, code := send(method, path)
			if reached != viewas.Allowed(method, path) {
				t.Errorf("%s %s: reached %t, allowed %t", method, path, reached, viewas.Allowed(method, path))
			}
			if !reached && code != http.StatusForbidden {
				t.Errorf("%s %s: refused with %d, want 403", method, path, code)
			}
		}
	}

	mustRefuse := []string{
		// downloads
		"/d", "/downloadFile", "/h/", "/hotlink/", "/dh/x/x", "/downloadPresigned",
		"/files/download", "/files/zip", "/cd", "/cz", "/gd", "/gz",
		// mutations and the rest
		"/api/", "/auth/token", "/uploadChunk", "/changePassword",
		"/collections/create", "/collections/modify", "/collections/delete",
		"/forgotpw", "/publicUpload", "/e2eSetup",
	}
	// every /groups/* POST route
	for _, route := range routes {
		if strings.HasPrefix(route, "/groups/") && !viewas.Allowed("GET", route) {
			mustRefuse = append(mustRefuse, route)
		}
	}
	for _, route := range []string{"/groups/fromcollection/preview", "/groups/linkmode", "/groups/stoplink",
		"/groups/nearkeep", "/groups/has", "/groups/add", "/groups/fromcollection", "/groups/create",
		"/groups/join", "/groups/rename", "/groups/extend", "/groups/resetcode", "/groups/removemember",
		"/groups/leave", "/groups/delete", "/groups/removefiles"} {
		mustRefuse = append(mustRefuse, route)
	}
	for _, path := range mustRefuse {
		for _, method := range []string{"GET", "POST"} {
			test.IsEqualBool(t, viewas.Allowed(method, path), false)
			reached, code := send(method, path)
			if reached || code != http.StatusForbidden {
				t.Errorf("%s %s must be refused: reached %t, code %d", method, path, reached, code)
			}
		}
	}

	// /logout passes, and ends it
	reached, _ := send("GET", "/logout")
	test.IsEqualBool(t, reached, true)
	_, active := viewas.Target(fx.admin.Id)
	test.IsEqualBool(t, active, false)
}
