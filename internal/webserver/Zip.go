package webserver

import (
	"net/http"
	"strings"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/errorHandling"
)

// downloadCollectionZip serves every live member of a collection as one zip.
// Same gate as /cd: a password-protected collection needs its pc cookie.
func downloadCollectionZip(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	collection, ok := storage.GetCollection(r.URL.Query().Get("id"))
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	if collection.IsPasswordProtected() && !isValidCollectionPwCookie(r, collection) {
		redirect(w, r, "c?id="+collection.Id)
		return
	}
	storage.ServeZip(collection.Files, collection.Name, w, r, true)
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
	files := make([]models.File, 0)
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		f, ok := storage.GetFile(id)
		if !ok || (f.UserId != user.Id && !user.HasPermissionListOtherUploads()) {
			continue
		}
		files = append(files, f)
	}
	storage.ServeZip(files, "Files", w, r, false)
}
