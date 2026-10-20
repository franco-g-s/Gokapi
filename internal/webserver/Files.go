package webserver

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

var fileTypeIcons = map[string]string{}

// fileTypeIconGroups maps an icon to the extensions that get it. media.js
// has the same table for files added live (TestFileTypeIconsMatchJs).
var fileTypeIconGroups = map[string]string{
	"bi-file-earmark-pdf":         "pdf",
	"bi-file-earmark-image":       "jpg jpeg png gif webp heic heif avif bmp tif tiff svg dng raw cr2 nef arw",
	"bi-file-earmark-play":        "mp4 mov m4v mkv webm avi wmv mpg mpeg 3gp",
	"bi-file-earmark-music":       "mp3 m4a wav flac aac ogg opus aiff",
	"bi-file-earmark-zip":         "zip rar 7z tar gz tgz bz2 xz zst dmg iso",
	"bi-file-earmark-text":        "txt md rtf log",
	"bi-file-earmark-word":        "doc docx odt pages",
	"bi-file-earmark-spreadsheet": "xls xlsx ods csv numbers",
	"bi-file-earmark-slides":      "ppt pptx odp key",
	"bi-file-earmark-code":        "go js ts py c h cpp java rs sh html css json xml yaml yml sql",
}

func init() {
	for icon, extensions := range fileTypeIconGroups {
		for _, extension := range strings.Fields(extensions) {
			fileTypeIcons[extension] = icon
		}
	}
}

// fileTypeIcon is the icon a file without a thumbnail gets, by extension
func fileTypeIcon(name string) string {
	if icon, ok := fileTypeIcons[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]; ok {
		return icon
	}
	return "bi-file-earmark"
}

// FilesView is the Files tab: the shared file browser over every file the
// user may list
type FilesView struct {
	AdminView
	Browser fileBrowser
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
	if !storage.ServeFile(file, w, tagDownloadUser(r, models.DownloadViaFile, user.Id), true, false, forceDecryption, true) {
		http.NotFound(w, r)
	}
}

// Handling of /files
// Every file the user may see, in the shared file browser (context "files").
// Users without PERM_LIST only ever receive their own files.
func showFiles(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		redirect(w, r, "login")
		return
	}
	config := configuration.Get()
	view := FilesView{}
	view.AdminView = *(&AdminView{}).convertGlobalConfig(ViewFiles, user)
	seeEveryone := user.HasPermissionListOtherUploads()
	names := map[int]string{}
	for _, u := range database.GetAllUsers() {
		names[u.Id] = u.Name
	}
	now := time.Now().Unix()
	items := make([]browserItem, 0)
	for _, f := range database.GetAllMetadata() {
		if f.UserId != user.Id && !seeEveryone {
			continue
		}
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		if !isListedFile(out, now) {
			continue
		}
		items = append(items, browserItemFromFile(r, f, user, ctxFiles, "", names))
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].File.UploadDate != items[j].File.UploadDate {
			return items[i].File.UploadDate > items[j].File.UploadDate
		}
		return items[i].File.Id > items[j].File.Id
	})
	view.Browser = newFileBrowser("fgs-files", ctxFiles, items, user)
	err = templateFolder.ExecuteTemplate(w, "files", &view)
	helper.CheckIgnoreTimeout(err)
}
