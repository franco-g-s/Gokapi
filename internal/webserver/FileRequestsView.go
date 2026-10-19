package webserver

import (
	"net/http"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

// fileRequestsZipUrl is what the "filerequest" browsers carry in data-zip-url.
// The built-in zip (ZipUrl plus ids) is not used: request files are zipped
// through the presigned flow, because /files/zip refuses request uploads. The
// value only switches the "Download as .zip" button on; admin_ui_filerequest.js
// handles the click.
const fileRequestsZipUrl = "./api/files/downloadzip"

// FileRequestsView is the File requests page: the admin frame, plus one
// shared file browser per request, keyed by the request's id.
type FileRequestsView struct {
	AdminView
	Browsers map[string]fileBrowser
}

// newFileRequestsView builds the page's view. Each request's browser has the
// DOM id fr-<request id> and the "filerequest" context, whose files are listed
// newest first (AdminView has already sorted them).
func newFileRequestsView(r *http.Request, admin AdminView, user models.User) FileRequestsView {
	names := map[int]string{}
	for _, u := range database.GetAllUsers() {
		names[u.Id] = u.Name
	}
	view := FileRequestsView{AdminView: admin, Browsers: make(map[string]fileBrowser, len(admin.FileRequests))}
	for _, request := range admin.FileRequests {
		items := make([]browserItem, 0, len(request.Files))
		for _, f := range request.Files {
			items = append(items, browserItemFromFile(r, f, user, ctxFileRequest, "", names))
		}
		browser := newFileBrowser("fr-"+request.Id, ctxFileRequest, items, user)
		browser.ZipUrl = fileRequestsZipUrl
		view.Browsers[request.Id] = browser
	}
	return view
}
