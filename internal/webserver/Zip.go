package webserver

import (
	"net/http"

	"github.com/forceu/gokapi/internal/storage"
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
	storage.ServeZip(collection.Files, collection.Name, w, r)
}
