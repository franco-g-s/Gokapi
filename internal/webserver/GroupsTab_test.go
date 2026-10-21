//go:build !integration && test

package webserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
	"github.com/forceu/gokapi/internal/webserver/ratelimiter"
)

const groupsTabServer = "http://localhost:53843"

// groupsTabClient never follows redirects, so their Location can be checked
var groupsTabClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

// groupsTabUsers saves the people of these tests with their own sessions:
// 5 the super admin, 7 the creator, 8 a member, 61 someone in no group, 62 a
// second joiner. Other tests reuse 8 under other names, so it is saved here.
func groupsTabUsers() {
	database.SaveUser(models.User{Id: 8, Name: "anna@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveUser(models.User{Id: 61, Name: "nobody@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveUser(models.User{Id: 62, Name: "ben@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	for token, id := range map[string]int{"gtab5": 5, "gtab7": 7, "gtab8": 8, "gtab61": 61, "gtab62": 62} {
		database.SaveSession(token, models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: id})
	}
}

// groupsTabDo sends a request on the real server, with the session if given
func groupsTabDo(t *testing.T, method, path, token, contentType, body string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	return groupsTabDoHeaders(t, method, path, token, map[string]string{"Content-Type": contentType}, body, cookies...)
}

// groupsTabDoHeaders is groupsTabDo with any request headers; empty ones are
// left out
func groupsTabDoHeaders(t *testing.T, method, path, token string, headers map[string]string, body string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, groupsTabServer+path, strings.NewReader(body))
	test.IsNil(t, err)
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := groupsTabClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	test.IsNil(t, err)
	return resp, string(raw)
}

// groupsTabPost posts v as JSON and decodes the answer
func groupsTabPost(t *testing.T, path, token string, v any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(v)
	test.IsNil(t, err)
	resp, raw := groupsTabDo(t, "POST", path, token, "application/json", string(body))
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("%s: not JSON (%d): %s", path, resp.StatusCode, raw)
	}
	return resp.StatusCode, result
}

// groupsTabRow returns the table row of the group on the Groups tab
func groupsTabRow(t *testing.T, page, id string) string {
	t.Helper()
	start := strings.Index(page, `href="./g?id=`+id+`"`)
	if start < 0 {
		t.Fatalf("group %s is not listed", id)
	}
	end := strings.Index(page[start:], "</tr>")
	if end < 0 {
		t.Fatalf("row of group %s does not end", id)
	}
	return page[start : start+end]
}

func TestGroupsTab(t *testing.T) {
	groupsTabUsers()

	// Someone in no group gets an empty state with only the two buttons
	resp, page := groupsTabDo(t, "GET", "/groups", "gtab61", "", "")
	test.IsEqualInt(t, resp.StatusCode, 200)
	test.IsEqualBool(t, strings.Contains(page, "Create group"), true)
	test.IsEqualBool(t, strings.Contains(page, "Join with a code"), true)
	test.IsEqualBool(t, strings.Contains(page, "<table"), false)
	test.IsEqualBool(t, strings.Contains(page, `href="./groups"`), true) // the nav link

	// Creating: only the four expiry choices, and days must be given
	status, answer := groupsTabPost(t, "/groups/create", "gtab7", map[string]any{"name": "Iceland trip", "days": 45})
	test.IsEqualInt(t, status, 400)
	test.IsEqualString(t, answer["Result"].(string), "error")
	test.IsEqualString(t, answer["ErrorMessage"].(string), storage.ErrGroupExpiryChoice.Error())
	status, _ = groupsTabPost(t, "/groups/create", "gtab7", map[string]any{"name": "Iceland trip"})
	test.IsEqualInt(t, status, 400)
	status, _ = groupsTabPost(t, "/groups/create", "gtab7", map[string]any{"name": " ", "days": 60})
	test.IsEqualInt(t, status, 400)

	status, answer = groupsTabPost(t, "/groups/create", "gtab7", map[string]any{"name": "Iceland trip", "days": 60})
	test.IsEqualInt(t, status, 200)
	test.IsEqualString(t, answer["Result"].(string), "OK")
	id := answer["Id"].(string)
	defer database.DeleteGroup(id)
	test.IsEqualString(t, answer["Url"].(string), "./g?id="+id)
	group, ok := storage.GetGroup(id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, answer["InviteUrl"].(string), storage.GroupInviteUrl(group))
	test.IsEqualString(t, answer["Code"].(string), storage.GroupCode(group))
	oldCode := answer["Code"].(string)

	// /groups/list shows it with role creator, and the tab lists it
	items := listAs(t, models.User{Id: 7, UserLevel: models.UserLevelUser}, "")
	test.IsEqualString(t, items[id].Role, models.GroupRoleCreator)
	_, page = groupsTabDo(t, "GET", "/groups", "gtab7", "", "")
	test.IsEqualBool(t, strings.Contains(page, "<table"), true)
	row := groupsTabRow(t, page, id)
	test.IsEqualBool(t, strings.Contains(row, "Iceland trip"), true)
	test.IsEqualBool(t, strings.Contains(row, "Creator"), true)
	test.IsEqualBool(t, strings.Contains(row, "not a member"), false)
	test.IsEqualBool(t, strings.Contains(page, closesLabel(group, time.Now())), true)

	// Every mutation endpoint answers 405 to anything but POST
	for _, path := range []string{"/groups/create", "/groups/join", "/groups/rename", "/groups/extend",
		"/groups/resetcode", "/groups/removemember", "/groups/leave", "/groups/delete", "/groups/removefiles"} {
		resp, _ = groupsTabDo(t, "GET", path, "gtab7", "", "")
		test.IsEqualInt(t, resp.StatusCode, http.StatusMethodNotAllowed)
	}
	// and 401 without a session
	resp, _ = groupsTabDo(t, "POST", "/groups/create", "", "application/json", `{"name":"x","days":30}`)
	test.IsEqualInt(t, resp.StatusCode, http.StatusUnauthorized)

	// Joining by code, typed in any case and spacing, as JSON
	status, answer = groupsTabPost(t, "/groups/join", "gtab8", map[string]any{"code": strings.ToLower(strings.ReplaceAll(oldCode, "-", " "))})
	test.IsEqualInt(t, status, 200)
	test.IsEqualString(t, answer["Id"].(string), id)
	role, member := storage.GroupRole(group, 8)
	test.IsEqualBool(t, member, true)
	test.IsEqualString(t, role, models.GroupRoleMember)
	status, answer = groupsTabPost(t, "/groups/join", "gtab8", map[string]any{"code": "ZZZ-ZZZ"})
	test.IsEqualInt(t, status, 400)
	test.IsEqualString(t, answer["ErrorMessage"].(string), storage.ErrInvalidJoin.Error())

	// The join page: a confirmation card, which itself never joins
	_, page = groupsTabDo(t, "GET", "/join?t="+group.InviteToken, "gtab62", "", "")
	test.IsEqualBool(t, strings.Contains(page, "Iceland trip"), true)
	test.IsEqualBool(t, strings.Contains(page, `action="./groups/join"`), true)
	test.IsEqualBool(t, strings.Contains(page, `name="t" value="`+group.InviteToken+`"`), true)
	test.IsEqualBool(t, strings.Contains(page, "2 people"), true)
	_, member = storage.GroupRole(group, 62)
	test.IsEqualBool(t, member, false)
	// After an internal sign-in the browser re-POSTs the login form here (307)
	resp, page = groupsTabDo(t, "POST", "/join?t="+group.InviteToken, "gtab62", "application/x-www-form-urlencoded", "username=x&password=y")
	test.IsEqualInt(t, resp.StatusCode, 200)
	test.IsEqualBool(t, strings.Contains(page, `action="./groups/join"`), true)
	_, member = storage.GroupRole(group, 62)
	test.IsEqualBool(t, member, false)

	// Joining by the form's t= answers 303 to the group page
	resp, _ = groupsTabDo(t, "POST", "/groups/join", "gtab62", "application/x-www-form-urlencoded", "t="+url.QueryEscape(group.InviteToken))
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "g?id="+id), true)
	_, member = storage.GroupRole(group, 62)
	test.IsEqualBool(t, member, true)
	// A member opening the link again goes straight to the group
	resp, _ = groupsTabDo(t, "GET", "/join?t="+group.InviteToken, "gtab62", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusTemporaryRedirect)
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "g?id="+id), true)
	// A bad token on the form goes back to the join page, which says so
	resp, _ = groupsTabDo(t, "POST", "/groups/join", "gtab62", "application/x-www-form-urlencoded", "t=nosuchtoken")
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	test.IsEqualString(t, resp.Header.Get("Location"), "../join?t=nosuchtoken")

	// Renaming: a member may not (403), someone outside does not see it (404)
	status, _ = groupsTabPost(t, "/groups/rename", "gtab8", map[string]any{"id": id, "name": "Mine now"})
	test.IsEqualInt(t, status, http.StatusForbidden)
	status, answer = groupsTabPost(t, "/groups/rename", "gtab61", map[string]any{"id": id, "name": "Mine now"})
	test.IsEqualInt(t, status, http.StatusNotFound)
	test.IsEqualString(t, answer["ErrorMessage"].(string), storage.ErrGroupNotFound.Error())
	status, _ = groupsTabPost(t, "/groups/rename", "gtab7", map[string]any{"id": "nosuchgroup", "name": "x"})
	test.IsEqualInt(t, status, http.StatusNotFound)
	status, answer = groupsTabPost(t, "/groups/rename", "gtab7", map[string]any{"id": id, "name": "  Iceland 2027 "})
	test.IsEqualInt(t, status, 200)
	test.IsEqualString(t, answer["Name"].(string), "Iceland 2027")

	// Extending: later only, and days must be given
	status, answer = groupsTabPost(t, "/groups/extend", "gtab7", map[string]any{"id": id, "days": 90})
	test.IsEqualInt(t, status, 200)
	group, _ = storage.GetGroup(id)
	test.IsEqualInt64(t, int64(answer["Expires"].(float64)), group.Expires)
	test.IsEqualBool(t, answer["NeverExpires"].(bool), false)
	test.IsEqualString(t, answer["Closes"].(string), closesLabel(group, time.Now()))
	status, answer = groupsTabPost(t, "/groups/extend", "gtab7", map[string]any{"id": id, "days": 30})
	test.IsEqualInt(t, status, 400)
	test.IsEqualString(t, answer["ErrorMessage"].(string), storage.ErrGroupNotLater.Error())
	status, _ = groupsTabPost(t, "/groups/extend", "gtab7", map[string]any{"id": id})
	test.IsEqualInt(t, status, 400)
	status, _ = groupsTabPost(t, "/groups/extend", "gtab8", map[string]any{"id": id, "days": 0})
	test.IsEqualInt(t, status, http.StatusForbidden)

	// Resetting the code: the old one stops working, the new one works
	status, answer = groupsTabPost(t, "/groups/resetcode", "gtab7", map[string]any{"id": id})
	test.IsEqualInt(t, status, 200)
	newCode := answer["Code"].(string)
	test.IsNotEqualString(t, newCode, oldCode)
	group, _ = storage.GetGroup(id)
	test.IsEqualString(t, answer["InviteUrl"].(string), storage.GroupInviteUrl(group))
	status, _ = groupsTabPost(t, "/groups/join", "gtab61", map[string]any{"code": oldCode})
	test.IsEqualInt(t, status, 400)
	status, _ = groupsTabPost(t, "/groups/join", "gtab61", map[string]any{"code": newCode})
	test.IsEqualInt(t, status, 200)
	status, _ = groupsTabPost(t, "/groups/resetcode", "gtab8", map[string]any{"id": id})
	test.IsEqualInt(t, status, http.StatusForbidden)

	// Leaving: the creator cannot, a member can
	status, answer = groupsTabPost(t, "/groups/leave", "gtab7", map[string]any{"id": id})
	test.IsEqualInt(t, status, 400)
	test.IsEqualString(t, answer["ErrorMessage"].(string), storage.ErrCreatorCannotLeave.Error())
	status, _ = groupsTabPost(t, "/groups/leave", "gtab61", map[string]any{"id": id})
	test.IsEqualInt(t, status, 200)
	_, member = storage.GroupRole(group, 61)
	test.IsEqualBool(t, member, false)

	// Removing files: a member only their own, the creator any
	mine := accessFile("groupstabfile00007", func(f *models.File) { f.SHA1 = "groupstabsha7" })
	theirs := accessFile("groupstabfile00008", func(f *models.File) { f.SHA1 = "groupstabsha8"; f.UserId = 8 })
	defer database.DeleteMetaData(mine.Id)
	defer database.DeleteMetaData(theirs.Id)
	_, err := storage.AddFilesToGroup(group, []string{mine.Id}, models.User{Id: 7, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	_, err = storage.AddFilesToGroup(group, []string{theirs.Id}, models.User{Id: 8, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	status, answer = groupsTabPost(t, "/groups/removefiles", "gtab8", map[string]any{"id": id, "fileIds": []string{mine.Id, theirs.Id}})
	test.IsEqualInt(t, status, 200)
	test.IsEqualInt(t, int(answer["Removed"].(float64)), 1)
	status, answer = groupsTabPost(t, "/groups/removefiles", "gtab7", map[string]any{"id": id, "fileIds": []string{mine.Id}})
	test.IsEqualInt(t, status, 200)
	test.IsEqualInt(t, int(answer["Removed"].(float64)), 1)
	test.IsEqualInt(t, len(storage.GroupFiles(group)), 0)

	// Deleting: a member may not; removing a member is the creator's
	status, _ = groupsTabPost(t, "/groups/delete", "gtab8", map[string]any{"id": id})
	test.IsEqualInt(t, status, http.StatusForbidden)
	status, _ = groupsTabPost(t, "/groups/removemember", "gtab8", map[string]any{"id": id, "userId": 62})
	test.IsEqualInt(t, status, http.StatusForbidden)
	status, answer = groupsTabPost(t, "/groups/removemember", "gtab7", map[string]any{"id": id, "userId": 7})
	test.IsEqualInt(t, status, 400)
	test.IsEqualString(t, answer["ErrorMessage"].(string), storage.ErrCannotRemoveCreator.Error())
	status, _ = groupsTabPost(t, "/groups/removemember", "gtab7", map[string]any{"id": id, "userId": 8})
	test.IsEqualInt(t, status, 200)
	_, member = storage.GroupRole(group, 8)
	test.IsEqualBool(t, member, false)

	// The admin sees a group he is not in, marked as such, and may delete it
	_, page = groupsTabDo(t, "GET", "/groups", "gtab5", "", "")
	row = groupsTabRow(t, page, id)
	test.IsEqualBool(t, strings.Contains(row, "Iceland 2027"), true)
	test.IsEqualBool(t, strings.Contains(row, "not a member"), true)
	status, _ = groupsTabPost(t, "/groups/delete", "gtab5", map[string]any{"id": id})
	test.IsEqualInt(t, status, 200)
	_, ok = storage.GetGroup(id)
	test.IsEqualBool(t, ok, false)
	// The files stay
	_, ok = database.GetMetaDataById(mine.Id)
	test.IsEqualBool(t, ok, true)
	// Once deleted, its link is no longer valid
	resp, page = groupsTabDo(t, "GET", "/join?t="+group.InviteToken, "gtab62", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusNotFound)
	test.IsEqualBool(t, strings.Contains(page, "This invite link is no longer valid."), true)
}

func TestGroupsTabJoinLinks(t *testing.T) {
	groupsTabUsers()
	group, err := storage.CreateGroup("Link test", 30, models.User{Id: 7, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	defer database.DeleteGroup(group.Id)

	// /g/join only redirects to the top-level join page
	resp, _ := groupsTabDo(t, "GET", "/g/join?t=x", "", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusTemporaryRedirect)
	test.IsEqualString(t, resp.Header.Get("Location"), "../join?t=x")

	// Without a session, the join page remembers itself and signs in first
	resp, _ = groupsTabDo(t, "GET", "/join?t="+group.InviteToken, "", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusTemporaryRedirect)
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "login"), true)
	test.IsEqualString(t, cookieValue(resp.Cookies(), "fgs_return"), "join?t="+group.InviteToken)

	// Signing in then returns there: already signed in
	returnTo := &http.Cookie{Name: "fgs_return", Value: "join?t=" + group.InviteToken}
	resp, _ = groupsTabDo(t, "GET", "/login", "gtab61", "", "", returnTo)
	test.IsEqualInt(t, resp.StatusCode, http.StatusTemporaryRedirect)
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "join?t="+group.InviteToken), true)
	// and with an internal sign-in
	form := url.Values{"username": {"test"}, "password": {"adminadmin"}, "csrf-token": {csrftoken.Generate(csrftoken.TypeLogin)}}
	resp, _ = groupsTabDo(t, "POST", "/login", "", "application/x-www-form-urlencoded", form.Encode(), returnTo)
	test.IsEqualInt(t, resp.StatusCode, http.StatusTemporaryRedirect)
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "join?t="+group.InviteToken), true)
	// A forged target is never followed
	resp, _ = groupsTabDo(t, "GET", "/login", "gtab61", "", "", &http.Cookie{Name: "fgs_return", Value: "//evil.com"})
	test.IsEqualBool(t, strings.HasSuffix(resp.Header.Get("Location"), "admin"), true)

	// An unknown token
	resp, page := groupsTabDo(t, "GET", "/join?t=nosuchtoken", "gtab61", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusNotFound)
	test.IsEqualBool(t, strings.Contains(page, "This invite link is no longer valid."), true)
}

func TestGroupsTabJoinRateLimit(t *testing.T) {
	groupsTabUsers()
	ratelimiter.SetUnitTestMode(false)
	defer ratelimiter.SetUnitTestMode(true)
	for i := 1; i <= 10; i++ {
		status, _ := groupsTabPost(t, "/groups/join", "gtab8", map[string]any{"code": "ZZZ-ZZZ"})
		test.IsEqualInt(t, status, 400)
	}
	status, answer := groupsTabPost(t, "/groups/join", "gtab8", map[string]any{"code": "ZZZ-ZZZ"})
	test.IsEqualInt(t, status, http.StatusTooManyRequests)
	test.IsEqualBool(t, strings.Contains(answer["ErrorMessage"].(string), "Too many attempts"), true)
	// The join page's form gets a page of its own, also 429
	resp, page := groupsTabDo(t, "POST", "/groups/join", "gtab8", "application/x-www-form-urlencoded", "t=abc")
	test.IsEqualInt(t, resp.StatusCode, http.StatusTooManyRequests)
	test.IsEqualBool(t, strings.Contains(page, "Too many attempts"), true)
	test.IsEqualBool(t, strings.Contains(page, `href="../join?t=abc"`), true)
}

func TestClosesLabel(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	sameYear := time.Date(2026, 12, 12, 9, 0, 0, 0, time.Local)
	test.IsEqualString(t, closesLabel(models.Group{Expires: sameYear.Unix()}, now), "closes 12 Dec")
	nextYear := time.Date(2027, 1, 12, 9, 0, 0, 0, time.Local)
	test.IsEqualString(t, closesLabel(models.Group{Expires: nextYear.Unix()}, now), "closes 12 Jan 2027")
	test.IsEqualString(t, closesLabel(models.Group{Expires: 0}, now), "never closes")
}

// I1: every group and collection JSON endpoint answers 415 to a body not
// declared as JSON, so a same-site <form enctype="text/plain"> cannot forge
// one. Nothing changes.
func TestGroupEndpointsRequireJson(t *testing.T) {
	groupsTabUsers()
	group, err := storage.CreateGroup("Json only", 30, models.User{Id: 7, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	defer database.DeleteGroup(group.Id)
	collection, err := storage.CreateCollection(storage.CollectionParameters{Name: "Json only"}, models.User{Id: 7})
	if err != nil {
		// A collection needs a live file; the delete case then uses a made-up id
		collection = models.Collection{Id: "nosuchcollection"}
	}
	body := `{"id":"` + group.Id + `","name":"forged","fileIds":[],"collectionId":"x","days":30,"code":"ZZZ-ZZZ"}`
	for _, path := range []string{
		// GroupsTab.go (groupRequest)
		"/groups/create", "/groups/rename", "/groups/extend", "/groups/resetcode", "/groups/removemember",
		"/groups/leave", "/groups/delete", "/groups/removefiles",
		// GroupsAdd.go
		"/groups/add", "/groups/fromcollection",
		// GroupPage.go
		"/groups/nearkeep",
		// /groups/join: neither JSON nor its form
		"/groups/join",
		// Collections.go
		"/collections/create", "/collections/modify", "/collections/delete?id=" + collection.Id,
	} {
		for _, contentType := range []string{"text/plain", ""} {
			resp, _ := groupsTabDo(t, "POST", path, "gtab7", contentType, body)
			if resp.StatusCode != http.StatusUnsupportedMediaType {
				t.Errorf("%s with Content-Type %q: %d, want 415", path, contentType, resp.StatusCode)
			}
		}
	}
	stored, ok := storage.GetGroup(group.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, stored.Name, "Json only")
	test.IsEqualString(t, stored.JoinCode, group.JoinCode)
	// A GET deletes no collection either (405)
	resp, _ := groupsTabDo(t, "GET", "/collections/delete?id="+collection.Id, "gtab7", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusMethodNotAllowed)
}

// I1: the join page's form is refused from any other origin, before the rate
// limiter is charged; from the server's own origin it joins
func TestGroupJoinFormOrigin(t *testing.T) {
	groupsTabUsers()
	group, err := storage.CreateGroup("Origin check", 30, models.User{Id: 7, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	defer database.DeleteGroup(group.Id)
	form := "t=" + url.QueryEscape(group.InviteToken)
	formType := "application/x-www-form-urlencoded"

	for _, h := range []map[string]string{
		{"Origin": "https://split.example.com"},
		{"Origin": "null"},
		{"Referer": "https://split.example.com/attack.html"},
	} {
		h["Content-Type"] = formType
		resp, _ := groupsTabDoHeaders(t, "POST", "/groups/join", "gtab62", h, form)
		test.IsEqualInt(t, resp.StatusCode, http.StatusForbidden)
		_, member := storage.GroupRole(group, 62)
		test.IsEqualBool(t, member, false)
	}

	// The server's own origin (configuration ServerUrl) joins: 303 to the group
	resp, _ := groupsTabDoHeaders(t, "POST", "/groups/join", "gtab62",
		map[string]string{"Content-Type": formType, "Origin": "http://127.0.0.1:53843"}, form)
	test.IsEqualInt(t, resp.StatusCode, http.StatusSeeOther)
	_, member := storage.GroupRole(group, 62)
	test.IsEqualBool(t, member, true)
}

// M8: a signed-out member opening a group page is sent back to it after
// signing in
func TestGroupPageRemembersReturn(t *testing.T) {
	resp, _ := groupsTabDo(t, "GET", "/g?id=SomeGroup_1", "", "", "")
	test.IsEqualBool(t, resp.StatusCode >= 300 && resp.StatusCode < 400, true)
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "fgs_return" {
			found = true
			test.IsEqualString(t, c.Value, "g?id=SomeGroup_1")
		}
	}
	test.IsEqualBool(t, found, true)
}

// Every row of the Groups tab has "Copy join link", copying the group's
// invite link through ClipboardJS: for the creator, and for the admin on a
// group he is not in. Someone outside the group never sees the link.
func TestGroupsTabCopyJoinLink(t *testing.T) {
	groupsTabUsers()
	group, err := storage.CreateGroup("Join link copy", 30, models.User{Id: 7, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	defer database.DeleteGroup(group.Id)
	link := `data-clipboard-text="` + storage.GroupInviteUrl(group) + `"`

	for _, token := range []string{"gtab7", "gtab5"} {
		_, page := groupsTabDo(t, "GET", "/groups", token, "", "")
		row := groupsTabRow(t, page, group.Id)
		for _, want := range []string{link, "copyurl", `aria-label="Copy join link"`, "Copy join link"} {
			if !strings.Contains(row, want) {
				t.Errorf("%s: row lacks %q:\n%s", token, want, row)
			}
		}
		// the toast the button confirms with is on the page
		test.IsEqualBool(t, strings.Contains(page, `id="toastnotification"`), true)
	}
	_, page := groupsTabDo(t, "GET", "/groups", "gtab61", "", "")
	test.IsEqualBool(t, strings.Contains(page, group.InviteToken), false)
}
