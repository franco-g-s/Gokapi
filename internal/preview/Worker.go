package preview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
)

// maxAttempts is how many times generation is tried for one blob in total.
// A failure is saved as failed straight away; backfill retries it on later
// startups while Attempts is below this.
const maxAttempts = 3

var (
	queue    chan models.File
	stopCh   chan struct{}
	startMu  sync.Mutex
	running  bool
	inFlight sync.Map // sha1 -> struct{}, so duplicates in the queue are dropped
)

// Dir is where preview outputs are stored
func Dir() string { return filepath.Join(configuration.Get().DataDir, "previews") }

// Start launches the single worker and queues every blob without a usable
// preview. It is idempotent.
func Start() {
	startMu.Lock()
	defer startMu.Unlock()
	if running {
		return
	}
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		fmt.Printf("preview: cannot create %s: %v\n", Dir(), err)
	}
	inFlight.Range(func(k, _ any) bool { inFlight.Delete(k); return true })
	queue = make(chan models.File, 10000)
	stopCh = make(chan struct{})
	running = true
	storage.OnBlobDeleted = removeBlob
	storage.OnBlobAdded = Enqueue
	go loop(queue, stopCh)
	go backfill()
}

// Stop ends the worker after its current job
func Stop() {
	startMu.Lock()
	defer startMu.Unlock()
	if !running {
		return
	}
	close(stopCh)
	running = false
}

// Enqueue schedules a file's blob for preview generation. Never blocks.
func Enqueue(file models.File) {
	enqueue(file, false)
}

// enqueue queues the blob and reports whether it did. With retry set, a
// finished (failed) row is queued again; backfill uses that for failures
// that are still under maxAttempts.
func enqueue(file models.File, retry bool) bool {
	startMu.Lock()
	q, isRunning := queue, running
	startMu.Unlock()
	if !isRunning || file.SHA1 == "" || file.Encryption.IsEndToEndEncrypted || !file.IsLocalStorage() {
		return false
	}
	prev, exists := database.GetPreview(file.SHA1)
	if exists && prev.Status != models.PreviewPending && !retry {
		return false
	}
	if _, loaded := inFlight.LoadOrStore(file.SHA1, struct{}{}); loaded {
		return false
	}
	// Attempts survives re-queueing, otherwise retries would never accumulate
	database.SavePreview(models.Preview{Sha1: file.SHA1, Status: models.PreviewPending, Kind: prev.Kind, Attempts: prev.Attempts, Updated: time.Now().Unix()})
	select {
	case q <- file:
		return true
	default:
		inFlight.Delete(file.SHA1) // queue full; the next backfill picks it up
		return false
	}
}

// backfill queues every blob that has no finished preview: pending rows (an
// interrupted run), missing rows, and failed rows still under maxAttempts.
// It returns how many files were queued.
func backfill() int {
	known := database.GetAllPreviewSha1s()
	seen := make(map[string]bool)
	queued := 0
	for _, f := range database.GetAllMetadata() {
		if seen[f.SHA1] {
			continue
		}
		seen[f.SHA1] = true
		if status, ok := known[f.SHA1]; ok && status != models.PreviewPending {
			if status != models.PreviewFailed {
				continue
			}
			if p, found := database.GetPreview(f.SHA1); !found || p.Attempts >= maxAttempts {
				continue
			}
		}
		if enqueue(f, true) {
			queued++
		}
	}
	if queued > 0 {
		// The only Status-log line the worker writes: one per startup, in the
		// "info" category, which the current notifier ignores.
		logging.LogInfo(fmt.Sprintf("Preview backfill queued %d file(s)", queued))
	}
	return queued
}

func loop(q chan models.File, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case f := <-q:
			process(f)
			inFlight.Delete(f.SHA1)
		}
	}
}

func process(f models.File) {
	kind := Kind(f.ContentType, f.Name)
	prev, _ := database.GetPreview(f.SHA1)
	if kind == "" {
		database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewUnsupported, Updated: time.Now().Unix()})
		return
	}
	src, cleanup, err := readablePath(f)
	if err == nil {
		var p models.Preview
		p, err = Generate(context.Background(), src, Dir(), f.SHA1, kind)
		cleanup()
		if err == nil {
			database.SavePreview(p)
			return
		}
	}
	// stdout only: see Global Constraints (the notifier emails warning lines)
	fmt.Printf("preview: %s (%s): %v\n", f.Id, f.Name, err)
	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewFailed, Kind: kind, Attempts: prev.Attempts + 1, Updated: time.Now().Unix()})
}

// readablePath returns a plain-bytes path for the blob. Unencrypted local files
// are read in place; server-side encrypted ones are decrypted to a temp file
// that cleanup removes.
func readablePath(f models.File) (string, func(), error) {
	path := filepath.Join(configuration.Get().DataDir, f.SHA1)
	if !f.Encryption.IsEncrypted {
		return path, func() {}, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return "", func() {}, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(Dir(), "dec-*"+filepath.Ext(f.Name))
	if err != nil {
		return "", func() {}, err
	}
	err = encryption.DecryptReader(f.Encryption, in, tmp)
	_ = tmp.Close()
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp.Name(), cleanup, nil
}

func removeBlob(sha1 string) {
	thumb, view, poster := Paths(Dir(), sha1)
	for _, p := range []string{thumb, view, poster} {
		_ = os.Remove(p)
	}
	database.DeletePreview(sha1)
}
