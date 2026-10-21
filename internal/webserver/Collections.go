package webserver

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
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
	// OwnerName is who made the collection, by display name ("by anna");
	// empty when that user no longer exists
	OwnerName string
	// CanManage shows the owner bar and loads collection_owner.js: the
	// visitor is signed in as the owner or an admin (managesCollection)
	CanManage   bool
	HasPassword bool
	// CanManageList gives the file list its management actions and loads the
	// admin bundle: the collection's owner, or an admin who may also list
	// other people's uploads (collectionListManager)
	CanManageList bool
	// CanTurnIntoGroup offers "Turn into a group" in the owner bar: not
	// once the collection is a group (R4)
	CanTurnIntoGroup bool
	// GroupBanner is set when the collection was turned into a group and
	// its link invites to join (R4)
	GroupBanner *collectionGroupBanner
	// Browser is the shared file list. For visitors it is collection-public:
	// no management actions, downloads through /cd, previews scoped with &c=.
	// For the owner and an admin who may list uploads (CanManageList) it is collection-admin, the
	// management list of the Files tab with "Remove from collection".
	Browser fileBrowser
	// ServerUrl feeds the management dialogs (CanManageList only)
	ServerUrl string
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
	// Part 2, R4: a collection turned into a group
	link, linked, answered := collectionGroupGate(w, r, collection)
	if answered {
		return
	}
	manages := managesCollection(r, collection)
	manager, managesList := collectionListManager(r, collection)
	ownerName := collectionOwnerName(collection)

	// Password gate. Same token/cookie scheme as password-protected files,
	// keyed on the collection id. Its owner and an admin pass without it.
	if !collectionUnlocked(r, collection) {
		_ = r.ParseForm()
		entered := r.PostForm.Get("password")
		view := CollectionView{
			Name: collection.Name, PublicName: configuration.Get().PublicName,
			BaseUrl: configuration.Get().ServerUrl, IsDownloadView: true,
			IsPasswordView: true, CustomContent: customStaticInfo,
			Collection: models.Collection{Id: collection.Id, Name: collection.Name},
			OwnerName:  ownerName,
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
	var banner *collectionGroupBanner
	if linked {
		if banner, answered = collectionGroupJoin(w, r, link); answered {
			return
		}
	}

	// A signed-in visitor who is not the owner finds the collection under
	// "Shared with me" from now on. Anonymous visits and the password form
	// (which returned above) record nothing.
	if visitor, signedIn := authentication.SessionIdentity(r); signedIn && visitor.Id != collection.UserId {
		database.SaveCollectionSeen(models.CollectionSeen{
			UserId: visitor.Id, CollectionId: collection.Id, FirstOpened: time.Now().Unix()})
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
		OwnerName:      ownerName,
		CanManage:      manages,
		CanManageList:  managesList,
		HasPassword:    collection.IsPasswordProtected(),
		// R4: a collection becomes a group once
		CanTurnIntoGroup: manages && !linked,
		GroupBanner:      banner,
	}
	if managesList {
		// The owner and an admin who may list uploads get the management list: "Remove from
		// collection" and the file actions of the Files tab. The page loads
		// the admin bundle and the dialogs it needs (html_collection.tmpl).
		// Names are shown as display names ("anna"), like the byline above
		users := database.GetAllUsers()
		names := make(map[int]string, len(users))
		for _, u := range users {
			names[u.Id] = displayName(u)
		}
		items := make([]browserItem, 0, len(collection.Files))
		for _, f := range collection.Files {
			items = append(items, browserItemFromFile(r, f, manager, ctxCollectionAdmin, "", names))
		}
		shown := manager
		shown.Name = displayName(manager) // the list renders the active user's name
		view.Browser = newFileBrowser("fgs-collection", ctxCollectionAdmin, items, shown)
		view.Browser.WithCollection(collection)
		view.ServerUrl = config.ServerUrl
	} else {
		// Visitors are not users: the zero User gives no uploader, ownership
		// or permissions, and the context keeps every management action out
		items := make([]browserItem, 0, len(collection.Files))
		for _, f := range collection.Files {
			items = append(items, browserItemFromFile(r, f, models.User{}, ctxCollectionPublic, collection.Id, nil))
		}
		view.Browser = newFileBrowser("fgs-collection", ctxCollectionPublic, items, models.User{})
		view.Browser.CollectionId = collection.Id
		view.Browser.ZipUrl = "./cz?id=" + collection.Id + "&files="
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
// Membership (and the collection's password, if any) authorises the request;
// a collection has no expiry or download budget of its own. The member's own
// limits still apply: GetCollectionFile only resolves live members, and
// ServeFile re-checks expiry and counts the download against the file.
func downloadCollectionFile(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	collection, ok := storage.GetCollection(r.URL.Query().Get("id"))
	if !ok {
		errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeFileNotFound)
		return
	}
	if !collectionUnlocked(r, collection) || collectionClosed(r, collection) {
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
	storage.ServeFile(file, w, tagDownload(r, models.DownloadViaCollection), true, true, forceDecryption, true)
}

// collectionCreateRequest is the JSON body posted by the admin page
type collectionCreateRequest struct {
	Name     string   `json:"name"`
	FileIds  []string `json:"fileIds"`
	Password string   `json:"password"`
}

// createCollection is called by the admin page to bundle selected files.
//
// Authenticated by the session cookie rather than an API token. The session
// cookie is SameSite=Lax, so a cross-site POST never carries it; jsonOnly
// keeps out same-site forms as well.
func createCollection(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
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
// The id is in the query, but the request must still be a POST declaring a
// JSON body: a link or a form cannot delete anything.
func deleteCollection(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
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

// collectionInfo is one row in the admin collections list
type collectionInfo struct {
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

// collectionExpiry is the "Available until" text of a collection: the expiry
// of its longest-lived member, empty when one never expires (the caller says
// "Unlimited" then)
func collectionExpiry(collection *models.Collection) (text string, never bool) {
	latest, never := collection.LatestExpiry()
	if never {
		return "", true
	}
	return time.Unix(latest, 0).Format("2006-01-02 15:04"), false
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
		// R4: a collection turned into a group lives on as the group's link
		if _, linked := storage.CollectionLinkOf(collection.Id); linked {
			continue
		}
		expiry, never := collectionExpiry(&collection)
		rows = append(rows, collectionInfo{
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
	view.SharedWithMe = sharedWithMe(user, config.ServerUrl)
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

// collectionManager returns the visitor if they are signed in as the
// collection's owner or as an admin. Only a real session counts
// (SessionIdentity), so an anonymous visitor is never anyone.
func collectionManager(r *http.Request, collection models.Collection) (models.User, bool) {
	user, signedIn := authentication.SessionIdentity(r)
	if !signedIn {
		return models.User{}, false
	}
	if user.Id == collection.UserId || (user.IsAdmin() && user.Id != 0) {
		return user, true
	}
	return models.User{}, false
}

// collectionListManager is who gets the management list on /c: the owner, or
// an admin who also holds PERM_LIST. The list downloads through
// /files/download and zips through /files/zip, which serve only files the user
// may list, so an admin without PERM_LIST would get a list whose downloads
// all 404: they keep the public list (and the owner bar, see
// collectionManager).
func collectionListManager(r *http.Request, collection models.Collection) (models.User, bool) {
	user, ok := collectionManager(r, collection)
	if !ok {
		return models.User{}, false
	}
	if user.Id == collection.UserId || user.HasPermissionListOtherUploads() {
		return user, true
	}
	return models.User{}, false
}

// managesCollection reports whether the visitor is the collection's owner or
// an admin (collectionManager)
func managesCollection(r *http.Request, collection models.Collection) bool {
	_, ok := collectionManager(r, collection)
	return ok
}

// collectionUnlocked is the password gate of /c, /cd and /cz: no password,
// a valid unlock cookie, or the owner or an admin, who never have to type
// the password of a collection they manage. Setting one from the owner bar
// therefore cannot lock its owner out.
func collectionUnlocked(r *http.Request, collection models.Collection) bool {
	return !collection.IsPasswordProtected() || isValidCollectionPwCookie(r, collection) ||
		managesCollection(r, collection)
}

// collectionOwnerName is the "by …" of a collection page: the owner's display
// name, or empty when that user no longer exists
func collectionOwnerName(collection models.Collection) string {
	if owner, ok := database.GetUser(collection.UserId); ok {
		return displayName(owner)
	}
	return ""
}

// collectionModifyRequest renames a collection and/or replaces its members
type collectionModifyRequest struct {
	Id      string   `json:"id"`
	Name    string   `json:"name"`
	FileIds []string `json:"fileIds"`
	// AddFileIds appends to the existing members, so a caller adding files does
	// not need to know (or round-trip) the current member list.
	AddFileIds []string `json:"addFileIds"`
	// RemoveFileIds drops these members and keeps the rest, for the same
	// reason: a page showing a stale member list must not, by replacing the
	// list, silently remove files someone added since.
	RemoveFileIds []string `json:"removeFileIds"`
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
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
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
	if len(request.RemoveFileIds) > 0 {
		// Removal applies to the final list: after an add or a replacement
		// in the same request, otherwise to the stored members
		base := collection.FileIds
		if request.FileIds != nil {
			base = request.FileIds
		}
		kept := make([]string, 0, len(base))
		for _, id := range base {
			if !slices.Contains(request.RemoveFileIds, id) {
				kept = append(kept, id)
			}
		}
		if request.FileIds != nil {
			// The check below refuses a list with no live member
			request.FileIds = kept
		} else {
			// Stored members only: dropped files may still be listed, so
			// "at least one file" means at least one that is still alive,
			// or the collection would answer 404 while the request said OK.
			// Nothing else is re-validated, so a member this user may not
			// list stays untouched.
			live := false
			for _, id := range kept {
				if _, ok := storage.GetFile(id); ok {
					live = true
					break
				}
			}
			if !live {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"Result":"error","ErrorMessage":"A collection needs at least one file"}`)
				return
			}
			collection.FileIds = kept
		}
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
	// Unlock tokens are keyed by collection id and live for 12 h, so a set,
	// changed or removed password has to revoke them - otherwise anyone who
	// knew the old password keeps the collection open. Done after the save,
	// so the old password cannot mint a fresh token in between.
	if request.Password != nil {
		downloadPasswordToken.DeleteAllForFile(collection.Id)
	}
	response := map[string]any{"Result": "OK", "Id": collection.Id, "Files": len(collection.FileIds)}
	// What the management list shows for the row, so a page that changed the
	// members can refresh it in place. Left out when nothing live remains.
	if live, ok := storage.GetCollection(collection.Id); ok {
		expiry, never := collectionExpiry(&live)
		response["FileCount"] = len(live.Files)
		response["TotalSize"] = helper.ByteCountSI(live.TotalSizeBytes)
		response["Expiry"] = expiry
		response["NeverExpires"] = never
	}
	_ = json.NewEncoder(w).Encode(response)
}

// listCollections returns the caller's collections as JSON, for the
// "add to existing collection" picker on the upload page. ?mine=1 limits it
// to collections the caller owns.
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
		// What the management list shows for the row, so a page can refresh
		// it after files were deleted or added from elsewhere
		TotalSize    string `json:"totalSize"`
		Expiry       string `json:"expiry"`
		NeverExpires bool   `json:"neverExpires"`
	}
	// ?mine=1 (the Files tab's picker): only the caller's own collections,
	// even for a PERM_LIST admin, so a file is never added to someone else's
	mineOnly := r.URL.Query().Get("mine") == "1"
	result := make([]entry, 0)
	for _, collection := range storage.GetAllCollectionsResolved() {
		if collection.UserId != user.Id && (mineOnly || !user.HasPermission(models.UserPermListOtherUploads)) {
			continue
		}
		if _, linked := storage.CollectionLinkOf(collection.Id); linked {
			continue // R4: hidden, like on the Collections tab
		}
		expiry, never := collectionExpiry(&collection)
		result = append(result, entry{collection.Id, collection.Name, len(collection.Files),
			helper.ByteCountSI(collection.TotalSizeBytes), expiry, never})
	}
	_ = json.NewEncoder(w).Encode(result)
}
