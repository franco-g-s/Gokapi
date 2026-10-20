//go:build !integration && test

package webserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/forceu/gokapi/internal/test"
)

func TestCustomFileServerServesOnlyBrandingFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"custom.css":   "body{}",
		"admin.js":     "//admin",
		"admin.js.bak": "//old",
		"notes.txt":    "private",
	}
	for name, content := range files {
		test.IsNil(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
	}
	handler := newCustomFileServer(dir)

	status := func(path string) int {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code
	}

	test.IsEqualInt(t, status("/custom/custom.css"), http.StatusOK)
	test.IsEqualInt(t, status("/custom/admin.js"), http.StatusOK)
	test.IsEqualInt(t, status("/custom/"), http.StatusNotFound)
	test.IsEqualInt(t, status("/custom/notes.txt"), http.StatusNotFound)
	test.IsEqualInt(t, status("/custom/admin.js.bak"), http.StatusNotFound)
	// A listed name that is not in the folder is a plain 404
	test.IsEqualInt(t, status("/custom/public.js"), http.StatusNotFound)
}
