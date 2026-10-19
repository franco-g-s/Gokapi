//go:build test

package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestPreviewsRoundTrip(t *testing.T) {
	// Own instance: file-name ordering runs this after TestRawSql, which closes dbInstance
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()

	p := models.Preview{Sha1: "abc123def4567890", Status: models.PreviewReady, Kind: models.PreviewKindImage,
		Width: 640, Height: 853, TakenAt: 1755174725, DurationMs: 1234, Color: "#787878",
		PHash: 0xF0F0F0F0F0F0F0F0, Attempts: 2, Updated: 1}
	_, ok := instance.GetPreview(p.Sha1)
	test.IsEqualBool(t, ok, false)

	instance.SavePreview(p)
	got, ok := instance.GetPreview("abc123def4567890")
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, got.Height, 853)
	test.IsEqualBool(t, got.PHash == p.PHash, true)
	test.IsEqualBool(t, got == p, true)
	test.IsEqualString(t, instance.GetAllPreviewSha1s()["abc123def4567890"], models.PreviewReady)

	p.Status = models.PreviewFailed
	p.Attempts = 3
	instance.SavePreview(p)
	got, _ = instance.GetPreview(p.Sha1)
	test.IsEqualString(t, got.Status, models.PreviewFailed)
	test.IsEqualInt(t, got.Attempts, 3)
	test.IsEqualInt(t, len(instance.GetAllPreviewSha1s()), 1)

	instance.DeletePreview("abc123def4567890")
	_, ok = instance.GetPreview("abc123def4567890")
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(instance.GetAllPreviewSha1s()), 0)
}

// Ruling 20: no schema bump. A database at upstream's version 17 without the
// Previews table gets it on the next start, and stays at version 17.
func TestPreviewsTableCreatedOnStartWithoutSchemaBump(t *testing.T) {
	test.IsEqualInt(t, DatabaseSchemeVersion, 17)
	cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "gokapi.sqlite"), Type: 0}

	// A brand-new database has the table and upstream's version
	instance, err := New(cfg)
	test.IsNil(t, err)
	test.IsEqualInt(t, instance.GetDbVersion(), 17)
	instance.SavePreview(models.Preview{Sha1: "new", Status: models.PreviewPending})
	_, ok := instance.GetPreview("new")
	test.IsEqualBool(t, ok, true)

	// Simulate the live database: version 17, no Previews table
	_, err = instance.sqliteDb.Exec("DROP TABLE Previews")
	test.IsNil(t, err)
	instance.Close()

	instance, err = New(cfg)
	test.IsNil(t, err)
	defer instance.Close()
	test.IsEqualInt(t, instance.GetDbVersion(), 17)
	instance.SavePreview(models.Preview{Sha1: "x", Status: models.PreviewPending})
	_, ok = instance.GetPreview("x")
	test.IsEqualBool(t, ok, true)

	// Idempotent: a further start leaves the rows alone
	again, err := New(cfg)
	test.IsNil(t, err)
	defer again.Close()
	_, ok = again.GetPreview("x")
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, again.GetDbVersion(), 17)
}
