package webserver

// POST /seed/photo (Part 4, S1), for e2e and the preview only: they run
// internal sign-in, where no Google photo can arrive, and the image has no
// sqlite3 to seed one. The route exists only when Start saw
// GOKAPI_E2E_SEED=1, so on live it is a 404. "View as…" refuses it, like
// every route it does not list.

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/people"
)

// seedRoutesEnabled reads GOKAPI_E2E_SEED; Start calls it once
func seedRoutesEnabled() bool { return os.Getenv("GOKAPI_E2E_SEED") == "1" }

// registerSeedRoutes adds the seed routes when enabled
func registerSeedRoutes(mux *http.ServeMux, enabled bool) {
	if !enabled {
		return
	}
	mux.HandleFunc("/seed/photo", requireLogin(seedPhoto, false, false))
}

type seedPhotoRequest struct {
	UserId     int    `json:"userId"`
	JpegBase64 string `json:"jpegBase64"`
}

var errSeedAdminOnly = errors.New("admin only")
var errSeedNoUser = errors.New("no such user")

// seedPhoto stores a photo for a user through the same checks as a fetched one
func seedPhoto(w http.ResponseWriter, r *http.Request) {
	var request seedPhotoRequest
	user, ok := groupRequest(w, r, &request)
	if !ok {
		return
	}
	if !user.IsAdmin() {
		writeJsonError(w, http.StatusForbidden, errSeedAdminOnly)
		return
	}
	target, found := database.GetUser(request.UserId)
	if !found {
		writeJsonError(w, http.StatusNotFound, errSeedNoUser)
		return
	}
	data, err := base64.StdEncoding.DecodeString(request.JpegBase64)
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return
	}
	if err = people.StorePhoto(target.Id, data); err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return
	}
	writeJson(w, struct {
		Result   string
		PhotoUrl string
	}{"OK", people.PhotoUrl(target)})
}
