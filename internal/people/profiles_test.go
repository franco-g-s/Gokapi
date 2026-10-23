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
	// Fix round 1: the display name is set before the sign-in, so "untouched"
	// is really checked (the brief's order set it only afterwards)
	test.IsNil(t, SetDisplayName(u.Id, "Annie"))
	OnSignIn(u, map[string]any{"name": "Anna M. Muster"}, false) // Google name refreshes, display name untouched
	test.IsEqualString(t, Name(u), "Annie")
	p, _ := database.GetProfile(u.Id)
	test.IsEqualString(t, p.GoogleName, "Anna M. Muster")
	test.IsEqualString(t, p.DisplayName, "Annie")
	test.IsEqualString(t, DefaultName(u), "Anna M. Muster")
}

// Fix round 1: format characters (bidi overrides, zero-width) are refused,
// and a name must contain something visible
func TestDisplayNameRefusesInvisibleCharacters(t *testing.T) {
	u := models.User{Id: 507, Name: "fritz@example.com"}
	t.Cleanup(func() { database.DeleteUser(u.Id) })
	for _, bad := range []string{
		"Fritz\u202Egnp.exe", // right-to-left override
		"Fr\u200Bitz",        // zero-width space
		"\u200B\u200B\u200B", // nothing visible at all
		"\u2066Fritz\u2069",  // isolates
		"\u00AD",             // soft hyphen only (Cf)
	} {
		err := SetDisplayName(u.Id, bad)
		if !errors.Is(err, ErrDisplayName) {
			t.Errorf("%q was accepted", bad)
		}
	}
	test.IsEqualString(t, Name(u), "fritz")
	test.IsNil(t, SetDisplayName(u.Id, "  Zoë Müller-Lüdenscheidt  "))
	test.IsEqualString(t, Name(u), "Zoë Müller-Lüdenscheidt")
	test.IsNil(t, SetDisplayName(u.Id, "e\u0301")) // a letter with a combining accent
	test.IsEqualString(t, Name(u), "e\u0301")

	// The same rule for the Google name: an invalid one is dropped
	OnSignIn(u, map[string]any{"name": "Good Name"}, false)
	OnSignIn(u, map[string]any{"name": "Evil\u202EName"}, false)
	OnSignIn(u, map[string]any{"name": "\u200B"}, false)
	p, _ := database.GetProfile(u.Id)
	test.IsEqualString(t, p.GoogleName, "Good Name")
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
