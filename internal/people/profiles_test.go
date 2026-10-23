//go:build test

package people

import (
	"errors"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestShownNameOrder(t *testing.T) {
	u := models.User{Id: 501, Name: "anna.m@example.com"}
	test.IsEqualString(t, Name(u), "anna.m")
	OnSignIn(u, map[string]any{"name": "Anna Muster"}, true)
	test.IsEqualString(t, Name(u), "Anna Muster")
	test.IsNil(t, SetDisplayName(u.Id, "  Annie  "))
	test.IsEqualString(t, Name(u), "Annie")
	test.IsNil(t, SetDisplayName(u.Id, ""))
	test.IsEqualString(t, Name(u), "Anna Muster")
	test.IsNotNil(t, SetDisplayName(u.Id, strings.Repeat("x", 41)))
	test.IsNotNil(t, SetDisplayName(u.Id, "bad\u0000name"))
	OnSignIn(u, map[string]any{"name": "Anna M. Muster"}, false) // Google name refreshes, display name untouched
	_ = SetDisplayName(u.Id, "Annie")
	test.IsEqualString(t, Name(u), "Annie")
}

func TestDisplayNameRules(t *testing.T) {
	u := models.User{Id: 504, Name: "dora@example.com"}
	// 40 characters, not bytes
	test.IsNil(t, SetDisplayName(u.Id, strings.Repeat("ü", 40)))
	test.IsEqualString(t, Name(u), strings.Repeat("ü", 40))
	err := SetDisplayName(u.Id, strings.Repeat("ü", 41))
	test.IsEqualBool(t, errors.Is(err, ErrDisplayName), true)
	test.IsEqualString(t, Name(u), strings.Repeat("ü", 40)) // a refusal changes nothing
	test.IsNotNil(t, SetDisplayName(u.Id, "tab\there"))
	// An invalid Google name is ignored, a valid one refreshes it
	OnSignIn(u, map[string]any{"name": "bad\u0007"}, false)
	p, _ := database.GetProfile(u.Id)
	test.IsEqualString(t, p.GoogleName, "")
	OnSignIn(u, map[string]any{"name": "  Dora Explorer "}, false)
	p, _ = database.GetProfile(u.Id)
	test.IsEqualString(t, p.GoogleName, "Dora Explorer")
	// A missing or non-string claim keeps the old Google name
	OnSignIn(u, map[string]any{"name": 42}, false)
	OnSignIn(u, map[string]any{}, false)
	p, _ = database.GetProfile(u.Id)
	test.IsEqualString(t, p.GoogleName, "Dora Explorer")
	database.DeleteUser(u.Id)
	_, ok := database.GetProfile(u.Id)
	test.IsEqualBool(t, ok, false)
}

func TestPreferencesPartialUpdate(t *testing.T) {
	on, off := true, false
	SetPreferences(502, &on, nil)
	test.IsEqualBool(t, WantsDownloadEmails(502), true)
	SetPreferences(502, nil, &on)
	test.IsEqualBool(t, WantsDownloadEmails(502), true) // untouched
	p, _ := database.GetProfile(502)
	test.IsEqualBool(t, p.UseInitials, true)
	SetPreferences(502, &off, nil)
	test.IsEqualBool(t, WantsDownloadEmails(502), false)
	p, _ = database.GetProfile(502)
	test.IsEqualBool(t, p.UseInitials, true) // untouched
	test.IsEqualBool(t, WantsDownloadEmails(9999), false) // default off
}
