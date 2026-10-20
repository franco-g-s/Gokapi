package authentication

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/test"
)

// returnToCookie returns the fgs_return cookie the response set, or nil
func returnToCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieReturnTo {
			return c
		}
	}
	return nil
}

// popWith calls PopReturnTo for a request carrying this fgs_return value
func popWith(value string) (string, *httptest.ResponseRecorder) {
	r := httptest.NewRequest("GET", "/login", nil)
	r.AddCookie(&http.Cookie{Name: cookieReturnTo, Value: value})
	w := httptest.NewRecorder()
	return PopReturnTo(w, r), w
}

func TestReturnToRoundTrip(t *testing.T) {
	w := httptest.NewRecorder()
	SetReturnTo(w, "join?t=abc")
	c := returnToCookie(w)
	test.IsEqualBool(t, c != nil, true)
	test.IsEqualString(t, c.Value, "join?t=abc")
	test.IsEqualBool(t, c.HttpOnly, true)
	test.IsEqualBool(t, c.SameSite == http.SameSiteLaxMode, true)
	test.IsEqualString(t, c.Path, "/")
	test.IsEqualInt(t, c.MaxAge, 3600)

	r := httptest.NewRequest("GET", "/login", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	test.IsEqualString(t, PopReturnTo(w, r), "join?t=abc")
	// Pop clears the cookie
	cleared := returnToCookie(w)
	test.IsEqualBool(t, cleared != nil, true)
	test.IsEqualInt(t, cleared.MaxAge, -1)
	test.IsEqualString(t, cleared.Path, "/")
}

func TestReturnToRefusesOtherTargets(t *testing.T) {
	for _, bad := range []string{"//evil.com", "https://evil.com", "admin", "join?t=a b", "join?t=abc&x=1", "join?t=", "",
		"g?id=", "g?id=abc&x=1", "g?id=//evil.com", "gd?id=abc"} {
		// SetReturnTo never stores them
		w := httptest.NewRecorder()
		SetReturnTo(w, bad)
		test.IsEqualBool(t, returnToCookie(w) == nil, true)

		// and a forged cookie is never followed, but is still cleared
		target, w := popWith(bad)
		test.IsEqualString(t, target, "admin")
		if bad != "" {
			cleared := returnToCookie(w)
			test.IsEqualBool(t, cleared != nil, true)
			test.IsEqualInt(t, cleared.MaxAge, -1)
		}
	}
}

func TestReturnToWithoutCookie(t *testing.T) {
	w := httptest.NewRecorder()
	test.IsEqualString(t, PopReturnTo(w, httptest.NewRequest("GET", "/login", nil)), "admin")
	// nothing to clear, so nothing is set
	test.IsEqualBool(t, returnToCookie(w) == nil, true)
}

// Google sign-in, which the live instance uses, comes back through the OAuth
// callback: it must return to the invite too
func TestReturnToAfterOauth(t *testing.T) {
	Init(modelOauth)
	info := OAuthUserInfo{
		ClaimsSent: testInfo{Output: []byte(`{"email":"test@test.com","email_verified":true,"groups":["admins","dev"]}`)},
		Subject:    "random",
		Email:      "random",
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth-callback", nil)
	r.AddCookie(&http.Cookie{Name: cookieReturnTo, Value: "join?t=abc"})
	test.IsNil(t, CheckOauthUserAndRedirect(w, r, info))
	test.ResponseIsRedirect(t, w, "join?t=abc", false)
	cleared := returnToCookie(w)
	test.IsEqualBool(t, cleared != nil, true)
	test.IsEqualInt(t, cleared.MaxAge, -1)
}

// M8: a group page is remembered as well, so a signed-out member lands on it
func TestReturnToGroupPage(t *testing.T) {
	w := httptest.NewRecorder()
	SetReturnTo(w, "g?id=Abc_123-x")
	c := returnToCookie(w)
	test.IsEqualBool(t, c != nil, true)
	r := httptest.NewRequest("GET", "/login", nil)
	r.AddCookie(c)
	test.IsEqualString(t, PopReturnTo(httptest.NewRecorder(), r), "g?id=Abc_123-x")
}
