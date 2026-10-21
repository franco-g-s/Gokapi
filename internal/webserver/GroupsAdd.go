package webserver

// Adding existing files to a group, and turning a collection into one (Part 2, G8)

import (
	"errors"
	"net/http"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type addDuplicate struct {
	FileId   string
	Name     string
	Uploader string
}

type addRefusal struct {
	FileId string
	Name   string
	Reason string
}

// addToGroup answers POST /groups/add {id, fileIds}: puts the caller's own
// files (anyone's, for the admin) into a group they can see
func addToGroup(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	var request struct {
		Id      string   `json:"id"`
		FileIds []string `json:"fileIds"`
	}
	if err = readJsonBody(r, &request); err != nil {
		writeJsonError(w, http.StatusBadRequest, errors.New("invalid request"))
		return
	}
	if !database.SupportsGroups() {
		writeJsonError(w, http.StatusNotImplemented, storage.ErrGroupsUnsupported)
		return
	}
	// A group the caller cannot see is reported as missing, not forbidden
	group, ok := storage.GetGroup(request.Id)
	if !ok || !storage.CanSeeGroup(group, user) {
		writeJsonError(w, http.StatusNotFound, storage.ErrGroupNotFound)
		return
	}
	if len(request.FileIds) == 0 {
		writeJsonError(w, http.StatusBadRequest, errors.New("no files given"))
		return
	}
	result, err := storage.AddFilesToGroup(group, request.FileIds, user)
	if err != nil {
		writeJsonError(w, groupErrorStatus(err), err)
		return
	}
	nameOf := func(fileId string) string {
		if f, found := database.GetMetaDataById(fileId); found {
			return f.Name
		}
		return ""
	}
	duplicates := make([]addDuplicate, 0, len(result.Duplicates))
	for _, d := range result.Duplicates {
		uploader := ""
		if u, found := database.GetUser(d.UploaderId); found {
			uploader = displayName(u)
		}
		duplicates = append(duplicates, addDuplicate{FileId: d.FileId, Name: nameOf(d.FileId), Uploader: uploader})
	}
	refused := make([]addRefusal, 0, len(result.Refused))
	for _, f := range result.Refused {
		refused = append(refused, addRefusal{FileId: f.FileId, Name: nameOf(f.FileId), Reason: f.Reason})
	}
	writeJson(w, struct {
		Result     string
		Added      int
		Duplicates []addDuplicate
		Refused    []addRefusal
	}{"OK", len(result.Added), duplicates, refused})
}

// groupFromCollection answers POST /groups/fromcollection {collectionId,
// days, addViewers, linkMode}: a new group named after the collection, with
// its files and, if asked, the people who opened its link while signed in.
// The collection leaves the Collections list; its link lives on in linkMode
// ("invite" when empty, or "require"). Part 2, G8 and R4.
func groupFromCollection(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	var request struct {
		CollectionId string `json:"collectionId"`
		Days         int    `json:"days"`
		AddViewers   bool   `json:"addViewers"`
		LinkMode     string `json:"linkMode"`
	}
	if err = readJsonBody(r, &request); err != nil {
		writeJsonError(w, http.StatusBadRequest, errors.New("invalid request"))
		return
	}
	group, result, err := storage.GroupFromCollection(request.CollectionId, request.Days,
		storage.FromCollectionOptions{AddViewers: request.AddViewers, LinkMode: request.LinkMode}, user)
	if err != nil {
		status := groupErrorStatus(err)
		if errors.Is(err, storage.ErrorCollectionNoFiles) {
			status = http.StatusNotFound
		}
		writeJsonError(w, status, err)
		return
	}
	writeJson(w, struct {
		Result       string
		Id           string
		Url          string
		Added        int
		MembersAdded int
	}{"OK", group.Id, "./g?id=" + group.Id, len(result.Added), result.MembersAdded})
}
