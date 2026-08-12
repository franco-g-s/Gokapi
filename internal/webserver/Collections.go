package webserver

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/errorHandling"
)

// CollectionView is passed to the public collection template. The header
// template only evaluates the admin fields when IsAdminView is true, so the
// public-facing subset below is sufficient.
type CollectionView struct {
	Name           string
	Size           string
	Id             string
	PublicName     string
	BaseUrl        string
	IsAdminView    bool
	IsDownloadView bool
	IsPasswordView bool
	IsFailedLogin  bool
	UsesHttps      bool
	CustomContent  customStatic
	Collection     models.Collection
	TotalSize      string
	FileCount      int
	ExpiryString   string
	ZipMaxBytes    int64
}

// zipMaxBytes is the size ceiling for the browser-side "Download all" button.
// Above this the files are downloaded individually, because zipping in the
// browser holds every byte in memory.
const zipMaxBytes int64 = 2 * 1024 * 1024 * 1024

// showCollection serves the public page for a collection. No login required -
// this is the link that gets shared.
func showCollection(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	collection, ok := storage.GetCollection(r.PathValue("id"))
	if !ok {
		redirectOnIncorrectId(w, r, "error")
		return
	}
	if len(collection.Files) == 0 {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}

	config := configuration.Get()
	view := CollectionView{
		Name:           collection.Name,
		Id:             collection.Id,
		PublicName:     config.PublicName,
		BaseUrl:        config.ServerUrl,
		IsAdminView:    false,
		IsDownloadView: true,
		UsesHttps:      configuration.UsesHttps(),
		CustomContent:  customStaticInfo,
		Collection:     collection,
		FileCount:      len(collection.Files),
		TotalSize:      helper.ByteCountSI(collection.TotalSizeBytes),
		ExpiryString:   collection.ExpireAtString(),
		ZipMaxBytes:    zipMaxBytes,
	}
	err := templateFolder.ExecuteTemplate(w, "collection", view)
	helper.CheckIgnoreTimeout(err)
}

// downloadCollectionFile serves one member of a collection.
//
// Membership plus the collection's own limits are what authorise this, so the
// member file's individual expiry is not re-checked - otherwise a file whose
// own link had lapsed would vanish from an otherwise valid share.
func downloadCollectionFile(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	collection, ok := storage.GetCollection(r.PathValue("id"))
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	fileId := r.URL.Query().Get("file")
	file, ok := storage.GetCollectionFile(collection, fileId)
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}

	storage.DecreaseCollectionDownloadCount(collection)
	logging.LogDownload(file, r, configuration.Get().SaveIp)

	forceDecryption := file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted
	// increaseCounter is false: the collection owns the budget, not the file.
	storage.ServeFile(file, w, r, true, false, forceDecryption, false)
}

// collectionCreateRequest is the JSON body posted by the admin page
type collectionCreateRequest struct {
	Name               string   `json:"name"`
	FileIds            []string `json:"fileIds"`
	ExpiryDays         int      `json:"expiryDays"`
	AllowedDownloads   int      `json:"allowedDownloads"`
	UnlimitedTime      bool     `json:"unlimitedTime"`
	UnlimitedDownloads bool     `json:"unlimitedDownloads"`
}

// createCollection is called by the admin page to bundle selected files.
//
// Authenticated by the session cookie rather than an API token. That is
// CSRF-safe because the session cookie is SameSite=Lax, so a cross-site POST
// never carries it.
func createCollection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"Not authenticated"}`)
		return
	}
	var request collectionCreateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"Invalid request body"}`)
		return
	}

	// Only bundle files the user may actually see
	permitted := make([]string, 0, len(request.FileIds))
	for _, id := range request.FileIds {
		file, ok := storage.GetFile(id)
		if !ok {
			continue
		}
		if file.UserId != user.Id && !user.HasPermission(models.UserPermListOtherUploads) {
			continue
		}
		permitted = append(permitted, id)
	}

	collection, err := storage.CreateCollection(storage.CollectionParameters{
		Name:               request.Name,
		FileIds:            permitted,
		ExpiryDays:         request.ExpiryDays,
		AllowedDownloads:   request.AllowedDownloads,
		UnlimitedTime:      request.UnlimitedTime,
		UnlimitedDownloads: request.UnlimitedDownloads,
	}, user)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Result": "error", "ErrorMessage": err.Error()})
		return
	}

	logging.LogCollectionCreate(collection, user)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"Result": "OK",
		"Id":     collection.Id,
		"Url":    configuration.Get().ServerUrl + "c/" + collection.Id,
		"Files":  len(collection.FileIds),
	})
}

// deleteCollection removes a collection. The files themselves are untouched.
func deleteCollection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"Not authenticated"}`)
		return
	}
	id := r.URL.Query().Get("id")
	collection, ok := database.GetCollection(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"Collection not found"}`)
		return
	}
	if collection.UserId != user.Id && !user.HasPermission(models.UserPermDeleteOtherUploads) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"No permission"}`)
		return
	}
	database.DeleteCollection(id)
	_, _ = io.WriteString(w, `{"Result":"OK"}`)
}
