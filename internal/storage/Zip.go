package storage

import (
	"net/http"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/api/mutex/apimutex"
	"github.com/forceu/gokapi/internal/webserver/sse"
)

// ZipEntryTime is the modified time stamped on each zip entry. It is a variable
// so the preview package can substitute the capture date once it is known,
// without storage importing preview. Default: the upload date.
var ZipEntryTime = func(file models.File) time.Time {
	return time.Unix(file.UploadDate, 0)
}

// ServeZip streams the downloadable subset of files as a store-only zip and
// returns how many were included. Each file is re-resolved, so expired,
// exhausted, file-request and client-decrypted (end-to-end, or encrypted on
// remote storage) files are skipped, and
// limited files are counted exactly as a single download through /cd would be.
func ServeZip(files []models.File, name string, w http.ResponseWriter, r *http.Request) int {
	included := make([]models.File, 0, len(files))
	now := time.Now().Unix()
	for _, f := range files {
		current, ok := GetFile(f.Id)
		// Files only the client can decrypt (end-to-end, or encrypted on
		// remote storage) would go into the zip as ciphertext
		if !ok || current.UploadRequestId != "" || current.Encryption.IsEndToEndEncrypted || current.RequiresClientDecryption() {
			continue
		}
		apimutex.Lock(apimutex.TypeMetaData, current.Id)
		if !current.UnlimitedDownloads {
			current.DownloadsRemaining = database.GetDownloadsRemaining(current.Id)
		}
		if IsExpiredFile(current, now) {
			apimutex.Unlock(apimutex.TypeMetaData, current.Id)
			continue
		}
		current.DownloadsRemaining--
		current.DownloadCount++
		database.IncreaseDownloadCount(current.Id, !current.UnlimitedDownloads)
		apimutex.Unlock(apimutex.TypeMetaData, current.Id)
		go sse.PublishDownloadCount(current)
		included = append(included, current)
	}
	if len(included) == 0 {
		http.Error(w, "Nothing left to download", http.StatusGone)
		return 0
	}
	ServeFilesAsZip(included, name, w, r)
	return len(included)
}
