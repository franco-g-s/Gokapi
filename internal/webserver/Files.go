package webserver

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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
	// PersonName is set on the per-person view (/files?user=<id>): whose files
	// the page lists. Empty on the ordinary Files tab.
	PersonName string
}

// personFilter reads ?user=<id> for the per-person view. It applies only to
// someone who may list other people's uploads, and only to a person who
// exists; anything else is ignored and the ordinary Files tab shows.
func personFilter(r *http.Request, viewer models.User) (models.User, bool) {
	raw := r.URL.Query().Get("user")
	if raw == "" || !viewer.HasPermissionListOtherUploads() {
		return models.User{}, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return models.User{}, false
	}
	return database.GetUser(id)
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
	view := FilesView{}
	view.AdminView = *(&AdminView{}).convertGlobalConfig(ViewFiles, user)
	source, meta := filesTabSource(r, user)
	view.Browser = newFileBrowser("fgs-files", ctxFiles, nil, user)
	if meta.InGroups {
		// In at least one live group: the switch, even while the groups
		// hold only their own files
		view.Browser.ShowScope = true
		view.Browser.ShowUploader = true
	}
	if meta.OnlyPerson {
		// Everything on the page is one person's: the My files / Everything
		// switch would have nothing to switch between
		view.Browser.ShowScope = false
		view.PersonName = displayName(meta.Person)
	}
	view.Browser.WithList(source, filesListUrl(meta))
	err = templateFolder.ExecuteTemplate(w, "files", &view)
	helper.CheckIgnoreTimeout(err)
}

// filesTabGroupItems is the Files tab's share of the viewer's live groups,
// for someone without PERM_LIST: the other people's live files in them, each
// once. They are someone else's, so nothing on them is editable and they
// carry no management data (Foreign). Each is downloaded, counted and
// recorded through the first of the viewer's groups that has it (./gd), and
// shows its uploader's display name (D5).
func filesTabGroupItems(r *http.Request, user models.User) []browserItem {
	items := make([]browserItem, 0)
	seen := make(map[string]bool)
	for _, g := range storage.GroupsOfUser(user.Id) {
		files := storage.GroupFiles(g)
		names := memberNamesOf(g, files)
		for _, f := range files {
			if f.UserId == user.Id || seen[f.Id] || f.IsFileRequest() || f.Encryption.IsEndToEndEncrypted {
				continue
			}
			seen[f.Id] = true
			item := browserItemFromFile(r, f, user, ctxGroup, g.Id, names)
			item.makeForeign()
			item.Mine = false
			item.CanEdit, item.CanDelete, item.CanReplace, item.CanRemove = false, false, false, false
			items = append(items, item)
		}
	}
	return items
}
