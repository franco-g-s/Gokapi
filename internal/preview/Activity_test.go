//go:build test

package preview

import (
	"context"
	"errors"
	"testing"

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
