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

// CollectionParameters holds the settings for a new collection.
//
// Deliberately no expiry or download limit: a collection is a view over its
// files and inherits their lifetimes. See models.Collection.
type CollectionParameters struct {
	Name     string
	FileIds  []string
	Password string
}

// CreateCollection groups the given files behind a single public link
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
		Id:           createNewId(),
		Name:         name,
		UserId:       user.Id,
		CreationDate: time.Now().Unix(),
		FileIds:      validIds,
	}
	if params.Password != "" {
		collection.PasswordHash = configuration.HashPassword(params.Password, false, "")
	}
	database.SaveCollection(collection)
	return collection, nil
}

// resolveMembers fills in Files with the members that are still downloadable,
// dropping any that have expired or run out of downloads.
func resolveMembers(collection *models.Collection) {
	allFiles := database.GetAllMetadata()
	timeNow := time.Now().Unix()
	collection.Files = make([]models.File, 0, len(collection.FileIds))
	collection.TotalSizeBytes = 0
	for _, id := range collection.FileIds {
		file, ok := allFiles[id]
		if !ok || file.IsPendingForDeletion() || IsExpiredFile(file, timeNow) {
			continue
		}
		collection.Files = append(collection.Files, file)
		collection.TotalSizeBytes = collection.TotalSizeBytes + file.SizeBytes
	}
}

// GetCollection returns the collection and true if it still has at least one
// downloadable member. A collection has no clock of its own: it is alive for
// exactly as long as something in it is.
func GetCollection(id string) (models.Collection, bool) {
	collection, ok := database.GetCollection(id)
	if !ok {
		return models.Collection{}, false
	}
	resolveMembers(&collection)
	if len(collection.Files) == 0 {
		return models.Collection{}, false
	}
	return collection, true
}

// GetAllCollectionsResolved returns every collection with its live members
// filled in, for the management list.
func GetAllCollectionsResolved() []models.Collection {
	all := database.GetAllCollections()
	for i := range all {
		resolveMembers(&all[i])
	}
	return all
}

// GetCollectionFile returns a file if it is a currently downloadable member
func GetCollectionFile(collection models.Collection, fileId string) (models.File, bool) {
	for _, file := range collection.Files {
		if file.Id == fileId {
			return file, true
		}
	}
	return models.File{}, false
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

// cleanUpCollections drops members whose metadata is gone and deletes any
// collection left with nothing downloadable in it.
func cleanUpCollections(timeNow int64) {
	allFiles := database.GetAllMetadata()
	for _, collection := range database.GetAllCollections() {
		remaining := make([]string, 0, len(collection.FileIds))
		alive := 0
		for _, id := range collection.FileIds {
			file, ok := allFiles[id]
			if !ok {
				continue // metadata gone for good, drop the reference
			}
			remaining = append(remaining, id)
			if !file.IsPendingForDeletion() && !IsExpiredFile(file, timeNow) {
				alive++
			}
		}
		if alive == 0 {
			database.DeleteCollection(collection.Id)
			continue
		}
		if len(remaining) != len(collection.FileIds) {
			collection.FileIds = remaining
			database.SaveCollection(collection)
		}
	}
}
