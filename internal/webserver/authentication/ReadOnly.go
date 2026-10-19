package authentication

import (
	"net/http"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
)

// IsAuthenticatedReadOnly answers the same question as IsAuthenticated for
// callers that have no ResponseWriter to renew a session on. IsAuthenticated
// panics on a nil writer when a session is due for renewal, and with a throwaway
// writer it would delete the user's session without delivering the new cookie.
func IsAuthenticatedReadOnly(r *http.Request) (models.User, bool) {
	switch authSettings.Method {
	case models.AuthenticationInternal, models.AuthenticationOAuth2:
		return sessionmanager.PeekSession(r)
	case models.AuthenticationHeader:
		user, ok, err := isGrantedHeader(r)
		if err != nil || !ok {
			return models.User{}, false
		}
		return user, true
	case models.AuthenticationDisabled:
		adminUser, ok := database.GetSuperAdmin()
		return adminUser, ok
	}
	return models.User{}, false
}
