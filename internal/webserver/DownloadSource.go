package webserver

import (
	"net/http"

	"github.com/forceu/gokapi/internal/storage/downloadlog"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// tagDownload marks r with the route and the signed-in person, if any, so that
// storage records a Downloads row for every original file it serves. Public
// routes read the session cookie without side effects (SessionIdentity). In
// Header or Disabled authentication there is no session, so public downloads
// stay anonymous.
func tagDownload(r *http.Request, via string) *http.Request {
	userId := 0
	if user, ok := authentication.SessionIdentity(r); ok {
		userId = user.Id
	}
	return downloadlog.With(r, via, userId)
}

// tagDownloadUser is for routes that already know who is asking: a
// requireLogin route, or an API key's owner
func tagDownloadUser(r *http.Request, via string, userId int) *http.Request {
	return downloadlog.With(r, via, userId)
}
