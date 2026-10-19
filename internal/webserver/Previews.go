package webserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/preview"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/access"
)

type previewInfo struct {
	Status   string // "ready", "pending", "failed", "unsupported", or "" if no row
	Kind     string
	Width    int
	Height   int
	Color    string
	TakenAt  int64
	Thumb    string // "" unless ready
	View     string
	Video    string // only for video
	Download string // the original, via the route the caller passes in
}

// previewUrls builds the display URLs for a file. collectionId scopes access
// checks to a collection page; download is the original-file URL to show.
func previewUrls(file models.File, collectionId, download string) previewInfo {
	info := previewInfo{Download: download}
	p, ok := database.GetPreview(file.SHA1)
	if !ok {
		return info
	}
	info.Status, info.Kind, info.Color = p.Status, p.Kind, p.Color
	info.Width, info.Height, info.TakenAt = p.Width, p.Height, p.TakenAt
	if p.Status != models.PreviewReady {
		return info
	}
	q := "?id=" + file.Id + "&v=" + p.Version()
	if collectionId != "" {
		q += "&c=" + collectionId
	}
	info.Thumb, info.View = "./p/thumb"+q, "./p/view"+q
	if p.Kind == models.PreviewKindVideo {
		info.Video = "./p/video" + q
	}
	return info
}

func servePreviewFile(which string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, ok := storage.GetFile(r.URL.Query().Get("id"))
		if !ok || !access.CanView(r, file, r.URL.Query().Get("c")) {
			http.NotFound(w, r)
			return
		}
		p, ok := database.GetPreview(file.SHA1)
		if !ok || p.Status != models.PreviewReady {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		thumb, view, poster := preview.Paths(preview.Dir(), file.SHA1)
		path := map[string]string{"thumb": thumb, "view": view, "poster": poster}[which]
		f, err := os.Open(path)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Query().Get("v") == p.Version() {
			w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "private, no-cache")
		}
		http.ServeContent(w, r, filepath.Base(path), time.Unix(p.Updated, 0), f)
	}
}

// streamVideo plays the original inline for the viewer. It is deliberately
// not a download: no counter, no download log, no Downloads row. A <video>
// element issues many range requests per play.
//
// It only serves files the worker has classified as video, so it is not a
// second, uncounted way to fetch an arbitrary upload, and never an upload's
// own Content-Type unless that is a video type.
func streamVideo(w http.ResponseWriter, r *http.Request) {
	file, ok := storage.GetFile(r.URL.Query().Get("id"))
	if !ok || !access.CanView(r, file, r.URL.Query().Get("c")) || !file.IsLocalStorage() || file.Encryption.IsEncrypted {
		http.NotFound(w, r)
		return
	}
	if p, ok := database.GetPreview(file.SHA1); !ok || p.Status != models.PreviewReady || p.Kind != models.PreviewKindVideo {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(configuration.Get().DataDir, file.SHA1))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	ct := file.ContentType
	if !strings.HasPrefix(ct, "video/") || ct == "video/quicktime" {
		ct = "video/mp4" // Chrome refuses video/quicktime for HEVC/H.264 MOVs it can otherwise play
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, "", time.Unix(file.UploadDate, 0), f)
}
