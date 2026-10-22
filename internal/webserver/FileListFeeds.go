package webserver

// The pages' list sources and their JSON feeds (Part 3, T1). Each page
// builds its whole list with its …Source function and renders the first
// batch (fileBrowser.WithList); its feed builds the same source behind the
// page's own access check and answers the later batches (writeList). Feeds
// are GET only and never write: no Downloads row, no CollectionSeen.

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

var (
	errListNotFound = errors.New("not found")
	errListSource   = errors.New("unknown list")
)

// getOnly is false, with a 405 written, unless the request is a GET
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", http.MethodGet)
	writeJsonError(w, http.StatusMethodNotAllowed, errMethodNotAllowed)
	return false
}

// ---------------------------------------------------------------- sources

// filesTabMeta is what the Files tab needs besides its list
type filesTabMeta struct {
	InGroups   bool // without PERM_LIST, in at least one live group
	OnlyPerson bool // the per-person view, /files?user=<id>
	Person     models.User
}

// filesTabSource is the Files tab's list: every listed file the user may
// list, and, without PERM_LIST, the other people's files in their groups.
// The My files / Everything switch applies wherever the page shows it.
func filesTabSource(r *http.Request, user models.User) (listSource, filesTabMeta) {
	config := configuration.Get()
	seeEveryone := user.HasPermissionListOtherUploads()
	meta := filesTabMeta{}
	meta.Person, meta.OnlyPerson = personFilter(r, user)
	names := map[int]string{}
	for _, u := range database.GetAllUsers() {
		names[u.Id] = u.Name
	}
	now := time.Now().Unix()
	items := make([]browserItem, 0)
	for _, f := range database.GetAllMetadata() {
		if f.UserId != user.Id && !seeEveryone {
			continue
		}
		if meta.OnlyPerson && f.UserId != meta.Person.Id {
			continue
		}
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		if !isListedFile(out, now) {
			continue
		}
		items = append(items, browserItemFromFile(r, f, user, ctxFiles, "", names))
	}
	// Without PERM_LIST, Everything is their own files plus those in their
	// groups (spec, "Files tab")
	if !seeEveryone && !meta.OnlyPerson && database.SupportsGroups() {
		meta.InGroups = len(storage.GroupsOfUser(user.Id)) > 0
		items = append(items, filesTabGroupItems(r, user)...)
	}
	source := listSource{Items: items, DefaultSort: "uploaded", DefaultDesc: true, ViewerId: user.Id,
		HasScope: (seeEveryone || meta.InGroups) && !meta.OnlyPerson}
	if source.HasScope {
		source.DefaultScope = "mine"
	}
	return source, meta
}

// filesListUrl is the Files tab's feed
func filesListUrl(meta filesTabMeta) string {
	if meta.OnlyPerson {
		return "./files/list?src=files&user=" + strconv.Itoa(meta.Person.Id) + "&"
	}
	return "./files/list?src=files&"
}

// fileRequestSource is one request's uploads on the File requests page
func fileRequestSource(r *http.Request, user models.User, request models.FileRequest) listSource {
	names := map[int]string{}
	for _, u := range database.GetAllUsers() {
		names[u.Id] = u.Name
	}
	items := make([]browserItem, 0, len(request.Files))
	for _, f := range request.Files {
		items = append(items, browserItemFromFile(r, f, user, ctxFileRequest, "", names))
	}
	return listSource{Items: items, DefaultSort: "uploaded", DefaultDesc: true, ViewerId: user.Id}
}

// collectionAdminSource is a collection's management list, for its owner or
// an admin who may list uploads (collectionListManager). Names are display
// names ("anna"), like the page's byline.
func collectionAdminSource(r *http.Request, manager models.User, c models.Collection) listSource {
	users := database.GetAllUsers()
	names := make(map[int]string, len(users))
	for _, u := range users {
		names[u.Id] = displayName(u)
	}
	items := make([]browserItem, 0, len(c.Files))
	for _, f := range c.Files {
		items = append(items, browserItemFromFile(r, f, manager, ctxCollectionAdmin, "", names))
	}
	return listSource{Items: items, DefaultSort: "uploaded", DefaultDesc: true, ViewerId: manager.Id}
}

// collectionPublicSource is a collection's list for visitors. Visitors are
// not users: the zero User gives no uploader, ownership or permissions, and
// no member's management data is sent (the page never renders it).
func collectionPublicSource(r *http.Request, c models.Collection) listSource {
	items := make([]browserItem, 0, len(c.Files))
	for _, f := range c.Files {
		item := browserItemFromFile(r, f, models.User{}, ctxCollectionPublic, c.Id, nil)
		item.blankManagement()
		item.File.UploaderId = 0
		items = append(items, item)
	}
	return listSource{Items: items, DefaultSort: "uploaded", DefaultDesc: true}
}

// groupSource is a group page's board in shooting order (D13), with the
// group's live files in the same order and the names of its people
func groupSource(r *http.Request, g models.Group, user models.User) (listSource, []models.File, map[int]string) {
	files := storage.GroupFiles(g)
	names := memberNamesOf(g, files)
	items, ordered := groupBoard(r, g, files, user, names)
	return listSource{Items: items, DefaultSort: "taken", DefaultDesc: false, ViewerId: user.Id, GroupTools: true},
		ordered, names
}

// collectionListShown reports whether /c shows a collection's list at all:
// not once its link was turned into a group that has gone, or that
// requires joining (the page then sends everyone elsewhere)
func collectionListShown(c models.Collection) bool {
	if len(c.Files) == 0 {
		return false
	}
	link, linked := storage.CollectionLinkOf(c.Id)
	return !linked || (link.Live && link.Link.LinkMode != models.CollectionLinkRequire)
}

// ---------------------------------------------------------------- feeds

// Handling of /files/list
// The later batches of the Files tab (src=files, with user= on the
// per-person view), of one file request (src=request&id=) and of a
// collection's management list (src=collection&id=), each behind its page's
// own check
func fileListFeed(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	id := r.URL.Query().Get("id")
	switch r.URL.Query().Get("src") {
	case "files":
		source, _ := filesTabSource(r, user)
		writeList(w, r, source)
	case "request":
		if !user.HasPermissionCreateFileRequests() {
			writeJsonError(w, http.StatusNotFound, errListNotFound)
			return
		}
		admin := (&AdminView{}).convertGlobalConfig(ViewFileRequests, user)
		for _, request := range admin.FileRequests {
			if request.Id == id {
				writeList(w, r, fileRequestSource(r, admin.ActiveUser, request))
				return
			}
		}
		writeJsonError(w, http.StatusNotFound, errListNotFound)
	case "collection":
		c, ok := storage.GetCollection(id)
		if !ok || !collectionListShown(c) {
			writeJsonError(w, http.StatusNotFound, errListNotFound)
			return
		}
		manager, manages := collectionListManager(r, c)
		if !manages {
			writeJsonError(w, http.StatusNotFound, errListNotFound)
			return
		}
		writeList(w, r, collectionAdminSource(r, manager, c))
	default:
		writeJsonError(w, http.StatusBadRequest, errListSource)
	}
}

// Handling of /cl
// The later batches of a collection page for visitors: the password gate of
// /c, /cd and /cz, and closed to anyone its group link shuts out. Records
// nothing: looking is not downloading, and not "Shared with me" either.
func collectionListFeed(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	c, ok := storage.GetCollection(r.URL.Query().Get("id"))
	if !ok || !collectionListShown(c) || !collectionUnlocked(r, c) || collectionClosed(r, c) {
		writeJsonError(w, http.StatusNotFound, errListNotFound)
		return
	}
	writeList(w, r, collectionPublicSource(r, c))
}

// Handling of /gl
// The later batches of a group page, for whoever can see the group
func groupListFeed(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	g, ok := visibleGroup(w, r.URL.Query().Get("id"), user)
	if !ok {
		return
	}
	source, _, _ := groupSource(r, g, user)
	writeList(w, r, source)
}

// replaceCandidate is one file "Replace content" may take its content from
type replaceCandidate struct {
	Id   string
	Name string
}

// Handling of /files/replacecandidates
// Every listed file the user may take content from for "Replace content":
// their own, or anyone's with PERM_LIST; never an end-to-end encrypted file
// or a request upload. Sorted by name.
func replaceCandidates(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	config := configuration.Get()
	seeEveryone := user.HasPermissionListOtherUploads()
	now := time.Now().Unix()
	result := make([]replaceCandidate, 0)
	for _, f := range database.GetAllMetadata() {
		if (f.UserId != user.Id && !seeEveryone) || f.Encryption.IsEndToEndEncrypted || f.IsFileRequest() {
			continue
		}
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		if !isListedFile(out, now) {
			continue
		}
		result = append(result, replaceCandidate{Id: f.Id, Name: f.Name})
	}
	slices.SortFunc(result, func(a, b replaceCandidate) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.Id, b.Id))
	})
	writeJson(w, result)
}

// listUrl builds a feed URL with one id parameter and the trailing "&"
func listUrl(base, id string) string {
	return base + url.QueryEscape(id) + "&"
}
