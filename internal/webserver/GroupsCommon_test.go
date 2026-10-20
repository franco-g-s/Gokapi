//go:build !integration && test

package webserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

func TestDisplayName(t *testing.T) {
	test.IsEqualString(t, displayName(models.User{Name: "anna.mueller@gmail.com"}), "anna.mueller")
	test.IsEqualString(t, displayName(models.User{Name: "Franco"}), "Franco")
	test.IsEqualString(t, displayName(models.User{Name: "@odd"}), "@odd")
}

// listAs calls /groups/list as this user
func listAs(t *testing.T, user models.User, query string) map[string]groupListItem {
	t.Helper()
	w := httptest.NewRecorder()
	listGroups(w, authentication.SetUserInRequest(httptest.NewRequest("GET", "/groups/list"+query, nil), user))
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualString(t, w.Header().Get("cache-control"), "no-store, no-cache")
	var items []groupListItem
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &items))
	result := map[string]groupListItem{}
	for _, item := range items {
		result[item.Id] = item
	}
	return result
}

func TestListGroups(t *testing.T) {
	user := models.User{Id: 7, Name: "User", UserLevel: models.UserLevelUser}
	other := models.User{Id: 8, Name: "anna@example.com", UserLevel: models.UserLevelUser}
	admin := models.User{Id: 5, Name: "Test", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	database.SaveUser(other, false)

	mine, err := storage.CreateGroup("Mine", 30, user)
	test.IsNil(t, err)
	defer database.DeleteGroup(mine.Id)
	joined, err := storage.CreateGroup("Joined", 0, other)
	test.IsNil(t, err)
	defer database.DeleteGroup(joined.Id)
	_, err = storage.JoinGroupByToken(joined.InviteToken, user)
	test.IsNil(t, err)
	foreign, err := storage.CreateGroup("Foreign", 60, other)
	test.IsNil(t, err)
	defer database.DeleteGroup(foreign.Id)
	closed, err := storage.CreateGroup("Closed", 30, user)
	test.IsNil(t, err)
	defer database.DeleteGroup(closed.Id)
	closed.Expires = time.Now().Unix() - 1
	database.SaveGroup(closed)

	f := accessFile("groupslist000000001", func(f *models.File) { f.SHA1 = "groupslistsha" })
	_, err = storage.AddFilesToGroup(mine, []string{f.Id}, user)
	test.IsNil(t, err)

	items := listAs(t, user, "")
	test.IsEqualString(t, items[mine.Id].Role, models.GroupRoleCreator)
	test.IsEqualString(t, items[mine.Id].Name, "Mine")
	test.IsEqualInt(t, items[mine.Id].Files, 1)
	test.IsEqualInt(t, items[mine.Id].Members, 1)
	test.IsEqualInt64(t, items[mine.Id].Expires, mine.Expires)
	test.IsEqualString(t, items[joined.Id].Role, models.GroupRoleMember)
	test.IsEqualBool(t, items[joined.Id].NeverExpires, true)
	test.IsEqualInt(t, items[joined.Id].Members, 2)
	_, seen := items[foreign.Id]
	test.IsEqualBool(t, seen, false)
	_, seen = items[closed.Id]
	test.IsEqualBool(t, seen, false)

	// ?all=1 is ignored for anyone but the admin
	items = listAs(t, user, "?all=1")
	_, seen = items[foreign.Id]
	test.IsEqualBool(t, seen, false)

	// The admin sees every live group, with no role in those they are not in
	items = listAs(t, admin, "?all=1")
	_, seen = items[foreign.Id]
	test.IsEqualBool(t, seen, true)
	test.IsEqualString(t, items[foreign.Id].Role, "")
	_, seen = items[closed.Id]
	test.IsEqualBool(t, seen, false)
	// and without it, only their own
	items = listAs(t, admin, "")
	_, seen = items[foreign.Id]
	test.IsEqualBool(t, seen, false)

	// Members see the part of the name before "@"
	names := memberNames(joined)
	test.IsEqualString(t, names[8], "anna")
	test.IsEqualString(t, names[7], "user") // SaveUser stores names lowercased
	names = memberNames(mine)
	test.IsEqualString(t, names[7], "user") // SaveUser stores names lowercased

	// Unauthenticated, on the real server: 401
	resp, err := http.Get("http://localhost:53843/groups/list")
	test.IsNil(t, err)
	resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, http.StatusUnauthorized)
}

func TestGroupJsonHelpers(t *testing.T) {
	w := httptest.NewRecorder()
	test.IsEqualBool(t, postOnly(w, httptest.NewRequest("GET", "/groups/x", nil)), false)
	test.IsEqualInt(t, w.Code, http.StatusMethodNotAllowed)
	test.IsEqualString(t, w.Header().Get("Allow"), "POST")
	w = httptest.NewRecorder()
	test.IsEqualBool(t, postOnly(w, httptest.NewRequest("POST", "/groups/x", nil)), true)
	test.IsEqualInt(t, w.Code, 200)

	w = httptest.NewRecorder()
	writeJsonError(w, http.StatusBadRequest, errors.New(`no "group"`))
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	var body map[string]string
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &body))
	test.IsEqualString(t, body["Result"], "error")
	test.IsEqualString(t, body["ErrorMessage"], `no "group"`)
	test.IsEqualString(t, w.Header().Get("Content-Type"), "application/json; charset=UTF-8")

	var v struct{ Name string }
	test.IsNil(t, readJsonBody(httptest.NewRequest("POST", "/", strings.NewReader(`{"Name":"x"}`)), &v))
	test.IsEqualString(t, v.Name, "x")
	big := `{"Name":"` + strings.Repeat("a", 1<<20) + `"}`
	test.IsNotNil(t, readJsonBody(httptest.NewRequest("POST", "/", strings.NewReader(big)), &v))
}
