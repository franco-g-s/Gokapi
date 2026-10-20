package api

// Part 2, G7: uploading into a group through chunk/complete

import (
	"math"
	"net/http"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/errorHandling/errorcodes"
)

// groupUploadRefusal is why an upload may not go into the group it names
type groupUploadRefusal struct {
	status  int
	code    int
	message string
}

// resolveUploadGroup checks, before anything is stored, that the user may
// upload into request.GroupId, and replaces the client's expiry, download
// limit and password with the group's terms. It runs synchronously: the
// non-blocking path answers 200 before the file is processed, so a refusal
// found later would never reach the client. An empty GroupId is a private
// upload and changes nothing.
func resolveUploadGroup(request *paramChunkComplete, user models.User) (models.Group, *groupUploadRefusal) {
	if request.GroupId == "" {
		return models.Group{}, nil
	}
	g, ok := storage.GetGroup(request.GroupId)
	if !ok || !storage.CanSeeGroup(g, user) {
		message := "You are not a member of this group"
		if stored, found := database.GetGroup(request.GroupId); found && (storage.IsGroupMember(stored, user.Id) || user.IsAdmin()) {
			// a member of a group that has closed since the page loaded
			message = "This group has closed"
		}
		return models.Group{}, &groupUploadRefusal{http.StatusForbidden, errorcodes.NoPermission, message}
	}
	if request.IsE2E {
		return models.Group{}, &groupUploadRefusal{http.StatusBadRequest, errorcodes.EndToEndNotSupported,
			"End-to-end encrypted files cannot go into a group"}
	}
	request.UnlimitedDownloads = true
	request.AllowedDownloads = 0
	request.Password = ""
	if g.NeverExpires() {
		request.UnlimitedTime = true
	} else {
		// ProcessParameter turned a client's "0 days" into UnlimitedTime,
		// which would outlive the group
		request.UnlimitedTime = false
		request.ExpiryDays = int(math.Ceil(float64(g.Expires-time.Now().Unix()) / 86400))
	}
	return g, nil
}

// applyGroupExpiry gives the upload exactly the group's closing time:
// CreateUploadConfig only knows "now + days"
func applyGroupExpiry(params *models.UploadParameters, g models.Group) {
	if g.Id != "" && !g.NeverExpires() {
		params.ExpiryTimestamp = g.Expires
	}
}

// attachToGroup puts a just-stored upload into its group. It reports false
// when a byte-identical copy was already there: AttachUploadToGroup has then
// deleted this one, and a blocking caller is told so. No metadata lock may
// be held here (AttachUploadToGroup takes it).
func attachToGroup(w http.ResponseWriter, g models.Group, file models.File, user models.User) bool {
	if g.Id == "" {
		return true
	}
	if _, dup := storage.AttachUploadToGroup(g, file, user); dup {
		sendError(w, http.StatusConflict, errorcodes.AlreadyExists, "Already in the group")
		return false
	}
	return true
}
