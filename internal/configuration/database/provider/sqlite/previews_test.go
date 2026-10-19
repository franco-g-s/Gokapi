//go:build test

package sqlite

import (
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

func TestPreviewsUpgradeCreatesTable(t *testing.T) {
	instance, err := New(configUpgrade)
	test.IsNil(t, err)
	defer instance.Close()
	_, err = instance.sqliteDb.Exec("DROP TABLE IF EXISTS Previews")
	test.IsNil(t, err)
	instance.Upgrade(17)
	instance.SavePreview(models.Preview{Sha1: "x", Status: models.PreviewPending})
	_, ok := instance.GetPreview("x")
	test.IsEqualBool(t, ok, true)
}
