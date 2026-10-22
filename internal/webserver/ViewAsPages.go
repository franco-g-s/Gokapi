package webserver

// "View as…" (Part 3, V1): the start and end endpoints. It is started from the
// Users tab (R2, V4); the state, the swap and the guard live in package viewas.

import (
	"errors"
	"net/http"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/viewas"
)

// realAdmin returns the real user behind the request, the admin behind a
// swapped request included, if it is a real admin
func realAdmin(r *http.Request) (models.User, bool) {
	user, ok := viewas.RealUser(r)
	if !ok {
		var err error
		user, err = authentication.GetUserFromRequest(r)
		if err != nil {
			return models.User{}, false
		}
	}
	return user, viewas.IsRealAdmin(user)
}

// viewasActive is true while the request comes from an admin in "View as…"
func viewasActive(r *http.Request) bool {
	_, _, ok := viewas.Active(r)
	return ok
}

type viewAsStartRequest struct {
	UserId int `json:"userId"`
}

var errViewAsMissingUser = errors.New("userId is missing")

// viewAsStart answers POST /viewas/start {"userId": n}
func viewAsStart(w http.ResponseWriter, r *http.Request) {
	admin, ok := realAdmin(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
	// Only a real session cookie is enforced by the guard. Under Header or
	// Disabled authentication there is none, so the swap would act unguarded.
	if !authentication.IsLogoutAvailable() {
		writeJsonError(w, http.StatusNotImplemented, errViewAsNeedsSession)
		return
	}
	var request viewAsStartRequest
	if err := readJsonBody(r, &request); err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return
	}
	if request.UserId == 0 {
		writeJsonError(w, http.StatusBadRequest, errViewAsMissingUser)
		return
	}
	err := viewas.Start(admin, request.UserId)
	switch {
	case errors.Is(err, viewas.ErrNotAdmin):
		http.NotFound(w, r)
	case errors.Is(err, viewas.ErrBadTarget):
		writeJsonError(w, http.StatusBadRequest, err)
	case errors.Is(err, viewas.ErrUnsupported):
		writeJsonError(w, http.StatusNotImplemented, err)
	case err != nil:
		writeJsonError(w, http.StatusInternalServerError, err)
	default:
		writeJson(w, struct {
			Result string `json:"Result"`
		}{"OK"})
	}
}

var errViewAsNeedsSession = errors.New("View as… needs internal or OAuth2 sign-in")

// viewAsEnd answers POST /viewas/end {}
func viewAsEnd(w http.ResponseWriter, r *http.Request) {
	admin, ok := realAdmin(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
	viewas.End(admin.Id, "exit")
	writeJson(w, struct {
		Result string `json:"Result"`
	}{"OK"})
}
