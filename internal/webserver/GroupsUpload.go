package webserver

// Part 2, G7: the duplicate pre-check for uploading into a group

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// groupHasResult answers /groups/has. Checkable is false when stored hashes
// are salted (D3); Exists is then false, and the server's check after the
// upload is the only one.
type groupHasResult struct {
	Checkable bool   `json:"checkable"`
	Exists    bool   `json:"exists"`
	Uploader  string `json:"uploader"`
	FileId    string `json:"fileId"`
}

var groupHasSha1Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// groupHas answers GET /groups/has?id=<group>&sha1=<hex>: whether a file
// with these bytes is already in the group, and who put it there. Only
// members and the admin may ask; to anyone else the group does not exist.
func groupHas(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	if !database.SupportsGroups() {
		writeJsonError(w, http.StatusNotImplemented, storage.ErrGroupsUnsupported)
		return
	}
	g, ok := storage.GetGroup(r.URL.Query().Get("id"))
	if !ok || !storage.CanSeeGroup(g, user) {
		writeJsonError(w, http.StatusNotFound, storage.ErrGroupNotFound)
		return
	}
	sha1 := strings.ToLower(r.URL.Query().Get("sha1"))
	if !groupHasSha1Pattern.MatchString(sha1) {
		writeJsonError(w, http.StatusBadRequest, errors.New("sha1 must be 40 hex characters"))
		return
	}
	result := groupHasResult{Checkable: storage.StoredSha1IsPlain()}
	if result.Checkable {
		if existing, found := storage.FindInGroup(g, sha1); found {
			result.Exists = true
			result.FileId = existing.Id
			if owner, known := database.GetUser(existing.UserId); known {
				result.Uploader = displayName(owner)
			}
		}
	}
	writeJson(w, result)
}
