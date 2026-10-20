package authentication

import (
	"net/http"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
)

// SessionIdentity returns the user behind a real login session cookie, and
// nothing else. It is for public routes that only want to widen access for a
// logged-in user (preview images, inline video), and it is deliberately not
// IsAuthenticated:
//   - IsAuthenticated renews a due session on the response (panics on a nil
//     writer, or deletes the session without delivering the new cookie),
//   - in Disabled mode it names the super admin for every anonymous request,
//   - in Header mode it trusts a request header and may create users.
//
// Only the Internal and OAuth2 session cookie counts. It performs no writes.
func SessionIdentity(r *http.Request) (models.User, bool) {
	switch authSettings.Method {
	case models.AuthenticationInternal, models.AuthenticationOAuth2:
		return sessionmanager.PeekSession(r)
	}
	return models.User{}, false
}
