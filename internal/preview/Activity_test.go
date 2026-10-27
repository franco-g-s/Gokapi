//go:build test

package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// Only the final failed attempt writes a warning_preview (Part 4, Q3)
func TestPreviewFailureRecordedOnFinalAttempt(t *testing.T) {
	orig := generateFn
	defer func() { generateFn = orig }()
	generateFn = func(ctx context.Context, src, outDir, sha1, kind string) (models.Preview, error) {
		return models.Preview{}, errors.New("cannot decode")
	}
	f := models.File{Id: "actpreview000000001", Name: "broken.jpg", SHA1: "actpreviewsha1", ContentType: "image/jpeg", UserId: 7}
	defer database.DeletePreview(f.SHA1)
	count := func() int {
		n := 0
		for _, a := range database.GetActivitySince(0, 100000) {
			if a.Kind == models.ActivityWarningPreview && a.FileId == f.Id {
				n++
			}
		}
		return n
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		process(f)
		p, _ := database.GetPreview(f.SHA1)
		test.IsEqualInt(t, p.Attempts, attempt)
		if attempt < maxAttempts {
			test.IsEqualInt(t, count(), 0)
		}
	}
	test.IsEqualInt(t, count(), 1)
}

// A final attempt that killed the process never reaches fail(); backfill
// marks it failed on the next start and warns then, once (Q3)
func TestBackfillWarnsOnPendingAtLimit(t *testing.T) {
	data := configuration.Get().DataDir
	test.IsNil(t, os.WriteFile(filepath.Join(data, "actcrashsha1"), []byte("junk"), 0600))
	f := models.File{Id: "actcrash00000000001", Name: "crash.jpg", SHA1: "actcrashsha1", ContentType: "image/jpeg", UserId: 7}
	database.SaveMetaData(f)
	defer database.DeleteMetaData(f.Id)
	defer database.DeletePreview(f.SHA1)
	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewPending, Kind: models.PreviewKindImage, Attempts: maxAttempts})
	count := func() int {
		n := 0
		for _, a := range database.GetActivitySince(0, 100000) {
			if a.Kind == models.ActivityWarningPreview && a.FileId == f.Id {
				n++
			}
		}
		return n
	}
	backfill()
	test.IsEqualInt(t, count(), 1)
	backfill()
	test.IsEqualInt(t, count(), 1)
}
