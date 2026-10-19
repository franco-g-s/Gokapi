package webserver

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/access"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
	"github.com/forceu/gokapi/internal/webserver/errorHandling"
	"github.com/forceu/gokapi/internal/webserver/ratelimiter"
)

// mediaButtons feeds the shared download-button partial
type mediaButtons struct {
	Count  int
	ZipUrl string
}

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
	NeverExpires   bool
	Buttons        mediaButtons
}

// showCollection serves the public page for a collection. No login required -
// this is the link that gets shared.
func showCollection(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	collection, ok := storage.GetCollection(r.URL.Query().Get("id"))
	if !ok {
		redirectOnIncorrectId(w, r, "error")
		return
	}
	if len(collection.Files) == 0 {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}

	// Password gate. Same token/cookie scheme as password-protected files,
	// keyed on the collection id.
	if collection.IsPasswordProtected() && !isValidCollectionPwCookie(r, collection) {
		_ = r.ParseForm()
		entered := r.PostForm.Get("password")
		view := CollectionView{
			Name: collection.Name, PublicName: configuration.Get().PublicName,
			BaseUrl: configuration.Get().ServerUrl, IsDownloadView: true,
			IsPasswordView: true, CustomContent: customStaticInfo,
			Collection: models.Collection{Id: collection.Id, Name: collection.Name},
		}
		if entered == "" {
			err := templateFolder.ExecuteTemplate(w, "collection", view)
			helper.CheckIgnoreTimeout(err)
			return
		}
		ratelimiter.WaitOnDownloadPassword(logging.GetIpAddress(r))
		if !storage.IsCorrectCollectionPassword(collection, entered) {
			view.IsFailedLogin = true
			err := templateFolder.ExecuteTemplate(w, "collection", view)
			helper.CheckIgnoreTimeout(err)
			return
		}
		writeCollectionPwCookie(w, collection)
		// redirect so a refresh does not resubmit the password
		redirect(w, r, "c?id="+collection.Id)
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
		Buttons:        mediaButtons{Count: len(collection.Files), ZipUrl: "./cz?id=" + collection.Id},
	}
	// Expiry is derived from the longest-lived member, not stored.
	latest, never := collection.LatestExpiry()
	view.NeverExpires = never
	if !never {
		view.ExpiryString = time.Unix(latest, 0).Format("2006-01-02 15:04")
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
	collection, ok := storage.GetCollection(r.URL.Query().Get("id"))
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	if collection.IsPasswordProtected() && !isValidCollectionPwCookie(r, collection) {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	fileId := r.URL.Query().Get("file")
	file, ok := storage.GetCollectionFile(collection, fileId)
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}

	forceDecryption := file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted
	// increaseCounter is true: a download through a collection is exactly a
	// download of that file, so it consumes the file's own budget. ServeFile
	// logs it and re-checks expiry itself.
	storage.ServeFile(file, w, r, true, true, forceDecryption, true)
}

// collectionCreateRequest is the JSON body posted by the admin page
type collectionCreateRequest struct {
	Name     string   `json:"name"`
	FileIds  []string `json:"fileIds"`
	Password string   `json:"password"`
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
		Name:     request.Name,
		FileIds:  permitted,
		Password: request.Password,
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
		"Url":    configuration.Get().ServerUrl + "c?id=" + collection.Id,
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

// collectionMember is one file inside a collection, for the management page
type collectionMember struct {
	Id   string
	Name string
	Size string
}

// collectionInfo is one row in the admin collections list
type collectionInfo struct {
	Files        []collectionMember
	Id           string
	Name         string
	Url          string
	FileCount    int
	TotalSize    string
	Created      string
	Expiry       string
	NeverExpires bool
	OwnerName    string
	IsOwn        bool
	HasPassword  bool
}

// showCollectionsAdmin renders the management list of collections
func showCollectionsAdmin(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		redirect(w, r, "login")
		return
	}
	config := configuration.Get()
	users := database.GetAllUsers()
	userNames := make(map[int]string, len(users))
	for _, u := range users {
		userNames[u.Id] = u.Name
	}

	seeAll := user.HasPermission(models.UserPermListOtherUploads)
	rows := make([]collectionInfo, 0)
	for _, collection := range storage.GetAllCollectionsResolved() {
		if collection.UserId != user.Id && !seeAll {
			continue
		}
		latest, never := collection.LatestExpiry()
		expiry := ""
		if !never {
			expiry = time.Unix(latest, 0).Format("2006-01-02 15:04")
		}
		members := make([]collectionMember, 0, len(collection.Files))
		for _, f := range collection.Files {
			members = append(members, collectionMember{Id: f.Id, Name: f.Name, Size: f.Size})
		}
		rows = append(rows, collectionInfo{
			Files:        members,
			Id:           collection.Id,
			Name:         collection.Name,
			Url:          config.ServerUrl + "c?id=" + collection.Id,
			FileCount:    len(collection.Files),
			TotalSize:    helper.ByteCountSI(collection.TotalSizeBytes),
			Created:      time.Unix(collection.CreationDate, 0).Format("2006-01-02 15:04"),
			Expiry:       expiry,
			NeverExpires: never,
			OwnerName:    userNames[collection.UserId],
			IsOwn:        collection.UserId == user.Id,
			HasPassword:  collection.IsPasswordProtected(),
		})
	}

	view := (&AdminView{}).convertGlobalConfig(ViewCollections, user)
	view.Collections = rows
	err = templateFolder.ExecuteTemplate(w, "collections_admin", view)
	helper.CheckIgnoreTimeout(err)
}

// collectionUnlockLifetime is how long entering a collection's password keeps
// it open. Long enough that a board of many previews keeps loading.
const collectionUnlockLifetime = 12 * time.Hour

// writeCollectionPwCookie stores a token proving the password was entered for
// this collection
func writeCollectionPwCookie(w http.ResponseWriter, collection models.Collection) {
	http.SetCookie(w, &http.Cookie{
		Name:     "pc" + collection.Id,
		Value:    downloadPasswordToken.GenerateWithTtl(collection.Id, collectionUnlockLifetime),
		Expires:  time.Now().Add(collectionUnlockLifetime),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// isValidCollectionPwCookie returns true if the request carries a valid token
func isValidCollectionPwCookie(r *http.Request, collection models.Collection) bool {
	return access.ValidCollectionPwCookie(r, collection.Id)
}

// collectionModifyRequest renames a collection and/or replaces its members
type collectionModifyRequest struct {
	Id      string   `json:"id"`
	Name    string   `json:"name"`
	FileIds []string `json:"fileIds"`
	// AddFileIds appends to the existing members, so a caller adding files does
	// not need to know (or round-trip) the current member list.
	AddFileIds []string `json:"addFileIds"`
	// Password: nil leaves it untouched, "" removes it, anything else sets it.
	// Stored hashed, so an existing password can be replaced but never read back.
	Password *string `json:"password"`
}

// canEditCollection returns the collection if the user is allowed to change it
func canEditCollection(r *http.Request, id string) (models.Collection, models.User, bool) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		return models.Collection{}, models.User{}, false
	}
	collection, ok := database.GetCollection(id)
	if !ok {
		return models.Collection{}, user, false
	}
	if collection.UserId != user.Id && !user.HasPermission(models.UserPermEditOtherUploads) {
		return models.Collection{}, user, false
	}
	return collection, user, true
}

// modifyCollection renames a collection and/or replaces its member list.
// A collection with no members left would be unreachable, so that is refused.
func modifyCollection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	var request collectionModifyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"Invalid request body"}`)
		return
	}
	collection, user, ok := canEditCollection(r, request.Id)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"Not found or no permission"}`)
		return
	}

	if request.Name != "" {
		collection.Name = request.Name
	}
	if request.Password != nil {
		if *request.Password == "" {
			collection.PasswordHash = ""
		} else {
			collection.PasswordHash = configuration.HashPassword(*request.Password, false, "")
		}
	}
	if len(request.AddFileIds) > 0 {
		request.FileIds = append(append([]string{}, collection.FileIds...), request.AddFileIds...)
	}
	if request.FileIds != nil {
		permitted := make([]string, 0, len(request.FileIds))
		seen := make(map[string]bool)
		for _, id := range request.FileIds {
			if seen[id] {
				continue
			}
			file, exists := storage.GetFile(id)
			if !exists {
				continue
			}
			if file.Encryption.IsEndToEndEncrypted {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"Result": "error", "ErrorMessage": storage.ErrorCollectionE2E.Error()})
				return
			}
			if file.UserId != user.Id && !user.HasPermission(models.UserPermListOtherUploads) {
				continue
			}
			seen[id] = true
			permitted = append(permitted, id)
		}
		if len(permitted) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"A collection needs at least one file"}`)
			return
		}
		collection.FileIds = permitted
	}

	database.SaveCollection(collection)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"Result": "OK", "Id": collection.Id, "Files": len(collection.FileIds)})
}

// listCollections returns the caller's collections as JSON, for the
// "add to existing collection" picker on the upload page.
func listCollections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `[]`)
		return
	}
	type entry struct {
		Id    string `json:"id"`
		Name  string `json:"name"`
		Files int    `json:"files"`
	}
	result := make([]entry, 0)
	for _, collection := range storage.GetAllCollectionsResolved() {
		if collection.UserId != user.Id && !user.HasPermission(models.UserPermListOtherUploads) {
			continue
		}
		result = append(result, entry{collection.Id, collection.Name, len(collection.Files)})
	}
	_ = json.NewEncoder(w).Encode(result)
}
