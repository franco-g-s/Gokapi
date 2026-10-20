//go:build test

package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestBlobHooks(t *testing.T) {
	// CleanUp sweeps the whole database, so put back whatever it removes
	// for the tests that run after this one.
	snapshot := database.GetAllMetadata()
	defer func() {
		for _, f := range snapshot {
			database.SaveMetaData(f)
		}
	}()
	var deleted []string
	var added []models.File
	OnBlobDeleted = func(sha1 string) { deleted = append(deleted, sha1) }
	OnBlobAdded = func(f models.File) { added = append(added, f) }
	defer func() { OnBlobDeleted, OnBlobAdded = nil, nil }()

	// Blobs that are not on disk are removed by CleanUp, and the hook must
	// still fire (previews outlive a missing original otherwise). A blob that
	// another file still uses must not fire it.
	base := models.File{Name: "hook.txt", UnlimitedDownloads: true, UnlimitedTime: true, ContentType: "text/plain"}
	gone, shared1, shared2 := base, base, base
	gone.Id, gone.SHA1 = "hookgone0000000000001", "hookgonesha1"
	shared1.Id, shared1.SHA1 = "hookshared00000000001", "hooksharedsha1"
	shared2.Id, shared2.SHA1 = "hookshared00000000002", "hooksharedsha1"
	for _, f := range []models.File{gone, shared1, shared2} {
		database.SaveMetaData(f)
	}
	CleanUp(false)
	test.IsEqualBool(t, contains(deleted, "hookgonesha1"), true)
	test.IsEqualBool(t, contains(deleted, "hooksharedsha1"), true) // last of the two to go
	test.IsEqualInt(t, count(deleted, "hooksharedsha1"), 1)

	orig, repl := base, base
	orig.Id, orig.SHA1 = "hookorig0000000000001", "hookorigsha1"
	repl.Id, repl.SHA1 = "hookrepl0000000000001", "hookreplsha1"
	for _, f := range []models.File{orig, repl} {
		test.IsNil(t, os.WriteFile(filepath.Join(configuration.Get().DataDir, f.SHA1), []byte("x"), 0600))
		database.SaveMetaData(f)
	}
	_, err := ReplaceFile(orig.Id, repl.Id, false)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(added), 1)
	test.IsEqualString(t, added[0].SHA1, "hookreplsha1")
	test.IsEqualString(t, added[0].Id, orig.Id)
}

func contains(list []string, s string) bool { return count(list, s) > 0 }

func count(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}
