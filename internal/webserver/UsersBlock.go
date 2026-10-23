package webserver

// Part 4, K: the blocklist endpoints, used by the "Blocked" section of the
// Users page. Both are POST + JSON only (D20) and admin only.

import (
	"errors"
	"net/http"

	"github.com/forceu/gokapi/internal/blocklist"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type blockRequest struct {
	Entry string `json:"entry"`
}

// blockAdmin returns the signed-in admin, or writes a 403 and returns false
func blockAdmin(w http.ResponseWriter, r *http.Request) (models.User, bool) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil || !user.IsAdmin() {
		writeJsonError(w, http.StatusForbidden, errBlockAdminOnly)
		return models.User{}, false
	}
	return user, true
}

var errBlockAdminOnly = errors.New("only an admin can change the blocklist")

// readBlockRequest checks the method and body and returns the entry
func readBlockRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return "", false
	}
	var request blockRequest
	if err := readJsonBody(r, &request); err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return "", false
	}
	return request.Entry, true
}

// blockEntry answers POST /users/block {"entry": "a@b.com" | "@b.com"}
func blockEntry(w http.ResponseWriter, r *http.Request) {
	admin, ok := blockAdmin(w, r)
	if !ok {
		return
	}
	entry, ok := readBlockRequest(w, r)
	if !ok {
		return
	}
	err := blocklist.Block(entry, admin)
	if err != nil {
		if errors.Is(err, blocklist.ErrAdmin) || errors.Is(err, blocklist.ErrEntry) {
			writeJsonError(w, http.StatusBadRequest, err)
			return
		}
		writeJsonError(w, http.StatusInternalServerError, err)
		return
	}
	writeJson(w, struct {
		Result string `json:"Result"`
	}{"OK"})
}

// unblockEntry answers POST /users/unblock {"entry": "..."}
func unblockEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := blockAdmin(w, r); !ok {
		return
	}
	entry, ok := readBlockRequest(w, r)
	if !ok {
		return
	}
	blocklist.Unblock(entry)
	writeJson(w, struct {
		Result string `json:"Result"`
	}{"OK"})
}
