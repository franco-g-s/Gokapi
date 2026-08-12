package storage

import (
	"errors"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

// ErrorCollectionNoFiles is returned if a collection would contain no usable files
var ErrorCollectionNoFiles = errors.New("no valid files were selected")

// ErrorCollectionE2E is returned if an end-to-end encrypted file was selected.
// The recipient of a collection link has no key, so the server could never
// hand them anything readable.
var ErrorCollectionE2E = errors.New("end-to-end encrypted files cannot be added to a collection")

// CollectionParameters holds the settings for a new collection
type CollectionParameters struct {
	Name               string
	FileIds            []string
	ExpiryDays         int
	AllowedDownloads   int
	UnlimitedTime      bool
	UnlimitedDownloads bool
	Password           string
}

// CreateCollection groups the given files behind a single public link.
//
// Files keep their own standalone links and their own expiry - the collection's
// limits govern access through the collection link only.
func CreateCollection(params CollectionParameters, user models.User) (models.Collection, error) {
	validIds := make([]string, 0, len(params.FileIds))
	seen := make(map[string]bool)
	for _, id := range params.FileIds {
		if seen[id] {
			continue
		}
		file, ok := GetFile(id)
		if !ok {
			continue
		}
		if file.Encryption.IsEndToEndEncrypted {
			return models.Collection{}, ErrorCollectionE2E
		}
		seen[id] = true
		validIds = append(validIds, file.Id)
	}
	if len(validIds) == 0 {
		return models.Collection{}, ErrorCollectionNoFiles
	}

	name := params.Name
	if name == "" {
		name = "Shared files"
	}

	collection := models.Collection{
		Id:                 createNewId(),
		Name:               name,
		UserId:             user.Id,
		CreationDate:       time.Now().Unix(),
		ExpireAt:           time.Now().Add(time.Duration(params.ExpiryDays) * time.Hour * 24).Unix(),
		UnlimitedTime:      params.UnlimitedTime,
		DownloadsRemaining: params.AllowedDownloads,
		UnlimitedDownloads: params.UnlimitedDownloads,
		DownloadCount:      0,
		PasswordHash:       "",
		FileIds:            validIds,
	}
	if params.Password != "" {
		collection.PasswordHash = configuration.HashPassword(params.Password, false, "")
	}
	database.SaveCollection(collection)
	return collection, nil
}

// GetCollection returns the collection and true if it exists and has not
// expired. Members that no longer exist are dropped from the result.
func GetCollection(id string) (models.Collection, bool) {
	collection, ok := database.GetCollection(id)
	if !ok {
		return models.Collection{}, false
	}
	if collection.IsExpired(time.Now().Unix()) {
		return models.Collection{}, false
	}
	collection.Populate(database.GetAllMetadata())
	return collection, true
}

// GetCollectionFile returns a file if it is a member of the collection.
//
// Going through the collection is what authorises the download, so the file's
// own expiry is deliberately not re-checked here - only membership and the
// collection's own limits matter.
func GetCollectionFile(collection models.Collection, fileId string) (models.File, bool) {
	for _, file := range collection.Files {
		if file.Id == fileId {
			return file, true
		}
	}
	return models.File{}, false
}

// DecreaseCollectionDownloadCount records one download against the collection.
// Member files' own counters are left alone: one share, one budget.
func DecreaseCollectionDownloadCount(collection models.Collection) {
	collection.DownloadCount = collection.DownloadCount + 1
	if !collection.UnlimitedDownloads {
		collection.DownloadsRemaining = collection.DownloadsRemaining - 1
	}
	database.SaveCollection(collection)
}

// IsCorrectCollectionPassword returns true if the supplied password matches
func IsCorrectCollectionPassword(collection models.Collection, password string) bool {
	if !collection.IsPasswordProtected() {
		return true
	}
	isValid, _ := configuration.VerifyPassword(password, collection.PasswordHash,
		configuration.Get().Authentication.SaltFiles)
	return isValid
}

// cleanUpCollections removes expired collections and prunes members that no
// longer exist. A collection that has lost every member is deleted.
func cleanUpCollections(timeNow int64) {
	allFiles := database.GetAllMetadata()
	for _, collection := range database.GetAllCollections() {
		if collection.IsExpired(timeNow) {
			database.DeleteCollection(collection.Id)
			continue
		}
		remaining := make([]string, 0, len(collection.FileIds))
		for _, id := range collection.FileIds {
			if _, ok := allFiles[id]; ok {
				remaining = append(remaining, id)
			}
		}
		if len(remaining) == 0 {
			database.DeleteCollection(collection.Id)
			continue
		}
		if len(remaining) != len(collection.FileIds) {
			collection.FileIds = remaining
			database.SaveCollection(collection)
		}
	}
}
