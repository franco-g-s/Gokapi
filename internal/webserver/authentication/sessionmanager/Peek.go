package sessionmanager

import (
	"net/http"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

// PeekSession is IsValidSession without side effects. IsValidSession renews a
// session that is due by deleting it and setting a new cookie on the response;
// a caller with no response to write to (an access check inside an image
// request) would log the user out. PeekSession never renews, deletes or touches
// last-online.
func PeekSession(r *http.Request) (models.User, bool) {
	cookie, err := r.Cookie("session_token")
	if err != nil || cookie.Value == "" {
		return models.User{}, false
	}
	session, ok := database.GetSession(cookie.Value)
	if !ok || session.ValidUntil < time.Now().Unix() {
		return models.User{}, false
	}
	user, ok := database.GetUser(session.UserId)
	if !ok {
		return models.User{}, false
	}
	return user, true
}
