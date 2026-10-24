//go:build test

package blocklist

import (
	"errors"
	"os"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

var admin = models.User{Id: 4001, Name: "blockadmin@admin.example", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	database.SaveUser(admin, false)
	exitVal := m.Run()
	testconfiguration.Delete()
	os.Exit(exitVal)
}

func reset(t *testing.T) {
	t.Helper()
	for _, b := range List() {
		Unblock(b.Entry)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	test.IsNil(t, err)
}

func countSessions(userId int) int {
	count := 0
	for _, id := range []string{"k-s1", "k-s2"} {
		if s, ok := database.GetSession(id); ok && s.UserId == userId {
			count++
		}
	}
	return count
}

func TestMatches(t *testing.T) {
	reset(t)
	must(t, Block("Spammer@Example.com", admin))
	must(t, Block("@gmail.com", admin))
	cases := map[string]bool{
		"spammer@example.com": true, "SPAMMER@EXAMPLE.COM": true, "other@example.com": false,
		"x@gmail.com": true, "x@GMAIL.com": true, "x@evilgmail.com": false, "x@mail.gmail.com": false,
		"gmail.com": false, "": false, " x@gmail.com ": true,
	}
	for email, want := range cases {
		if Matches(email) != want {
			t.Errorf("Matches(%q) = %v, want %v", email, !want, want)
		}
	}
}

func TestBlockRefusesAdminsAndBadEntries(t *testing.T) {
	reset(t)
	test.IsEqualBool(t, errors.Is(Block(admin.Name, admin), ErrAdmin), true)
	test.IsEqualBool(t, errors.Is(Block("@admin.example", admin), ErrAdmin), true)
	test.IsEqualBool(t, errors.Is(Block("@ADMIN.example", admin), ErrAdmin), true)
	test.IsEqualInt(t, len(List()), 0)
	for _, bad := range []string{"", "nobody", "@", "a@", "@@x.com", "a b@x.com", "a@nodot"} {
		if !errors.Is(Block(bad, admin), ErrEntry) {
			t.Errorf("Block(%q) was not ErrEntry", bad)
		}
	}
}

func TestBlockIsIdempotentAndUnblockRemoves(t *testing.T) {
	reset(t)
	must(t, Block("a@x.example", admin))
	must(t, Block("A@X.example", admin))
	test.IsEqualInt(t, len(List()), 1)
	Unblock("A@x.example")
	test.IsEqualInt(t, len(List()), 0)
	test.IsEqualBool(t, Matches("a@x.example"), false)
}

func TestBlockEndsSessionsOfEveryoneCovered(t *testing.T) {
	reset(t)
	u := models.User{Id: 4002, Name: "victim@sessions.example", UserLevel: models.UserLevelUser}
	other := models.User{Id: 4003, Name: "bystander@fine.example", UserLevel: models.UserLevelUser}
	database.SaveUser(u, false)
	database.SaveUser(other, false)
	defer database.DeleteUser(u.Id)
	defer database.DeleteUser(other.Id)
	database.SaveSession("k-s1", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: u.Id})
	database.SaveSession("k-s2", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: u.Id})
	database.SaveSession("k-s3", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: other.Id})
	defer database.DeleteSession("k-s3")
	test.IsEqualInt(t, countSessions(u.Id), 2)
	must(t, Block("@sessions.example", admin))
	test.IsEqualInt(t, countSessions(u.Id), 0)
	_, ok := database.GetSession("k-s3")
	test.IsEqualBool(t, ok, true)
	reset(t)
}

func TestGuard(t *testing.T) {
	reset(t)
	var told []string
	old := onBlocked
	onBlocked = func(email string) { told = append(told, email) }
	defer func() { onBlocked = old }()
	test.IsNil(t, Guard("fine@x.example"))
	must(t, Block("@x.example", admin))
	test.IsEqualBool(t, errors.Is(Guard("Who@X.example"), authentication.ErrSignInRefused), true)
	test.IsEqualInt(t, len(told), 1)
	test.IsEqualString(t, told[0], "Who@X.example")
	reset(t)
}
