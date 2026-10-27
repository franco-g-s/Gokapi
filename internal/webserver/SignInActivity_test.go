//go:build !integration && test

package webserver

import (
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type activityClaims struct{}

func (activityClaims) Claims(v interface{}) error { return nil }

// The hook wired in wireSignIn records first_signin for a new account
// only, through the real OAuth sign-in (Part 4, A)
func TestFirstSignInThroughWiredHook(t *testing.T) {
	authentication.ResetSignInForTest()
	wireSignIn()
	defer func() {
		authentication.ResetSignInForTest()
		wireSignIn()
	}()
	const email = "first-signin@example.com"
	info := authentication.OAuthUserInfo{Subject: "firstsignin", Email: email, ClaimsSent: activityClaims{}}
	test.IsNil(t, authentication.CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	user, ok := database.GetUserByName(email)
	test.IsEqualBool(t, ok, true)
	defer database.DeleteUser(user.Id)
	test.IsNil(t, authentication.CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	n := 0
	for _, a := range database.GetActivitySince(0, 100000) {
		if a.Kind == models.ActivityFirstSignIn && a.ActorId == user.Id {
			n++
		}
	}
	test.IsEqualInt(t, n, 1)
}
