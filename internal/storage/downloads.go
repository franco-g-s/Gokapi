package storage

import (
	"net/http"
	"time"

	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/downloadlog"
)

// recordDownload writes one Downloads row for an original file served to this
// request, if the route tagged it (downloadlog.With). Untagged requests, such
// as every preview route, record nothing. A HEAD request sends no file, so it
// is not a download either.
func recordDownload(file models.File, r *http.Request) {
	src, ok := downloadlog.From(r)
	if !ok || r.Method == http.MethodHead || !database.SupportsGroups() {
		return
	}
	database.SaveDownload(models.Download{FileId: file.Id, UserId: src.UserId, At: time.Now().Unix(), Via: src.Via})
	activity.Download(file, src.UserId, src.Via)
}
