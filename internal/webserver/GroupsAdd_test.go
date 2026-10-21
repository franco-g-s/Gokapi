//go:build !integration && test

package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type addAnswer struct {
	Result       string
	ErrorMessage string
	Added        int
	Duplicates   []struct{ FileId, Name, Uploader string }
	Refused      []struct{ FileId, Name, Reason string }
}

// postAs calls a group handler as this user with a JSON body
func postAs(t *testing.T, handler func(http.ResponseWriter, *http.Request), method string, user models.User, body string) (*httptest.ResponseRecorder, addAnswer) {
	t.Helper()
	w := httptest.NewRecorder()
	r := jsonRequest(method, "/groups/x", strings.NewReader(body))
	handler(w, authentication.SetUserInRequest(r, user))
	var answer addAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &answer)
	return w, answer
}

func TestGroupsAddEndpoint(t *testing.T) {
	member := models.User{Id: 31, Name: "Member", UserLevel: models.UserLevelUser}
	creator := models.User{Id: 32, Name: "anna@example.com", UserLevel: models.UserLevelUser}
	outsider := models.User{Id: 33, Name: "Outsider", UserLevel: models.UserLevelUser}
	admin := models.User{Id: 34, Name: "Boss", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	for _, u := range []models.User{member, creator, outsider, admin} {
		database.SaveUser(u, false)
	}
	g, err := storage.CreateGroup("Iceland trip", 60, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, err = storage.JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)

	owner := func(id int) func(*models.File) { return func(f *models.File) { f.UserId = id } }
	mine := accessFile("groupsadd0000000001", func(f *models.File) { f.UserId = 31; f.SHA1 = "groupsaddsha1"; f.Name = "mine.jpg" })
	theirs := accessFile("groupsadd0000000002", func(f *models.File) { f.UserId = 32; f.SHA1 = "groupsaddsha2"; f.Name = "theirs.jpg" })
	annas := accessFile("groupsadd0000000003", func(f *models.File) { f.UserId = 32; f.SHA1 = "groupsaddsha3"; f.Name = "anna.jpg" })
	copyOfAnnas := accessFile("groupsadd0000000004", func(f *models.File) { f.UserId = 31; f.SHA1 = "groupsaddsha3"; f.Name = "copy.jpg" })
	e2e := accessFile("groupsadd0000000005", func(f *models.File) {
		owner(31)(f)
		f.SHA1 = "e2e-groupsadd"
		f.Name = "secret.bin"
		f.Encryption.IsEndToEndEncrypted = true
	})
	adminAdds := accessFile("groupsadd0000000006", func(f *models.File) { f.UserId = 33; f.SHA1 = "groupsaddsha6"; f.Name = "stranger.jpg" })
	defer func() {
		for _, f := range []models.File{mine, theirs, annas, copyOfAnnas, e2e, adminAdds} {
			database.DeleteMetaData(f.Id)
		}
	}()
	body := func(id string, fileIds ...string) string {
		b, _ := json.Marshal(map[string]any{"id": id, "fileIds": fileIds})
		return string(b)
	}

	// The creator puts a photo in first, so the member's copy of it is a duplicate
	w, answer := postAs(t, addToGroup, "POST", creator, body(g.Id, annas.Id))
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, answer.Result, "OK")
	test.IsEqualInt(t, answer.Added, 1)

	// Own files are added; another user's file is refused; e2e is refused; a
	// byte-identical copy is reported with its uploader
	w, answer = postAs(t, addToGroup, "POST", member, body(g.Id, mine.Id, theirs.Id, e2e.Id, copyOfAnnas.Id))
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, answer.Result, "OK")
	test.IsEqualInt(t, answer.Added, 1)
	test.IsEqualInt(t, len(answer.Duplicates), 1)
	test.IsEqualString(t, answer.Duplicates[0].FileId, copyOfAnnas.Id)
	test.IsEqualString(t, answer.Duplicates[0].Name, "copy.jpg")
	test.IsEqualString(t, answer.Duplicates[0].Uploader, "anna")
	reasons := map[string]string{}
	names := map[string]string{}
	for _, r := range answer.Refused {
		reasons[r.FileId] = r.Reason
		names[r.FileId] = r.Name
	}
	test.IsEqualInt(t, len(answer.Refused), 2)
	test.IsEqualString(t, reasons[theirs.Id], "not yours")
	test.IsEqualString(t, names[theirs.Id], "theirs.jpg")
	test.IsEqualString(t, reasons[e2e.Id], "end-to-end encrypted")
	test.IsEqualBool(t, contains(database.GetGroupFileIds(g.Id), mine.Id), true)
	test.IsEqualBool(t, contains(database.GetGroupFileIds(g.Id), theirs.Id), false)

	// The admin adds anyone's, without being a member
	w, answer = postAs(t, addToGroup, "POST", admin, body(g.Id, adminAdds.Id, "doesnotexist"))
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualInt(t, answer.Added, 1)
	test.IsEqualInt(t, len(answer.Refused), 1)
	test.IsEqualString(t, answer.Refused[0].Reason, "not found")

	// A non-member does not learn the group exists
	w, answer = postAs(t, addToGroup, "POST", outsider, body(g.Id, theirs.Id))
	test.IsEqualInt(t, w.Code, 404)
	test.IsEqualString(t, answer.Result, "error")
	w, _ = postAs(t, addToGroup, "POST", member, body("nosuchgroup", mine.Id))
	test.IsEqualInt(t, w.Code, 404)

	// Bad requests
	w, _ = postAs(t, addToGroup, "POST", member, body(g.Id))
	test.IsEqualInt(t, w.Code, 400)
	w, _ = postAs(t, addToGroup, "POST", member, "not json")
	test.IsEqualInt(t, w.Code, 400)

	// Nothing but POST
	w, _ = postAs(t, addToGroup, "GET", member, body(g.Id, mine.Id))
	test.IsEqualInt(t, w.Code, 405)
	test.IsEqualString(t, w.Header().Get("Allow"), "POST")
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

type fromCollectionAnswer struct {
	Result       string
	ErrorMessage string
	Id           string
	Url          string
	Added        int
}

func TestGroupFromCollection(t *testing.T) {
	owner := models.User{Id: 41, Name: "Owner", UserLevel: models.UserLevelUser}
	stranger := models.User{Id: 42, Name: "Stranger", UserLevel: models.UserLevelUser}
	admin := models.User{Id: 43, Name: "Boss", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	for _, u := range []models.User{owner, stranger, admin} {
		database.SaveUser(u, false)
	}
	one := accessFile("groupscol0000000001", func(f *models.File) { f.UserId = 41; f.SHA1 = "groupscolsha1" })
	two := accessFile("groupscol0000000002", func(f *models.File) { f.UserId = 41; f.SHA1 = "groupscolsha2" })
	defer database.DeleteMetaData(one.Id)
	defer database.DeleteMetaData(two.Id)
	collection, err := storage.CreateCollection(storage.CollectionParameters{Name: "Summer 2026", FileIds: []string{one.Id, two.Id}}, owner)
	test.IsNil(t, err)
	defer database.DeleteCollection(collection.Id)

	call := func(method string, user models.User, id string, days int) (*httptest.ResponseRecorder, fromCollectionAnswer) {
		b, _ := json.Marshal(map[string]any{"collectionId": id, "days": days})
		w := httptest.NewRecorder()
		groupFromCollection(w, authentication.SetUserInRequest(jsonRequest(method, "/groups/fromcollection", strings.NewReader(string(b))), user))
		var answer fromCollectionAnswer
		_ = json.Unmarshal(w.Body.Bytes(), &answer)
		return w, answer
	}

	// A stranger, a bad choice of days and an unknown collection change nothing
	w, _ := call("POST", stranger, collection.Id, 60)
	test.IsEqualInt(t, w.Code, 403)
	w, _ = call("POST", owner, collection.Id, 45)
	test.IsEqualInt(t, w.Code, 400)
	w, _ = call("POST", owner, "nosuchcollection", 60)
	test.IsEqualInt(t, w.Code, 404)
	w, _ = call("GET", owner, collection.Id, 60)
	test.IsEqualInt(t, w.Code, 405)
	test.IsEqualInt(t, len(storage.GroupsOfUser(owner.Id)), 0)

	// The owner gets a group with the collection's name and files
	w, answer := call("POST", owner, collection.Id, 60)
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, answer.Result, "OK")
	test.IsEqualInt(t, answer.Added, 2)
	test.IsEqualString(t, answer.Url, "./g?id="+answer.Id)
	defer database.DeleteGroup(answer.Id)
	g, ok := storage.GetGroup(answer.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, g.Name, "Summer 2026")
	test.IsEqualInt(t, len(storage.GroupFiles(g)), 2)
	test.IsEqualInt(t, g.CreatorId, owner.Id)

	// The collection and its link are untouched
	again, ok := storage.GetCollection(collection.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, len(again.Files), 2)
	test.IsEqualString(t, again.Name, "Summer 2026")

	// M4: a collection none of whose files can go into a group (here: all
	// someone else's) leaves no empty group behind
	theirs := accessFile("groupfromcolthr0001", func(f *models.File) { f.UserId = stranger.Id; f.SHA1 = "groupfromcolthrsha" })
	defer database.DeleteMetaData(theirs.Id)
	foreign, err := storage.CreateCollection(storage.CollectionParameters{Name: "Not mine", FileIds: []string{theirs.Id}}, owner)
	test.IsNil(t, err)
	defer database.DeleteCollection(foreign.Id)
	before := len(storage.GroupsOfUser(owner.Id))
	w, answer = call("POST", owner, foreign.Id, 60)
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	test.IsEqualString(t, answer.ErrorMessage, "None of these files could go into a group")
	test.IsEqualInt(t, len(storage.GroupsOfUser(owner.Id)), before)
	for _, g := range storage.GetAllGroups() {
		test.IsEqualBool(t, g.Name == "Not mine", false)
	}

	// A collection is turned into a group once (R4)
	w, answer = call("POST", owner, collection.Id, 60)
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	test.IsEqualString(t, answer.ErrorMessage, storage.ErrCollectionAlreadyGroup.Error())

	// The admin may turn anyone's collection into a group of their own
	w, answer = call("POST", admin, foreign.Id, 0)
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, answer.Result, "OK")
	defer database.DeleteGroup(answer.Id)
	g, _ = storage.GetGroup(answer.Id)
	test.IsEqualBool(t, g.NeverExpires(), true)
}

// "Turn into a group" is on the collections the user owns, and on every row
// for the admin; not on someone else's collection that PERM_LIST only lets
// them see
func TestCollectionsPageTurnIntoGroupButton(t *testing.T) {
	owner := models.User{Id: 51, Name: "Colowner", UserLevel: models.UserLevelUser}
	lister := models.User{Id: 52, Name: "Lister", UserLevel: models.UserLevelUser, Permissions: models.UserPermListOtherUploads}
	admin := models.User{Id: 53, Name: "Colboss", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	for _, u := range []models.User{owner, lister, admin} {
		database.SaveUser(u, false)
	}
	f := accessFile("groupsbtn0000000001", func(f *models.File) { f.UserId = 51; f.SHA1 = "groupsbtnsha1" })
	defer database.DeleteMetaData(f.Id)
	c, err := storage.CreateCollection(storage.CollectionParameters{Name: "btn-collection", FileIds: []string{f.Id}}, owner)
	test.IsNil(t, err)
	defer database.DeleteCollection(c.Id)

	page := func(user models.User) string {
		w := httptest.NewRecorder()
		showCollectionsAdmin(w, authentication.SetUserInRequest(httptest.NewRequest("GET", "/collections", nil), user))
		test.IsEqualInt(t, w.Code, 200)
		return w.Body.String()
	}
	const button = `class="btn btn-sm btn-outline-light collection-togroup"`
	for _, user := range []models.User{owner, admin} {
		html := page(user)
		test.IsEqualBool(t, strings.Contains(html, button), true)
		test.IsEqualBool(t, strings.Contains(html, `id="togroup-modal"`), true)
		test.IsEqualBool(t, strings.Contains(html, `value="60" checked`), true)
		// R4: the one shared dialog script, and the dialog's new choices
		test.IsEqualBool(t, strings.Contains(html, "collection_togroup.min.js"), true)
		test.IsEqualBool(t, strings.Contains(html, "Add the people who opened this link while signed in"), true)
		test.IsEqualBool(t, strings.Contains(html, `value="invite" checked`), true)
		test.IsEqualBool(t, strings.Contains(html, "Require signing in and joining"), true)
	}
	html := page(lister)
	test.IsEqualBool(t, strings.Contains(html, "btn-collection"), true)
	test.IsEqualBool(t, strings.Contains(html, button), false)
}
