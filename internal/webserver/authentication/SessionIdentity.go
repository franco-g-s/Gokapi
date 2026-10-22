package authentication

import (
	"net/http"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
	"github.com/forceu/gokapi/internal/webserver/viewas"
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
		u, ok := sessionmanager.PeekSession(r)
		if !ok {
			return models.User{}, false
		}
		// Part 3, V1: an admin in "View as…" is the target here too, as the guard decided
		if t, ok := viewas.SwapFor(r, u); ok {
			return t, true
		}
		return u, true
	}
	return models.User{}, false
}
