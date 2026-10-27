package api

import "github.com/forceu/gokapi/internal/models"

// changesGroupTerms reports whether a /files/modify request would change
// what a group sets for its files (D4): a download limit, an expiry other
// than the current one, or the password. Renaming and lifting the limit do
// not count. Sending the current expiry, as the edit dialog does, keeps it.
func changesGroupTerms(request *paramFilesModify, file models.File) bool {
	if !request.UnlimitedDownloads && request.AllowedDownloads != 0 {
		return true
	}
	if request.UnlimitedExpiry && !file.UnlimitedTime {
		return true
	}
	if !request.UnlimitedExpiry && request.ExpiryTimestamp != 0 &&
		(file.UnlimitedTime || request.ExpiryTimestamp != file.ExpireAt) {
		return true
	}
	// Without originalPassword the request sets the password it sends, and
	// an empty one removes the current password
	if !request.KeepPassword && (request.Password != "" || file.PasswordHash != "") {
		return true
	}
	return false
}
