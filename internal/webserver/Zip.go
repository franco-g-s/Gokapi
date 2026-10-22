package webserver

import (
	"net/http"
	"strings"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/errorHandling"
)

// downloadCollectionZip serves every live member of a collection as one zip,
// or with files= only the selected ones (selectedMembers). Same gate as /cd:
// a password-protected collection needs its pc cookie, unless the visitor
// manages it (collectionUnlocked).
func downloadCollectionZip(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	collection, ok := storage.GetCollection(r.URL.Query().Get("id"))
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	if collectionClosed(r, collection) {
		// R4: the link requires joining a group this visitor cannot see
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	if !collectionUnlocked(r, collection) {
		redirect(w, r, "c?id="+collection.Id)
		return
	}
	storage.ServeZip(selectedMembers(collection.Files, r), collection.Name, w, tagDownload(r, models.DownloadViaZip), true)
}

// selectedMembers applies /cz's optional files= filter: the comma-separated
// ids of the members to include. It only narrows the collection's own live
// members - an id that is not one of them is ignored, never resolved - and a
// filter that matches nothing yields nothing, never the whole collection.
// It reads the query string and, for a POST, the form body, so a selection
// too long for a URL can be posted (Part 3).
func selectedMembers(members []models.File, r *http.Request) []models.File {
	_ = r.ParseForm()
	if !r.Form.Has("files") {
		return members
	}
	wanted := make(map[string]bool)
	for _, id := range strings.Split(r.Form.Get("files"), ",") {
		if id != "" {
			wanted[id] = true
		}
	}
	result := make([]models.File, 0, len(wanted))
	for _, f := range members {
		if wanted[f.Id] {
			result = append(result, f)
		}
	}
	return result
}

// downloadFilesZip serves the selected files from the Files tab as one zip.
// Each ID must belong to the user, unless they may list other users' uploads;
// anything else is silently dropped, leaving a 410 if nothing remains.
// Not counted: the owner (or a PERM_LIST admin) fetching files they manage
// must not spend the download budget of the links they shared (Ruling 18).
func downloadFilesZip(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	// ids= from the query string or, for a POST, the form body (Part 3)
	_ = r.ParseForm()
	files := make([]models.File, 0)
	for _, id := range strings.Split(r.Form.Get("ids"), ",") {
		f, ok := storage.GetFile(id)
		if !ok || (f.UserId != user.Id && !user.HasPermissionListOtherUploads()) {
			continue
		}
		files = append(files, f)
	}
	storage.ServeZip(files, "Files", w, tagDownloadUser(r, models.DownloadViaZip, user.Id), false)
}
