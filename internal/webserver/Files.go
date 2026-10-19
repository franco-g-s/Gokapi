package webserver

import (
	"net/http"
	"sort"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
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

// directDownloadUrl is the counted single-file download for a tile, or "" when
// /downloadFile would answer with a page rather than the file (a password, or
// decryption that has to happen in the browser).
func directDownloadUrl(f models.FileApiOutput) string {
	if f.IsPasswordProtected || f.RequiresClientSideDecryption {
		return ""
	}
	return "./downloadFile?id=" + f.Id
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
		row.Preview = previewUrls(f, "", directDownloadUrl(out))
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
