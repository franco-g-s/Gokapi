//go:build test

package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	code := m.Run()
	testconfiguration.Delete()
	os.Exit(code)
}

func waitStatus(t *testing.T, sha string) models.Preview {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := database.GetPreview(sha); ok && p.Status != models.PreviewPending {
			return p
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("preview for %s never left pending", sha)
	return models.Preview{}
}

func waitAttempts(t *testing.T, sha string, attempts int) models.Preview {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := database.GetPreview(sha); ok && p.Status != models.PreviewPending && p.Attempts >= attempts {
			return p
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("preview for %s never reached %d attempts", sha, attempts)
	return models.Preview{}
}

func TestWorkerProcessesAndDedupes(t *testing.T) {
	dir := fixtures(t)
	data := configuration.Get().DataDir
	raw, _ := os.ReadFile(filepath.Join(dir, "rot.jpg"))
	test.IsNil(t, os.WriteFile(filepath.Join(data, "workersha1"), raw, 0600))
	f := models.File{Id: "workerfile000000001", Name: "IMG_1.JPG", SHA1: "workersha1", ContentType: "image/jpeg"}

	Start()
	defer Stop()
	Enqueue(f)
	Enqueue(f) // same blob twice: processed once, no error
	p := waitStatus(t, "workersha1")
	test.IsEqualString(t, p.Status, models.PreviewReady)
	thumb, _, _ := Paths(Dir(), "workersha1")
	test.FileExists(t, thumb)

	test.IsNil(t, os.WriteFile(filepath.Join(data, "badsha1"), []byte("junk"), 0600))
	Enqueue(models.File{Id: "workerfile000000002", Name: "bad.jpg", SHA1: "badsha1"})
	bad := waitStatus(t, "badsha1")
	test.IsEqualString(t, bad.Status, models.PreviewFailed)
	test.IsEqualInt(t, bad.Attempts, 1)
	test.IsEqualString(t, bad.Kind, models.PreviewKindImage)

	Enqueue(models.File{Id: "workerfile000000003", Name: "doc.pdf", SHA1: "pdfsha1", ContentType: "application/pdf"})
	test.IsEqualString(t, waitStatus(t, "pdfsha1").Status, models.PreviewUnsupported)

	removeBlob("workersha1")
	_, ok := database.GetPreview("workersha1")
	test.IsEqualBool(t, ok, false)
	_, err := os.Stat(thumb)
	test.IsEqualBool(t, os.IsNotExist(err), true)
}

func TestBackfillRetriesFailedUnderLimit(t *testing.T) {
	data := configuration.Get().DataDir
	test.IsNil(t, os.WriteFile(filepath.Join(data, "retrysha1"), []byte("junk"), 0600))
	test.IsNil(t, os.WriteFile(filepath.Join(data, "givenupsha1"), []byte("junk"), 0600))
	database.SaveMetaData(models.File{Id: "backfillfile00000001", Name: "retry.jpg", SHA1: "retrysha1", ContentType: "image/jpeg"})
	database.SaveMetaData(models.File{Id: "backfillfile00000002", Name: "givenup.jpg", SHA1: "givenupsha1", ContentType: "image/jpeg"})
	database.SavePreview(models.Preview{Sha1: "retrysha1", Status: models.PreviewFailed, Kind: models.PreviewKindImage, Attempts: 1})
	database.SavePreview(models.Preview{Sha1: "givenupsha1", Status: models.PreviewFailed, Kind: models.PreviewKindImage, Attempts: maxAttempts})
	defer database.DeleteMetaData("backfillfile00000001")
	defer database.DeleteMetaData("backfillfile00000002")

	Start()
	defer Stop()
	// Start runs its own backfill; wait for it to retry the retryable row
	// before calling backfill again, so the counts below are deterministic.
	p := waitAttempts(t, "retrysha1", 2)
	test.IsEqualString(t, p.Status, models.PreviewFailed)

	// More backfills take it to the limit. The database may hold other files
	// too, so the assertions are on these two rows, not on the count.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		waitIdle(t)
		if p, _ = database.GetPreview("retrysha1"); p.Attempts >= maxAttempts {
			break
		}
		backfill()
	}
	waitIdle(t)
	// At the limit nothing is requeued any more: further backfills must leave
	// both rows alone (a requeue would raise Attempts to 4).
	for i := 0; i < 2; i++ {
		backfill()
		waitIdle(t)
	}
	for _, sha := range []string{"retrysha1", "givenupsha1"} {
		p, _ = database.GetPreview(sha)
		test.IsEqualString(t, p.Status, models.PreviewFailed)
		test.IsEqualInt(t, p.Attempts, maxAttempts)
	}
}

// waitIdle blocks until the worker has no queued or running job
func waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		inFlight.Range(func(_, _ any) bool { busy = true; return false })
		if !busy {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("worker never went idle")
}

func TestEnqueueKeepsAttempts(t *testing.T) {
	Start()
	defer Stop()
	f := models.File{Id: "keepattempts00000001", Name: "x.jpg", SHA1: "keepattemptssha1", ContentType: "image/jpeg"}
	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewPending, Attempts: 2})
	test.IsNil(t, os.WriteFile(filepath.Join(configuration.Get().DataDir, f.SHA1), []byte("junk"), 0600))
	Enqueue(f)
	p := waitAttempts(t, f.SHA1, 3)
	test.IsEqualString(t, p.Status, models.PreviewFailed)
	test.IsEqualInt(t, p.Attempts, 3)
}

func TestStartSweepsStaleDecryptedFiles(t *testing.T) {
	test.IsNil(t, os.MkdirAll(Dir(), 0700))
	stale := filepath.Join(Dir(), "dec-stale.jpg")
	test.IsNil(t, os.WriteFile(stale, []byte("plaintext"), 0600))
	Start()
	defer Stop()
	_, err := os.Stat(stale)
	test.IsEqualBool(t, os.IsNotExist(err), true)
}

func TestBackfillRemovesOrphanedPreviews(t *testing.T) {
	Start()
	defer Stop()
	waitIdle(t)
	thumb, _, _ := Paths(Dir(), "orphansha1")
	test.IsNil(t, os.WriteFile(thumb, []byte("x"), 0600))
	database.SavePreview(models.Preview{Sha1: "orphansha1", Status: models.PreviewReady})
	backfill()
	_, ok := database.GetPreview("orphansha1")
	test.IsEqualBool(t, ok, false)
	_, err := os.Stat(thumb)
	test.IsEqualBool(t, os.IsNotExist(err), true)
}

func TestBackfillGivesUpOnPendingAtLimit(t *testing.T) {
	data := configuration.Get().DataDir
	test.IsNil(t, os.WriteFile(filepath.Join(data, "crashsha1"), []byte("junk"), 0600))
	database.SaveMetaData(models.File{Id: "crashfile0000000001", Name: "crash.jpg", SHA1: "crashsha1", ContentType: "image/jpeg"})
	defer database.DeleteMetaData("crashfile0000000001")
	// A job that was started three times and never finished (crash on every boot)
	database.SavePreview(models.Preview{Sha1: "crashsha1", Status: models.PreviewPending, Kind: models.PreviewKindImage, Attempts: maxAttempts})
	Start()
	defer Stop()
	backfill()
	waitIdle(t)
	p, _ := database.GetPreview("crashsha1")
	test.IsEqualString(t, p.Status, models.PreviewFailed)
	test.IsEqualInt(t, p.Attempts, maxAttempts) // not run again
}

func TestProcessCountsAttemptBeforeWorkAndSurvivesPanic(t *testing.T) {
	Start()
	defer Stop()
	waitIdle(t)
	orig := generateFn
	defer func() { generateFn = orig }()
	var seenDuring models.Preview
	generateFn = func(ctx context.Context, src, outDir, sha1, kind string) (models.Preview, error) {
		seenDuring, _ = database.GetPreview(sha1)
		panic("boom")
	}
	f := models.File{Id: "panicfile0000000001", Name: "p.jpg", SHA1: "panicsha1", ContentType: "image/jpeg"}
	process(f) // must not panic out
	// the attempt was on the row while Generate ran
	test.IsEqualString(t, seenDuring.Status, models.PreviewPending)
	test.IsEqualInt(t, seenDuring.Attempts, 1)
	p, _ := database.GetPreview("panicsha1")
	test.IsEqualString(t, p.Status, models.PreviewFailed)
	test.IsEqualInt(t, p.Attempts, 1) // counted once, not twice

	// worker keeps running after a panic
	generateFn = orig
	Enqueue(models.File{Id: "panicfile0000000002", Name: "q.pdf", SHA1: "afterpanicsha1", ContentType: "application/pdf"})
	test.IsEqualString(t, waitStatus(t, "afterpanicsha1").Status, models.PreviewUnsupported)
}
