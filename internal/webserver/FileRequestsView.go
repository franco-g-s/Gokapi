package webserver

import (
	"net/http"

	"github.com/forceu/gokapi/internal/models"
)

// FileRequestsView is the File requests page: the admin frame, plus one
// shared file browser per request, keyed by the request's id.
type FileRequestsView struct {
	AdminView
	Browsers map[string]fileBrowser
}

// newFileRequestsView builds the page's view. Each request's browser has the
// DOM id fr-<request id> and the "filerequest" context, whose files are listed
// newest first, in batches from /files/list?src=request (fileRequestSource).
func newFileRequestsView(r *http.Request, admin AdminView, user models.User) FileRequestsView {
	view := FileRequestsView{AdminView: admin, Browsers: make(map[string]fileBrowser, len(admin.FileRequests))}
	for _, request := range admin.FileRequests {
		browser := newFileBrowser("fr-"+request.Id, ctxFileRequest, nil, user)
		// /files/zip refuses request uploads: admin_ui_filerequest.js zips
		// them through the presigned flow
		browser.ZipByHandler = true
		browser.WithList(fileRequestSource(r, user, request), listUrl("./files/list?src=request&id=", request.Id))
		view.Browsers[request.Id] = browser
	}
	return view
}
