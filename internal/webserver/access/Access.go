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
// edited model cannot widen access.
func CanView(r *http.Request, file models.File, collectionId string) bool {
	current, ok := storage.GetFile(file.Id)
	if !ok || current.UploadRequestId != "" || current.Encryption.IsEndToEndEncrypted {
		return false
	}
	if user, loggedIn := authentication.IsAuthenticatedReadOnly(r); loggedIn {
		if user.Id == current.UserId || user.HasPermission(models.UserPermListOtherUploads) {
			return true
		}
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
