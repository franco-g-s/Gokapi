// Package access answers one question for every new route: may this request
// see this file? Keeping it in one place is what stops a preview, a board feed
// or a zip from leaking something its download page would refuse.
package access

import (
	"net/http"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// CanView reports whether the request may see the file, optionally in the
// context of a collection page. The file is re-resolved by id, so a stale or
// edited model cannot widen access. File request uploads are viewable only by
// the file's owner and PERM_LIST users, identified by their session.
func CanView(r *http.Request, file models.File, collectionId string) bool {
	current, ok := storage.GetFile(file.Id)
	if !ok || current.Encryption.IsEndToEndEncrypted {
		return false
	}
	if user, loggedIn := authentication.SessionIdentity(r); loggedIn {
		if user.Id == current.UserId || user.HasPermission(models.UserPermListOtherUploads) {
			return true
		}
	}
	// A file request upload belongs to the request's owner. Nobody else may
	// see it, however it was shared: it has no password or collection of its
	// own to gate a preview on.
	if current.UploadRequestId != "" {
		return false
	}
	// A member of a live group the file is in (spec, "Access")
	if user, loggedIn := authentication.SessionIdentity(r); loggedIn && storage.UserSharesGroupWithFile(user.Id, current.Id) {
		return true
	}
	if collectionId != "" {
		collection, ok := storage.GetCollection(collectionId)
		if ok {
			if _, member := storage.GetCollectionFile(collection, current.Id); member {
				return !collection.IsPasswordProtected() || ValidCollectionPwCookie(r, collection.Id)
			}
		}
	}
	return current.PasswordHash == "" || ValidFilePwCookie(r, current.Id)
}

// MayStream reports whether /p/video may play the file inline for this
// request, on top of CanView. Streaming is never counted, so a file with a
// download cap streams only for its uploader or a PERM_LIST user - anyone
// else could otherwise watch it without ever spending the cap (Ruling 19).
// Remote and encrypted files are never streamed: the server would have to
// proxy or decrypt them. Pass the current model (storage.GetFile).
func MayStream(r *http.Request, file models.File) bool {
	if !file.IsLocalStorage() || file.Encryption.IsEncrypted {
		return false
	}
	if file.UnlimitedDownloads {
		return true
	}
	user, loggedIn := authentication.SessionIdentity(r)
	return loggedIn && (user.Id == file.UserId || user.HasPermission(models.UserPermListOtherUploads))
}

// ValidFilePwCookie checks the cookie set after entering a file's password
func ValidFilePwCookie(r *http.Request, fileId string) bool {
	c, err := r.Cookie("p" + fileId)
	return err == nil && downloadPasswordToken.IsValid(c.Value, fileId)
}

// ValidCollectionPwCookie checks the cookie set after entering a collection's password
func ValidCollectionPwCookie(r *http.Request, collectionId string) bool {
	c, err := r.Cookie("pc" + collectionId)
	return err == nil && downloadPasswordToken.IsValid(c.Value, collectionId)
}
