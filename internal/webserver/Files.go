package webserver

import (
	"net/http"
	"sort"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type fileRow struct {
	File     models.FileApiOutput
	Uploader string
	Mine     bool
	TakenAt  int64
	Preview  previewInfo
}

// FilesView is the Files tab: the upload table's actions over a Finder-like grid
type FilesView struct {
	AdminView
	Rows           []fileRow
	CanSeeEveryone bool
}

// isListedFile is the upload table's filter: no pending deletions, no file
// request uploads, nothing expired and nothing out of downloads.
func isListedFile(f models.FileApiOutput, now int64) bool {
	if f.IsPendingDeletion || f.IsFileRequest {
		return false
	}
	if !(f.ExpireAt > now || f.UnlimitedTime) {
		return false
	}
	return f.DownloadsRemaining > 0 || f.UnlimitedDownloads
}

// directDownloadUrl is the uncounted single-file download for a tile, or ""
// for an end-to-end encrypted file, which only the browser could decrypt.
// Password-protected and remote-encrypted files have a URL: the owner needs no
// password, and the server decrypts as it does for the upload page's download.
func directDownloadUrl(f models.FileApiOutput) string {
	if f.IsEndToEndEncrypted {
		return ""
	}
	return "./files/download?id=" + f.Id
}

// Handling of /files/download
// The owner (or a PERM_LIST admin) downloading the original from the Files
// tab. Not counted - they are managing the file, not consuming the share - as
// the upload page's presigned download already is (Ruling 18). Anything the
// user may not list is a 404, like a file that does not exist.
func downloadOwnFile(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, ok := storage.GetFile(r.URL.Query().Get("id"))
	if !ok || file.IsFileRequest() || file.Encryption.IsEndToEndEncrypted ||
		(file.UserId != user.Id && !user.HasPermissionListOtherUploads()) {
		http.NotFound(w, r)
		return
	}
	forceDecryption := file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted
	if !storage.ServeFile(file, w, r, true, false, forceDecryption, true) {
		http.NotFound(w, r)
	}
}

// Handling of /files
// Every file the user may see, as a grid of thumbnails or a list. Users
// without PERM_LIST only ever receive their own files.
func showFiles(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		redirect(w, r, "login")
		return
	}
	config := configuration.Get()
	view := FilesView{CanSeeEveryone: user.HasPermissionListOtherUploads()}
	view.AdminView = *(&AdminView{}).convertGlobalConfig(ViewFiles, user)
	names := map[int]string{}
	for _, u := range database.GetAllUsers() {
		names[u.Id] = u.Name
	}
	now := time.Now().Unix()
	for _, f := range database.GetAllMetadata() {
		if f.UserId != user.Id && !view.CanSeeEveryone {
			continue
		}
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		if !isListedFile(out, now) {
			continue
		}
		row := fileRow{File: out, Uploader: names[f.UserId], Mine: f.UserId == user.Id}
		row.Preview = previewUrls(r, f, "", directDownloadUrl(out))
		row.TakenAt = row.Preview.TakenAt
		view.Rows = append(view.Rows, row)
	}
	sort.SliceStable(view.Rows, func(i, j int) bool {
		if view.Rows[i].File.UploadDate != view.Rows[j].File.UploadDate {
			return view.Rows[i].File.UploadDate > view.Rows[j].File.UploadDate
		}
		return view.Rows[i].File.Id > view.Rows[j].File.Id
	})
	err = templateFolder.ExecuteTemplate(w, "files", &view) // a pointer, so the template can call User methods with pointer receivers
	helper.CheckIgnoreTimeout(err)
}
