//go:build test

package authentication

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
)

func TestSignInGuardRefusesBeforeCreate(t *testing.T) {
	Init(modelOauth)
	ResetSignInForTest()
	defer ResetSignInForTest()
	AddSignInGuard(func(email string) error {
		if email == "blocked@example.com" {
			return ErrSignInRefused
		}
		return nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth-callback", nil)
	info := OAuthUserInfo{Subject: "s1", Email: "blocked@example.com", ClaimsSent: testInfo{Output: []byte(`{}`)}}
	test.IsNil(t, CheckOauthUserAndRedirect(w, r, info))
	_, exists := database.GetUserByName("blocked@example.com")
	test.IsEqualBool(t, exists, false) // refused before getOrCreateUser
	test.IsEqualBool(t, w.Code == http.StatusTemporaryRedirect && strings.Contains(w.Header().Get("Location"), "error"), true)
}

func TestSignInHookSeesCreated(t *testing.T) {
	Init(modelOauth)
	ResetSignInForTest()
	defer ResetSignInForTest()
	var calls []bool
	var names []any
	AddSignInHook(func(u models.User, claims map[string]any, created bool) {
		calls = append(calls, created)
		names = append(names, claims["name"])
	})
	info := OAuthUserInfo{Subject: "s2", Email: "newperson@example.com", ClaimsSent: testInfo{Output: []byte(`{"name":"New Person"}`)}}
	test.IsNil(t, CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	test.IsNil(t, CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	test.IsEqualInt(t, len(calls), 2)
	test.IsEqualBool(t, calls[0], true)
	test.IsEqualBool(t, calls[1], false)
	test.IsEqualString(t, names[0].(string), "New Person")
}

func TestPasswordLoginRunsGuards(t *testing.T) {
	ResetSignInForTest()
	defer ResetSignInForTest()
	_, ok, _ := IsCorrectUsernameAndPassword("test", "adminadmin", csrftoken.Generate(csrftoken.TypeLogin))
	test.IsEqualBool(t, ok, true)
	AddSignInGuard(func(email string) error { return ErrSignInRefused })
	_, ok, csrfOk := IsCorrectUsernameAndPassword("test", "adminadmin", csrftoken.Generate(csrftoken.TypeLogin))
	test.IsEqualBool(t, ok, false)
	test.IsEqualBool(t, csrfOk, true)
}
