//go:build !integration && test

package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/blocklist"
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type blockTestClaims struct{}

func (blockTestClaims) Claims(v interface{}) error { return json.Unmarshal([]byte(`{}`), v) }

// Not parallel: it resets the process-wide sign-in lists and wires them again
func TestBlockedOauthSignInIsRefusedThroughWiring(t *testing.T) {
	authentication.ResetSignInForTest()
	wireSignIn()
	test.IsEqualInt(t, authentication.GuardCountForTest(), 1)
	admin := models.User{Id: 9301, Name: "blockwiring-admin@admin.wiring.example", UserLevel: models.UserLevelAdmin}
	database.SaveUser(admin, false)
	defer database.DeleteUser(admin.Id)
	test.IsNil(t, blocklist.Block("@blocked.wiring.example", admin))
	defer blocklist.Unblock("@blocked.wiring.example")

	info := authentication.OAuthUserInfo{Subject: "s", Email: "New@Blocked.Wiring.Example", ClaimsSent: blockTestClaims{}}
	w := httptest.NewRecorder()
	test.IsNil(t, authentication.CheckOauthUserAndRedirect(w, httptest.NewRequest("GET", "/x", nil), info))
	_, created := database.GetUserByName("New@Blocked.Wiring.Example")
	test.IsEqualBool(t, created, false)
	test.IsEqualBool(t, strings.Contains(w.Header().Get("Location"), "error"), true)

	// control: an address that is not blocked is created, so the check above can fail
	info.Email = "Fine@Allowed.Wiring.Example"
	test.IsNil(t, authentication.CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	fine, created := database.GetUserByName("Fine@Allowed.Wiring.Example")
	test.IsEqualBool(t, created, true)
	database.DeleteUser(fine.Id)

	// password login goes through the same guard
	pwUser := models.User{Id: 9302, Name: "pwuser@blocked.wiring.example", UserLevel: models.UserLevelUser,
		Password: configuration.HashPassword("pw-secret-1", false, "")}
	database.SaveUser(pwUser, false)
	defer database.DeleteUser(pwUser.Id)
	_, ok, _ := authentication.IsCorrectUsernameAndPassword(pwUser.Name, "pw-secret-1", csrftoken.Generate(csrftoken.TypeLogin))
	test.IsEqualBool(t, ok, false) // blocked by "@blocked.wiring.example"
	blocklist.Unblock("@blocked.wiring.example")
	_, ok, _ = authentication.IsCorrectUsernameAndPassword(pwUser.Name, "pw-secret-1", csrftoken.Generate(csrftoken.TypeLogin))
	test.IsEqualBool(t, ok, true) // control: unblocked, same password works
}

// A session that exists when the entry is saved, without Block's sign-out
// (the race), is refused on its next use and deleted
func TestSessionOfLaterBlockedPersonIsRefusedThroughWiring(t *testing.T) {
	authentication.ResetSignInForTest()
	wireSignIn()
	person := models.User{Id: 9303, Name: "late@late.wiring.example", UserLevel: models.UserLevelUser}
	database.SaveUser(person, false)
	defer database.DeleteUser(person.Id)
	database.SaveSession("late-wiring-session", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: person.Id})
	defer database.DeleteSession("late-wiring-session")
	check := func() (models.User, bool) {
		r := httptest.NewRequest("GET", "/x", nil)
		r.AddCookie(&http.Cookie{Name: "session_token", Value: "late-wiring-session"})
		return sessionmanager.IsValidSession(httptest.NewRecorder(), r, false, 1)
	}
	_, ok := check()
	test.IsEqualBool(t, ok, true)
	database.SaveBlock(models.BlockEntry{Entry: "@late.wiring.example", Added: 1})
	defer database.DeleteBlock("@late.wiring.example")
	_, ok = check()
	test.IsEqualBool(t, ok, false)
	_, found := database.GetSession("late-wiring-session")
	test.IsEqualBool(t, found, false)
}

func TestBlockEndpoints(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9310)
	block := requireLogin(blockEntry, false, false)
	unblock := requireLogin(unblockEntry, false, false)
	const js = "application/json"

	// a non-admin
	w := asSessionDirect(block, "POST", "/users/block", fx.bSession, js, `{"entry":"@nonadmin.block.example"}`)
	test.IsEqualInt(t, w.Code, http.StatusForbidden)
	test.IsEqualBool(t, blocklistHas("@nonadmin.block.example"), false)

	// wrong method and wrong content type
	w = asSessionDirect(block, "GET", "/users/block", fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusMethodNotAllowed)
	w = asSessionDirect(block, "POST", "/users/block", fx.adminSession, "application/x-www-form-urlencoded", "entry=@form.block.example")
	test.IsEqualInt(t, w.Code, http.StatusUnsupportedMediaType)
	test.IsEqualBool(t, blocklistHas("@form.block.example"), false)
	w = asSessionDirect(unblock, "GET", "/users/unblock", fx.adminSession, "", "")
	test.IsEqualInt(t, w.Code, http.StatusMethodNotAllowed)

	// an admin cannot be blocked
	w = asSessionDirect(block, "POST", "/users/block", fx.adminSession, js, `{"entry":"`+fx.otherAdmin.Name+`"}`)
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), "You cannot block an admin."), true)

	// a bad entry
	w = asSessionDirect(block, "POST", "/users/block", fx.adminSession, js, `{"entry":"nobody"}`)
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)

	// twice is the same as once, and it signs the person out
	for i := 0; i < 2; i++ {
		w = asSessionDirect(block, "POST", "/users/block", fx.adminSession, js, `{"entry":"`+strings.ToUpper(fx.c.Name)+`"}`)
		test.IsEqualInt(t, w.Code, http.StatusOK)
	}
	test.IsEqualInt(t, countEntries(fx.c.Name), 1)
	_, ok := database.GetSession(fx.bSession)
	test.IsEqualBool(t, ok, true) // b was not blocked

	// unblock removes it
	w = asSessionDirect(unblock, "POST", "/users/unblock", fx.adminSession, js, `{"entry":"`+fx.c.Name+`"}`)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, blocklistHas(strings.ToLower(fx.c.Name)), false)
}

func TestBlockEndsTheBlockedPersonsSession(t *testing.T) {
	t.Parallel()
	fx := newViewAsFixture(t, 9320)
	block := requireLogin(blockEntry, false, false)
	w := asSessionDirect(block, "POST", "/users/block", fx.adminSession, "application/json", `{"entry":"`+fx.b.Name+`"}`)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	defer blocklist.Unblock(fx.b.Name)
	_, ok := database.GetSession(fx.bSession)
	test.IsEqualBool(t, ok, false)
}

func blocklistHas(entry string) bool { return countEntries(entry) > 0 }

func countEntries(entry string) int {
	count := 0
	for _, b := range blocklist.List() {
		if b.Entry == strings.ToLower(entry) {
			count++
		}
	}
	return count
}

func TestUsersPageShowsBlockedSection(t *testing.T) {
	admin := models.User{Id: 9330, Name: "blockpage-admin@admin.page.example", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}
	person := models.User{Id: 9331, Name: "blockpage-person@page.example", UserLevel: models.UserLevelUser}
	database.SaveUser(admin, false)
	database.SaveUser(person, false)
	defer database.DeleteUser(admin.Id)
	defer database.DeleteUser(person.Id)
	test.IsNil(t, blocklist.Block("@spam.page.example", admin))
	defer blocklist.Unblock("@spam.page.example")

	page := getPage(t, "http://localhost:53843/users", filesAdminSession())
	test.IsEqualBool(t, strings.Contains(page, `<h5>Blocked</h5>`), true)
	test.IsEqualBool(t, strings.Contains(page, `placeholder="name@example.com or @example.com"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-fgs-unblock="@spam.page.example"`), true)
	row := userRow(t, page, "9331")
	test.IsEqualBool(t, strings.Contains(row, "Block this person"), true)
	test.IsEqualBool(t, strings.Contains(row, `data-fgs-block-user="blockpage-person@page.example"`), true)
	adminRow := userRow(t, page, "9330")
	test.IsEqualBool(t, strings.Contains(adminRow, "Block this person"), false)
}

func TestUsersPageHidesBlockedFromNonAdminManager(t *testing.T) {
	manager := models.User{Id: 9340, Name: "blockmgr@mgr.example", UserLevel: models.UserLevelUser, Permissions: models.UserPermManageUsers}
	person := models.User{Id: 9341, Name: "blockmgr-person@mgr.example", UserLevel: models.UserLevelUser}
	database.SaveUser(manager, false)
	database.SaveUser(person, false)
	database.SaveSession("blockmgr-session", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: manager.Id})
	defer database.DeleteUser(manager.Id)
	defer database.DeleteUser(person.Id)
	defer database.DeleteSession("blockmgr-session")

	page := getPage(t, "http://localhost:53843/users", test.Cookie{Name: "session_token", Value: "blockmgr-session"})
	test.IsEqualBool(t, strings.Contains(page, "blockmgr-person@mgr.example"), true)
	test.IsEqualBool(t, strings.Contains(page, `id="blocked-card"`), false)
	test.IsEqualBool(t, strings.Contains(page, "Block this person"), false)
}
