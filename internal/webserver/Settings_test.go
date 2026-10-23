//go:build !integration && test

package webserver

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/viewas"
)

// settingsUsers saves the people of the Settings and avatar tests, with
// their own sessions: 9400 Anna (with a photo, in the tests that add one),
// 9401 a stranger, 9402 who shares a group with Anna. Profiles go with them.
func settingsUsers(t *testing.T) {
	t.Helper()
	users := []models.User{
		{Id: 9400, Name: "anna.s1@example.com", UserLevel: models.UserLevelUser},
		{Id: 9401, Name: "stranger.s1@example.com", UserLevel: models.UserLevelUser},
		{Id: 9402, Name: "mate.s1@example.com", UserLevel: models.UserLevelUser},
	}
	for _, u := range users {
		database.SaveUser(u, false)
		database.SaveSession("s1session"+itoa(u.Id), models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: u.Id})
	}
	database.SaveSession("s1session5", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 5})
	t.Cleanup(func() {
		for _, u := range users {
			database.DeleteSession("s1session" + itoa(u.Id))
			database.DeleteUser(u.Id)
		}
		database.DeleteSession("s1session5")
	})
}

func checkedSwitch(body, id string) bool {
	return regexp.MustCompile(`id="` + id + `"[^>]*\schecked`).MatchString(body)
}

func TestSettingsPage(t *testing.T) {
	settingsUsers(t)
	on := true
	people.SetPreferences(9400, &on, nil)
	test.IsNil(t, people.SetDisplayName(9400, "Anna <Muster>"))

	resp, body := groupsTabDo(t, "GET", "/settings", "s1session9400", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
	for _, want := range []string{"Settings", "Your name", "Your photo", "Emails", "Display name",
		"Shown to the people you share with. Leave empty to use your Google name.",
		"Use initials instead of my Google photo", "Email me when my files are downloaded",
		"At most one email a day, at 19:00, and only if someone downloaded something.", "Save",
		"Anna &lt;Muster&gt;", "anna.s1@example.com", `data-default="Saved"`} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page is missing %q", want)
		}
	}
	test.IsEqualBool(t, strings.Contains(body, "Anna <Muster>"), false) // escaped
	// Fix round 1: the placeholder is what an empty field falls back to,
	// here the email prefix, even with a display name set and no Google name
	test.IsEqualBool(t, strings.Contains(body, `placeholder="anna.s1"`), true)
	test.IsEqualBool(t, strings.Contains(body, "Your account has no email address"), false)
	test.IsEqualBool(t, checkedSwitch(body, "settingsEmailDownloads"), true)
	test.IsEqualBool(t, checkedSwitch(body, "settingsUseInitials"), false)

	// tester (user 7, "User") has no email address
	resp, body = groupsTabDo(t, "GET", "/settings", "validsession", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(body, "Your account has no email address, so it cannot get emails."), true)
	test.IsEqualBool(t, regexp.MustCompile(`id="settingsEmailDownloads"[^>]*\sdisabled`).MatchString(body), true)
	tester, _ := database.GetUser(7)
	if m := regexp.MustCompile(`id="settingsShownName"[^>]*>([^<]*)<`).FindStringSubmatch(body); m == nil || m[1] != people.Name(tester) || m[1] == "" {
		t.Errorf("shown name of user 7: %v", m)
	}

	// No session: the login page
	resp, _ = groupsTabDo(t, "GET", "/settings", "", "", "")
	test.IsEqualBool(t, resp.StatusCode >= 300 && resp.StatusCode < 400, true)
	test.IsEqualBool(t, strings.Contains(resp.Header.Get("Location"), "login"), true)
}

func TestSettingsSave(t *testing.T) {
	settingsUsers(t)
	code, answer := groupsTabPost(t, "/settings/save", "s1session9400", map[string]any{"displayName": "  Annie  "})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, answer["Result"].(string), "OK")
	test.IsEqualString(t, answer["Name"].(string), "Annie")
	test.IsEqualBool(t, people.WantsDownloadEmails(9400), false)

	// Partial: only the fields sent change
	code, answer = groupsTabPost(t, "/settings/save", "s1session9400", map[string]any{"emailDownloads": true})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, answer["Name"].(string), "Annie")
	test.IsEqualBool(t, people.WantsDownloadEmails(9400), true)
	code, _ = groupsTabPost(t, "/settings/save", "s1session9400", map[string]any{"useInitials": true})
	test.IsEqualInt(t, code, http.StatusOK)
	p := people.Of(9400)
	test.IsEqualBool(t, p.UseInitials && p.EmailDownloads, true)
	test.IsEqualString(t, p.DisplayName, "Annie")

	// Empty goes back to the default
	code, answer = groupsTabPost(t, "/settings/save", "s1session9400", map[string]any{"displayName": ""})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, answer["Name"].(string), "anna.s1")

	// 41 characters: 400, nothing changes, the other fields included
	code, answer = groupsTabPost(t, "/settings/save", "s1session9400",
		map[string]any{"displayName": strings.Repeat("x", 41), "emailDownloads": false})
	test.IsEqualInt(t, code, http.StatusBadRequest)
	test.IsEqualString(t, answer["ErrorMessage"].(string), people.ErrDisplayName.Error())
	test.IsEqualBool(t, people.WantsDownloadEmails(9400), true)

	// No email address: the email switch cannot be turned on
	code, _ = groupsTabPost(t, "/settings/save", "validsession", map[string]any{"emailDownloads": true})
	test.IsEqualInt(t, code, http.StatusBadRequest)
	test.IsEqualBool(t, people.WantsDownloadEmails(7), false)

	// D20: POST + JSON only
	resp, _ := groupsTabDo(t, "GET", "/settings/save", "s1session9400", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusMethodNotAllowed)
	resp, _ = groupsTabDo(t, "POST", "/settings/save", "s1session9400", "application/x-www-form-urlencoded", "displayName=x")
	test.IsEqualInt(t, resp.StatusCode, http.StatusUnsupportedMediaType)
	resp, _ = groupsTabDo(t, "POST", "/settings/save", "", "application/json", `{"displayName":"x"}`)
	test.IsEqualInt(t, resp.StatusCode, http.StatusUnauthorized)
}

func TestSettingsDuringViewAs(t *testing.T) {
	test.IsEqualBool(t, viewas.Allowed("GET", "/settings"), true)
	test.IsEqualBool(t, viewas.Allowed("GET", "/avatar"), true)
	test.IsEqualBool(t, viewas.Allowed("POST", "/settings/save"), false)
	test.IsEqualBool(t, viewas.Allowed("POST", "/seed/photo"), false)
	fx := newViewAsFixture(t, 9410)
	test.IsNil(t, people.SetDisplayName(fx.b.Id, "Bea Target"))
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))

	resp, body := groupsTabDo(t, "GET", "/settings", fx.adminSession, "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(body, "Bea Target"), true) // the target's settings
	test.IsEqualBool(t, regexp.MustCompile(`id="settingsDisplayName"[^>]*\sdisabled`).MatchString(body), true)
	test.IsEqualBool(t, strings.Contains(body, `id="settingsSave"`), false)
	test.IsEqualBool(t, strings.Contains(body, "html.fgs-viewas"), true)

	code, _ := groupsTabPost(t, "/settings/save", fx.adminSession, map[string]any{"displayName": "Hacked"})
	test.IsEqualInt(t, code, http.StatusForbidden)
	test.IsEqualString(t, people.Of(fx.b.Id).DisplayName, "Bea Target")
}
