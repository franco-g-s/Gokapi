package webserver

// The group page /g?id= and the routes behind it (Part 2, G10): the board in
// shooting order, its counted downloads, the live add of a finished upload
// and "Keep both" for near-copies. Every rule lives in storage/Groups.go.

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

const msgGroupNotFound = "This group does not exist or has closed."

// groupPagePerson is one member in the page header
type groupPagePerson struct {
	Id        int
	Name      string
	Initials  string
	Hue       int // of the initials circle, derived from the id (D9)
	IsCreator bool
	Removable bool // the viewer may remove them: creator or admin, never the creator
}

// groupExtendChoice is one radio of Extend; disabled when it would not be later (D18)
type groupExtendChoice struct {
	Days     int
	Label    string
	Disabled bool
}

// groupNearSide is one item of a near-copy pair, with what the full-screen
// compare needs when its tile is filtered out of the board
type groupNearSide struct {
	Id       string
	Name     string
	Thumb    string
	View     string
	Download string
	Uploader string
	Width    int
	Height   int
}

// groupNearCopy is one "Looks like Anna's photo from 14:32" entry: the newer
// item and the older one it looks like
type groupNearCopy struct {
	Newer     groupNearSide
	Older     groupNearSide
	Mine      bool   // the viewer uploaded the newer item: "Remove mine"; otherwise "Remove newer"
	MatchUser string // who uploaded the older item
	NewerUser string // who uploaded the newer item
	MatchAt   int64
	MatchTime string // HH:MM in the server's zone; the browser rewrites it in its own
}

// groupNearSideOf describes one item of a pair
func groupNearSideOf(item browserItem) groupNearSide {
	return groupNearSide{Id: item.File.Id, Name: item.File.Name, Thumb: item.Preview.Thumb,
		View: item.Preview.View, Download: item.DownloadUrl, Uploader: item.Uploader,
		Width: item.Preview.Width, Height: item.Preview.Height}
}

// groupStackSize is how many avatars the members button shows before "+N"
const groupStackSize = 5

// GroupView is the group page
type GroupView struct {
	AdminView
	Group         models.Group
	Closes        string
	InviteUrl     string
	Code          string
	People        []groupPagePerson
	Stack         []groupPagePerson // the first few People, for the members button
	MoreMembers   int               // how many People the stack leaves out
	IsMember      bool
	CanManage     bool
	CanLeave      bool
	ExtendChoices []groupExtendChoice
	NearCopies    []groupNearCopy
	Browser       fileBrowser
}

// avatarHue is the colour of a person's initials circle: hsl(hue, 45%, 45%)
func avatarHue(userId int) int {
	return (userId*47%360 + 360) % 360
}

// groupExtendChoices are Extend's four radios. A choice is disabled when it
// would not close later than now, as ExtendGroup would refuse it; every one
// is when the group never closes.
func groupExtendChoices(g models.Group, now int64) []groupExtendChoice {
	result := make([]groupExtendChoice, 0, len(storage.GroupExpiryDays))
	for _, choice := range groupExpiryChoices() {
		expires, err := storage.ExpiryFromChoice(choice.Days, now)
		disabled := err != nil || g.NeverExpires() || (expires != 0 && expires <= g.Expires)
		result = append(result, groupExtendChoice{Days: choice.Days, Label: choice.Label, Disabled: disabled})
	}
	return result
}

// shootingKey orders the board: the date taken, else the upload date (D13).
// media.js (shootingKey) holds the same rule for files added live.
func shootingKey(item browserItem) int64 {
	if item.TakenAt != 0 {
		return item.TakenAt
	}
	return item.File.UploadDate
}

// groupBrowserItem builds one board item for the viewer: the download ticks from
// the Downloads table, and who may take it out of the group (D4)
func groupBrowserItem(r *http.Request, g models.Group, f models.File, user models.User,
	names map[int]string, downloaded map[string]bool, manager bool) browserItem {
	item := browserItemFromFile(r, f, user, ctxGroup, g.Id, names)
	item.Downloaded = downloaded[f.Id]
	item.CanRemove = manager || (user.Id != 0 && f.UserId == user.Id)
	return item
}

// groupBoard returns the group's live files as board items and as files,
// both in shooting order, with the file id as the tie-break
func groupBoard(r *http.Request, g models.Group, files []models.File, user models.User, names map[int]string) ([]browserItem, []models.File) {
	downloaded := database.GetDownloadedFileIds(user.Id)
	manager := storage.CanManageGroup(g, user)
	byId := make(map[string]models.File, len(files))
	items := make([]browserItem, 0, len(files))
	for _, f := range files {
		byId[f.Id] = f
		items = append(items, groupBrowserItem(r, g, f, user, names, downloaded, manager))
	}
	sort.SliceStable(items, func(i, j int) bool {
		ki, kj := shootingKey(items[i]), shootingKey(items[j])
		if ki != kj {
			return ki < kj
		}
		return items[i].File.Id < items[j].File.Id
	})
	ordered := make([]models.File, 0, len(items))
	for _, item := range items {
		ordered = append(ordered, byId[item.File.Id])
	}
	return items, ordered
}

// groupFileById returns a live file of the group, as GroupFiles would list
// it, with one membership query and one metadata read rather than reading
// the whole group (M3: every /gd of a 1,000-item group did that)
func groupFileById(g models.Group, fileId string) (models.File, bool) {
	if fileId == "" || !slices.Contains(database.GetGroupIdsOfFile(fileId), g.Id) {
		return models.File{}, false
	}
	f, ok := database.GetMetaDataById(fileId)
	if !ok || f.IsPendingForDeletion() || storage.IsExpiredFile(f, time.Now().Unix()) {
		return models.File{}, false
	}
	return f, true
}

// Handling of /g
// The group page, for its members and the admin. To anyone else a group is
// unlisted, so it does not exist: 404.
func showGroup(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		redirect(w, r, "login")
		return
	}
	if !database.SupportsGroups() {
		showJoinMessage(w, user, http.StatusNotImplemented, capitalise(storage.ErrGroupsUnsupported.Error()))
		return
	}
	g, ok := storage.GetGroup(r.URL.Query().Get("id"))
	if !ok || !storage.CanSeeGroup(g, user) {
		showJoinMessage(w, user, http.StatusNotFound, msgGroupNotFound)
		return
	}
	now := time.Now()
	role, member := storage.GroupRole(g, user.Id)
	manager := storage.CanManageGroup(g, user)
	files := storage.GroupFiles(g)
	names := memberNamesOf(g, files)
	items, files := groupBoard(r, g, files, user, names)

	view := GroupView{
		AdminView: *(&AdminView{}).convertGlobalConfig(ViewGroups, user),
		Group:     g,
		Closes:    closesLabel(g, now),
		InviteUrl: storage.GroupInviteUrl(g),
		Code:      storage.GroupCode(g),
		IsMember:  member,
		CanManage: manager,
		CanLeave:  member && role != models.GroupRoleCreator && g.CreatorId != user.Id,
		People:    make([]groupPagePerson, 0),
	}
	if manager {
		view.ExtendChoices = groupExtendChoices(g, now.Unix())
	}

	// The members panel lists every member. The person filter lists everyone with
	// an item: members first, in member order, then people who have left
	// (their files stay, D8), in shooting order of their first item.
	hasItems := make(map[int]bool)
	for _, f := range files {
		hasItems[f.UserId] = true
	}
	people := make([]browserPerson, 0)
	listed := make(map[int]bool)
	for _, m := range database.GetGroupMembers(g.Id) {
		name, known := names[m.UserId]
		if !known {
			continue
		}
		creator := m.Role == models.GroupRoleCreator || m.UserId == g.CreatorId
		view.People = append(view.People, groupPagePerson{
			Id:        m.UserId,
			Name:      name,
			Initials:  initialsOf(name),
			Hue:       avatarHue(m.UserId),
			IsCreator: creator,
			Removable: manager && !creator,
		})
		if hasItems[m.UserId] && !listed[m.UserId] {
			listed[m.UserId] = true
			people = append(people, browserPerson{Id: m.UserId, Name: name})
		}
	}
	view.Stack = view.People[:min(groupStackSize, len(view.People))]
	view.MoreMembers = len(view.People) - len(view.Stack)
	for _, f := range files {
		name, known := names[f.UserId]
		if !known || listed[f.UserId] {
			continue
		}
		listed[f.UserId] = true
		people = append(people, browserPerson{Id: f.UserId, Name: name})
	}

	itemById := make(map[string]browserItem, len(items))
	for _, item := range items {
		itemById[item.File.Id] = item
	}
	// The newer item's uploader sees a pair; the group's creator and the
	// admin see every one
	someone := func(userId int) string {
		if name := names[userId]; name != "" {
			return name
		}
		return "someone" // the uploader's account is gone
	}
	for _, near := range storage.NearCopies(files, user.Id, database.GetNearCopiesKept(g.Id), manager) {
		view.NearCopies = append(view.NearCopies, groupNearCopy{
			Newer:     groupNearSideOf(itemById[near.FileId]),
			Older:     groupNearSideOf(itemById[near.MatchId]),
			Mine:      user.Id != 0 && near.FileUser == user.Id,
			MatchUser: someone(near.MatchUser),
			NewerUser: someone(near.FileUser),
			MatchAt:   near.MatchAt,
			MatchTime: time.Unix(near.MatchAt, 0).Format("15:04"),
		})
	}

	view.Browser = newFileBrowser("fgs-group", ctxGroup, items, user)
	view.Browser.WithGroup(g, people)
	view.Browser.UploadSink = true
	addNoCacheHeader(w)
	helper.CheckIgnoreTimeout(templateFolder.ExecuteTemplate(w, "group_page", &view))
}

// Handling of /gd
// One original from a group, for its members and the admin: counted, and
// recorded via "group" for the signed-in person (requireLogin). Anything
// else is a 404.
func downloadGroupFile(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, ok := storage.GetGroup(r.URL.Query().Get("id"))
	if !ok || !storage.CanSeeGroup(g, user) {
		http.NotFound(w, r)
		return
	}
	file, ok := groupFileById(g, r.URL.Query().Get("file"))
	if !ok || file.Encryption.IsEndToEndEncrypted {
		http.NotFound(w, r)
		return
	}
	forceDecryption := file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted
	if !storage.ServeFile(file, w, tagDownloadUser(r, models.DownloadViaGroup, user.Id), true, true, forceDecryption, true) {
		http.NotFound(w, r)
	}
}

// Handling of /gz
// The selected live files of a group as one zip (files=, selectedMembers),
// counted and recorded via "zip" for the signed-in person
func downloadGroupZip(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, ok := storage.GetGroup(r.URL.Query().Get("id"))
	if !ok || !storage.CanSeeGroup(g, user) {
		http.NotFound(w, r)
		return
	}
	storage.ServeZip(selectedMembers(storage.GroupFiles(g), r), g.Name, w,
		tagDownloadUser(r, models.DownloadViaZip, user.Id), true)
}

// groupItemAnswer is /groups/item: the FileApiOutput fields the live add
// reads (UploaderId among them), plus the group page's own
type groupItemAnswer struct {
	models.FileApiOutput
	UploaderName string
	Downloaded   bool
	CanRemove    bool
	GroupLock    string // the group whose terms the file follows (D4)
	Preview      previewInfo
}

// Handling of /groups/item
// One file of a group as the board shows it, for a finished upload added
// live (G7). Records nothing: looking is not downloading.
func groupItem(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	g, ok := visibleGroup(w, r.URL.Query().Get("id"), user)
	if !ok {
		return
	}
	file, ok := groupFileById(g, r.URL.Query().Get("file"))
	if !ok {
		writeJsonError(w, http.StatusNotFound, storage.ErrGroupNotFound)
		return
	}
	// Only the uploader's name is shown
	names := map[int]string{}
	if owner, found := database.GetUser(file.UserId); found {
		names[file.UserId] = displayName(owner)
	}
	item := groupBrowserItem(r, g, file, user, names,
		database.GetDownloadedFileIds(user.Id), storage.CanManageGroup(g, user))
	writeJson(w, groupItemAnswer{
		FileApiOutput: item.File,
		UploaderName:  item.Uploader,
		Downloaded:    item.Downloaded,
		CanRemove:     item.CanRemove,
		GroupLock:     item.GroupLock,
		Preview:       item.Preview,
	})
}

var errNotYourFile = errors.New("only the file's uploader, or the group's creator, can keep it")

// Handling of /groups/nearkeep
// POST {id, fileId}: "Keep both" for a near-copy. The file's uploader decides,
// and so do the group's creator and the admin, who see every pair; it is a
// 403 for any other member. It settles the pair for everyone.
func keepNearCopy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		groupIdRequest
		FileId string `json:"fileId"`
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	file, ok := groupFileById(g, req.FileId)
	if !ok {
		writeJsonError(w, http.StatusNotFound, storage.ErrGroupNotFound)
		return
	}
	if !storage.CanManageGroup(g, user) && (user.Id == 0 || file.UserId != user.Id) {
		writeJsonError(w, http.StatusForbidden, errNotYourFile)
		return
	}
	database.SaveNearCopyKept(g.Id, file.Id)
	writeJson(w, answerOK)
}
