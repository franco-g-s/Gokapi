//go:build !integration && test

package webserver

// Part 2, R4: "Turn into a group" hides the collection, adds the people who
// opened it, and keeps its old link working in one of two modes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/access"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// The people of these tests, each with a session: 81 owns the collection,
// 82 and 83 opened its link signed in, 84 is signed in but in no group, 85
// becomes a plain member, 5 is the super admin
var (
	r4Owner    = models.User{Id: 81, Name: "olive@example.com", UserLevel: models.UserLevelUser}
	r4Vera     = models.User{Id: 82, Name: "vera@example.com", UserLevel: models.UserLevelUser}
	r4Walt     = models.User{Id: 83, Name: "walt@example.com", UserLevel: models.UserLevelUser}
	r4Stranger = models.User{Id: 84, Name: "nina@example.com", UserLevel: models.UserLevelUser}
	r4Member   = models.User{Id: 85, Name: "mia@example.com", UserLevel: models.UserLevelUser}
)

func r4Users() {
	for _, u := range []models.User{r4Owner, r4Vera, r4Walt, r4Stranger, r4Member} {
		database.SaveUser(u, false)
	}
	for token, id := range map[string]int{"r4s81": 81, "r4s82": 82, "r4s83": 83, "r4s84": 84, "r4s85": 85, "r4s5": 5} {
		database.SaveSession(token, models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: id})
	}
}

// r4Collection saves a collection of two of the owner's files. The cleanup
// deletes the collection (and with it any group link) and the files.
func r4Collection(t *testing.T, id string) (models.Collection, []models.File) {
	t.Helper()
	a := accessFile(id+"fa", func(f *models.File) { f.UserId = r4Owner.Id; f.SHA1 = id + "sha-a"; f.Name = "a.jpg" })
	b := accessFile(id+"fb", func(f *models.File) { f.UserId = r4Owner.Id; f.SHA1 = id + "sha-b"; f.Name = "b.jpg" })
	c, err := storage.CreateCollection(storage.CollectionParameters{Name: "Trip " + id, FileIds: []string{a.Id, b.Id}}, r4Owner)
	test.IsNil(t, err)
	t.Cleanup(func() {
		database.DeleteCollection(c.Id)
		database.DeleteMetaData(a.Id)
		database.DeleteMetaData(b.Id)
	})
	return c, []models.File{a, b}
}

// r4Convert turns the collection into a group through the endpoint, as the owner
func r4Convert(t *testing.T, collectionId string, addViewers bool, mode string) (models.Group, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"collectionId": collectionId, "days": 30, "addViewers": addViewers, "linkMode": mode})
	w := groupPageCall(groupFromCollection, "POST", "/groups/fromcollection", r4Owner, string(raw))
	test.IsEqualInt(t, w.Code, http.StatusOK)
	answer := map[string]any{}
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &answer))
	g, ok := storage.GetGroup(answer["Id"].(string))
	test.IsEqualBool(t, ok, true)
	t.Cleanup(func() { database.DeleteGroup(g.Id) })
	return g, answer
}

// r4Open requests a collection route with the session, if any, without following redirects
func r4Open(handler http.HandlerFunc, target, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func TestR4PreviewAndConversionAddViewers(t *testing.T) {
	r4Users()
	c, _ := r4Collection(t, "r4viewers")
	database.SaveCollectionSeen(models.CollectionSeen{UserId: r4Walt.Id, CollectionId: c.Id, FirstOpened: 20})
	database.SaveCollectionSeen(models.CollectionSeen{UserId: r4Vera.Id, CollectionId: c.Id, FirstOpened: 10})

	preview := func(user models.User, id string) (int, []map[string]any) {
		w := groupPageCall(groupFromCollectionPreview, "GET", "/groups/fromcollection/preview?collectionId="+url.QueryEscape(id), user, "")
		var answer struct {
			Viewers []map[string]any `json:"viewers"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &answer)
		return w.Code, answer.Viewers
	}
	// The owner sees who opened it, by display name, first opened first
	code, viewers := preview(r4Owner, c.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualInt(t, len(viewers), 2)
	test.IsEqualString(t, viewers[0]["name"].(string), "vera")
	test.IsEqualInt(t, int(viewers[0]["id"].(float64)), r4Vera.Id)
	test.IsEqualString(t, viewers[1]["name"].(string), "walt")
	// So does an admin; nobody else, and an unknown collection is a 404
	code, viewers = preview(models.User{Id: 5, UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}, c.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualInt(t, len(viewers), 2)
	code, _ = preview(r4Stranger, c.Id)
	test.IsEqualInt(t, code, http.StatusForbidden)
	code, _ = preview(r4Owner, "nosuchcollection")
	test.IsEqualInt(t, code, http.StatusNotFound)

	// Converting with addViewers makes exactly those two members
	g, answer := r4Convert(t, c.Id, true, models.CollectionLinkInvite)
	test.IsEqualInt(t, int(answer["Added"].(float64)), 2)
	test.IsEqualInt(t, int(answer["MembersAdded"].(float64)), 2)
	members := map[int]bool{}
	for _, m := range database.GetGroupMembers(g.Id) {
		members[m.UserId] = true
	}
	test.IsEqualInt(t, len(members), 3)
	test.IsEqualBool(t, members[r4Owner.Id] && members[r4Vera.Id] && members[r4Walt.Id], true)

	// The collection leaves the owner's Collections list and the Files tab's
	// picker; its link keeps working
	w := groupPageCall(showCollectionsAdmin, "GET", "/collections", r4Owner, "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), `id="collection-`+c.Id+`"`), false)
	w = groupPageCall(listCollections, "GET", "/collections/list?mine=1", r4Owner, "")
	test.IsEqualBool(t, strings.Contains(w.Body.String(), c.Id), false)
	_, ok := storage.GetCollection(c.Id)
	test.IsEqualBool(t, ok, true)
}

func TestR4ConversionWithoutViewers(t *testing.T) {
	r4Users()
	c, _ := r4Collection(t, "r4noviewers")
	database.SaveCollectionSeen(models.CollectionSeen{UserId: r4Vera.Id, CollectionId: c.Id, FirstOpened: 10})
	g, answer := r4Convert(t, c.Id, false, "")
	test.IsEqualInt(t, int(answer["MembersAdded"].(float64)), 0)
	test.IsEqualInt(t, len(database.GetGroupMembers(g.Id)), 1)
	link, _ := database.GetCollectionGroup(c.Id)
	test.IsEqualString(t, link.LinkMode, models.CollectionLinkInvite) // the default
}

func TestR4InviteModeBanner(t *testing.T) {
	r4Users()
	c, _ := r4Collection(t, "r4invite")
	g, _ := r4Convert(t, c.Id, false, models.CollectionLinkInvite)
	const banner = "This is now a group — sign in to join and add your own"
	joinForm := `action="./groups/join"`

	// Anonymous: the photos as before, the banner, and a sign-in link back to the join page
	body := openCollection(c.Id, "")
	test.IsEqualBool(t, strings.Contains(body, "./cz?id="+c.Id), true)
	test.IsEqualBool(t, strings.Contains(body, banner), true)
	test.IsEqualBool(t, strings.Contains(body, `href="./join?t=`+g.InviteToken+`"`), true)
	test.IsEqualBool(t, strings.Contains(body, "Sign in to join"), true)
	test.IsEqualBool(t, strings.Contains(body, joinForm), false)

	// Signed in, not a member: Join posts the group's current token
	body = openCollection(c.Id, "r4s84")
	test.IsEqualBool(t, strings.Contains(body, "./cz?id="+c.Id), true)
	test.IsEqualBool(t, strings.Contains(body, banner), true)
	test.IsEqualBool(t, strings.Contains(body, joinForm), true)
	test.IsEqualBool(t, strings.Contains(body, `name="t" value="`+g.InviteToken+`"`), true)
	test.IsEqualBool(t, strings.Contains(body, ">Join</button>"), true)

	// and joining works
	form := url.Values{"t": {g.InviteToken}}.Encode()
	resp, _ := groupsTabDoHeaders(t, "POST", "/groups/join", "r4s84",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://127.0.0.1:53843"}, form)
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	test.IsEqualBool(t, storage.IsGroupMember(g, r4Stranger.Id), true)

	// A member is pointed at the group instead
	body = openCollection(c.Id, "r4s84")
	test.IsEqualBool(t, strings.Contains(body, banner), false)
	test.IsEqualBool(t, strings.Contains(body, joinForm), false)
	test.IsEqualBool(t, strings.Contains(body, `href="./g?id=`+g.Id+`"`), true)

	// The owner keeps the bar, but there is nothing left to turn into a group
	body = openCollection(c.Id, "r4s81")
	test.IsEqualBool(t, strings.Contains(body, "data-fgs-owner-bar"), true)
	test.IsEqualBool(t, strings.Contains(body, `data-owner-act="togroup"`), false)
	test.IsEqualBool(t, strings.Contains(body, `id="togroup-modal"`), false)

	// The token is read when the page is served, so a reset code keeps the link working
	reset, err := storage.ResetGroupCode(g, r4Owner)
	test.IsNil(t, err)
	body = openCollection(c.Id, "")
	test.IsEqualBool(t, strings.Contains(body, `href="./join?t=`+reset.InviteToken+`"`), true)
	test.IsEqualBool(t, strings.Contains(body, g.InviteToken), false)
}

func TestR4RequireModeRedirects(t *testing.T) {
	r4Users()
	c, files := r4Collection(t, "r4require")
	g, _ := r4Convert(t, c.Id, false, models.CollectionLinkRequire)
	_, err := storage.JoinGroupByToken(g.InviteToken, r4Member)
	test.IsNil(t, err)
	page := "/c?id=" + c.Id

	// Anonymous: to the join page, which signs in first and comes back
	resp, _ := groupsTabDo(t, "GET", page, "", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	test.IsEqualString(t, resp.Header.Get("Location"), "join?t="+g.InviteToken)
	resp, _ = groupsTabDo(t, "GET", "/"+resp.Header.Get("Location"), "", "", "")
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "login"), true)
	test.IsEqualString(t, cookieValue(resp.Cookies(), "fgs_return"), "join?t="+g.InviteToken)

	// Signed in but not a member: the join page too
	resp, _ = groupsTabDo(t, "GET", page, "r4s84", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	test.IsEqualString(t, resp.Header.Get("Location"), "join?t="+g.InviteToken)

	// A member, the creator and the admin: the group
	for _, token := range []string{"r4s85", "r4s81", "r4s5"} {
		resp, _ = groupsTabDo(t, "GET", page, token, "", "")
		test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
		test.IsEqualString(t, resp.Header.Get("Location"), "g?id="+g.Id)
	}

	// The collection's zip and downloads are closed to the same people
	served := func(handler http.HandlerFunc, target, token string) bool {
		return r4Open(handler, target, token).Code == http.StatusOK
	}
	for _, tc := range []struct {
		token string
		in    bool
	}{{"", false}, {"r4s84", false}, {"r4s85", true}} {
		if served(downloadCollectionZip, "/cz?id="+c.Id, tc.token) != tc.in {
			t.Errorf("%q: /cz served = %v, want %v", tc.token, !tc.in, tc.in)
		}
		if served(downloadCollectionFile, "/cd?id="+c.Id+"&file="+files[0].Id, tc.token) != tc.in {
			t.Errorf("%q: /cd served = %v, want %v", tc.token, !tc.in, tc.in)
		}
	}

	// Previews in the collection's context: a file with its own password is
	// viewable through an invite link, not through a require one
	locked := accessFile("r4requirelocked", func(f *models.File) { f.UserId = r4Owner.Id; f.SHA1 = "r4requirelockedsha"; f.PasswordHash = "x" })
	defer database.DeleteMetaData(locked.Id)
	other, _ := r4Collection(t, "r4previews")
	other.FileIds = append(other.FileIds, locked.Id)
	database.SaveCollection(other)
	og, _ := r4Convert(t, other.Id, false, models.CollectionLinkInvite)
	test.IsEqualBool(t, access.CanView(sessionRequest(""), locked, other.Id), true)
	test.IsNil(t, storage.SetGroupLinkMode(og, models.CollectionLinkRequire, r4Owner))
	test.IsEqualBool(t, access.CanView(sessionRequest(""), locked, other.Id), false)

	// A code reset: the old link now leads to the new token
	reset, err := storage.ResetGroupCode(g, r4Owner)
	test.IsNil(t, err)
	resp, _ = groupsTabDo(t, "GET", page, "", "", "")
	test.IsEqualString(t, resp.Header.Get("Location"), "join?t="+reset.InviteToken)
}

func TestR4LinkModeAndStopLink(t *testing.T) {
	r4Users()
	c, _ := r4Collection(t, "r4stop")
	g, _ := r4Convert(t, c.Id, false, models.CollectionLinkInvite)
	_, err := storage.JoinGroupByToken(g.InviteToken, r4Member)
	test.IsNil(t, err)
	modeBody := func(mode string) string {
		raw, _ := json.Marshal(map[string]any{"id": g.Id, "mode": mode})
		return string(raw)
	}
	idBody := `{"id":"` + g.Id + `"}`
	currentMode := func() string {
		link, _ := database.GetCollectionGroup(c.Id)
		return link.LinkMode
	}

	// The group page shows the link to the creator, not to a member
	w := groupPageCall(showGroup, "GET", "/g?id="+g.Id, r4Owner, "")
	body := w.Body.String()
	for _, want := range []string{"Also shared as a link", "Stop the link", "c?id=" + c.Id,
		"Show the photos and invite to join", "Require signing in and joining", `value="invite" checked`} {
		if !strings.Contains(body, want) {
			t.Errorf("the creator's group page lacks %q", want)
		}
	}
	body = groupPageCall(showGroup, "GET", "/g?id="+g.Id, r4Member, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, "Also shared as a link"), false)

	// A plain member may not switch or stop it; someone outside does not see the group
	test.IsEqualInt(t, groupPageCall(setGroupLinkMode, "POST", "/groups/linkmode", r4Member, modeBody("require")).Code, http.StatusForbidden)
	test.IsEqualInt(t, groupPageCall(stopGroupLink, "POST", "/groups/stoplink", r4Member, idBody).Code, http.StatusForbidden)
	test.IsEqualInt(t, groupPageCall(setGroupLinkMode, "POST", "/groups/linkmode", r4Stranger, modeBody("require")).Code, http.StatusNotFound)
	test.IsEqualInt(t, groupPageCall(stopGroupLink, "POST", "/groups/stoplink", r4Stranger, idBody).Code, http.StatusNotFound)
	test.IsEqualString(t, currentMode(), models.CollectionLinkInvite)
	// POST and JSON only
	test.IsEqualInt(t, groupPageCall(setGroupLinkMode, "GET", "/groups/linkmode", r4Owner, modeBody("require")).Code, http.StatusMethodNotAllowed)
	test.IsEqualInt(t, groupPageCall(stopGroupLink, "GET", "/groups/stoplink", r4Owner, idBody).Code, http.StatusMethodNotAllowed)
	r := httptest.NewRequest("POST", "/groups/stoplink", strings.NewReader(idBody))
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	stopGroupLink(w, authentication.SetUserInRequest(r, r4Owner))
	test.IsEqualInt(t, w.Code, http.StatusUnsupportedMediaType)

	// The creator switches it, and only to one of the two modes
	test.IsEqualInt(t, groupPageCall(setGroupLinkMode, "POST", "/groups/linkmode", r4Owner, modeBody("open")).Code, http.StatusBadRequest)
	test.IsEqualInt(t, groupPageCall(setGroupLinkMode, "POST", "/groups/linkmode", r4Owner, modeBody("require")).Code, http.StatusOK)
	test.IsEqualString(t, currentMode(), models.CollectionLinkRequire)
	body = groupPageCall(showGroup, "GET", "/g?id="+g.Id, r4Owner, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, `value="require" checked`), true)

	// Stopping it deletes the collection: /c answers as for a deleted one
	w = groupPageCall(stopGroupLink, "POST", "/groups/stoplink", r4Owner, idBody)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	_, ok := database.GetCollection(c.Id)
	test.IsEqualBool(t, ok, false)
	gone := r4Open(showCollection, "/c?id="+c.Id, "")
	never := r4Open(showCollection, "/c?id=nosuchcollection", "")
	test.IsEqualInt(t, gone.Code, never.Code)
	test.IsEqualString(t, gone.Header().Get("Location"), never.Header().Get("Location"))
	// The group stays, without the link
	body = groupPageCall(showGroup, "GET", "/g?id="+g.Id, r4Owner, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, "Also shared as a link"), false)
	test.IsEqualInt(t, groupPageCall(stopGroupLink, "POST", "/groups/stoplink", r4Owner, idBody).Code, http.StatusNotFound)
}

func TestR4DeleteGroupRemovesCollection(t *testing.T) {
	r4Users()
	c, files := r4Collection(t, "r4delete")
	g, _ := r4Convert(t, c.Id, false, models.CollectionLinkInvite)
	w := groupPageCall(deleteGroup, "POST", "/groups/delete", r4Owner, `{"id":"`+g.Id+`"}`)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	_, ok := database.GetCollection(c.Id)
	test.IsEqualBool(t, ok, false)
	_, linked := database.GetCollectionGroup(c.Id)
	test.IsEqualBool(t, linked, false)
	_, ok = database.GetMetaDataById(files[0].Id)
	test.IsEqualBool(t, ok, true) // the files stay
}

// Fix round 1: an admin turning user 7's collection into a group makes 7 its
// creator, so 7 keeps the link, and the admin does not become a member
func TestR4AdminConvertsSomeoneElsesCollection(t *testing.T) {
	r4Users()
	owner, ok := database.GetUser(7)
	test.IsEqualBool(t, ok, true)
	admin, ok := database.GetUser(5)
	test.IsEqualBool(t, ok, true)
	a := accessFile("r4adminconvfa", func(f *models.File) { f.UserId = 7; f.SHA1 = "r4adminconvsha-a" })
	b := accessFile("r4adminconvfb", func(f *models.File) { f.UserId = 7; f.SHA1 = "r4adminconvsha-b" })
	c, err := storage.CreateCollection(storage.CollectionParameters{Name: "Seven's trip", FileIds: []string{a.Id, b.Id}}, owner)
	test.IsNil(t, err)
	t.Cleanup(func() {
		database.DeleteCollection(c.Id)
		database.DeleteMetaData(a.Id)
		database.DeleteMetaData(b.Id)
	})

	raw, _ := json.Marshal(map[string]any{"collectionId": c.Id, "days": 30, "addViewers": true, "linkMode": models.CollectionLinkRequire})
	w := groupPageCall(groupFromCollection, "POST", "/groups/fromcollection", admin, string(raw))
	test.IsEqualInt(t, w.Code, http.StatusOK)
	answer := map[string]any{}
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &answer))
	g, ok := storage.GetGroup(answer["Id"].(string))
	test.IsEqualBool(t, ok, true)
	t.Cleanup(func() { database.DeleteGroup(g.Id) })

	// 7 is the creator, as a member with that role; the admin is not a member
	test.IsEqualInt(t, g.CreatorId, 7)
	role, member := storage.GroupRole(g, 7)
	test.IsEqualBool(t, member, true)
	test.IsEqualString(t, role, models.GroupRoleCreator)
	_, adminMember := storage.GroupRole(g, 5)
	test.IsEqualBool(t, adminMember, false)
	test.IsEqualInt(t, len(database.GetGroupMembers(g.Id)), 1)

	// In require mode, 7's own link lands on the group
	resp, _ := groupsTabDo(t, "GET", "/c?id="+c.Id, "validsession", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	test.IsEqualString(t, resp.Header.Get("Location"), "g?id="+g.Id)

	// 7 may switch the mode and stop the link
	test.IsEqualInt(t, groupPageCall(setGroupLinkMode, "POST", "/groups/linkmode", owner,
		`{"id":"`+g.Id+`","mode":"invite"}`).Code, http.StatusOK)
	link, _ := database.GetCollectionGroup(c.Id)
	test.IsEqualString(t, link.LinkMode, models.CollectionLinkInvite)
	test.IsEqualInt(t, groupPageCall(stopGroupLink, "POST", "/groups/stoplink", owner, `{"id":"`+g.Id+`"}`).Code, http.StatusOK)
	_, ok = database.GetCollection(c.Id)
	test.IsEqualBool(t, ok, false)
}
