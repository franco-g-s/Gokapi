//go:build !integration && test

package webserver

import (
	"regexp"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/test"
)

// The undo of a single delete must never reload the page: a reload collapses
// an open collection or request row. The behaviour itself is checked in the
// browser; this keeps the source from sliding back.
func TestUndoNeverReloads(t *testing.T) {
	read := func(name string) string {
		source, err := staticFolderEmbedded.ReadFile("web/static/js/" + name)
		test.IsNil(t, err)
		return string(source)
	}
	body := func(source, signature string) string {
		start := strings.Index(source, signature)
		test.IsEqualBool(t, start >= 0, true)
		rest := source[start:]
		end := regexp.MustCompile(`\n}\n`).FindStringIndex(rest)
		test.IsEqualBool(t, end != nil, true)
		return rest[:end[1]]
	}
	for _, f := range []struct{ file, signature string }{
		{"admin_ui_upload.js", "function handleUndo(button) {"},
		{"admin_ui_filerequest.js", "function handleUndoFr(button) {"},
	} {
		fn := body(read(f.file), f.signature)
		test.IsEqualBool(t, strings.Contains(fn, "reload"), false)
		test.IsEqualBool(t, strings.Contains(fn, "FGS.restoreFile("), true)
		test.IsEqualBool(t, strings.Contains(fn, "FGS.undoToast("), true)
	}
}

// One selected file is a single delete (10 s undo) wherever it is chosen
// from; the selection bar always sets bulk, so it cannot be the test.
func TestSingleFileDeleteKeepsUndo(t *testing.T) {
	source, err := staticFolderEmbedded.ReadFile("web/static/js/admin_ui_filebrowser.js")
	test.IsNil(t, err)
	test.IsEqualBool(t, strings.Contains(string(source), "if (a.ids.length > 1) bulkDelete(a);"), true)
	test.IsEqualBool(t, strings.Contains(string(source), "if (a.bulk) bulkDelete(a);"), false)
}

// The undo toast outlives neither the server's 10 s window nor falls short of
// it by much: every place that shows it uses the one shared duration.
func TestUndoToastLastsAlmostTheServerWindow(t *testing.T) {
	all, err := staticFolderEmbedded.ReadFile("web/static/js/admin_ui_allPages.js")
	test.IsNil(t, err)
	test.IsEqualBool(t, strings.Contains(string(all), "var UNDO_TOAST_MS = 9000;"), true)
	for _, name := range []string{"admin_ui_upload.js", "admin_ui_filerequest.js", "admin_ui_filebrowser.js"} {
		source, err := staticFolderEmbedded.ReadFile("web/static/js/" + name)
		test.IsNil(t, err)
		test.IsEqualBool(t, strings.Contains(string(source), "UNDO_TOAST_MS"), true)
	}
}
